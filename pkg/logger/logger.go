package logger

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	// logger 是一个全局 logger 实例
	logger *zap.Logger
	once   sync.Once
)

// Init 初始化日志系统
func Init(level string, outputPath string) {
	once.Do(func() {
		// 解析日志级别
		var logLevel zapcore.Level
		switch strings.ToLower(level) {
		case "debug":
			logLevel = zapcore.DebugLevel
		case "info":
			logLevel = zapcore.InfoLevel
		case "warn":
			logLevel = zapcore.WarnLevel
		case "error":
			logLevel = zapcore.ErrorLevel
		default:
			logLevel = zapcore.InfoLevel
		}

		// 创建日志目录
		if outputPath != "" {
			if err := os.MkdirAll(outputPath, 0755); err != nil {
				panic("无法创建日志目录: " + err.Error())
			}
		}

		// 配置日志编码器
		encoderConfig := zap.NewProductionEncoderConfig()
		encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
		encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder

		// 配置输出
		var cores []zapcore.Core

		// 控制台日志输出
		consoleEncoder := zapcore.NewConsoleEncoder(encoderConfig)
		consoleCore := zapcore.NewCore(
			consoleEncoder,
			zapcore.AddSync(os.Stdout),
			logLevel,
		)
		cores = append(cores, consoleCore)

		// 文件日志输出
		if outputPath != "" {
			// 常规日志文件
			logFilePath := filepath.Join(outputPath, "app.log")
			logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err == nil {
				fileEncoder := zapcore.NewJSONEncoder(encoderConfig)
				fileCore := zapcore.NewCore(
					fileEncoder,
					zapcore.AddSync(logFile),
					logLevel,
				)
				cores = append(cores, fileCore)
			}

			// 错误日志文件
			errorFilePath := filepath.Join(outputPath, "error.log")
			errorFile, err := os.OpenFile(errorFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err == nil {
				fileEncoder := zapcore.NewJSONEncoder(encoderConfig)
				errorCore := zapcore.NewCore(
					fileEncoder,
					zapcore.AddSync(errorFile),
					zapcore.ErrorLevel, // 错误文件只记录错误及以上级别
				)
				cores = append(cores, errorCore)
			}
		}

		// 创建 logger
		core := zapcore.NewTee(cores...)
		logger = zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1), zap.AddStacktrace(zapcore.ErrorLevel))
	})
}

// Debug 记录调试信息
func Debug(msg string, fields ...zap.Field) {
	if logger != nil {
		logger.Debug(msg, fields...)
	}
}

// Info 记录一般信息
func Info(msg string, fields ...zap.Field) {
	if logger != nil {
		logger.Info(msg, fields...)
	}
}

// Warn 记录警告信息
func Warn(msg string, fields ...zap.Field) {
	if logger != nil {
		logger.Warn(msg, fields...)
	}
}

// Error 记录错误信息
func Error(msg string, fields ...zap.Field) {
	if logger != nil {
		logger.Error(msg, fields...)
	}
}

// Fatal 记录致命错误并退出程序
func Fatal(msg string, fields ...zap.Field) {
	if logger != nil {
		logger.Fatal(msg, fields...)
	}
}

// WithFields 返回带有字段的日志接口
func WithFields(fields ...zap.Field) *zap.Logger {
	if logger != nil {
		return logger.With(fields...)
	}
	return nil
}

// Sync 刷新日志缓冲
func Sync() {
	if logger != nil {
		_ = logger.Sync()
	}
}

// Now 返回当前时间，用于计算请求延迟时间
func Now() time.Time {
	return time.Now()
}

// Since 计算从指定时间到现在的持续时间
func Since(t time.Time) time.Duration {
	return time.Since(t)
}
