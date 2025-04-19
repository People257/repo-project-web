package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

// GitHub API 响应结构
type GitHubContent struct {
	Type    string `json:"type"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

func handleGitHubRepo(c *gin.Context) {
	url := c.Query("url")
	if url == "" {
		c.String(http.StatusBadRequest, "请提供 GitHub 仓库 URL")
		return
	}

	// 解析 GitHub URL
	owner, repo, err := parseGitHubURL(url)
	if err != nil {
		c.String(http.StatusBadRequest, fmt.Sprintf("无效的 GitHub URL: %v", err))
		return
	}

	// 创建一个临时的 buffer 来存储文件内容
	var buffer bytes.Buffer
	buffer.WriteString("文件树结构:\n")

	// 获取仓库文件树
	tree, err := getGitHubRepoTree(owner, repo)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("获取仓库内容失败: %v", err))
		return
	}

	// 打印文件树
	tree.print(&buffer, "", true)
	buffer.WriteString("\n文件内容:\n\n")

	// 获取并处理文件内容
	err = processGitHubFiles(owner, repo, &buffer)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("处理仓库文件失败: %v", err))
		return
	}

	// 设置响应头并发送数据
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", outputFilename))
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.String(http.StatusOK, buffer.String())
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
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/main?recursive=1", owner, repo)
	resp, err := http.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
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

func processGitHubFiles(owner, repo string, buffer *bytes.Buffer) error {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents", owner, repo)
	return processGitHubDir(apiURL, "", buffer)
}

func processGitHubDir(apiURL, path string, buffer *bytes.Buffer) error {
	url := apiURL
	if path != "" {
		url = fmt.Sprintf("%s/%s", apiURL, path)
	}

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var contents []GitHubContent
	if err := json.NewDecoder(resp.Body).Decode(&contents); err != nil {
		return err
	}

	for _, content := range contents {
		if content.Type == "dir" {
			if err := processGitHubDir(apiURL, content.Path, buffer); err != nil {
				log.Printf("警告: 处理目录 %s 失败: %v", content.Path, err)
				continue
			}
		} else if isLikelyTextFile(content.Path) && !isExcluded(content.Path, 0) {
			// 获取文件内容
			fileResp, err := http.Get(fmt.Sprintf("%s/%s", apiURL, content.Path))
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

			// 写入文件内容
			separator := fmt.Sprintf("---\nFile: %s\n---\n\n", content.Path)
			buffer.WriteString(separator)
			buffer.WriteString(fileContent.Content)
			buffer.WriteString("\n\n")

			log.Printf("已处理: %s", content.Path)
		}
	}

	return nil
}

func handleCombineCodeGin(c *gin.Context) {
	// 获取上传文件
	fileHeader, err := c.FormFile("codeZip")
	if err != nil {
		log.Printf("文件上传错误: %v", err)
		c.String(http.StatusBadRequest, "请上传名为 'codeZip' 的 ZIP 文件")
		return
	}

	// 基本文件头检查
	if fileHeader.Size == 0 {
		c.String(http.StatusBadRequest, "上传的文件为空")
		return
	}

	if fileHeader.Size > maxUploadSize {
		c.String(http.StatusBadRequest, fmt.Sprintf("文件大小超过限制 (%d MB)", maxUploadSize/(1024*1024)))
		return
	}

	// 打开文件流
	file, err := fileHeader.Open()
	if err != nil {
		log.Printf("无法打开上传的文件: %v", err)
		c.String(http.StatusInternalServerError, "无法处理上传的文件")
		return
	}
	defer file.Close()

	// 调用核心处理器
	combinedContent, err := processZipStream(file, fileHeader.Size)
	if err != nil {
		log.Printf("处理 ZIP 文件失败: %v", err)
		c.String(http.StatusInternalServerError, "处理 ZIP 文件时出错")
		return
	}

	// 如果没有内容
	if len(combinedContent) == 0 {
		c.String(http.StatusBadRequest, "未找到任何有效的文本文件内容")
		return
	}

	// 设置响应头并发送数据
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", outputFilename))
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", combinedContent)

	log.Printf("成功处理文件 %s", fileHeader.Filename)
}
