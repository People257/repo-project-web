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
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// GitHub API 响应结构
type GitHubContent struct {
	Type    string `json:"type"`
	Path    string `json:"path"`
	Content string `json:"content"`
	Message string `json:"message"`
}

// GitHubError represents a GitHub API error response
type GitHubError struct {
	Message string `json:"message"`
}

var (
	githubClient = &http.Client{
		Timeout: 30 * time.Second,
	}
	defaultBranches = []string{"main", "master"}
)

func handleGitHubRepo(c *gin.Context) {
	url := c.Query("url")
	if url == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供 GitHub 仓库 URL"})
		return
	}

	useBase64 := c.DefaultQuery("base64", "false") == "true"
	format := c.DefaultQuery("format", "text")

	// 解析 GitHub URL
	owner, repo, err := parseGitHubURL(url)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("无效的 GitHub URL: %v", err)})
		return
	}

	// 创建一个临时的 buffer 来存储文件树
	var treeBuffer bytes.Buffer

	// 获取仓库文件树
	tree, err := getGitHubRepoTree(owner, repo)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("获取仓库内容失败: %v", err)})
		return
	}

	// 打印文件树
	treeBuffer.WriteString("文件树结构:\n")
	tree.print(&treeBuffer, "", true)

	// 获取并处理文件内容
	var files []FileContent
	err = processGitHubFilesWithFormat(owner, repo, &files, useBase64)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("处理仓库文件失败: %v", err)})
		return
	}

	result := ProcessResult{
		TreeStructure: treeBuffer.String(),
		Files:         files,
	}

	if format == "json" {
		c.JSON(http.StatusOK, result)
	} else {
		// 生成文本格式输出
		var buffer bytes.Buffer
		buffer.WriteString(result.TreeStructure)
		buffer.WriteString("\n文件内容:\n\n")

		for _, file := range result.Files {
			separator := fmt.Sprintf("---\nFile: %s\n---\n\n", file.Path)
			buffer.WriteString(separator)
			if file.IsBase64 {
				decoded, err := base64.StdEncoding.DecodeString(file.Content)
				if err != nil {
					log.Printf("警告: 解码文件 %s 内容失败: %v", file.Path, err)
					continue
				}
				buffer.Write(decoded)
			} else {
				buffer.WriteString(file.Content)
			}
			buffer.WriteString("\n\n")
		}

		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", outputFilename))
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.String(http.StatusOK, buffer.String())
	}
}

func handleCombineCodeGin(c *gin.Context) {
	useBase64 := c.DefaultQuery("base64", "false") == "true"
	format := c.DefaultQuery("format", "text")

	// 获取上传文件
	fileHeader, err := c.FormFile("codeZip")
	if err != nil {
		log.Printf("文件上传错误: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "请上传名为 'codeZip' 的 ZIP 文件"})
		return
	}

	// 基本文件头检查
	if fileHeader.Size == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "上传的文件为空"})
		return
	}

	if fileHeader.Size > maxUploadSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("文件大小超过限制 (%d MB)", maxUploadSize/(1024*1024))})
		return
	}

	// 打开文件流
	file, err := fileHeader.Open()
	if err != nil {
		log.Printf("无法打开上传的文件: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法处理上传的文件"})
		return
	}
	defer file.Close()

	// 调用核心处理器
	result, err := processZipStreamWithFormat(file, fileHeader.Size, useBase64)
	if err != nil {
		log.Printf("处理 ZIP 文件失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "处理 ZIP 文件时出错"})
		return
	}

	// 如果没有内容
	if len(result.Files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未找到任何有效的文本文件内容"})
		return
	}

	if format == "json" {
		c.JSON(http.StatusOK, result)
	} else {
		// 生成文本格式输出
		var buffer bytes.Buffer
		buffer.WriteString(result.TreeStructure)
		buffer.WriteString("\n文件内容:\n\n")

		for _, file := range result.Files {
			separator := fmt.Sprintf("---\nFile: %s\n---\n\n", file.Path)
			buffer.WriteString(separator)
			if file.IsBase64 {
				decoded, err := base64.StdEncoding.DecodeString(file.Content)
				if err != nil {
					log.Printf("警告: 解码文件 %s 内容失败: %v", file.Path, err)
					continue
				}
				buffer.Write(decoded)
			} else {
				buffer.WriteString(file.Content)
			}
			buffer.WriteString("\n\n")
		}

		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", outputFilename))
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.String(http.StatusOK, buffer.String())
	}

	log.Printf("成功处理文件 %s", fileHeader.Filename)
}

