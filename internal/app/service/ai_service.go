package service

import (
	"bytes"
	"fmt"
	"repo-prompt-web/internal/domain/models"
	"repo-prompt-web/internal/infrastructure/gemini"
	"repo-prompt-web/pkg/config"
	"repo-prompt-web/pkg/logger"
	"repo-prompt-web/pkg/types"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// AIService 提供AI相关服务的结构体
type AIService struct {
	geminiClient   *gemini.Client
	cfg            *config.Config
	sessionHistory map[string]*ConversationContext
	mu             sync.RWMutex
}

// ConversationContext 维护对话上下文的结构体
type ConversationContext struct {
	InitialPrompt string            // 初始提示（包含项目信息）
	Messages      []ConversationMsg // 对话消息记录
	LastActive    time.Time         // 最后活跃时间
}

// ConversationMsg 对话消息结构体
type ConversationMsg struct {
	Role    string // 角色，可以是 "user" 或 "assistant"
	Content string // 消息内容
}

// NewAIService 创建新的AI服务实例
func NewAIService(cfg *config.Config) *AIService {
	service := &AIService{
		geminiClient:   gemini.GetClient(cfg),
		cfg:            cfg,
		sessionHistory: make(map[string]*ConversationContext),
	}

	// 启动定期清理过期会话的后台任务
	go service.cleanupExpiredSessions()

	return service
}

// cleanupExpiredSessions 定期清理过期会话
func (s *AIService) cleanupExpiredSessions() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		for id, context := range s.sessionHistory {
			// 2小时不活跃则清理
			if time.Since(context.LastActive) > 2*time.Hour {
				delete(s.sessionHistory, id)
				logger.Debug("清理过期AI会话上下文", zap.String("session_id", id))
			}
		}
		s.mu.Unlock()
	}
}

// GenerateProjectAnalysis 根据项目文件生成分析结果
func (s *AIService) GenerateProjectAnalysis(projectInfo string) (string, error) {
	// 构建提示语
	prompt := "请分析以下项目结构和代码，提供一个详细的项目概述、主要功能和组件分析：\n\n" + projectInfo

	// 调用Gemini API
	response, err := s.geminiClient.SendPrompt(prompt)
	if err != nil {
		logger.Error("调用Gemini API生成项目分析失败", zap.Error(err))
		return "", err
	}

	return response, nil
}

// GenerateCodeExplanation 根据代码生成解释
func (s *AIService) GenerateCodeExplanation(code string, functionName string) (string, error) {
	// 构建提示语
	prompt := "请解释以下" + functionName + "函数的功能、参数和返回值：\n\n" + code

	// 调用Gemini API
	response, err := s.geminiClient.SendPrompt(prompt)
	if err != nil {
		logger.Error("调用Gemini API生成代码解释失败", zap.Error(err))
		return "", err
	}

	return response, nil
}

// buildInitialPrompt 构建初始化提示（包含代码上下文）
func (s *AIService) buildInitialPrompt(result *types.ProcessResult, projectAnalysis *models.ProjectAnalysis) string {
	promptBuilder := &StringBuilder{}

	// 添加系统提示
	promptBuilder.AppendLine("你是一位代码分析助手，正在分析一个代码库并回答关于代码的问题。请基于以下代码库的内容和项目架构分析来回答问题。")

	// 添加项目分析
	if projectAnalysis != nil && len(projectAnalysis.PromptSuggestions) > 0 {
		promptBuilder.AppendLine("\n## 项目架构分析")
		promptBuilder.AppendLine(projectAnalysis.PromptSuggestions[0])
	}

	// 添加文件树结构
	promptBuilder.AppendLine("\n## 文件结构")
	if result.FileTree != nil {
		buffer := &bytes.Buffer{}
		result.FileTree.Print(buffer, "", true)
		promptBuilder.AppendLine(buffer.String())
	}

	// 添加文件内容 (最多10个文件，并限制大小)
	promptBuilder.AppendLine("\n## 文件内容")
	fileCount := 0
	for path, content := range result.FileContents {
		if fileCount >= 10 {
			break
		}
		// 跳过二进制内容
		if content.IsBase64 {
			continue
		}

		// 限制每个文件内容大小
		fileContent := content.Content
		if len(fileContent) > 5000 {
			fileContent = fileContent[:5000] + "...(内容已截断)"
		}

		promptBuilder.AppendLine(fmt.Sprintf("\n### %s", path))
		promptBuilder.AppendLine("```")
		promptBuilder.AppendLine(fileContent)
		promptBuilder.AppendLine("```")
		fileCount++
	}

	return promptBuilder.String()
}

