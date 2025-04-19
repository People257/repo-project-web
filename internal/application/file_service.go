package application

import (
	"io"
	"mime/multipart"

	"repo-prompt-web/internal/domain/models"
	"repo-prompt-web/internal/domain/services"
)

// FileService 文件应用服务
type FileService struct {
	fileProcessor *services.FileProcessor
}

// NewFileService 创建文件应用服务实例
func NewFileService(fileProcessor *services.FileProcessor) *FileService {
	return &FileService{
		fileProcessor: fileProcessor,
	}
}

// ProcessZipFile 处理ZIP文件
func (s *FileService) ProcessZipFile(file *multipart.FileHeader, useBase64 bool) (*models.ProcessResult, error) {
	src, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	return s.fileProcessor.ProcessZipFile(src.(io.ReaderAt), file.Size, useBase64)
}

// FormatOutput 格式化输出
func (s *FileService) FormatOutput(result *models.ProcessResult) string {
	return s.fileProcessor.FormatOutput(result)
}
