package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"repo-prompt-web/internal/application"
	"repo-prompt-web/internal/domain/models"
	"repo-prompt-web/pkg/config"

	"github.com/gin-gonic/gin"
)

// PromptHandler 提示词 HTTP 处理器
type PromptHandler struct {
	promptService *application.PromptService
	fileService   *application.FileService
	config        *config.Config
}

// NewPromptHandler 创建提示词 HTTP 处理器实例
func NewPromptHandler(promptService *application.PromptService, fileService *application.FileService, cfg *config.Config) *PromptHandler {
	return &PromptHandler{
		promptService: promptService,
		fileService:   fileService,
		config:        cfg,
	}
}

// HandleGeneratePrompt 处理生成提示词请求
func (h *PromptHandler) HandleGeneratePrompt(c *gin.Context) {
	var request models.PromptRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数", "details": err.Error()})
		return
	}

	// 验证参数
	if request.ProjectPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目路径不能为空"})
		return
	}

	if request.ApiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "API 密钥不能为空"})
		return
	}

	// 生成提示词
	response, err := h.promptService.GeneratePromptWithApiKey(request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成提示词失败", "details": err.Error()})
		return
	}

	if !response.Success {
		c.JSON(http.StatusBadRequest, gin.H{"error": response.Error})
		return
	}

	c.JSON(http.StatusOK, response)
}

// HandlePreProcess 处理 ZIP 文件预处理并生成提示词
func (h *PromptHandler) HandlePreProcess(c *gin.Context) {
	// 获取 API 密钥
	apiKey := c.PostForm("apiKey")
	if apiKey == "" {
		// 尝试从配置或请求参数获取 API 密钥
		apiKey = h.config.GetDeepseekAPIKey()
		if apiKey == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "未提供 DeepSeek API 密钥"})
			return
		}
	}

	// 获取 ZIP 文件
	file, err := c.FormFile("codeZip")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请上传 ZIP 文件"})
		return
	}

	// 检查文件大小
	if file.Size > h.config.GetMaxUploadSize() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件大小超过限制"})
		return
	}

	// 创建临时目录
	tempDir, err := os.MkdirTemp("", "zip-prompt-*")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法创建临时目录"})
		return
	}
	defer os.RemoveAll(tempDir)

	// 保存上传的文件到临时目录
	tempFile := filepath.Join(tempDir, file.Filename)
	if err := c.SaveUploadedFile(file, tempFile); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("保存文件失败: %v", err)})
		return
	}

	// 处理 ZIP 文件内容
	result, err := h.fileService.ProcessZipFile(file, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("处理 ZIP 文件失败: %v", err)})
		return
	}

	// 将处理结果写入临时文件夹
	extractDir := filepath.Join(tempDir, "extracted")
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建解压目录失败"})
		return
	}

	// 写入文件内容
	for path, content := range result.FileContents {
		fullPath := filepath.Join(extractDir, path)
		dirPath := filepath.Dir(fullPath)

		// 创建目录
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			continue
		}

		// 写入文件内容
		if err := os.WriteFile(fullPath, []byte(content.Content), 0644); err != nil {
			continue
		}
	}

	// 生成提示词响应格式
	format := c.DefaultQuery("format", "json")
	includeContent := c.DefaultQuery("include_content", "false") == "true"

	// 使用临时目录生成项目架构分析
	contextPrompt, err := h.promptService.GenerateContextPrompt(extractDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("生成提示词失败: %v", err)})
		return
	}

	// 根据格式返回响应
	if format == "json" {
		response := gin.H{
			"success":            true,
			"prompt_suggestions": contextPrompt.PromptSuggestions,
			"generated_at":       contextPrompt.GeneratedAt,
		}

		// 如果需要包含文件内容
		if includeContent {
			response["directory_structure"] = contextPrompt.DirectoryStructure
			response["file_tree"] = result.FileTree
			response["file_contents"] = result.FileContents
		}

		c.JSON(http.StatusOK, response)
	} else {
		// 文本格式
		var output string
		if len(contextPrompt.PromptSuggestions) > 0 {
			output = fmt.Sprintf("# 项目架构分析\n\n%s\n\n", contextPrompt.PromptSuggestions[0])
		}

		// 如果需要包含文件内容
		if includeContent {
			output += fmt.Sprintf("# 目录结构\n\n%s\n\n# 文件内容\n\n%s",
				contextPrompt.DirectoryStructure,
				h.fileService.FormatOutput(result))
		}

		c.String(http.StatusOK, output)
	}
}
