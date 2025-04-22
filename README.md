# project-prompt
# Repo Prompt Web

一个基于 Go 的 Web 服务，用于处理代码仓库的智能提示词生成和代码处理。

## 功能特点

- **ZIP 文件处理**：上传 ZIP 文件，提取文本内容并合并
- **GitHub 仓库处理**：输入 GitHub 仓库 URL，获取代码内容和结构
- **智能提示词生成**：基于项目结构和文档内容，使用 DeepSeek API 生成智能提示词
- **项目架构分析**：自动分析项目结构，为大型语言模型提供更好的上下文理解
- **预处理支持**：为 Gemini 等大型语言模型提供定制化的项目上下文
- **AI 代码问答**：使用 Gemini API 支持基于上传代码的智能对话和问答
- **代理支持**：支持为 Gemini API 配置代理，解决网络访问问题

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
- Gemini API 密钥（用于代码问答功能）
- GitHub API 密钥（可选，用于访问私有仓库或提高 API 限制）

### 安装

```bash
# 克隆仓库
git clone https://github.com/yourusername/repo-prompt-web.git
cd repo-prompt-web

# 安装依赖
go mod tidy

# 编译（可选）
go build -o repo-prompt-web
```

### 配置

在运行前，复制并编辑 `config.yml` 文件：

```yaml
# 文件大小限制
file_limits:
  max_upload_size: 1000  # MB
  max_file_size: 1000    # MB
  read_buffer_size: 4096

# 输出设置
output:
  filename: "combined_code.txt"

# API 密钥设置
api_keys:
  deepseek: "your_deepseek_api_key"  # 在此处填入 DeepSeek API 密钥
  github: "your_github_api_key"      # 在此处填入 GitHub API 密钥（可选）
  gemini: "your_gemini_api_key"      # 在此处填入 Gemini API 密钥

# Gemini API 集成
gemini:
  enabled: true
  api_endpoint: "https://generativelanguage.googleapis.com/v1/models"
  model: "gemini-pro"
  proxy_url: "http://127.0.0.1:7890"  # 代理服务器地址，如果不需要可留空

# 日志配置
logging:
  level: "info"  # 可选值：debug, info, warn, error
  output_path: "./logs"
```

也可以使用环境变量设置 API 密钥：

```bash
# Linux/macOS
export DEEPSEEK_API_KEY=your_deepseek_api_key
export GITHUB_API_KEY=your_github_api_key
export GEMINI_API_KEY=your_gemini_api_key
export GEMINI_PROXY=http://your-proxy:port  # 可选，覆盖配置文件中的代理设置

# Windows CMD
set DEEPSEEK_API_KEY=your_deepseek_api_key
set GITHUB_API_KEY=your_github_api_key
set GEMINI_API_KEY=your_gemini_api_key
set GEMINI_PROXY=http://your-proxy:port

# Windows PowerShell
$env:DEEPSEEK_API_KEY="your_deepseek_api_key"
$env:GITHUB_API_KEY="your_github_api_key"
$env:GEMINI_API_KEY="your_gemini_api_key"
$env:GEMINI_PROXY="http://your-proxy:port"
```

### 运行

