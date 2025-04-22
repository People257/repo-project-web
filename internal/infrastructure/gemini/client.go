package gemini

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"repo-prompt-web/pkg/config"
	"repo-prompt-web/pkg/logger"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Client 是 Gemini API 客户端
type Client struct {
	apiKey     string
	apiUrl     string
	model      string
	httpClient *http.Client
}

// GeminiRequest Gemini API 请求结构
type GeminiRequest struct {
	Contents []Content `json:"contents"`
	Stream   bool      `json:"stream,omitempty"`
}

// Content 内容结构
type Content struct {
	Parts []Part `json:"parts"`
}

// Part 内容片段
type Part struct {
	Text string `json:"text"`
}

// GeminiResponse Gemini API 响应结构
type GeminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason,omitempty"`
	} `json:"promptFeedback"`
}

// StreamChunk 表示流式响应的一个片段
type StreamChunk struct {
	Text         string
	FinishReason string
	Error        error
}

// getProxy 获取代理配置
func getProxy(cfg *config.Config) func(*http.Request) (*url.URL, error) {
	// 检查配置中是否有明确的代理设置
	proxyURL := cfg.GetGeminiProxyURL()
	if proxyURL != "" {
		proxy, err := url.Parse(proxyURL)
		if err != nil {
			logger.Warn("无效的代理URL配置，将使用系统代理",
				zap.String("proxy_url", proxyURL),
				zap.Error(err))
			return http.ProxyFromEnvironment
		}
		logger.Info("使用配置的Gemini API代理",
			zap.String("proxy_url", proxyURL))
		return http.ProxyURL(proxy)
	}

	// 否则使用系统环境变量中的代理
	return http.ProxyFromEnvironment
}

// NewClient 创建一个新的 Gemini 客户端
func NewClient(cfg *config.Config) *Client {
	// 创建一个带有自定义传输层的HTTP客户端
	transport := &http.Transport{
		Proxy: getProxy(cfg), // 使用代理配置
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second, // 连接超时时间
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   30 * time.Second, // 增加TLS握手超时时间
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	}

	return &Client{
		apiKey: cfg.GetGeminiAPIKey(),
		apiUrl: fmt.Sprintf("%s/%s:generateContent", cfg.GetGeminiApiEndpoint(), cfg.GetGeminiModel()),
		model:  cfg.GetGeminiModel(),
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   180 * time.Second, // 增加整体超时时间到3分钟
		},
	}
}

// SendPrompt 发送提示词到 Gemini API
func (c *Client) SendPrompt(prompt string) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("Gemini API 密钥未配置")
	}

	logger.Debug("准备发送提示词到 Gemini API",
		zap.String("model", c.model),
		zap.Int("prompt_length", len(prompt)))

	// 构建请求体
	reqBody := GeminiRequest{
		Contents: []Content{
			{
				Parts: []Part{
					{
						Text: prompt,
					},
				},
			},
		},
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("序列化请求失败: %w", err)
	}

	// 添加重试逻辑
	var response string
	maxRetries := 3
	retryDelay := 2 * time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			logger.Info("重试 Gemini API 请求",
				zap.Int("attempt", attempt+1),
				zap.Int("max_retries", maxRetries))
			time.Sleep(retryDelay)
			// 指数退避策略
			retryDelay *= 2
		}

		// 构建请求
		req, err := http.NewRequest("POST", c.apiUrl, bytes.NewBuffer(reqJSON))
		if err != nil {
			return "", fmt.Errorf("创建请求失败: %w", err)
		}

		// 添加查询参数和请求头
		q := req.URL.Query()
		q.Add("key", c.apiKey)
		req.URL.RawQuery = q.Encode()

		req.Header.Set("Content-Type", "application/json")

		// 发送请求
		resp, err := c.httpClient.Do(req)
		if err != nil {
			logger.Warn("Gemini API 请求失败, 将重试",
				zap.Error(err),
				zap.Int("attempt", attempt+1),
				zap.Int("max_retries", maxRetries))
			continue // 重试
		}

		defer resp.Body.Close()

		// 处理非 2xx 响应
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			errMsg := fmt.Sprintf("API 返回错误: %s (%d): %s", resp.Status, resp.StatusCode, string(bodyBytes))

			// 如果是服务器错误(5xx)，尝试重试
			if resp.StatusCode >= 500 && attempt < maxRetries-1 {
				logger.Warn("Gemini API 服务器错误, 将重试",
					zap.Int("status_code", resp.StatusCode),
					zap.Int("attempt", attempt+1),
					zap.Int("max_retries", maxRetries))
				continue // 重试
			}

			return "", fmt.Errorf(errMsg)
		}

		// 解析响应
		var geminiResp GeminiResponse
		if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
			if attempt < maxRetries-1 {
				logger.Warn("解析 Gemini 响应失败, 将重试",
					zap.Error(err),
					zap.Int("attempt", attempt+1),
					zap.Int("max_retries", maxRetries))
				continue // 重试
			}
			return "", fmt.Errorf("解析响应失败: %w", err)
		}

		// 检查是否被阻止
		if geminiResp.PromptFeedback.BlockReason != "" {
			return "", fmt.Errorf("提示词被阻止: %s", geminiResp.PromptFeedback.BlockReason)
		}

		// 检查是否有有效响应
		if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
			if attempt < maxRetries-1 {
				logger.Warn("Gemini API 返回空响应, 将重试",
					zap.Int("attempt", attempt+1),
					zap.Int("max_retries", maxRetries))
				continue // 重试
			}
			return "", fmt.Errorf("API 返回空响应")
		}

		response = geminiResp.Candidates[0].Content.Parts[0].Text

		logger.Debug("从 Gemini 收到响应",
			zap.Int("response_length", len(response)),
			zap.String("finish_reason", geminiResp.Candidates[0].FinishReason))

		break // 成功获取响应，退出重试循环
	}

	if response == "" {
		return "", fmt.Errorf("Gemini API 请求失败，已达到最大重试次数")
	}

	return response, nil
}

