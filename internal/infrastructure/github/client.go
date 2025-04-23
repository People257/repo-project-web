package github

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"repo-prompt-web/internal/domain/models"
	"repo-prompt-web/pkg/config"
)

// Content 表示 GitHub API 响应
type Content struct {
	Type        string `json:"type"`
	Path        string `json:"path"`
	Content     string `json:"content"`
	DownloadURL string `json:"download_url"`
}

// Client GitHub 客户端
type Client struct {
	config *config.Config
}

// NewClient 创建 GitHub 客户端实例
func NewClient(cfg *config.Config) *Client {
	return &Client{
		config: cfg,
	}
}

// GetRepoContents 获取仓库内容
func (c *Client) GetRepoContents(owner, repo, token string, useBase64 bool) (*models.ProcessResult, error) {
	log.Printf("开始获取 GitHub 仓库内容: %s/%s", owner, repo)

	branches := []string{"main", "master"}
	var lastError error

	for _, branch := range branches {
		log.Printf("尝试分支: %s", branch)
		tree, contents, err := c.getTreeContents(owner, repo, branch, token, useBase64)
		if err != nil {
			log.Printf("分支 %s 获取失败: %v", branch, err)
			lastError = err
			continue
		}

		log.Printf("成功获取仓库内容，共 %d 个文件", len(contents))
		return &models.ProcessResult{
			FileTree:     tree,
			FileContents: contents,
		}, nil
	}

	return nil, fmt.Errorf("无法获取仓库内容: %v", lastError)
}

// getTreeContents 获取文件树内容
func (c *Client) getTreeContents(owner, repo, branch, token string, useBase64 bool) (*models.TreeNode, map[string]models.FileContent, error) {
	root := models.NewTreeNode("", false)
	fileContents := make(map[string]models.FileContent)

	// 首先尝试获取递归树结构
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1", owner, repo, branch)
	log.Printf("获取仓库结构: %s", apiURL)

	resp, err := c.makeRequest(apiURL, token)
	if err != nil {
		return nil, nil, fmt.Errorf("请求仓库树失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("API 返回错误: 状态码 %d, 响应: %s", resp.StatusCode, string(body))
		return nil, nil, fmt.Errorf("GitHub API 请求失败: %s - %s", resp.Status, string(body))
	}

	// 解析树响应
	var treeResp struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
			URL  string `json:"url"`
			Size int64  `json:"size"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&treeResp); err != nil {
		return nil, nil, fmt.Errorf("解析树响应失败: %w", err)
	}

	// 如果树被截断，提供警告
	if treeResp.Truncated {
		log.Print("警告: 仓库树被截断，可能不包含所有文件")
	}

	// 优先收集文档和重要文件
	importantFiles := map[string]bool{
		"README.md":        true,
		"README":           true,
		"LICENSE":          true,
		"CONTRIBUTING.md":  true,
		"go.mod":           true,
		"package.json":     true,
		"requirements.txt": true,
		"Cargo.toml":       true,
		"Dockerfile":       true,
	}

	// 优先处理的文件类型
	priorityExtensions := map[string]bool{
		".md":       true,
		".markdown": true,
		".txt":      true,
		".go":       true,
		".py":       true,
		".js":       true,
		".ts":       true,
		".java":     true,
		".c":        true,
		".cpp":      true,
		".h":        true,
	}

	// 分类文件用于处理
	var priorityPaths []string
	var regularPaths []string

	log.Printf("找到 %d 个文件/目录节点", len(treeResp.Tree))

	// 添加所有项目到文件树，并分类文件
	for _, item := range treeResp.Tree {
		// 如果是文件，检查是否要获取内容
		if item.Type == "blob" {
			ext := strings.ToLower(filepath.Ext(item.Path))
			filename := filepath.Base(item.Path)

			// 优先级排序
			if importantFiles[filename] || priorityExtensions[ext] {
				priorityPaths = append(priorityPaths, item.Path)
			} else if !c.config.IsExcluded(item.Path, uint64(item.Size)) && c.config.IsLikelyTextFile(item.Path) {
				regularPaths = append(regularPaths, item.Path)
			}
		}

		// 无论是否处理内容，都添加到文件树中
		root.AddPath(item.Path)
	}

	// 限制常规文件数量以防止请求过多
	const maxRegularFiles = 50
	if len(regularPaths) > maxRegularFiles {
		log.Printf("常规文件过多 (%d)，限制为 %d 个", len(regularPaths), maxRegularFiles)
		regularPaths = regularPaths[:maxRegularFiles]
	}

	// 处理优先文件
	log.Printf("处理 %d 个优先文件", len(priorityPaths))
	for _, path := range priorityPaths {
		content, err := c.getFileContent(owner, repo, path, token, useBase64)
		if err != nil {
			log.Printf("获取文件内容失败 %s: %v", path, err)
			continue
		}

		if content != "" {
			fileContents[path] = models.FileContent{
				Path:     path,
				Content:  content,
				IsBase64: useBase64,
			}
		}
	}

	// 处理常规文件
	log.Printf("处理 %d 个常规文件", len(regularPaths))
	for _, path := range regularPaths {
		content, err := c.getFileContent(owner, repo, path, token, useBase64)
		if err != nil {
			log.Printf("获取文件内容失败 %s: %v", path, err)
			continue
		}

		if content != "" {
			fileContents[path] = models.FileContent{
				Path:     path,
				Content:  content,
				IsBase64: useBase64,
			}
		}
	}

	log.Printf("完成获取仓库内容，成功获取 %d 个文件", len(fileContents))
	return root, fileContents, nil
}

// getFileContent 获取文件内容
func (c *Client) getFileContent(owner, repo, path, token string, useBase64 bool) (string, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", owner, repo, path)

	resp, err := c.makeRequest(apiURL, token)
	if err != nil {
		return "", fmt.Errorf("请求文件失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("获取文件内容失败: %s - %s", resp.Status, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	var content Content
	if err := json.Unmarshal(body, &content); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if !c.config.IsLikelyTextFile(path) {
		return "", nil
	}

	// 检查文件大小
	const maxContentSize = 100000 // 约100KB
	if len(content.Content) > maxContentSize {
		log.Printf("文件过大，跳过: %s", path)
		return "", nil
	}

	// 尝试解码Base64内容
	decoded, err := base64.StdEncoding.DecodeString(content.Content)
	if err != nil {
		return "", fmt.Errorf("解码内容失败: %w", err)
	}

	if useBase64 {
		return base64.StdEncoding.EncodeToString(decoded), nil
	}

	return string(decoded), nil
}

// makeRequest 发送 HTTP 请求
func (c *Client) makeRequest(url, token string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Repo-Prompt-Web/1.0")

	client := &http.Client{
		Timeout: 20 * time.Second,
	}
	return client.Do(req)
}

// ParseRepoURL 解析 GitHub 仓库 URL
func ParseRepoURL(url string) (owner, repo string, err error) {
	patterns := []string{
		`github\.com[:/]([^/]+)/([^/]+?)(?:\.git)?/?$`,
		`github\.com/([^/]+)/([^/]+)`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(url)
		if len(matches) == 3 {
			return matches[1], matches[2], nil
		}
	}

	return "", "", fmt.Errorf("无效的 GitHub 仓库 URL")
}
