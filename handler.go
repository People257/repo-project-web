package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"

	"repo-prompt-web/config"
	"repo-prompt-web/types"

	"github.com/gin-gonic/gin"
)

// GitHubContent 表示 GitHub API 响应
type GitHubContent struct {
	Type        string `json:"type"`
	Path        string `json:"path"`
	Content     string `json:"content"`
	DownloadURL string `json:"download_url"`
}

// handleCombineCodeGin 处理 ZIP 文件上传请求
func handleCombineCodeGin(c *gin.Context) {
	// 获取文件
	file, err := c.FormFile("codeZip")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请上传 ZIP 文件"})
		return
	}

	// 检查文件大小
	if file.Size > int64(config.Get().GetMaxUploadSize()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("文件大小超过限制 %d MB", config.Get().GetMaxUploadSize()/(1024*1024))})
		return
	}

	// 打开文件
	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法打开上传的文件"})
		return
	}
	defer src.Close()

	// 获取输出格式和 base64 编码选项
	format := c.DefaultQuery("format", "text")
	useBase64 := c.DefaultQuery("base64", "false") == "true"

	// 处理 ZIP 文件
	result, err := processZipStreamWithFormat(src, file.Size, useBase64)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 根据格式返回结果
	if format == "json" {
		c.JSON(http.StatusOK, result)
	} else {
		c.String(http.StatusOK, formatTextOutput(result))
	}
}

// handleGitHubRepo 处理 GitHub 仓库请求
func handleGitHubRepo(c *gin.Context) {
	// 获取 GitHub 仓库 URL
	repoURL := c.Query("url")
	if repoURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供 GitHub 仓库 URL"})
		return
	}

	// 获取输出格式和 base64 编码选项
	format := c.DefaultQuery("format", "text")
	useBase64 := c.DefaultQuery("base64", "false") == "true"

	// 获取 GitHub Token（可选）
	token := c.Query("token")

	// 解析 GitHub URL
	owner, repo, err := parseGitHubURL(repoURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 获取仓库文件树
	fileTree, fileContents, err := getGitHubRepoTree(owner, repo, token, useBase64)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result := &types.ProcessResult{
		FileTree:     fileTree,
		FileContents: fileContents,
	}

	// 根据格式返回结果
	if format == "json" {
		c.JSON(http.StatusOK, result)
	} else {
		c.String(http.StatusOK, formatTextOutput(result))
	}
}

// makeGitHubRequest 发送 GitHub API 请求
func makeGitHubRequest(url, token string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{}
	return client.Do(req)
}

// parseGitHubURL 解析 GitHub URL
func parseGitHubURL(url string) (owner, repo string, err error) {
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

// getGitHubRepoTree 获取 GitHub 仓库文件树
func getGitHubRepoTree(owner, repo, token string, useBase64 bool) (*types.TreeNode, map[string]types.FileContent, error) {
	// 创建根节点
	root := types.NewTreeNode("", false)
	fileContents := make(map[string]types.FileContent)

	// 尝试不同的分支名
	branches := []string{"main", "master"}
	var lastError error

	for _, branch := range branches {
		apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1", owner, repo, branch)
		resp, err := makeGitHubRequest(apiURL, token)
		if err != nil {
			lastError = err
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == 200 {
			// 处理文件树
			var treeResp struct {
				Tree []struct {
					Path string `json:"path"`
					Type string `json:"type"`
					URL  string `json:"url"`
				} `json:"tree"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&treeResp); err != nil {
				return nil, nil, err
			}

			// 处理文件内容
			for _, item := range treeResp.Tree {
				if item.Type == "blob" {
					// 检查文件是否应该被排除
					if config.Get().IsExcluded(item.Path, 0) {
						continue
					}

					// 获取文件内容
					content, err := getGitHubFileContent(owner, repo, item.Path, token, useBase64)
					if err != nil {
						log.Printf("获取文件内容失败 %s: %v", item.Path, err)
						continue
					}

					if content != "" {
						fileContents[item.Path] = types.FileContent{
							Path:     item.Path,
							Content:  content,
							IsBase64: useBase64,
						}
					}
				}

				// 添加到文件树
				root.AddPath(item.Path)
			}

			return root, fileContents, nil
		}

		lastError = fmt.Errorf("GitHub API 请求失败: %s", resp.Status)
	}

	return nil, nil, lastError
}

// getGitHubFileContent 获取 GitHub 文件内容
func getGitHubFileContent(owner, repo, path, token string, useBase64 bool) (string, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", owner, repo, path)
	resp, err := makeGitHubRequest(apiURL, token)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("获取文件内容失败: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var content GitHubContent
	if err := json.Unmarshal(body, &content); err != nil {
		return "", err
	}

	// 检查文件类型
	if !config.Get().IsLikelyTextFile(path) {
		return "", nil
	}

	// 解码 base64 内容
	decoded, err := base64.StdEncoding.DecodeString(content.Content)
	if err != nil {
		return "", err
	}

	// 如果需要 base64 编码输出
	if useBase64 {
		return base64.StdEncoding.EncodeToString(decoded), nil
	}

	return string(decoded), nil
}

// formatTextOutput 格式化文本输出
func formatTextOutput(result *types.ProcessResult) string {
	var buf bytes.Buffer

	// 输出文件树
	buf.WriteString("文件结构:\n")
	result.FileTree.Print(&buf, "", true)
	buf.WriteString("\n文件内容:\n")

	// 输出文件内容
	for path, content := range result.FileContents {
		buf.WriteString(fmt.Sprintf("\n=== %s ===\n", path))
		buf.WriteString(content.Content)
		buf.WriteString("\n")
	}

	return buf.String()
}
