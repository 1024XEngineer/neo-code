# NeoCode
NeoCode 是一个基于 Go 和 Bubble Tea 构建的本地 Coding Agent MVP。它把 Provider 防腐层、工具调用闭环、本地会话持久化和沉浸式瀑布流 TUI 组合在一起，用于日常代码理解、修改和终端内协作。

## 功能特性
- 基于防腐层的 Provider 架构，当前支持 OpenAI 兼容流式接口，并为 Anthropic、Gemini 预留了扩展位置。
- ReAct 风格的 runtime 主循环，能够读取模型输出、执行工具调用、回灌结果并继续推理。
- 瀑布流终端界面，支持流式 transcript、内联工具事件、Slash Command 和会话侧边栏。
- 本地 YAML 配置、`.env` 加载与并发安全的热更新能力。
- 本地 JSON 会话持久化，不依赖远端服务即可恢复历史上下文。
- 文件系统高级工具：读取、写入、搜索、Glob 探测和精准替换。

## 环境要求
- Go 1.21 及以上
- 至少一个可用的模型 Provider API Key，例如 `OPENAI_API_KEY`

## 快速开始
1. 克隆仓库。
2. 准备 Provider 的 API Key，可以通过环境变量或 NeoCode 托管的 `.env` 文件提供。
3. 运行应用：

```bash
go run ./cmd/neocode
```

首次启动时，NeoCode 会在当前用户主目录下创建自己的托管目录、默认配置和会话存储结构。

## Slash Command
- `/set url <url>`：更新当前选中 Provider 的 Base URL。
- `/set key <key>`：将当前 Provider 的密钥写入托管 `.env` 并立即重新加载。
- `/model`：打开交互式模型选择列表。

## 开发与验证
在提交 Pull Request 前建议至少执行以下命令：

```bash
gofmt -w ./cmd ./internal
go test ./...
```

## 架构概览
- `internal/config`：负责 YAML 加载、`.env` 集成、默认值管理和并发安全更新。
- `internal/provider`：将厂商特定的请求和流式响应抹平成统一领域模型。
- `internal/runtime`：负责事件总线、ReAct loop、Provider 动态构建和会话持久化。
- `internal/tools`：提供工具注册表以及各类具体工具实现。
- `internal/tui`：负责终端交互体验，以及 runtime 事件到 Bubble Tea 消息的桥接。

## 目录结构
```text
.
|-- cmd/neocode
|-- docs
|-- internal/app
|-- internal/config
|-- internal/provider
|-- internal/runtime
|-- internal/tools
`-- internal/tui
```

## 当前状态
NeoCode 目前聚焦于 MVP 闭环：本地对话、工具调用、Session 持久化和终端交互体验。当前版本正在继续向“高质量开源项目”标准收敛，重点补强文档、测试覆盖率和工具能力。
