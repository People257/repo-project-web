package services

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"repo-prompt-web/internal/domain/models"
	"repo-prompt-web/pkg/config"
)

// FileProcessor 文件处理服务
type FileProcessor struct {
	config *config.Config
}

// NewFileProcessor 创建文件处理服务实例
func NewFileProcessor(cfg *config.Config) *FileProcessor {
	return &FileProcessor{
		config: cfg,
	}
}

// ProcessZipFile 处理ZIP文件
func (fp *FileProcessor) ProcessZipFile(file io.ReaderAt, size int64, useBase64 bool) (*models.ProcessResult, error) {
	reader, err := zip.NewReader(file, size)
	if err != nil {
		return nil, fmt.Errorf("无法读取ZIP文件: %w", err)
	}

	root := models.NewTreeNode("", false)
	fileContents := make(map[string]models.FileContent)

	for _, zipEntry := range reader.File {
		if zipEntry.FileInfo().IsDir() {
			continue
		}

		filePath := zipEntry.Name
		if fp.config.IsExcluded(filePath, zipEntry.UncompressedSize64) {
			log.Printf("排除 (规则): %s", filePath)
			continue
		}

		if !fp.config.IsLikelyTextFile(filePath) {
			log.Printf("排除 (非文本扩展名): %s", filePath)
			continue
		}

		rc, err := zipEntry.Open()
		if err != nil {
			log.Printf("警告: 无法打开文件 %s: %v", filePath, err)
			continue
		}

		contentBytes, err := io.ReadAll(io.LimitReader(rc, fp.config.GetMaxFileSize()+1))
		rc.Close()

		if err != nil {
			log.Printf("警告: 读取文件 %s 失败: %v", filePath, err)
			continue
		}

		if int64(len(contentBytes)) > fp.config.GetMaxFileSize() {
			log.Printf("排除 (文件内容超限): %s", filePath)
			continue
		}

		contentType := http.DetectContentType(contentBytes)
		if !strings.HasPrefix(contentType, "text/") && !fp.config.IsTextContentTypeException(contentType) {
			log.Printf("排除 (检测到二进制内容 %s): %s", contentType, filePath)
			continue
		}

		normalizedPath := filepath.ToSlash(filePath)
		fileContents[normalizedPath] = fp.processContent(normalizedPath, contentBytes, useBase64)
		root.AddPath(normalizedPath)
		log.Printf("已处理: %s", filePath)
	}

	return &models.ProcessResult{
		FileTree:     root,
		FileContents: fileContents,
	}, nil
}

// processContent 处理文件内容
func (fp *FileProcessor) processContent(path string, content []byte, useBase64 bool) models.FileContent {
	if useBase64 {
		return models.FileContent{
			Path:     path,
			Content:  base64.StdEncoding.EncodeToString(content),
			IsBase64: true,
		}
	}
	return models.FileContent{
		Path:     path,
		Content:  string(content),
		IsBase64: false,
	}
}

// FormatOutput 格式化输出
func (fp *FileProcessor) FormatOutput(result *models.ProcessResult) string {
	var buf bytes.Buffer

	buf.WriteString("文件结构:\n")
	fp.printTree(result.FileTree, &buf, "", true)
	buf.WriteString("\n文件内容:\n")

	for path, content := range result.FileContents {
		buf.WriteString(fmt.Sprintf("\n=== %s ===\n", path))
		buf.WriteString(content.Content)
		buf.WriteString("\n")
	}

	return buf.String()
}

// printTree 打印文件树
func (fp *FileProcessor) printTree(node *models.TreeNode, buffer *bytes.Buffer, prefix string, isLast bool) {
	if node.Name != "" {
		buffer.WriteString(prefix)
		if isLast {
			buffer.WriteString("└── ")
			prefix += "    "
		} else {
			buffer.WriteString("├── ")
			prefix += "│   "
		}
		buffer.WriteString(node.Name + "\n")
	}

	var children []*models.TreeNode
	for _, child := range node.Children {
		children = append(children, child)
	}
	sort.Slice(children, func(i, j int) bool {
		if children[i].IsDir != children[j].IsDir {
			return children[i].IsDir
		}
		return children[i].Name < children[j].Name
	})

	for i, child := range children {
		fp.printTree(child, buffer, prefix, i == len(children)-1)
	}
}
