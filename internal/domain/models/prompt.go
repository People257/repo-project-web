package models

import "time"

// Document 表示项目中的文档文件
type Document struct {
	Path    string // 文件路径
	Content string // 文件内容
	Size    int64  // 文件大小
}

// ContextPrompt 表示生成的上下文提示
type ContextPrompt struct {
	DirectoryStructure string     // 目录结构
	Documents          []Document // 文档集合
	PromptSuggestions  []string   // 提示词建议
	GeneratedAt        time.Time  // 生成时间
}

// PromptRequest 表示提示词生成请求
type PromptRequest struct {
	ProjectPath string // 项目路径
	ApiKey      string // API 密钥
}

// PromptResponse 表示提示词生成响应
type PromptResponse struct {
	Success bool          // 是否成功
	Error   string        // 错误信息
	Prompt  ContextPrompt // 生成的提示
}
