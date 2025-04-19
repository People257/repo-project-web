package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"repo-prompt-web/config"
	"repo-prompt-web/types"
)

// FileContent represents a file's content and metadata
type FileContent struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	IsBase64 bool   `json:"is_base64,omitempty"`
}

// TreeNode represents a node in the file tree
type TreeNode struct {
	name     string
	isDir    bool
	children map[string]*TreeNode
}

// ProcessResult represents the result of processing files
type ProcessResult struct {
	TreeStructure string        `json:"tree_structure"`
	Files         []FileContent `json:"files"`
}

// 创建新的树节点
func newTreeNode(name string, isDir bool) *TreeNode {
	return &TreeNode{
		name:     name,
		isDir:    isDir,
		children: make(map[string]*TreeNode),
	}
}

// 递归打印文件树
func (n *TreeNode) print(buffer *bytes.Buffer, prefix string, isLast bool) {
	// 当前节点的前缀
	if n.name != "" {
		buffer.WriteString(prefix)
		if isLast {
			buffer.WriteString("└── ")
			prefix += "    "
		} else {
			buffer.WriteString("├── ")
			prefix += "│   "
		}
		buffer.WriteString(n.name + "\n")
	}

	// 获取并排序子节点
	var children []*TreeNode
	for _, child := range n.children {
		children = append(children, child)
	}
	sort.Slice(children, func(i, j int) bool {
		// 目录优先，然后按名称排序
		if children[i].isDir != children[j].isDir {
			return children[i].isDir
		}
		return children[i].name < children[j].name
	})

	// 递归打印子节点
	for i, child := range children {
		child.print(buffer, prefix, i == len(children)-1)
	}
}

func processZipStream(file multipart.File, size int64) ([]byte, error) {
	result, err := processZipStreamWithFormat(file, size, false)
	if err != nil {
		return nil, err
	}

	var buffer bytes.Buffer
	buffer.WriteString("文件树结构:\n")
	result.FileTree.Print(&buffer, "", true)
	buffer.WriteString("\n文件内容:\n\n")

	for path, content := range result.FileContents {
		separator := fmt.Sprintf("---\nFile: %s\n---\n\n", path)
		buffer.WriteString(separator)
		buffer.WriteString(content.Content)
		buffer.WriteString("\n\n")
	}

	return buffer.Bytes(), nil
}

func processZipStreamWithFormat(file io.ReaderAt, size int64, useBase64 bool) (*types.ProcessResult, error) {
	// 创建 ZIP Reader
	reader, err := zip.NewReader(file, size)
	if err != nil {
		return nil, fmt.Errorf("无法读取ZIP文件: %w", err)
	}

	// 创建根节点
	root := types.NewTreeNode("", false)
	fileContents := make(map[string]types.FileContent)

	// 遍历 ZIP 条目处理文件内容
	for _, zipEntry := range reader.File {
		// 跳过目录
		if zipEntry.FileInfo().IsDir() {
			continue
		}

		filePath := zipEntry.Name
		if config.Get().IsExcluded(filePath, zipEntry.UncompressedSize64) {
			log.Printf("排除 (规则): %s", filePath)
			continue
		}

		if !config.Get().IsLikelyTextFile(filePath) {
			log.Printf("排除 (非文本扩展名): %s", filePath)
			continue
		}

		rc, err := zipEntry.Open()
		if err != nil {
			log.Printf("警告: 无法打开文件 %s: %v", filePath, err)
			continue
		}

		contentBytes, err := io.ReadAll(io.LimitReader(rc, config.Get().GetMaxFileSize()+1))
		rc.Close()

		if err != nil {
			log.Printf("警告: 读取文件 %s 失败: %v", filePath, err)
			continue
		}

		if int64(len(contentBytes)) > config.Get().GetMaxFileSize() {
			log.Printf("排除 (文件内容超限): %s", filePath)
			continue
		}

		contentType := http.DetectContentType(contentBytes)
		if !strings.HasPrefix(contentType, "text/") && !config.Get().IsTextContentTypeException(contentType) {
			log.Printf("排除 (检测到二进制内容 %s): %s", contentType, filePath)
			continue
		}

		normalizedPath := filepath.ToSlash(filePath)
		fileContents[normalizedPath] = processContent(normalizedPath, contentBytes, useBase64)
		root.AddPath(normalizedPath)
		log.Printf("已处理: %s", filePath)
	}

	return &types.ProcessResult{
		FileTree:     root,
		FileContents: fileContents,
	}, nil
}

func processContent(path string, content []byte, useBase64 bool) types.FileContent {
	if useBase64 {
		return types.FileContent{
			Path:     path,
			Content:  base64.StdEncoding.EncodeToString(content),
			IsBase64: true,
		}
	}
	return types.FileContent{
		Path:     path,
		Content:  string(content),
		IsBase64: false,
	}
}
