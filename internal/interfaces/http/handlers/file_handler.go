package handlers

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"repo-prompt-web/internal/app/service"
	"repo-prompt-web/internal/application"
	"repo-prompt-web/internal/domain/models"
	"repo-prompt-web/internal/infrastructure/github"
	"repo-prompt-web/pkg/config"
	"repo-prompt-web/pkg/logger"
	"repo-prompt-web/pkg/types"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// SessionData 存储会话数据
type SessionData struct {
	Result          *types.ProcessResult
	ProjectAnalysis *models.ProjectAnalysis
	CreatedAt       time.Time
}

// SessionStorage 会话数据存储
type SessionStorage struct {
	sessions  map[string]SessionData
	expiresIn time.Duration
	mu        sync.RWMutex
}

// NewSessionStorage 创建新的会话存储
func NewSessionStorage(expiresIn time.Duration) *SessionStorage {
	if expiresIn <= 0 {
		expiresIn = 30 * time.Minute
	}

	ss := &SessionStorage{
		sessions:  make(map[string]SessionData),
		expiresIn: expiresIn,
	}

	// 启动清理过期会话的后台任务
	go ss.cleanExpiredSessions()

	return ss
}

// cleanExpiredSessions 清理过期会话
func (ss *SessionStorage) cleanExpiredSessions() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		ss.mu.Lock()
		now := time.Now()
		for id, session := range ss.sessions {
			if now.Sub(session.CreatedAt) > ss.expiresIn {
				delete(ss.sessions, id)
				logger.Debug("已清理过期会话", zap.String("session_id", id))
			}
		}
		ss.mu.Unlock()
	}
}

// Put 存储会话数据
func (ss *SessionStorage) Put(result *types.ProcessResult, analysis *models.ProjectAnalysis) string {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	sessionID := uuid.New().String()
	ss.sessions[sessionID] = SessionData{
		Result:          result,
		ProjectAnalysis: analysis,
		CreatedAt:       time.Now(),
	}

	return sessionID
}

// Get 获取会话数据
func (ss *SessionStorage) Get(sessionID string) (SessionData, bool) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	session, exists := ss.sessions[sessionID]
	if !exists {
		return SessionData{}, false
	}

	// 检查是否过期
	if time.Now().Sub(session.CreatedAt) > ss.expiresIn {
		return SessionData{}, false
	}

	return session, true
}

// 全局会话存储
var sessionStorage = NewSessionStorage(30 * time.Minute)

// FileHandler HTTP 处理器
type FileHandler struct {
	fileService   *application.FileService
	promptService *application.PromptService
	githubClient  *github.Client
	aiService     *service.AIService
	config        *config.Config
}

// NewFileHandler 创建 HTTP 处理器实例
func NewFileHandler(fileService *application.FileService, promptService *application.PromptService, githubClient *github.Client, aiService *service.AIService, cfg *config.Config) *FileHandler {
	return &FileHandler{
		fileService:   fileService,
		promptService: promptService,
		githubClient:  githubClient,
		aiService:     aiService,
		config:        cfg,
	}
}

