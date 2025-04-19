package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
)

// 用于构建文件树的节点结构
type TreeNode struct {
	name     string
	isDir    bool
	children map[string]*TreeNode
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
	// 创建 ZIP Reader
	reader, err := zip.NewReader(file, size)
	if err != nil {
		return nil, fmt.Errorf("无法读取ZIP文件: %w", err)
	}

	// 创建根节点
	root := newTreeNode("", false)

	// 构建文件树
	for _, zipEntry := range reader.File {
		parts := strings.Split(filepath.ToSlash(zipEntry.Name), "/")
		current := root

		// 构建路径
		for i, part := range parts {
			isLast := i == len(parts)-1
			isDir := !isLast || zipEntry.FileInfo().IsDir()

			if _, exists := current.children[part]; !exists {
				current.children[part] = newTreeNode(part, isDir)
			}
			current = current.children[part]
		}
	}

	// 初始化输出 Buffer
	var outputBuffer bytes.Buffer

	// 首先输出文件树
	outputBuffer.WriteString("文件树结构:\n")
	root.print(&outputBuffer, "", true)
	outputBuffer.WriteString("\n文件内容:\n\n")

	// 遍历 ZIP 条目处理文件内容
	for _, zipEntry := range reader.File {
		// 获取文件信息
		filePath := zipEntry.Name
		fileInfo := zipEntry.FileInfo()
		uncompressedSize := zipEntry.UncompressedSize64

		// 跳过目录
		if fileInfo.IsDir() {
			continue
		}

		// 执行过滤
		if isExcluded(filePath, uncompressedSize) {
			log.Printf("排除 (规则): %s", filePath)
			continue
		}

		// 初步文本文件检查
		if !isLikelyTextFile(filePath) {
			log.Printf("排除 (非文本扩展名): %s", filePath)
			continue
		}

		// 打开 ZIP 内文件流
		rc, err := zipEntry.Open()
		if err != nil {
			log.Printf("警告: 无法打开文件 %s: %v", filePath, err)
			continue
		}

		// 确保在函数返回前关闭文件流
		defer rc.Close()

		// 读取文件内容（带限制）
		limitedReader := io.LimitReader(rc, maxFileSize+1)
		contentBytes, err := io.ReadAll(limitedReader)
		if err != nil {
			log.Printf("警告: 读取文件 %s 失败: %v", filePath, err)
			rc.Close()
			continue
		}

		// 检查是否超限
		if int64(len(contentBytes)) > maxFileSize {
			log.Printf("排除 (文件内容超限): %s", filePath)
			rc.Close()
			continue
		}

		// 二进制内容检测
		contentType := http.DetectContentType(contentBytes)
		if !strings.HasPrefix(contentType, "text/") && !isTextContentTypeException(contentType) {
			log.Printf("排除 (检测到二进制内容 %s): %s", contentType, filePath)
			rc.Close()
			continue
		}

		// 内容追加到 Buffer
		normalizedPath := filepath.ToSlash(filePath)
		separator := fmt.Sprintf("---\nFile: %s\n---\n\n", normalizedPath)
		outputBuffer.WriteString(separator)
		outputBuffer.Write(contentBytes)
		outputBuffer.WriteString("\n\n")

		log.Printf("已处理: %s", filePath)
		rc.Close()
	}

	return outputBuffer.Bytes(), nil
}
