package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"repo-prompt-web/internal/domain/models"
)

// PromptGenerator 提示词生成服务
type PromptGenerator struct {
	deepseekAPIKey     string
	maxDocumentSize    int64
	documentExtensions map[string]bool
}

// NewPromptGenerator 创建提示词生成服务
func NewPromptGenerator(apiKey string) *PromptGenerator {
	// 支持的文档文件类型
	docExtensions := map[string]bool{
		".md":       true,
		".markdown": true,
		".txt":      true,
		".rst":      true,
		".org":      true,
		".wiki":     true,
		".adoc":     true,
	}

	return &PromptGenerator{
		deepseekAPIKey:     apiKey,
		maxDocumentSize:    1024 * 1024, // 1MB
		documentExtensions: docExtensions,
	}
}

// ProcessDirectoryContext 处理目录上下文并生成提示词
func (pg *PromptGenerator) ProcessDirectoryContext(rootDir string) (*models.ContextPrompt, error) {
	log.Printf("正在处理目录: %s", rootDir)

	// 收集目录结构
	dirStructure, err := pg.buildDirectoryTree(rootDir)
	if err != nil {
		return nil, fmt.Errorf("构建目录树失败: %w", err)
	}
	log.Printf("目录树构建完成, 长度: %d 字节", len(dirStructure))

	// 收集文档内容 - 仅收集README和重要配置文件
	docs, err := pg.collectImportantDocuments(rootDir)
	if err != nil {
		return nil, fmt.Errorf("收集文档内容失败: %w", err)
	}
	log.Printf("收集到 %d 个重要文档文件", len(docs))

	// 调用 DeepSeek API 生成提示词
	promptSuggestions, err := pg.generateArchitectPrompt(dirStructure, docs)
	if err != nil {
		log.Printf("生成提示词时出错: %v", err)
		return nil, fmt.Errorf("生成提示词建议失败: %w", err)
	}
	log.Printf("生成了 %d 个提示词建议", len(promptSuggestions))

	return &models.ContextPrompt{
		DirectoryStructure: dirStructure,
		Documents:          docs,
		PromptSuggestions:  promptSuggestions,
		GeneratedAt:        time.Now(),
	}, nil
}

// 构建目录树结构
func (pg *PromptGenerator) buildDirectoryTree(rootDir string) (string, error) {
	var buffer bytes.Buffer
	buffer.WriteString("项目目录结构:\n")

	// 检查目录是否存在
	if _, err := os.Stat(rootDir); os.IsNotExist(err) {
		return "", fmt.Errorf("目录不存在: %s", rootDir)
	}

	// 获取目录的绝对路径
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return "", err
	}
	log.Printf("开始构建目录树: %s", absRoot)

	err = filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("访问路径出错 %s: %v", path, err)
			return nil // 继续处理其他文件
		}

		// 忽略 .git, node_modules 等目录
		if info.IsDir() && (strings.HasPrefix(info.Name(), ".") ||
			info.Name() == "node_modules" ||
			info.Name() == "vendor" ||
			info.Name() == "dist") {
			return filepath.SkipDir
		}

		// 计算相对路径和缩进
		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			log.Printf("计算相对路径出错 %s: %v", path, err)
			return nil
		}
		if relPath == "." {
			return nil
		}

		depth := len(strings.Split(relPath, string(filepath.Separator))) - 1
		indent := strings.Repeat("  ", depth)

		if info.IsDir() {
			buffer.WriteString(fmt.Sprintf("%s📁 %s/\n", indent, info.Name()))
		} else {
			buffer.WriteString(fmt.Sprintf("%s📄 %s (%s)\n", indent, info.Name(), formatFileSize(info.Size())))
		}

		return nil
	})

	if err != nil {
		return "", err
	}

	result := buffer.String()
	log.Printf("目录树构建完成，包含 %d 行", strings.Count(result, "\n"))
	return result, nil
}

