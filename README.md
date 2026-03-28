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

## 安装指南 (快速开始)

NeoCode 提供了极其原生的系统级安装体验，真正的开箱即用（无需 Go 环境）。请根据你的操作系统选择最适合的安装方式：

### 🍎 macOS / Linux (推荐 Homebrew)
对于习惯使用 Homebrew 的用户，只需一行命令即可接入官方 Tap 并安装：
```bash
brew install 1024XEngineer/homebrew-neocode/neocode
```

### 🪟 Windows (推荐 Scoop)

对于 Windows 极客玩家，我们提供了官方的 Scoop 分发源：

```PowerShell
scoop bucket add neocode https://github.com/1024XEngineer/scoop-bucket.git
scoop install neocode
```

### 🐧 Ubuntu / Debian (.deb)

前往项目的 [Releases 页面](https://www.google.com/search?q=https://github.com/pionxe/neo-code/releases) 下载最新版本的 `.deb` 安装包，然后在终端执行：

```Bash
sudo dpkg -i neo-code_*_linux_amd64.deb
```

### 🚀 兜底方案：一键安装脚本

如果你的系统没有安装上述包管理器，也可以使用我们提供的自动化脚本一键下载并配置：

**Linux / macOS:**

```Bash
curl -sSL https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.sh | bash
```

**Windows (PowerShell):**

```PowerShell
irm https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.ps1 | iex
```

------

### 🛠️ 源码运行（面向开发者）

如果你希望参与二次开发，可以从源码启动：

1. 确保已安装 Go 1.21 及以上环境，然后克隆仓库。
2. 准备 Provider 的 API Key，可以通过环境变量或 NeoCode 托管的 `.env` 文件提供。
3. 运行应用：

```Bash
go run ./cmd/neocode
```

*注：首次启动时（无论哪种安装方式），NeoCode 都会在当前用户主目录下自动创建托管目录、默认配置和会话存储结构。*

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
