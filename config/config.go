package config

import (
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// Config 表示应用程序的配置
type Config struct {
	FileLimits struct {
		MaxUploadSize  int64 `yaml:"max_upload_size"`
		MaxFileSize    int64 `yaml:"max_file_size"`
		ReadBufferSize int   `yaml:"read_buffer_size"`
	} `yaml:"file_limits"`

	Output struct {
		Filename string `yaml:"filename"`
	} `yaml:"output"`

	ExcludedDirPrefixes []string `yaml:"excluded_dir_prefixes"`
	ExcludedExtensions  []string `yaml:"excluded_extensions"`
	TextExtensions      []string `yaml:"text_extensions"`
	TextFilenames       []string `yaml:"text_filenames"`
	TextMimeTypes       []string `yaml:"text_mime_types"`

	// 运行时缓存
	excludedExtMap map[string]struct{}
	textExtMap     map[string]struct{}
	textMimeMap    map[string]struct{}
}

var (
	config *Config
	once   sync.Once
)

// Load 加载配置文件
func Load(configPath string) error {
	var err error
	once.Do(func() {
		config = &Config{}
		err = loadConfig(configPath, config)
		if err != nil {
			return
		}

		// 初始化映射
		config.excludedExtMap = make(map[string]struct{})
		config.textExtMap = make(map[string]struct{})
		config.textMimeMap = make(map[string]struct{})

		// 转换扩展名列表为映射
		for _, ext := range config.ExcludedExtensions {
			config.excludedExtMap[ext] = struct{}{}
		}
		for _, ext := range config.TextExtensions {
			config.textExtMap[ext] = struct{}{}
		}
		for _, mime := range config.TextMimeTypes {
			config.textMimeMap[mime] = struct{}{}
		}

		// 转换大小为字节
		config.FileLimits.MaxUploadSize *= 1024 * 1024 // MB to bytes
		config.FileLimits.MaxFileSize *= 1024 * 1024   // MB to bytes
	})
	return err
}

// Get 返回配置实例
func Get() *Config {
	return config
}

// loadConfig 从文件加载配置
func loadConfig(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, cfg)
}

// IsExcluded 检查文件是否应该被排除
func (c *Config) IsExcluded(filePath string, fileSize uint64) bool {
	if fileSize > uint64(c.FileLimits.MaxFileSize) {
		return true
	}

	// 规范化路径
	normalizedPath := filepath.ToSlash(filePath)

	// 检查目录前缀
	for _, prefix := range c.ExcludedDirPrefixes {
		if filepath.HasPrefix(normalizedPath, prefix) {
			return true
		}
	}

	// 检查扩展名
	ext := filepath.Ext(normalizedPath)
	_, excluded := c.excludedExtMap[ext]
	return excluded
}

// IsLikelyTextFile 检查文件是否可能是文本文件
func (c *Config) IsLikelyTextFile(filePath string) bool {
	ext := filepath.Ext(filePath)
	if _, ok := c.textExtMap[ext]; ok {
		return true
	}

	// 处理无扩展名的常见文本文件
	baseName := filepath.Base(filePath)
	for _, name := range c.TextFilenames {
		if name == baseName {
			return true
		}
	}
	return false
}

// IsTextContentTypeException 检查MIME类型是否为文本类型的例外
func (c *Config) IsTextContentTypeException(contentType string) bool {
	_, isException := c.textMimeMap[contentType]
	return isException
}

// GetMaxUploadSize 返回最大上传大小
func (c *Config) GetMaxUploadSize() int64 {
	return c.FileLimits.MaxUploadSize
}

// GetMaxFileSize 返回最大文件大小
func (c *Config) GetMaxFileSize() int64 {
	return c.FileLimits.MaxFileSize
}

// GetOutputFilename 返回输出文件名
func (c *Config) GetOutputFilename() string {
	return c.Output.Filename
}

// GetReadBufferSize 返回读取缓冲区大小
func (c *Config) GetReadBufferSize() int {
	return c.FileLimits.ReadBufferSize
}
