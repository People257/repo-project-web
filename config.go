package main

import (
	"path/filepath"
	"strings"
)

const (
	maxUploadSize      int64  = 100 * 1024 * 1024 // 50MB
	maxFileSize        int64  = 100 * 1024 * 1024 // 2MB
	outputFilename     string = "combined_code.txt"
	fileReadBufferSize        = 40960
)

var excludedDirPrefixes = []string{
	".git/",
	"node_modules/",
	"vendor/",
	"bin/",
	"obj/",
	"dist/",
	"build/",
}

var excludedExtensions = map[string]struct{}{
	".exe":   {},
	".dll":   {},
	".so":    {},
	".dylib": {},
	".bin":   {},
	".zip":   {},
	".tar":   {},
	".gz":    {},
	".rar":   {},
	".jpg":   {},
	".jpeg":  {},
	".png":   {},
	".gif":   {},
	".bmp":   {},
	".ico":   {},
	".pdf":   {},
	".doc":   {},
	".docx":  {},
	".xls":   {},
	".xlsx":  {},
}

var likelyTextExtensions = map[string]struct{}{
	".go":    {},
	".py":    {},
	".js":    {},
	".ts":    {},
	".jsx":   {},
	".tsx":   {},
	".html":  {},
	".css":   {},
	".scss":  {},
	".less":  {},
	".json":  {},
	".xml":   {},
	".yaml":  {},
	".yml":   {},
	".md":    {},
	".txt":   {},
	".ini":   {},
	".conf":  {},
	".cfg":   {},
	".sh":    {},
	".bash":  {},
	".sql":   {},
	".proto": {},
}

func isExcluded(filePath string, fileSize uint64) bool {
	if fileSize > uint64(maxFileSize) {
		return true
	}

	// 规范化路径
	normalizedPath := filepath.ToSlash(filePath)

	// 检查目录前缀
	for _, prefix := range excludedDirPrefixes {
		if strings.HasPrefix(normalizedPath, prefix) {
			return true
		}
	}

	// 检查扩展名
	ext := strings.ToLower(filepath.Ext(normalizedPath))
	_, excluded := excludedExtensions[ext]
	return excluded
}

func isLikelyTextFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	if _, ok := likelyTextExtensions[ext]; ok {
		return true
	}

	// 处理无扩展名的常见文本文件
	baseName := filepath.Base(filePath)
	switch strings.ToUpper(baseName) {
	case "README", "LICENSE", "CHANGELOG", "CONTRIBUTING", "AUTHORS", "MAINTAINERS", "VERSION":
		return true
	}
	return false
}

func isTextContentTypeException(contentType string) bool {
	// 一些 MIME 类型虽然不是以 text/ 开头，但应该被视为文本文件
	textMimeTypes := map[string]struct{}{
		"application/json":         {},
		"application/xml":          {},
		"application/javascript":   {},
		"application/x-javascript": {},
		"application/ecmascript":   {},
		"application/x-httpd-php":  {},
	}
	_, isException := textMimeTypes[contentType]
	return isException
}
