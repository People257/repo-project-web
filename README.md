# project-prompt
# Repo Prompt Web

一个基于 Go 的 Web 服务，用于处理代码仓库的智能提示词生成和代码处理。

## 功能特点

- **ZIP 文件处理**：上传 ZIP 文件，提取文本内容并合并
- **GitHub 仓库处理**：输入 GitHub 仓库 URL，获取代码内容和结构
- **智能提示词生成**：基于项目结构和文档内容，使用 DeepSeek API 生成智能提示词
- **项目架构分析**：自动分析项目结构，为大型语言模型提供更好的上下文理解
- **预处理支持**：为 Gemini 等大型语言模型提供定制化的项目上下文

## 项目结构

项目采用领域驱动设计 (DDD) 架构:

```
repo-prompt-web/
├── config.yml                 # 配置文件
├── go.mod/go.sum              # Go 依赖管理
├── main.go                    # 程序入口
├── internal/                  # 内部代码
│   ├── domain/                # 领域层
│   │   ├── models/            # 领域模型
│   │   └── services/          # 领域服务
│   ├── application/           # 应用层
│   ├── infrastructure/        # 基础设施层
│   │   └── github/            # GitHub 集成
│   └── interfaces/            # 接口层
│       └── http/              # HTTP 接口
│           └── handlers/      # HTTP 处理器
└── pkg/                       # 公共包
    ├── config/                # 配置管理
    └── types/                 # 通用类型
```

## 安装和使用

### 前置条件

- Go 1.21 或更高版本
- DeepSeek API 密钥（用于智能提示词生成）

### 安装

```bash
# 克隆仓库
git clone https://github.com/yourusername/repo-prompt-web.git
cd repo-prompt-web

# 安装依赖
go mod tidy
```

### 配置 API 密钥

在 `config.yml` 文件中配置 DeepSeek API 密钥:

```yaml
# API 密钥设置
api_keys:
  deepseek: "your_deepseek_api_key"  # 在此处填入你的 DeepSeek API 密钥
  github: "your_github_api_key"      # 在此处填入你的 GitHub API 密钥（可选）
```

或者使用环境变量:

```bash
# Linux/macOS
export DEEPSEEK_API_KEY=your_deepseek_api_key

# Windows CMD
set DEEPSEEK_API_KEY=your_deepseek_api_key

# Windows PowerShell
$env:DEEPSEEK_API_KEY="your_deepseek_api_key"
```

### 运行

```bash
# 启动服务
go run .
```

服务将在 http://localhost:8080 运行。

## API 接口

### 1. 处理 ZIP 文件

```
POST /api/combine-code
```

表单参数:
- `codeZip`: ZIP 文件

查询参数:
- `format` (可选): 输出格式，支持 `text` (默认) 或 `json`
- `base64` (可选): 是否使用 base64 编码输出，默认 `false`
- `generate_prompt` (可选): 是否生成项目架构分析，默认 `false`
- `prompt_only` (可选): 是否只返回提示词而不包含文件内容，默认 `false`
- `include_content` (可选): 是否在提示词响应中包含文件内容，默认 `false`

响应示例 (prompt_only=true):
```json
{
  "success": true,
  "prompt_suggestions": ["项目架构分析内容..."],
  "generated_at": "2023-04-19T12:34:56Z"
}
```

### 2. 处理 GitHub 仓库

```
GET /api/github-code?url=<repo_url>
```

查询参数:
- `url`: GitHub 仓库 URL
- `token` (可选): GitHub 个人访问令牌
- `format` (可选): 输出格式，支持 `text` (默认) 或 `json`
- `base64` (可选): 是否使用 base64 编码输出，默认 `false`
- `generate_prompt` (可选): 是否生成项目架构分析，默认 `false`
- `prompt_only` (可选): 是否只返回提示词而不包含文件内容，默认 `false`
- `include_content` (可选): 是否在提示词响应中包含文件内容，默认 `false`

响应示例 (prompt_only=true):
```json
{
  "success": true,
  "prompt_suggestions": ["项目架构分析内容..."],
  "generated_at": "2023-04-19T12:34:56Z"
}
```

### 3. 生成智能提示词

```
POST /api/generate-prompt
```

JSON 参数:
```json
{
  "projectPath": "/path/to/project",
  "apiKey": "your_deepseek_api_key"
}
```

### 4. 上传 ZIP 文件直接生成提示词

```
POST /api/preprocess-zip
```

表单参数:
- `codeZip`: ZIP 文件
- `apiKey` (可选): DeepSeek API 密钥，如果未提供则使用配置文件中的密钥

查询参数:
- `format` (可选): 输出格式，支持 `json` (默认) 或 `text`
- `include_content` (可选): 是否在响应中包含文件内容，默认 `false`

响应 (JSON 格式):
```json
{
  "success": true,
  "prompt_suggestions": ["项目架构分析内容..."],
  "generated_at": "2023-04-19T12:34:56Z"
}
```

如果 `include_content=true`，还会包含:
```json
{
  "directory_structure": "项目目录结构...",
  "file_tree": {...},
  "file_contents": {...}
}
```

## 参数组合使用说明

各个接口的参数可以组合使用，这里是一些常见的组合：

1. **只获取提示词**:
   ```
   ?prompt_only=true&format=json
   ```
   仅返回项目架构分析，适合用于提示词生成场景。

2. **获取提示词和文件树**:
   ```
   ?generate_prompt=true&format=json
   ```
   返回项目架构分析和完整的处理结果。

3. **提示词 + 选择性内容**:
   ```
   ?generate_prompt=true&include_content=true&format=json
   ```
   返回项目架构分析和文件内容，格式化后更方便使用。

## 配置选项

配置文件 `config.yml` 包含以下选项:

- 文件大小限制
- 输出设置
- API 密钥设置
- 排除的目录和文件类型
- 支持的文本文件类型

## 项目架构分析功能

使用 `generate_prompt=true` 或 `prompt_only=true` 参数可以生成项目架构分析。这个分析由 DeepSeek API 生成，作为架构师视角对项目进行全面分析，包括:

- 项目的主要目的和核心功能
- 使用的架构模式及其实现方式
- 关键组件及其职责
- 业务领域模型和主要概念
- 核心业务流程和交互方式
- 技术栈和重要依赖
- 数据流和接口设计
- 项目的特点和设计考量

这个分析将作为提示词提供给 Gemini 等大型语言模型，帮助它更好地理解项目上下文。