```bash
# 直接运行
go run main.go

# 或使用编译后的二进制文件
./repo-prompt-web
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

响应示例 (JSON 格式):
```json
{
  "success": true,
  "session_id": "bf7c8172-5c37-4d89-a0c7-b8e1dbfb011a",
  "prompt_suggestions": ["项目架构分析内容..."],
  "file_tree": {
    "name": "root",
    "is_dir": true,
    "children": [
      {
        "name": "main.go",
        "is_dir": false,
        "children": null
      },
      {
        "name": "internal",
        "is_dir": true,
        "children": [...]
      }
    ]
  },
  "file_contents": {
    "main.go": {
      "path": "main.go",
      "content": "package main\n\nimport...",
      "is_base64": false
    },
    "internal/app.go": {
      "path": "internal/app.go",
      "content": "package internal\n\n...",
      "is_base64": false
    }
  },
  "generated_at": "2023-04-19T12:34:56Z"
}
```

仅提示词响应示例 (prompt_only=true):
```json
{
  "success": true,
  "session_id": "bf7c8172-5c37-4d89-a0c7-b8e1dbfb011a",
  "prompt_suggestions": ["项目架构分析内容..."],
  "generated_at": "2023-04-19T12:34:56Z"
}
```

### 2. 处理 GitHub 仓库

```
GET /api/github-code?url=<repo_url>
```

查询参数:
- `url`: GitHub 仓库 URL (必需)
- `token` (可选): GitHub 个人访问令牌
- `format` (可选): 输出格式，支持 `text` (默认) 或 `json`
- `base64` (可选): 是否使用 base64 编码输出，默认 `false`
- `generate_prompt` (可选): 是否生成项目架构分析，默认 `false`
- `prompt_only` (可选): 是否只返回提示词而不包含文件内容，默认 `false`
- `include_content` (可选): 是否在提示词响应中包含文件内容，默认 `false`

请求示例:
```
GET /api/github-code?url=https://github.com/username/repo&generate_prompt=true&format=json
```

响应结构与 `/api/combine-code` 相同。

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

响应示例:
```json
{
  "success": true,
  "prompt_suggestions": [
    "这个项目是一个基于Go的Web服务，用于处理代码仓库并生成智能提示词。主要功能包括：\n1. 处理ZIP文件：提取代码文件并合并内容\n2. 处理GitHub仓库：直接从GitHub获取代码\n3. 生成智能提示词：分析项目结构和内容，生成适合大型语言模型的提示词\n4. AI代码问答：集成Gemini API，实现基于代码的智能问答\n\n项目采用领域驱动设计(DDD)架构，分为以下几层：\n- 领域层（domain）：包含核心业务逻辑和模型\n- 应用层（application）：协调领域对象完成用户任务\n- 基础设施层（infrastructure）：提供技术实现\n- 接口层（interfaces）：处理外部接口"
  ],
  "generated_at": "2023-04-19T12:34:56Z"
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

响应示例:
```json
{
  "success": true,
  "prompt_suggestions": [
    "项目架构分析内容..."
  ],
  "directory_structure": "项目目录结构...",
  "generated_at": "2023-04-19T12:34:56Z"
}
```

### 5. 询问关于代码的问题

```
GET/POST /api/ask-code-question
```

查询参数:
- `session_id`: 会话ID（通过上传ZIP文件或获取GitHub仓库后返回的）
- `question`: 想问的关于代码的问题
- `stream` (可选): 是否使用流式响应，支持 `true` 或 `false`(默认)

请求示例:
```
GET /api/ask-code-question?session_id=bf7c8172-5c37-4d89-a0c7-b8e1dbfb011a&question=这个项目的主要功能是什么?
```

响应示例 (stream=false):
```json
{
  "success": true,
  "question": "这个项目的主要功能是什么?",
  "answer": "这个项目是一个基于Go语言的Web服务，主要用于处理代码仓库的智能提示词生成和代码处理。它提供了以下核心功能：\n\n1. ZIP文件处理：用户可以上传ZIP格式的代码压缩包，系统会自动解析并提取其中的文本文件内容。\n\n2. GitHub仓库处理：用户可以提供GitHub仓库URL，系统会自动获取该仓库的内容和结构。\n\n3. 智能提示词生成：利用DeepSeek API基于项目结构和内容生成智能提示词，帮助大型语言模型更好地理解代码上下文。\n\n4. AI代码问答：集成Gemini API，实现基于上传代码的智能对话和问答功能。\n\n这个项目主要面向开发者和AI用户，帮助他们更高效地与大语言模型交流关于代码的问题，并获得更准确、更有上下文感知的回答。"
}
```

如果 `stream=true`，则以Server-Sent Events格式返回响应:
```
event: message
data: 这个项目是一个基于Go语言的Web服务，主要

event: message
data: 用于处理代码仓库的智能提示词生成和代码处理

event: message
data: ...

event: error (仅当出错时)
data: {"error": "错误信息"}
```

## 参数组合使用说明

各个接口的参数可以组合使用，这里是一些常见的组合：

1. **只获取提示词**:
   ```
   ?prompt_only=true&format=json
   ```
   仅返回项目架构分析，适合用于提示词生成场景。
   
   示例: `POST /api/combine-code?prompt_only=true&format=json`

2. **获取提示词和文件树**:
   ```
   ?generate_prompt=true&format=json
   ```
   返回项目架构分析和完整的处理结果。
   
   示例: `GET /api/github-code?url=https://github.com/user/repo&generate_prompt=true&format=json`

3. **提示词 + 选择性内容**:
   ```
   ?prompt_only=true&include_content=true&format=json
   ```
   返回项目架构分析和文件内容，格式化后更方便使用。
   
   示例: `POST /api/combine-code?prompt_only=true&include_content=true&format=json`

4. **流式代码问答**:
   ```
   ?session_id=<id>&question=<问题>&stream=true
   ```
   使用流式返回问题答案，适合实时显示。
   
   示例: `GET /api/ask-code-question?session_id=bf7c8172&question=如何使用这个API?&stream=true`

## 详细配置选项

配置文件 `config.yml` 包含以下选项:

### 文件大小限制
```yaml
file_limits:
  max_upload_size: 1000  # 最大上传大小，单位MB
  max_file_size: 1000    # 单个文件最大大小，单位MB
  read_buffer_size: 4096 # 读取缓冲区大小，单位字节
```

### API密钥设置
```yaml
api_keys:
  deepseek: "your_key"   # DeepSeek API密钥
  github: "your_key"     # GitHub API密钥（可选）
  gemini: "your_key"     # Gemini API密钥
```

### Gemini API设置
```yaml
gemini:
  enabled: true          # 是否启用Gemini集成
  api_endpoint: "https://generativelanguage.googleapis.com/v1/models"
  model: "gemini-pro"    # 使用的模型名称
  proxy_url: "http://127.0.0.1:7890"  # 代理服务器地址（可选）
```

### 日志配置
```yaml
logging:
  level: "info"          # 日志级别: debug, info, warn, error
  output_path: "./logs"  # 日志输出目录
```

### 文件过滤设置
```yaml
# 排除的目录前缀
excluded_dir_prefixes:
  - ".git/"
  - "node_modules/"
  - "vendor/"
  # ...更多排除目录

# 排除的文件扩展名
excluded_extensions:
  - ".exe"
  - ".dll"
  - ".jpg"
  # ...更多排除扩展名

# 支持的文本文件扩展名
text_extensions:
  - ".txt"
  - ".md"
  - ".go"
  - ".py"
  # ...更多文本扩展名
```

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

分析示例:
```
这个项目是一个基于Go语言的Web服务，专注于代码仓库处理和提示词生成。它采用领域驱动设计(DDD)架构，将系统分为领域层、应用层、基础设施层和接口层。

核心功能包括:
1. 代码文件处理：支持上传ZIP文件或直接获取GitHub仓库内容
2. 智能提示词生成：利用DeepSeek API分析项目结构，生成有价值的上下文信息
3. AI代码问答：基于Gemini API，实现针对代码库的智能问答

技术栈:
- 后端: Go (gin框架)
- API集成: DeepSeek API, Gemini API, GitHub API
- 配置: YAML
- 日志: Zap

关键组件:
- FileProcessor: 处理ZIP文件和过滤文件内容
- GitHubClient: 与GitHub API交互获取仓库内容
- AIService: 处理AI相关功能，包括提示词生成和代码问答
- Handlers: HTTP请求处理

数据流:
1. 用户上传代码或提供GitHub URL
2. 系统处理并提取文本内容
3. 可选地生成项目架构分析
4. 用户可以基于上传的代码进行问答
```

这个分析将作为提示词提供给 Gemini 等大型语言模型，帮助它更好地理解项目上下文，回答有关代码的问题。

## 技术实现细节

### 代码解析流程

1. **文件过滤**：根据配置文件中的规则过滤非文本文件和排除的目录
2. **文本检测**：通过扩展名和MIME类型双重检查确定文件是否为文本文件
3. **文件大小限制**：跳过超过配置的大小限制的文件
4. **字符集检测**：尝试确定文本文件的编码并转换为UTF-8

### 会话管理

代码问答功能使用会话ID进行状态管理：
1. 首次上传代码或获取GitHub仓库时会生成会话ID
2. 会话包含完整的代码上下文信息
3. 系统会自动清理2小时内无活动的会话
4. 同一会话中的连续问题会保持对话历史上下文

### 代理支持

Gemini API支持通过以下方式配置代理：
1. 配置文件中的`gemini.proxy_url`设置
2. 环境变量`GEMINI_PROXY`
3. 系统环境变量中的代理设置

优先级顺序为：环境变量 > 配置文件 > 系统代理

## 使用场景

1. **AI代码助手准备**：为大语言模型提供完整的代码上下文
2. **代码库分析**：快速获取项目架构概览
3. **团队协作**：帮助新成员理解现有项目结构
4. **代码问答**：针对特定代码库进行问答交互
5. **API集成**：作为其他系统的代码处理和分析服务

## 贡献指南

欢迎提交问题报告和功能请求。如果您想贡献代码：

1. Fork 这个仓库
2. 创建您的特性分支 (`git checkout -b feature/amazing-feature`)
3. 提交您的更改 (`git commit -m 'Add some amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 创建一个 Pull Request

## 许可证

本项目使用 MIT 许可证 - 查看 LICENSE 文件了解详情。
