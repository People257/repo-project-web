package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"repo-prompt-web/internal/application"
	"repo-prompt-web/internal/infrastructure/github"
	"repo-prompt-web/pkg/config"
	"repo-prompt-web/pkg/logger"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// FileHandler HTTP 处理器
type FileHandler struct {
	fileService   *application.FileService
	promptService *application.PromptService
	githubClient  *github.Client
	config        *config.Config
}

// NewFileHandler 创建 HTTP 处理器实例
func NewFileHandler(fileService *application.FileService, promptService *application.PromptService, githubClient *github.Client, cfg *config.Config) *FileHandler {
	return &FileHandler{
		fileService:   fileService,
		promptService: promptService,
		githubClient:  githubClient,
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
	var architectPrompt string
	var generatedAt time.Time
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
		contextPrompt, err := h.promptService.GenerateContextPrompt(tempDir)
		if err == nil && len(contextPrompt.PromptSuggestions) > 0 {
			architectPrompt = contextPrompt.PromptSuggestions[0]
			generatedAt = contextPrompt.GeneratedAt
			logger.Info("项目架构分析生成成功",
				zap.String("request_id", requestID),
				zap.Time("generated_at", generatedAt))
		} else {
			logger.Warn("项目架构分析生成失败",
				zap.String("request_id", requestID),
				zap.Error(err))
		}
	}

	// 根据参数和格式决定返回方式
	logger.Info("返回响应",
		zap.String("request_id", requestID),
		zap.String("format", format),
		zap.Bool("prompt_only", promptOnly),
		zap.Bool("generate_prompt", generatePrompt),
		zap.Bool("has_prompt", architectPrompt != ""))

	if promptOnly && architectPrompt != "" {
		// 只返回提示词
		if format == "json" {
			c.JSON(http.StatusOK, gin.H{
				"success":            true,
				"prompt_suggestions": []string{architectPrompt},
				"generated_at":       generatedAt,
			})
		} else {
			c.String(http.StatusOK, fmt.Sprintf("# 项目架构分析\n\n%s", architectPrompt))
		}
	} else if generatePrompt && architectPrompt != "" {
		// 返回提示词和内容
		if format == "json" {
			response := gin.H{
				"success":            true,
				"prompt_suggestions": []string{architectPrompt},
				"generated_at":       generatedAt,
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
			output := fmt.Sprintf("# 项目架构分析\n\n%s\n\n", architectPrompt)
			if includeContent {
				output += fmt.Sprintf("# 文件内容\n\n%s", h.fileService.FormatOutput(result))
			}
			c.String(http.StatusOK, output)
		}
	} else {
		// 正常响应，不包含提示词
		if format == "json" {
			c.JSON(http.StatusOK, result)
		} else {
			c.String(http.StatusOK, h.fileService.FormatOutput(result))
		}
	}
}

// HandleGitHubRepo 处理 GitHub 仓库请求
func (h *FileHandler) HandleGitHubRepo(c *gin.Context) {
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
	var architectPrompt string
	var generatedAt time.Time
	if (generatePrompt || promptOnly) && h.config.GetDeepseekAPIKey() != "" {
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
		contextPrompt, err := h.promptService.GenerateContextPrompt(tempDir)
		if err == nil && len(contextPrompt.PromptSuggestions) > 0 {
			architectPrompt = contextPrompt.PromptSuggestions[0]
			generatedAt = contextPrompt.GeneratedAt
		}
	}

	// 根据参数和格式决定返回方式
	if promptOnly && architectPrompt != "" {
		// 只返回提示词
		if format == "json" {
			c.JSON(http.StatusOK, gin.H{
				"success":            true,
				"prompt_suggestions": []string{architectPrompt},
				"generated_at":       generatedAt,
			})
		} else {
			c.String(http.StatusOK, fmt.Sprintf("# 项目架构分析\n\n%s", architectPrompt))
		}
	} else if generatePrompt && architectPrompt != "" {
		// 返回提示词和内容
		if format == "json" {
			response := gin.H{
				"success":            true,
				"prompt_suggestions": []string{architectPrompt},
				"generated_at":       generatedAt,
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
			output := fmt.Sprintf("# 项目架构分析\n\n%s\n\n", architectPrompt)
			if includeContent {
				output += fmt.Sprintf("# 文件内容\n\n%s", h.fileService.FormatOutput(result))
			}
			c.String(http.StatusOK, output)
		}
	} else {
		// 正常响应，不包含提示词
		if format == "json" {
			c.JSON(http.StatusOK, result)
		} else {
			c.String(http.StatusOK, h.fileService.FormatOutput(result))
		}
	}
}