// HandleCombineCode 处理文件合并请求
func (h *FileHandler) HandleCombineCode(c *gin.Context) {
	requestID := c.GetString("RequestID")
	logger.Info("处理合并代码请求",
		zap.String("request_id", requestID),
		zap.String("client_ip", c.ClientIP()))

	file, err := c.FormFile("codeZip")
	if err != nil {
		logger.Warn("未上传ZIP文件",
			zap.String("request_id", requestID),
			zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "请上传 ZIP 文件"})
		return
	}

	if file.Size > h.config.GetMaxUploadSize() {
		logger.Warn("文件大小超过限制",
			zap.String("request_id", requestID),
			zap.String("file_name", file.Filename),
			zap.Int64("file_size", file.Size),
			zap.Int64("max_size", h.config.GetMaxUploadSize()))
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件大小超过限制"})
		return
	}

	logger.Debug("接收到文件上传",
		zap.String("request_id", requestID),
		zap.String("file_name", file.Filename),
		zap.Int64("file_size", file.Size))

	// 从表单和URL查询参数中获取参数
	formatQuery := c.DefaultQuery("format", "text")
	formatForm := c.PostForm("format")
	format := formatQuery
	if formatForm != "" {
		format = formatForm
	}

	useBase64Query := c.DefaultQuery("base64", "false") == "true"
	useBase64Form := c.PostForm("base64") == "true"
	useBase64 := useBase64Query || useBase64Form

	// 是否生成项目架构分析
	generatePromptQuery := c.DefaultQuery("generate_prompt", "false") == "true"
	generatePromptForm := c.PostForm("generate_prompt") == "true"
	generatePrompt := generatePromptQuery || generatePromptForm

	// 是否只返回提示词而不包含文件内容
	promptOnlyQuery := c.DefaultQuery("prompt_only", "false") == "true"
	promptOnlyForm := c.PostForm("prompt_only") == "true"
	promptOnly := promptOnlyQuery || promptOnlyForm

	// 是否包含文件内容（与 promptOnly 互斥）
	includeContentQuery := c.DefaultQuery("include_content", "false") == "true"
	includeContentForm := c.PostForm("include_content") == "true"
	includeContent := (includeContentQuery || includeContentForm) && !promptOnly

	logger.Debug("请求参数",
		zap.String("request_id", requestID),
		zap.String("format", format),
		zap.Bool("use_base64", useBase64),
		zap.Bool("generate_prompt", generatePrompt),
		zap.Bool("prompt_only", promptOnly),
		zap.Bool("include_content", includeContent))

	// 处理 ZIP 文件
	result, err := h.fileService.ProcessZipFile(file, useBase64)
	if err != nil {
		logger.Error("处理ZIP文件失败",
			zap.String("request_id", requestID),
			zap.String("file_name", file.Filename),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.Info("ZIP文件处理成功",
		zap.String("request_id", requestID),
		zap.String("file_name", file.Filename),
		zap.Int("files_count", len(result.FileContents)))

	// 如果需要生成项目架构分析
	var projectAnalysis *models.ProjectAnalysis
	if (generatePrompt || promptOnly) && h.config.GetDeepseekAPIKey() != "" {
		logger.Info("开始生成项目架构分析",
			zap.String("request_id", requestID))

		// 将处理结果写入临时文件夹
		tempDir, err := os.MkdirTemp("", "repo-prompt-*")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "无法创建临时目录"})
			return
		}
		defer os.RemoveAll(tempDir)

		// 创建临时项目结构
		for path, content := range result.FileContents {
			fullPath := filepath.Join(tempDir, path)
			dirPath := filepath.Dir(fullPath)

			// 创建目录
			if err := os.MkdirAll(dirPath, 0755); err != nil {
				continue
			}

			// 写入文件内容
			fileContent := content.Content
			if content.IsBase64 {
				// 这里应该有 base64 解码逻辑，但为简化示例，跳过
				continue
			}

			if err := os.WriteFile(fullPath, []byte(fileContent), 0644); err != nil {
				continue
			}
		}

		// 使用临时目录生成项目架构分析
		projectAnalysis, err = h.promptService.GetProjectAnalysis(tempDir)
		if err != nil {
			logger.Warn("项目架构分析生成失败",
				zap.String("request_id", requestID),
				zap.Error(err))
		} else {
			logger.Info("项目架构分析生成成功",
				zap.String("request_id", requestID))
		}
	}

	// 根据参数和格式决定返回方式
	logger.Info("返回响应",
		zap.String("request_id", requestID),
		zap.String("format", format),
		zap.Bool("prompt_only", promptOnly),
		zap.Bool("generate_prompt", generatePrompt),
		zap.Bool("has_prompt", projectAnalysis != nil))

	// 保存会话数据以便后续提问
	sessionID := sessionStorage.Put(result, projectAnalysis)
	logger.Debug("已创建会话",
		zap.String("request_id", requestID),
		zap.String("session_id", sessionID))

	if promptOnly && projectAnalysis != nil {
		// 只返回提示词
		if format == "json" {
			c.JSON(http.StatusOK, gin.H{
				"success":          true,
				"session_id":       sessionID,
				"project_analysis": projectAnalysis,
			})
		} else {
			c.String(http.StatusOK, fmt.Sprintf("# 会话ID\n%s\n\n# 项目架构分析\n\n%s", sessionID, projectAnalysis.PromptSuggestions[0]))
		}
	} else if generatePrompt && projectAnalysis != nil {
		// 返回提示词和内容
		if format == "json" {
			response := gin.H{
				"success":          true,
				"session_id":       sessionID,
				"project_analysis": projectAnalysis,
			}

			// 如果需要包含文件内容
			if includeContent {
				response["file_tree"] = result.FileTree
				response["file_contents"] = result.FileContents
			} else {
				response["result"] = result
			}

			c.JSON(http.StatusOK, response)
		} else {
			output := fmt.Sprintf("# 会话ID\n%s\n\n# 项目架构分析\n\n%s\n\n", sessionID, projectAnalysis.PromptSuggestions[0])
			if includeContent {
				output += fmt.Sprintf("# 文件内容\n\n%s", h.fileService.FormatOutput(result))
			}
			c.String(http.StatusOK, output)
		}
	} else {
		// 正常响应，不包含提示词
		if format == "json" {
			c.JSON(http.StatusOK, gin.H{
				"success":    true,
				"session_id": sessionID,
				"result":     result,
			})
		} else {
			output := fmt.Sprintf("# 会话ID\n%s\n\n# 文件内容\n\n%s", sessionID, h.fileService.FormatOutput(result))
			c.String(http.StatusOK, output)
		}
	}
}

