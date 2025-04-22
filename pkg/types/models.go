package types

import (
	"bytes"
	"path/filepath"
	"sort"
	"strings"
)

// TreeNode represents a node in the file tree
type TreeNode struct {
	Name     string               `json:"name"`
	IsDir    bool                 `json:"is_dir"`
	Children map[string]*TreeNode `json:"children,omitempty"`
}

// FileContent represents a file's content and metadata
type FileContent struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	IsBase64 bool   `json:"is_base64,omitempty"`
}

// ProcessResult represents the result of processing files
type ProcessResult struct {
	FileTree     *TreeNode              `json:"file_tree"`
	FileContents map[string]FileContent `json:"file_contents"`
}

// Document represents a documentation file
type Document struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Type    string `json:"type"` // e.g., "readme", "license", "config"
}

// ProjectAnalysis represents the analysis of a project
type ProjectAnalysis struct {
	PromptSuggestions []string   `json:"prompt_suggestions"`
	Documents         []Document `json:"documents,omitempty"`
	GeneratedAt       string     `json:"generated_at"`
}

// NewTreeNode creates a new tree node
func NewTreeNode(name string, isDir bool) *TreeNode {
	return &TreeNode{
		Name:     name,
		IsDir:    isDir,
		Children: make(map[string]*TreeNode),
	}
}

// Print recursively prints the file tree
func (n *TreeNode) Print(buffer *bytes.Buffer, prefix string, isLast bool) {
	// Print current node
	if n.Name != "" {
		buffer.WriteString(prefix)
		if isLast {
			buffer.WriteString("└── ")
			prefix += "    "
		} else {
			buffer.WriteString("├── ")
			prefix += "│   "
		}
		buffer.WriteString(n.Name + "\n")
	}

	// Get and sort children
	var children []*TreeNode
	for _, child := range n.Children {
		children = append(children, child)
	}
	sort.Slice(children, func(i, j int) bool {
		// Directories first, then sort by name
		if children[i].IsDir != children[j].IsDir {
			return children[i].IsDir
		}
		return children[i].Name < children[j].Name
	})

	// Recursively print children
	for i, child := range children {
		child.Print(buffer, prefix, i == len(children)-1)
	}
}

// AddPath adds a path to the tree
func (n *TreeNode) AddPath(path string) {
	if path == "" {
		return
	}

	parts := strings.Split(filepath.ToSlash(path), "/")
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
