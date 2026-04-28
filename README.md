[中文](README.md) | [EN](README.en.md)

# <img src="docs/assert/readme/neo-code.svg" alt="neo-code" />

> 一个本地优先的 AI Coding Agent，帮助你理解代码、修改项目、调用工具，并把开发任务接入终端、桌面端和自动化工作流。

<p align="center">
  <a href="https://go.dev/">
    <img src="https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white" alt="Go Version" />
  </a>
  <a href="https://github.com/1024XEngineer/neo-code/actions/workflows/ci.yml">
    <img src="https://img.shields.io/github/actions/workflow/status/1024XEngineer/neo-code/ci.yml?branch=main&label=CI" alt="CI Status" />
  </a>
  <a href="https://github.com/1024XEngineer/neo-code/blob/main/LICENSE">
    <img src="https://img.shields.io/github/license/1024XEngineer/neo-code?color=97CA00" alt="License" />
  </a>
  <a href="https://neocode-docs.pages.dev/">
    <img src="https://img.shields.io/badge/Docs-Official-1677FF?logo=readthedocs&logoColor=white" alt="Docs" />
  </a>
  <a href="https://neocode-docs.pages.dev/guide/install">
    <img src="https://img.shields.io/badge/Platform-Windows%20%7C%20macOS%20%7C%20Linux-4EAA25" alt="Platform" />
  </a>
</p>

<p align="center">
  <a href="https://neocode-docs.pages.dev/">文档</a>
  ·
  <a href="https://github.com/1024XEngineer/neo-code/issues">Issues</a>
  ·
  <a href="https://github.com/1024XEngineer/neo-code/discussions">Discussions</a>
</p>

---

## NeoCode 是什么？

NeoCode 是一个运行在本地开发环境中的 AI Coding Agent。

它可以在工作区中读取项目、理解代码、调用工具、执行命令、管理会话，并通过本地 Gateway 暴露统一的 JSON-RPC / SSE / WebSocket 接口，方便终端、桌面端或第三方客户端接入。

核心闭环：

`用户输入(TUI) -> 网关中继(Gateway) -> Agent推理(Runtime) -> 调用工具(Tools) -> 结果回传 -> UI展示`

---

## 功能特性

- 本地优先：在你的工作区中运行，面向真实项目上下文。
- 终端交互：基于 TUI 的对话式 coding agent 体验。
- 工具调用：支持读取文件、分析项目、执行命令和调用系统工具。
- 多模型 Provider：支持 OpenAI、Gemini、ModelScope、Qiniu、OpenLL 以及自定义 Provider。
- 会话持久化：保存和恢复历史会话，减少重复沟通。
- 记忆能力：保存偏好、项目事实和跨会话上下文。
- Skills 系统：为不同任务启用专用行为和流程。
- MCP 接入：通过 MCP stdio server 扩展外部工具能力。
- Gateway 模式：通过本地 JSON-RPC / SSE / WebSocket 接口连接桌面端、脚本和第三方客户端。

---

## 预览

![NeoCode TUI 对话视图](docs/assert/readme/preview-1.png)
![NeoCode TUI 执行视图](docs/assert/readme/preview-4.png)
![NeoCode Gateway 交互示例](docs/assert/readme/preview-5.png)

---

## 快速开始

### 1. 安装

macOS / Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.sh | bash
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.ps1 | iex
```

### 2. 从源码运行

```bash
git clone https://github.com/1024XEngineer/neo-code.git
cd neo-code
go run ./cmd/neocode
```

### 3. 配置 API Key

按你使用的 Provider 设置对应环境变量，例如：

```bash
export OPENAI_API_KEY="your_key_here"
```

Windows PowerShell:

```powershell
$env:OPENAI_API_KEY = "your_key_here"
```

然后在项目目录中启动：

```bash
neocode --workdir /path/to/your/project
```

### 4. 常用命令

```text
/help                 查看帮助
/provider             切换 Provider
/model                切换模型
/compact              压缩当前会话上下文
/memo                 查看记忆
/remember <text>      保存记忆
/skills               查看可用 skills
/skill use <id>       启用 skill
/skill off <id>       停用 skill
```

### 5. url scheme使用
详细指南链接： [HTTP URL 唤醒使用指南（用户故事版）](https://neocode-docs.pages.dev/guide/http-daemon-wake-user-guide)

```bash
# 启动本地 HTTP daemon（默认 127.0.0.1:18921）
go run ./cmd/neocode daemon serve

