package application

import (
	"repo-prompt-web/internal/domain/models"
	"repo-prompt-web/internal/domain/services"
)

// PromptService 提示词应用服务
type PromptService struct {
	promptGenerator *services.PromptGenerator
}

// NewPromptService 创建提示词应用服务实例
func NewPromptService(apiKey string) *PromptService {
	return &PromptService{
		promptGenerator: services.NewPromptGenerator(apiKey),
	}
}

// GenerateContextPrompt 生成上下文提示
func (s *PromptService) GenerateContextPrompt(projectPath string) (*models.ContextPrompt, error) {
	return s.promptGenerator.ProcessDirectoryContext(projectPath)
}

// GeneratePromptWithApiKey 使用指定的 API 密钥生成提示
func (s *PromptService) GeneratePromptWithApiKey(request models.PromptRequest) (*models.PromptResponse, error) {
	// 创建临时生成器使用请求指定的 API 密钥
	generator := services.NewPromptGenerator(request.ApiKey)

	prompt, err := generator.ProcessDirectoryContext(request.ProjectPath)
	if err != nil {
		return &models.PromptResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &models.PromptResponse{
		Success: true,
		Prompt:  *prompt,
	}, nil
}
