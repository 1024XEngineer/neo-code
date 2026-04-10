# NeoCode

> 基于 Go + Bubble Tea 的本地 Coding Agent

NeoCode 是一个在终端中运行的 AI 编码助手，采用 ReAct（Reason-Act-Observe）循环模式，能够自主推理、调用工具并完成任务。

## 核心特性

- **流式输出** — 实时展示模型思考过程
- **工具系统** — 文件操作、代码执行、搜索等内置工具
- **内建 Provider 支持** — OpenAI、Gemini、OpenLL、Qiniu，模型列表支持动态发现与缓存
- **终端原生体验** — 基于 Bubble Tea 的现代化 TUI

## 一键安装

NeoCode 提供了跨平台的一键安装脚本。无论你是哪种操作系统，只需在终端执行以下命令，脚本将自动探测系统架构、拉取最新 Release 产物并配置好环境变量：

### 🍎 macOS / Linux
打开终端（Terminal）并运行：
```bash
curl -fsSL https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.sh | bash
```

### Windows

打开 PowerShell 并运行：



```PowerShell
irm https://raw.githubusercontent.com/pionxe/neo-code/main/scripts/install.ps1 | iex
```



## 快速开始

### 环境要求

- Go 1.21+
- API Key（OpenAI 或 Google Gemini）

### 安装与运行

```bash
# 克隆仓库
git clone https://github.com/yourusername/neocode.git
cd neocode

# 设置 API Key
export OPENAI_API_KEY=your_key_here

# 运行
go run ./cmd/neocode
```

### 基本使用

在 TUI 中输入自然语言指令，例如：

```
帮我看看当前项目的目录结构
创建一个 HTTP 服务器，监听 8080 端口
分析 runtime.go 的主要逻辑
```

使用 slash 命令快速切换配置：

- `/provider` — 切换内建模型提供商
- `/model` — 切换模型

## 架构概览

```
┌─────────────────────────────────────────┐
│              TUI (Bubble Tea)           │
└────────────────┬────────────────────────┘
                 │ Events
┌────────────────▼────────────────────────┐
│          Runtime (ReAct Loop)           │
└────────┬───────────────────┬────────────┘
         │                   │
    ┌────▼─────┐        ┌────▼──────┐
    │ Provider │        │   Tools   │
    │  (LLM)   │        │ Registry  │
    └──────────┘        └───────────┘
```

核心模块职责：

- **`internal/config`** — 配置管理、环境变量、YAML 加载
- **`internal/context`** — system prompt、消息裁剪与上下文构建
- **`internal/provider`** — Provider 契约、驱动注册与通用领域类型
- **`internal/provider/openaicompat`** — OpenAI-compatible 协议入口、discovery 与 `api_style` 分流
- **`internal/provider/openaicompat/chatcompletions`** — `/chat/completions` 请求组装、SSE 解析与 tool-call 增量处理
- **`internal/provider/catalog`** — 模型发现、catalog 缓存与后台刷新
- **`internal/provider/builtin`** — 内建 driver 注册
- **`internal/runtime`** — ReAct 主循环与事件流编排（不直接承载会话存储实现；不再导出会话模型与存储类型）
- **`internal/session`** — 会话模型、会话存储抽象与 JSON 持久化实现（统一对外暴露 `Session` / `Summary` / `Store`）
- **`internal/tools`** — 工具注册表与具体工具实现
- **`internal/tui`** — 终端 UI、交互体验、事件桥接
- **`internal/app`** — 应用装配与依赖注入

## 目录结构

```text
.
├── cmd/neocode          # CLI 入口
├── docs                 # 架构与设计文档
│   ├── guides           # 使用指南
│   └── *.md             # 设计文档
├── internal
│   ├── app              # 应用装配
│   ├── config           # 配置管理
│   ├── context          # 上下文构建
│   ├── provider         # Provider 契约与驱动注册
│   │   ├── builtin      # 内建 driver 注册
│   │   ├── catalog      # 模型发现与缓存
│   │   └── openaicompat # OpenAI-compatible 协议入口与子协议实现
│   ├── runtime          # ReAct 循环与事件流
│   ├── session          # 会话模型与持久化
│   ├── tools            # 工具系统
│   └── tui              # 终端 UI
└── README.md
```

## 文档

- **[配置指南](docs/guides/configuration.md)** — Provider 策略、配置文件、环境变量
- **[扩展 Provider](docs/guides/adding-providers.md)** — 如何添加新的模型提供商
- **[架构设计](docs/neocode-coding-agent-mvp-architecture.md)** — 整体架构与设计理念
- **[事件流](docs/runtime-provider-event-flow.md)** — Runtime 与 Provider 的事件交互
- **[Session 持久化](docs/session-persistence-design.md)** — 会话 JSON 存储、token 累计与工作区隔离

## 开发

```bash
# 格式化代码
gofmt -w ./cmd ./internal

# 运行测试
go test ./...

# 编译
go build ./...
```

## 当前状态

NeoCode 正处于 MVP 阶段，核心闭环已可用：

✅ 用户输入 → Agent 推理 → 工具调用 → 结果返回 → UI 展示

正在持续迭代中，重点关注：

- 📚 文档完善
- 🧪 测试覆盖率
- 🛠️ 工具能力扩展
- 🔧 稳定性与性能



## 自动化发版指南

NeoCode 已经集成了 GoReleaser 与 GitHub Actions 的全自动化 CI/CD 流水线。

**作为项目维护者，发布新版本时绝对不需要在本地手动编译或打包二进制文件。** 只需要通过 Git 打一个语义化版本标签（Tag）即可触发全自动构建：

1. **确保主分支代码已就绪**：所有新特性和 Bug 修复均已合并至 `main` 分支。

2. **在本地打上版本标签**（版本号必须以 `v` 开头，如 `v0.1.0`）：

   ```Bash
   git tag v0.1.0
   ```

3. **将标签推送到远程仓库**：

   ```Bash
   git push origin v0.1.0
   ```

**发布流水线说明：** 推送到远程后，GitHub Actions 会自动接管，整个过程通常耗时 1~2 分钟：

- 自动读取 `.goreleaser.yaml` 配置。
- 执行跨平台（Windows/macOS/Linux）与多架构（amd64/arm64）的静态交叉编译。
- 自动将编译产物打包压缩（`.tar.gz` 和 `.zip`），并计算 SHA256 校验和。
- 自动在项目的 Releases 页面创建一个全新的发版记录，并将所有压缩包作为资产（Assets）挂载上去。

## License

MIT

## Manual Compact

NeoCode 支持通过 `/compact` 手动压缩当前会话上下文。配置项见 `docs/guides/configuration.md`，流程和摘要约定见 `docs/context-compact.md`。

## CLI Workdir

NeoCode 现在支持通过 CLI 启动参数覆盖本次运行工作区：

```bash
go run ./cmd/neocode --workdir /path/to/workspace
```

说明：

- `--workdir` 只影响当前进程，不会写回 `config.yaml`
- 当前工作区会同时用于工具执行根目录与 session 存储分桶
- session 历史现在按工作区隔离存储，不同工作区默认互不可见

[![Contributors](https://hub-io-mcells-projects.vercel.app/r/1024XEngineer/neo-code)](https://github.com/1024XEngineer/neo-code/graphs/contributors)