func parseGitHubURL(url string) (owner, repo string, err error) {
	// 支持以下格式：
	// https://github.com/owner/repo
	// https://github.com/owner/repo.git
	// git@github.com:owner/repo.git
	re := regexp.MustCompile(`(?:github\.com[:/])([\w-]+)/([\w.-]+?)(?:\.git)?$`)
	matches := re.FindStringSubmatch(url)
	if len(matches) != 3 {
		return "", "", fmt.Errorf("无法解析 GitHub URL")
	}
	return matches[1], matches[2], nil
}

func getGitHubRepoTree(owner, repo string) (*TreeNode, error) {
	var lastErr error
	for _, branch := range defaultBranches {
		apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1", owner, repo, branch)
		resp, err := githubClient.Get(apiURL)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			lastErr = fmt.Errorf("branch %s not found", branch)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			var githubErr GitHubError
			if err := json.NewDecoder(resp.Body).Decode(&githubErr); err != nil {
				lastErr = fmt.Errorf("GitHub API error: %s", resp.Status)
			} else {
				lastErr = fmt.Errorf("GitHub API error: %s", githubErr.Message)
			}
			continue
		}

		var result struct {
			Tree []struct {
				Path string `json:"path"`
				Type string `json:"type"`
			} `json:"tree"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			lastErr = err
			continue
		}

		root := newTreeNode("", false)
		for _, item := range result.Tree {
			parts := strings.Split(item.Path, "/")
			current := root
			for i, part := range parts {
				isLast := i == len(parts)-1
				isDir := !isLast || item.Type == "tree"
				if _, exists := current.children[part]; !exists {
					current.children[part] = newTreeNode(part, isDir)
				}
				current = current.children[part]
			}
		}
		return root, nil
	}
	return nil, fmt.Errorf("无法获取仓库树结构: %v", lastErr)
}

func processGitHubFilesWithFormat(owner, repo string, files *[]FileContent, useBase64 bool) error {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents", owner, repo)
	return processGitHubDirWithFormat(apiURL, "", files, useBase64)
}

func processGitHubDirWithFormat(apiURL, path string, files *[]FileContent, useBase64 bool) error {
	url := apiURL
	if path != "" {
		url = fmt.Sprintf("%s/%s", apiURL, path)
	}

	resp, err := githubClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var githubErr GitHubError
		if err := json.NewDecoder(resp.Body).Decode(&githubErr); err != nil {
			return fmt.Errorf("GitHub API error: %s", resp.Status)
		}
		return fmt.Errorf("GitHub API error: %s", githubErr.Message)
	}

	// 先尝试解析为数组（目录）
	var contents []GitHubContent
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %v", err)
	}

	// 尝试解析为数组
	if err := json.Unmarshal(bodyBytes, &contents); err != nil {
		// 如果解析数组失败，尝试解析为单个文件
		var singleFile GitHubContent
		if err := json.Unmarshal(bodyBytes, &singleFile); err != nil {
			return fmt.Errorf("解析响应失败: %v", err)
		}
		// 如果是单个文件，将其添加到数组中
		contents = []GitHubContent{singleFile}
	}

	for _, content := range contents {
		if content.Type == "dir" {
			if err := processGitHubDirWithFormat(apiURL, content.Path, files, useBase64); err != nil {
				log.Printf("警告: 处理目录 %s 失败: %v", content.Path, err)
				continue
			}
		} else if isLikelyTextFile(content.Path) && !isExcluded(content.Path, 0) {
			// 如果已经有内容，直接使用
			if content.Content != "" {
				decodedContent, err := base64.StdEncoding.DecodeString(content.Content)
				if err != nil {
					log.Printf("警告: 解码文件 %s 内容失败: %v", content.Path, err)
					continue
				}

				*files = append(*files, processContent(content.Path, decodedContent, useBase64))
				log.Printf("已处理: %s", content.Path)
				continue
			}

			// 否则获取文件内容
			fileResp, err := githubClient.Get(fmt.Sprintf("%s/%s", apiURL, content.Path))
			if err != nil {
				log.Printf("警告: 获取文件 %s 失败: %v", content.Path, err)
				continue
			}

			var fileContent GitHubContent
			if err := json.NewDecoder(fileResp.Body).Decode(&fileContent); err != nil {
				fileResp.Body.Close()
				log.Printf("警告: 解析文件 %s 内容失败: %v", content.Path, err)
				continue
			}
			fileResp.Body.Close()

			// GitHub API returns base64 encoded content, decode it first
			decodedContent, err := base64.StdEncoding.DecodeString(fileContent.Content)
			if err != nil {
				log.Printf("警告: 解码文件 %s 内容失败: %v", content.Path, err)
				continue
			}

			*files = append(*files, processContent(content.Path, decodedContent, useBase64))
			log.Printf("已处理: %s", content.Path)
		}
	}

	return nil
}