# 安装用户态自启动 + best-effort hosts 别名写入（127.0.0.1 neocode）
go run ./cmd/neocode daemon install

# 查看运行与安装状态
go run ./cmd/neocode daemon status

# 卸载自启动配置
go run ./cmd/neocode daemon uninstall
```

可点击链接示例：

```text
http://neocode:18921/review?path=README.md
http://neocode:18921/run?prompt=写一个简单的HTTP服务器
```

> 当前支持动作：
> - `review`：必须携带 `path` 参数。
> - `run`：必须携带 `prompt` 参数，网关会返回 `session_id` 并触发终端接管链路。

会话接管启动方式：

```bash
go run ./cmd/neocode --session <session_id>
```

> 当传入 `--session` 时，TUI 会优先按会话历史中的 `workdir` 进行上下文接管；若该路径在本地失效，会保留当前工作区并显示告警。
>
> Linux（及其他非 Windows/macOS）当前尚未接入自动弹窗终端；`wake.run` 会返回 `not_supported`，可手动执行 `neocode --session <session_id>` 接管。
>
> `daemon serve` 不提供 `--token-file`，默认仅监听 `127.0.0.1`，并限制 Host 白名单为 `neocode` / `localhost` / `127.0.0.1`。
>
> Linux 自启动策略：优先 `systemd --user`，若不可用则回落到 `~/.config/autostart/neocode-daemon.desktop`。
>
> 若未通过安装脚本安装（例如 `go build` / 裸二进制），请手动执行一次 `neocode daemon install`。

---

## Gateway / MCP / Skills

详细说明在文档内：

- [Gateway 集成与协议（Guide）](https://neocode-docs.pages.dev/guide/gateway)
- [MCP 工具接入（Guide）](https://neocode-docs.pages.dev/guide/mcp)
- [Skills 使用（Guide）](https://neocode-docs.pages.dev/guide/skills)
- [工具与权限（Guide）](https://neocode-docs.pages.dev/guide/tools-permissions)
- [Runtime / Provider 事件流（Repo Doc）](docs/runtime-provider-event-flow.md)

---

## 文档

- 官方文档站：[https://neocode-docs.pages.dev/](https://neocode-docs.pages.dev/)
- 快速引导（中文）：[www/guide/index.md](www/guide/index.md)
- [配置指南](https://neocode-docs.pages.dev/guide/configuration)
- [工具与权限](https://neocode-docs.pages.dev/guide/tools-permissions)
- [Skills 使用](https://neocode-docs.pages.dev/guide/skills)
- [MCP 工具接入](https://neocode-docs.pages.dev/guide/mcp)
- [升级与版本检查](https://neocode-docs.pages.dev/guide/update)
- [排障与常见问题](https://neocode-docs.pages.dev/guide/troubleshooting)

文档站源码位于 `www/`，本地预览：

```bash
cd www
pnpm install
pnpm docs:dev
```

---

## 参与贡献

欢迎通过 Issue、Discussion 或 Pull Request 参与 NeoCode。

建议流程：

1. 先在 Issue 中描述问题、需求或设计想法。
2. Fork 仓库并创建功能分支。
3. 保持改动聚焦，说明动机和影响范围。
4. 提交前运行基础检查：

```bash
gofmt -w ./cmd ./internal
go test ./...
go build ./...
```

---

## License

MIT
