package gemini

import (
	"repo-prompt-web/pkg/config"
	"sync"
)

var (
	instance *Client
	once     sync.Once
)

// GetClient 获取Gemini客户端单例实例
func GetClient(cfg *config.Config) *Client {
	// 只初始化一次
	once.Do(func() {
		instance = NewClient(cfg)
	})
	return instance
}
