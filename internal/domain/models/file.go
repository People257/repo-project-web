package models

import (
	"repo-prompt-web/pkg/types"
)

// FileContent alias to unified model
type FileContent = types.FileContent

// TreeNode alias to unified model
type TreeNode = types.TreeNode

// ProcessResult alias to unified model
type ProcessResult = types.ProcessResult

// NewTreeNode alias to unified function
func NewTreeNode(name string, isDir bool) *TreeNode {
	return types.NewTreeNode(name, isDir)
}

// AddPathToTree is a helper function that wraps the TreeNode.AddPath method
func AddPathToTree(node *TreeNode, path string) {
	if node != nil {
		node.AddPath(path)
	}
}