// SendPromptStream 流式发送提示词到 Gemini API，支持实时响应
func (c *Client) SendPromptStream(prompt string) (<-chan StreamChunk, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("Gemini API 密钥未配置")
	}

	logger.Debug("准备流式发送提示词到 Gemini API",
		zap.String("model", c.model),
		zap.Int("prompt_length", len(prompt)))

	// 构建请求体
	reqBody := GeminiRequest{
		Contents: []Content{
			{
				Parts: []Part{
					{
						Text: prompt,
					},
				},
			},
		},
		Stream: true,
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	// 构建请求
	req, err := http.NewRequest("POST", c.apiUrl, bytes.NewBuffer(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 添加查询参数和请求头
	q := req.URL.Query()
	q.Add("key", c.apiKey)
	q.Add("alt", "sse") // 添加 Server-Sent Events 参数
	req.URL.RawQuery = q.Encode()

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	// 创建返回通道
	resultChan := make(chan StreamChunk, 100)

	// 异步发送请求和处理流式响应
	go func() {
		defer close(resultChan)

		// 添加重试逻辑
		maxRetries := 2 // 流式响应重试次数少一些
		retryDelay := 2 * time.Second

		for attempt := 0; attempt < maxRetries; attempt++ {
			if attempt > 0 {
				logger.Info("重试流式 Gemini API 请求",
					zap.Int("attempt", attempt+1),
					zap.Int("max_retries", maxRetries))
				time.Sleep(retryDelay)
				retryDelay *= 2
			}

			// 创建一个新的请求副本
			reqCopy := *req
			reqCopy.Body = io.NopCloser(bytes.NewBuffer(reqJSON))

			// 发送请求
			resp, err := c.httpClient.Do(&reqCopy)
			if err != nil {
				if attempt < maxRetries-1 {
					logger.Warn("流式 Gemini API 请求失败, 将重试",
						zap.Error(err),
						zap.Int("attempt", attempt+1),
						zap.Int("max_retries", maxRetries))
					continue // 重试
				}
				resultChan <- StreamChunk{Error: fmt.Errorf("请求失败: %w", err)}
				return
			}

			// 使用闭包确保在每次重试时关闭前一个响应
			func() {
				defer resp.Body.Close()

				// 处理非 2xx 响应
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					bodyBytes, _ := io.ReadAll(resp.Body)
					errMsg := fmt.Errorf("API 返回错误: %s (%d): %s", resp.Status, resp.StatusCode, string(bodyBytes))

					// 如果是服务器错误(5xx)，尝试重试
					if resp.StatusCode >= 500 && attempt < maxRetries-1 {
						logger.Warn("流式 Gemini API 服务器错误, 将重试",
							zap.Int("status_code", resp.StatusCode),
							zap.Int("attempt", attempt+1),
							zap.Int("max_retries", maxRetries))
						return // 继续重试
					}

					resultChan <- StreamChunk{Error: errMsg}
					return
				}

				// 读取 SSE 流
				scanner := bufio.NewScanner(resp.Body)

				// 增加缓冲区大小以支持更长的行
				const maxScanTokenSize = 1024 * 1024 // 1MB
				buf := make([]byte, maxScanTokenSize)
				scanner.Buffer(buf, maxScanTokenSize)

				successfulStream := false

				for scanner.Scan() {
					line := scanner.Text()

					// 跳过空行和非数据行
					if line == "" || !strings.HasPrefix(line, "data: ") {
						continue
					}

					// 移除 "data: " 前缀
					data := strings.TrimPrefix(line, "data: ")

					// 特殊处理：如果收到 [DONE] 信号，表示流结束
					if data == "[DONE]" {
						successfulStream = true
						break
					}

					// 解析 JSON 响应
					var streamResp GeminiResponse
					if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
						resultChan <- StreamChunk{Error: fmt.Errorf("解析响应失败: %w", err)}
						continue
					}

					// 检查是否被阻止
					if streamResp.PromptFeedback.BlockReason != "" {
						resultChan <- StreamChunk{Error: fmt.Errorf("提示词被阻止: %s", streamResp.PromptFeedback.BlockReason)}
						return
					}

					// 检查是否有有效响应
					if len(streamResp.Candidates) > 0 && len(streamResp.Candidates[0].Content.Parts) > 0 {
						chunk := StreamChunk{
							Text:         streamResp.Candidates[0].Content.Parts[0].Text,
							FinishReason: streamResp.Candidates[0].FinishReason,
						}
						resultChan <- chunk

						// 表示已成功获取至少一个响应块
						successfulStream = true

						// 如果有结束原因，表示流结束
						if streamResp.Candidates[0].FinishReason != "" {
							break
						}
					}
				}

				if err := scanner.Err(); err != nil {
					if !successfulStream && attempt < maxRetries-1 {
						logger.Warn("读取流失败, 将重试",
							zap.Error(err),
							zap.Int("attempt", attempt+1),
							zap.Int("max_retries", maxRetries))
						return // 继续重试
					}
					resultChan <- StreamChunk{Error: fmt.Errorf("读取流失败: %w", err)}
					return
				}

				// 如果成功处理了流，跳出重试循环
				if successfulStream {
					attempt = maxRetries // 强制跳出循环
				}
			}()
		}
	}()

	return resultChan, nil
}