// HandleGitHubRepo 处理 GitHub 仓库请求
func (h *FileHandler) HandleGitHubRepo(c *gin.Context) {
	requestID := c.GetString("RequestID")
	logger.Info("处理GitHub仓库请求",
		zap.String("request_id", requestID),
		zap.String("client_ip", c.ClientIP()))

	repoURL := c.Query("url")
	if repoURL == "" {
		repoURL = c.PostForm("url")
		if repoURL == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请提供 GitHub 仓库 URL"})
			return
		}
	}

	// 从表单和URL查询参数中获取参数
	formatQuery := c.DefaultQuery("format", "text")
	formatForm := c.PostForm("format")
	format := formatQuery
	if formatForm != "" {
		format = formatForm
	}

	useBase64Query := c.DefaultQuery("base64", "false") == "true"
	useBase64Form := c.PostForm("base64") == "true"
	useBase64 := useBase64Query || useBase64Form

	// 是否生成项目架构分析
	generatePromptQuery := c.DefaultQuery("generate_prompt", "false") == "true"
	generatePromptForm := c.PostForm("generate_prompt") == "true"
	generatePrompt := generatePromptQuery || generatePromptForm

	// 是否只返回提示词而不包含文件内容
	promptOnlyQuery := c.DefaultQuery("prompt_only", "false") == "true"
	promptOnlyForm := c.PostForm("prompt_only") == "true"
	promptOnly := promptOnlyQuery || promptOnlyForm

	// 是否包含文件内容（与 promptOnly 互斥）
	includeContentQuery := c.DefaultQuery("include_content", "false") == "true"
	includeContentForm := c.PostForm("include_content") == "true"
	includeContent := (includeContentQuery || includeContentForm) && !promptOnly

	token := c.Query("token")
	if token == "" {
		token = c.PostForm("token")
		if token == "" {
			token = h.config.GetGithubAPIKey()
		}
	}

	owner, repo, err := github.ParseRepoURL(repoURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.githubClient.GetRepoContents(owner, repo, token, useBase64)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 如果需要生成项目架构分析
	var projectAnalysis *models.ProjectAnalysis
	if (generatePrompt || promptOnly) && h.config.GetDeepseekAPIKey() != "" {
		logger.Info("开始生成项目架构分析",
			zap.String("request_id", requestID))

		// 将处理结果写入临时文件夹
		tempDir, err := os.MkdirTemp("", "repo-prompt-*")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "无法创建临时目录"})
			return
		}
		defer os.RemoveAll(tempDir)

		// 创建临时项目结构
		for path, content := range result.FileContents {
			fullPath := filepath.Join(tempDir, path)
			dirPath := filepath.Dir(fullPath)

			// 创建目录
			if err := os.MkdirAll(dirPath, 0755); err != nil {
				continue
			}

			// 写入文件内容
			fileContent := content.Content
			if content.IsBase64 {
				// 这里应该有 base64 解码逻辑，但为简化示例，跳过
				continue
			}

			if err := os.WriteFile(fullPath, []byte(fileContent), 0644); err != nil {
				continue
			}
		}

		// 使用临时目录生成项目架构分析
		projectAnalysis, err = h.promptService.GetProjectAnalysis(tempDir)
		if err != nil {
			logger.Warn("项目架构分析生成失败",
				zap.String("request_id", requestID),
				zap.Error(err))
		} else {
			logger.Info("项目架构分析生成成功",
				zap.String("request_id", requestID))
		}
	}

	// 根据参数和格式决定返回方式
	logger.Info("返回响应",
		zap.String("request_id", requestID),
		zap.String("format", format),
		zap.Bool("prompt_only", promptOnly),
		zap.Bool("generate_prompt", generatePrompt),
		zap.Bool("has_prompt", projectAnalysis != nil))

	// 保存会话数据以便后续提问
	sessionID := sessionStorage.Put(result, projectAnalysis)
	logger.Debug("已创建会话",
		zap.String("request_id", requestID),
		zap.String("session_id", sessionID))

	if promptOnly && projectAnalysis != nil {
		// 只返回提示词
		if format == "json" {
			c.JSON(http.StatusOK, gin.H{
				"success":          true,
				"session_id":       sessionID,
				"project_analysis": projectAnalysis,
			})
		} else {
			c.String(http.StatusOK, fmt.Sprintf("# 会话ID\n%s\n\n# 项目架构分析\n\n%s", sessionID, projectAnalysis.PromptSuggestions[0]))
		}
	} else if generatePrompt && projectAnalysis != nil {
		// 返回提示词和内容
		if format == "json" {
			response := gin.H{
				"success":          true,
				"session_id":       sessionID,
				"project_analysis": projectAnalysis,
			}

			// 如果需要包含文件内容
			if includeContent {
				response["file_tree"] = result.FileTree
				response["file_contents"] = result.FileContents
			} else {
				response["result"] = result
			}

			c.JSON(http.StatusOK, response)
		} else {
			output := fmt.Sprintf("# 会话ID\n%s\n\n# 项目架构分析\n\n%s\n\n", sessionID, projectAnalysis.PromptSuggestions[0])
			if includeContent {
				output += fmt.Sprintf("# 文件内容\n\n%s", h.fileService.FormatOutput(result))
			}
			c.String(http.StatusOK, output)
		}
	} else {
		// 正常响应，不包含提示词
		if format == "json" {
			c.JSON(http.StatusOK, gin.H{
				"success":    true,
				"session_id": sessionID,
				"result":     result,
			})
		} else {
			output := fmt.Sprintf("# 会话ID\n%s\n\n# 文件内容\n\n%s", sessionID, h.fileService.FormatOutput(result))
			c.String(http.StatusOK, output)
		}
	}
}