// AskQuestionAboutCode 询问关于代码的问题
func (s *AIService) AskQuestionAboutCode(result *types.ProcessResult, projectAnalysis *models.ProjectAnalysis, question string, sessionID string) (string, error) {
	s.mu.Lock()

	// 检查是否有现有会话
	context, exists := s.sessionHistory[sessionID]
	if !exists {
		// 创建新会话
		initialPrompt := s.buildInitialPrompt(result, projectAnalysis)
		context = &ConversationContext{
			InitialPrompt: initialPrompt,
			Messages:      []ConversationMsg{},
			LastActive:    time.Now(),
		}
		s.sessionHistory[sessionID] = context
		logger.Debug("创建新的AI会话上下文", zap.String("session_id", sessionID))
	}

	// 更新最后活跃时间
	context.LastActive = time.Now()

	// 添加用户问题到会话历史
	context.Messages = append(context.Messages, ConversationMsg{
		Role:    "user",
		Content: question,
	})

	// 构建完整提示词
	var prompt string
	if len(context.Messages) <= 1 {
		// 首次提问，包含完整代码上下文
		prompt = context.InitialPrompt + "\n\n## 问题\n" + question
		logger.Debug("首次提问，使用完整代码上下文",
			zap.String("session_id", sessionID),
			zap.Int("prompt_length", len(prompt)))
	} else {
		// 后续提问，仅包含对话历史
		promptBuilder := &StringBuilder{}
		promptBuilder.AppendLine(context.InitialPrompt)
		promptBuilder.AppendLine("\n## 对话历史")

		// 只保留最近10次对话
		startIdx := 0
		if len(context.Messages) > 10 {
			startIdx = len(context.Messages) - 10
		}

		for i := startIdx; i < len(context.Messages); i++ {
			msg := context.Messages[i]
			promptBuilder.AppendLine(fmt.Sprintf("\n%s: %s", msg.Role, msg.Content))
		}

		prompt = promptBuilder.String()
		logger.Debug("后续提问，使用对话历史",
			zap.String("session_id", sessionID),
			zap.Int("message_count", len(context.Messages)),
			zap.Int("prompt_length", len(prompt)))
	}

	s.mu.Unlock()

	// 打印发送给Gemini的内容
	fmt.Println("\n===== 发送给Gemini的内容开始 =====")
	fmt.Println(prompt)
	fmt.Println("===== 发送给Gemini的内容结束 =====")

	// 调用Gemini API
	response, err := s.geminiClient.SendPrompt(prompt)
	if err != nil {
		logger.Error("调用Gemini API回答代码问题失败", zap.Error(err))
		return "", err
	}

	// 添加回复到会话历史
	s.mu.Lock()
	if context, exists := s.sessionHistory[sessionID]; exists {
		context.Messages = append(context.Messages, ConversationMsg{
			Role:    "assistant",
			Content: response,
		})
	}
	s.mu.Unlock()

	return response, nil
}

// AskQuestionAboutCodeStream 流式询问关于代码的问题
func (s *AIService) AskQuestionAboutCodeStream(result *types.ProcessResult, projectAnalysis *models.ProjectAnalysis, question string, sessionID string) (<-chan gemini.StreamChunk, error) {
	s.mu.Lock()

	// 检查是否有现有会话
	context, exists := s.sessionHistory[sessionID]
	if !exists {
		// 创建新会话
		initialPrompt := s.buildInitialPrompt(result, projectAnalysis)
		context = &ConversationContext{
			InitialPrompt: initialPrompt,
			Messages:      []ConversationMsg{},
			LastActive:    time.Now(),
		}
		s.sessionHistory[sessionID] = context
		logger.Debug("创建新的AI会话上下文（流式）", zap.String("session_id", sessionID))
	}

	// 更新最后活跃时间
	context.LastActive = time.Now()

	// 添加用户问题到会话历史
	context.Messages = append(context.Messages, ConversationMsg{
		Role:    "user",
		Content: question,
	})

	// 构建完整提示词
	var prompt string
	if len(context.Messages) <= 1 {
		// 首次提问，包含完整代码上下文
		prompt = context.InitialPrompt + "\n\n## 问题\n" + question
		logger.Debug("首次提问（流式），使用完整代码上下文",
			zap.String("session_id", sessionID),
			zap.Int("prompt_length", len(prompt)))
	} else {
		// 后续提问，仅包含对话历史
		promptBuilder := &StringBuilder{}
		promptBuilder.AppendLine(context.InitialPrompt)
		promptBuilder.AppendLine("\n## 对话历史")

		// 只保留最近10次对话
		startIdx := 0
		if len(context.Messages) > 10 {
			startIdx = len(context.Messages) - 10
		}

		for i := startIdx; i < len(context.Messages); i++ {
			msg := context.Messages[i]
			promptBuilder.AppendLine(fmt.Sprintf("\n%s: %s", msg.Role, msg.Content))
		}

		prompt = promptBuilder.String()
		logger.Debug("后续提问（流式），使用对话历史",
			zap.String("session_id", sessionID),
			zap.Int("message_count", len(context.Messages)),
			zap.Int("prompt_length", len(prompt)))
	}

	s.mu.Unlock()

	// 打印发送给Gemini的内容
	fmt.Println("\n===== 发送给Gemini的内容开始 =====")
	fmt.Println(prompt)
	fmt.Println("===== 发送给Gemini的内容结束 =====")

	// 创建响应通道
	responseChan := make(chan gemini.StreamChunk, 100)

	// 调用Gemini API流式接口
	streamChan, err := s.geminiClient.SendPromptStream(prompt)
	if err != nil {
		close(responseChan)
		logger.Error("流式调用Gemini API回答代码问题失败", zap.Error(err))
		return responseChan, err
	}

	// 启动goroutine来收集完整响应并保存到会话历史
	go func() {
		defer close(responseChan)

		// 用于收集完整响应
		responseBuilder := strings.Builder{}

		for chunk := range streamChan {
			if chunk.Error != nil {
				responseChan <- chunk
				return
			}

			// 收集响应
			responseBuilder.WriteString(chunk.Text)

			// 转发响应块
			responseChan <- chunk
		}

		// 添加完整响应到会话历史
		completeResponse := responseBuilder.String()
		s.mu.Lock()
		if context, exists := s.sessionHistory[sessionID]; exists {
			context.Messages = append(context.Messages, ConversationMsg{
				Role:    "assistant",
				Content: completeResponse,
			})
		}
		s.mu.Unlock()
	}()

	return responseChan, nil
}

// StringBuilder 是一个简单的字符串构建器
type StringBuilder struct {
	builder strings.Builder
}

// AppendLine 添加一行文本
func (sb *StringBuilder) AppendLine(line string) {
	sb.builder.WriteString(line)
	sb.builder.WriteString("\n")
}

// String 获取构建的字符串
func (sb *StringBuilder) String() string {
	return sb.builder.String()
}
