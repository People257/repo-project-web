package main

import (
	"log"
	"path/filepath"

	"repo-prompt-web/internal/app/service"
	"repo-prompt-web/internal/application"
	"repo-prompt-web/internal/domain/services"
	"repo-prompt-web/internal/infrastructure/github"
	"repo-prompt-web/internal/interfaces/http/handlers"
	"repo-prompt-web/pkg/config"
	"repo-prompt-web/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// RequestIDMiddleware 为每个请求生成唯一ID
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := uuid.New().String()
		c.Set("RequestID", requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

// LoggerMiddleware 记录HTTP请求日志
func LoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 请求前
		start := logger.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery
		requestID := c.GetString("RequestID")

		logger.Info("收到HTTP请求",
			zap.String("request_id", requestID),
			zap.String("client_ip", c.ClientIP()),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("query", query),
			zap.String("user_agent", c.Request.UserAgent()))

		// 处理请求
		c.Next()

		// 请求后
		latency := logger.Since(start)
		status := c.Writer.Status()

		logFunc := logger.Info
		if status >= 400 {
			logFunc = logger.Warn
		}
		if status >= 500 {
			logFunc = logger.Error
		}

		logFunc("完成HTTP请求",
			zap.String("request_id", requestID),
			zap.Int("status", status),
			zap.Duration("latency", latency),
			zap.Int("bytes", c.Writer.Size()),
			zap.String("errors", c.Errors.ByType(gin.ErrorTypePrivate).String()))
	}
}

// CORSMiddleware 添加CORS支持
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

func main() {
	// 加载配置文件
	configPath := filepath.Join(".", "config.yml")
	if err := config.Load(configPath); err != nil {
		log.Fatalf("加载配置文件失败: %v", err)
	}

	// 初始化日志
	cfg := config.Get()
	logger.Init(cfg.GetLogLevel(), cfg.GetLogOutputPath())
	defer logger.Sync()

	logger.Info("服务启动", zap.String("config_path", configPath))

	// 获取环境变量中的 DeepSeek API 密钥
	deepseekAPIKey := cfg.GetDeepseekAPIKey()

	// 创建依赖
	fileProcessor := services.NewFileProcessor(cfg)
	fileService := application.NewFileService(fileProcessor)
	githubClient := github.NewClient(cfg)
	aiService := service.NewAIService(cfg)

	// 创建提示词服务和处理器
	promptService := application.NewPromptService(deepseekAPIKey)
	promptHandler := handlers.NewPromptHandler(promptService, fileService, cfg)

	// 创建文件处理器
	fileHandler := handlers.NewFileHandler(fileService, promptService, githubClient, aiService, cfg)

	// 创建 Gin 引擎
	router := gin.Default()

	// 添加中间件
	router.Use(CORSMiddleware())
	router.Use(RequestIDMiddleware())
	router.Use(LoggerMiddleware())

	// 设置上传限制
	router.MaxMultipartMemory = cfg.GetMaxUploadSize()

	// 注册文件处理路由
	router.POST("/api/combine-code", fileHandler.HandleCombineCode)
	router.GET("/api/github-code", fileHandler.HandleGitHubRepo)

	// 注册提示词生成路由
	router.POST("/api/generate-prompt", promptHandler.HandleGeneratePrompt)
	router.POST("/api/preprocess-zip", promptHandler.HandlePreProcess)

	// 注册代码问答路由
	router.POST("/api/ask-code-question", fileHandler.HandleAskCodeQuestion)
	router.GET("/api/ask-code-question", fileHandler.HandleAskCodeQuestion)

	// 定义监听地址
	listenAddr := ":8080"

	// 提示如何设置 API 密钥
	if deepseekAPIKey == "" {
		logger.Warn("未设置 DeepSeek API 密钥，提示词生成功能将无法使用")
		logger.Info("请在 config.yml 文件中配置 api_keys.deepseek 或设置环境变量 DEEPSEEK_API_KEY")
	} else {
		logger.Info("已配置 DeepSeek API 密钥，提示词生成功能可用")
	}

	// 检查Gemini API密钥
	if cfg.GetGeminiAPIKey() == "" {
		logger.Warn("未设置 Gemini API 密钥，代码问答功能将无法使用")
		logger.Info("请在 config.yml 文件中配置 api_keys.gemini 或设置环境变量 GEMINI_API_KEY")
	} else {
		logger.Info("已配置 Gemini API 密钥，代码问答功能可用")
	}

	// 打印使用方法
	logger.Info("启动服务", zap.String("listen_addr", listenAddr))
	logger.Info("API使用方法",
		zap.String("combine_code", "POST http://localhost"+listenAddr+"/api/combine-code"),
		zap.String("github_code", "GET http://localhost"+listenAddr+"/api/github-code?url=<repo_url>"),
		zap.String("generate_prompt", "POST http://localhost"+listenAddr+"/api/generate-prompt"),
		zap.String("preprocess_zip", "POST http://localhost"+listenAddr+"/api/preprocess-zip"),
		zap.String("ask_code_question", "GET/POST http://localhost"+listenAddr+"/api/ask-code-question?session_id=<id>&question=<question>&stream=true|false"))

	if err := router.Run(listenAddr); err != nil {
		logger.Fatal("启动 Gin 服务失败", zap.Error(err))
	}
}

//TIP See GoLand help at <a href="https://www.jetbrains.com/help/go/">jetbrains.com/help/go/</a>.
// Also, you can try interactive lessons for GoLand by selecting 'Help | Learn IDE Features' from the main menu.