// 收集重要文档文件内容
func (pg *PromptGenerator) collectImportantDocuments(rootDir string) ([]models.Document, error) {
	var documents []models.Document

	// 重要文件列表 - 优先级从高到低
	importantFiles := map[string]bool{
		"README.md":        true,
		"README":           true,
		"README.txt":       true,
		"go.mod":           true,
		"package.json":     true,
		"requirements.txt": true,
		"Cargo.toml":       true,
		"Dockerfile":       true,
		"LICENSE":          true,
	}

	// 每种类型的文件计数
	fileTypeCount := make(map[string]int)
	const maxFilesPerType = 1 // 每种类型最多收集的文件数
	const maxTotalFiles = 5   // 总共最多收集的文件数

	var collectedFiles int

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if collectedFiles >= maxTotalFiles {
			return filepath.SkipDir // 已收集足够的文件
		}

		if err != nil {
			log.Printf("访问路径出错 %s: %v", path, err)
			return nil
		}

		// 忽略大型二进制文件和特定目录
		if info.IsDir() && (strings.HasPrefix(info.Name(), ".") ||
			info.Name() == "node_modules" ||
			info.Name() == "vendor" ||
			info.Name() == "dist") {
			return filepath.SkipDir
		}

		// 只处理重要文件
		if !info.IsDir() {
			filename := filepath.Base(path)
			ext := strings.ToLower(filepath.Ext(path))
			fileType := ext
			if fileType == "" {
				fileType = filename
			}

			isImportant := importantFiles[filename]
			isDoc := pg.documentExtensions[ext]

			if (isImportant || isDoc) && info.Size() < pg.maxDocumentSize/2 {
				// 检查此类型的文件是否已达到上限
				if fileTypeCount[fileType] >= maxFilesPerType {
					return nil
				}

				relPath, err := filepath.Rel(rootDir, path)
				if err != nil {
					log.Printf("计算相对路径出错 %s: %v", path, err)
					return nil
				}

				content, err := os.ReadFile(path)
				if err != nil {
					log.Printf("读取文件出错 %s: %v", path, err)
					return nil
				}

				// 如果内容太大，只保留头部
				const maxContentSize = 10 * 1024 // 10KB
				contentStr := string(content)
				if len(contentStr) > maxContentSize {
					contentStr = contentStr[:maxContentSize] + "\n... [内容已截断] ..."
				}

				documents = append(documents, models.Document{
					Path:    relPath,
					Content: contentStr,
					Size:    info.Size(),
				})

				fileTypeCount[fileType]++
				collectedFiles++
				log.Printf("收集重要文档: %s (%s)", relPath, formatFileSize(info.Size()))
			}
		}

		return nil
	})

	return documents, err
}

// 生成架构师视角的提示词
func (pg *PromptGenerator) generateArchitectPrompt(dirStructure string, docs []models.Document) ([]string, error) {
	if pg.deepseekAPIKey == "" {
		return []string{"请配置 DeepSeek API 密钥以启用提示词生成功能"}, nil
	}

	// 构建请求内容
	var docsContent string
	log.Printf("准备处理 %d 个文档", len(docs))

	// 限制目录结构大小
	if len(dirStructure) > 5000 {
		log.Printf("目录结构过大，进行截断")
		lines := strings.Split(dirStructure, "\n")
		if len(lines) > 50 {
			dirStructure = strings.Join(lines[:50], "\n") + "\n... [目录结构已截断] ...\n"
		}
	}

	// 构建文档内容
	for _, doc := range docs {
		docEntry := fmt.Sprintf("--- %s ---\n%s\n\n", doc.Path, doc.Content)
		docsContent += docEntry
	}

	log.Printf("文档内容准备完成，长度: %d 字节", len(docsContent))

	// 简化 system prompt
	systemPrompt := `你是一位软件架构师。请分析项目结构和文档，生成一个简洁的项目分析，包括：
1. 项目的主要目的和功能
2. 使用的架构模式
3. 关键组件及其职责
4. 技术栈和依赖
5. 主要接口和设计特点
分析需要专业且清晰，帮助其他开发者快速理解项目。`

	// 简化用户提示
	userPrompt := fmt.Sprintf(`分析这个项目并提供简明架构概述：

1. 项目目录结构：
%s

2. 项目文档：
%s`, dirStructure, docsContent)

	// 调用 DeepSeek API
	requestBody, err := json.Marshal(map[string]interface{}{
		"model": "deepseek-chat",
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": systemPrompt,
			},
			{
				"role":    "user",
				"content": userPrompt,
			},
		},
		"temperature": 0.1,  // 降低温度增加确定性
		"max_tokens":  1500, // 减少输出长度
	})
	if err != nil {
		return nil, err
	}

	log.Printf("准备调用 DeepSeek API，请求大小: %d 字节", len(requestBody))
	req, err := http.NewRequest("POST", "https://api.deepseek.com/v1/chat/completions", bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+pg.deepseekAPIKey)

	// 增加超时时间
	client := &http.Client{Timeout: 120 * time.Second}
	log.Printf("发送请求到 DeepSeek API，超时设置: 120秒")
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("调用 DeepSeek API 失败: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("DeepSeek API 返回错误: 状态码 %d, 响应: %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("API调用失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("解析 DeepSeek API 响应失败: %v", err)
		return nil, err
	}

	// 解析响应
	choices, ok := result["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		log.Printf("DeepSeek API 响应格式无效")
		return nil, fmt.Errorf("无效的API响应格式")
	}

	choice := choices[0].(map[string]interface{})
	message := choice["message"].(map[string]interface{})
	content := message["content"].(string)

	log.Printf("成功从 DeepSeek API 获取响应，长度: %d 字节", len(content))
	// 将响应作为一个完整的提示词返回
	return []string{content}, nil
}

// 格式化文件大小
func formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}
