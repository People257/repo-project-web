package models

import "strings"

// FileContent 表示文件内容和元数据
type FileContent struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	IsBase64 bool   `json:"is_base64,omitempty"`
}

// TreeNode 表示文件树中的节点
type TreeNode struct {
	Name     string
	IsDir    bool
	Children map[string]*TreeNode
}

// ProcessResult 表示文件处理结果
type ProcessResult struct {
	FileTree     *TreeNode              `json:"file_tree"`
	FileContents map[string]FileContent `json:"file_contents"`
}

// NewTreeNode 创建新的树节点
func NewTreeNode(name string, isDir bool) *TreeNode {
	return &TreeNode{
		Name:     name,
		IsDir:    isDir,
		Children: make(map[string]*TreeNode),
	}
}

// AddPath 添加路径到树中
func (n *TreeNode) AddPath(path string) {
	if path == "" {
		return
	}

	parts := strings.Split(path, "/")
	current := n

	for i, part := range parts {
		isLast := i == len(parts)-1
		isDir := !isLast

		if _, exists := current.Children[part]; !exists {
			current.Children[part] = NewTreeNode(part, isDir)
		}
		current = current.Children[part]
	}
}