// HandleAskCodeQuestion 处理关于代码的问题
func (h *FileHandler) HandleAskCodeQuestion(c *gin.Context) {
	requestID := c.GetString("RequestID")
	logger.Info("处理代码问题请求",
		zap.String("request_id", requestID),
		zap.String("client_ip", c.ClientIP()))

	// 获取问题
	question := c.Query("question")
	if question == "" {
		question = c.PostForm("question")
		if question == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请提供问题内容"})
			return
		}
	}

	// 获取会话ID (用于关联先前上传的ZIP文件)
	sessionID := c.Query("session_id")
	if sessionID == "" {
		sessionID = c.PostForm("session_id")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请提供会话ID"})
			return
		}
	}

	// 检查会话数据是否存在
	sessionData, exists := sessionStorage.Get(sessionID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "会话不存在或已过期，请重新上传代码"})
		return
	}

	// 获取流式参数
	streamParam := c.DefaultQuery("stream", "false")
	useStream := streamParam == "true"

	logger.Debug("问题参数",
		zap.String("request_id", requestID),
		zap.String("question", question),
		zap.String("session_id", sessionID),
		zap.Bool("stream", useStream))

	// 根据是否流式处理选择不同的方法
	if useStream {
		// 流式处理
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("Transfer-Encoding", "chunked")

		// 获取响应通道
		responseChan, err := h.aiService.AskQuestionAboutCodeStream(
			sessionData.Result,
			sessionData.ProjectAnalysis,
			question,
			sessionID, // 传递sessionID用于会话记忆
		)
		if err != nil {
			logger.Error("流式处理代码问题失败",
				zap.String("request_id", requestID),
				zap.Error(err))
			c.SSEvent("error", gin.H{"error": err.Error()})
			c.Writer.Flush()
			return
		}

		// 设置请求上下文，以便在客户端断开连接时取消处理
		clientGone := c.Writer.CloseNotify()
		c.Stream(func(w io.Writer) bool {
			select {
			case <-clientGone:
				// 客户端断开连接
				return false
			case chunk, ok := <-responseChan:
				if !ok {
					// 通道已关闭
					return false
				}

				if chunk.Error != nil {
					// 发生错误
					c.SSEvent("error", gin.H{"error": chunk.Error.Error()})
					return false
				}

				// 发送数据块
				c.SSEvent("message", chunk.Text)
				return true
			}
		})
	} else {
		// 非流式处理
		response, err := h.aiService.AskQuestionAboutCode(
			sessionData.Result,
			sessionData.ProjectAnalysis,
			question,
			sessionID, // 传递sessionID用于会话记忆
		)
		if err != nil {
			logger.Error("处理代码问题失败",
				zap.String("request_id", requestID),
				zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		logger.Info("代码问题处理成功",
			zap.String("request_id", requestID),
			zap.String("question", question),
			zap.Int("response_length", len(response)))

		// 返回结果
		c.JSON(http.StatusOK, gin.H{
			"success":  true,
			"question": question,
			"answer":   response,
		})
	}
}
