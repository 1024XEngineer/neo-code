# NeoCode

<<<<<<< HEAD
> 基于 Go + Bubble Tea 的本地 Coding Agent

NeoCode 是一个基于 Go 和 Bubble Tea 的本地 Coding Agent MVP。

它当前聚焦一条最重要的主链路：

`用户输入 -> Agent 推理 -> 调用工具 -> 获取结果 -> 继续推理 -> UI 展示`

如果你是第一次打开这个仓库，可以先把它理解成一个运行在终端里的本地 AI 编码助手。它会在 TUI 中接收你的问题，调用模型进行推理，在需要时使用本地工具，再把过程和结果实时展示出来。

## 这份 README 适合谁

- 想先把项目跑起来的新同学
- 想快速看懂仓库分层的贡献者
- 想知道配置文件、Provider、Tools 应该放在哪一层的协作者

如果你想直接看详细设计，可以跳到本文末尾的“文档索引”。

## 当前能力

=======
NeoCode 是一个基于 Go 和 Bubble Tea 的本地 Coding Agent MVP。

它当前聚焦一条最重要的主链路：

`用户输入 -> Agent 推理 -> 调用工具 -> 获取结果 -> 继续推理 -> UI 展示`

如果你是第一次打开这个仓库，可以先把它理解成一个运行在终端里的本地 AI 编码助手。它会在 TUI 中接收你的问题，调用模型进行推理，在需要时使用本地工具，再把过程和结果实时展示出来。

## 这份 README 适合谁

- 想先把项目跑起来的新同学
- 想快速看懂仓库分层的贡献者
- 想知道配置文件、Provider、Tools 应该放在哪一层的协作者

如果你想直接看详细设计，可以跳到本文末尾的“文档索引”。

## 当前能力

>>>>>>> 93a2f3f92c3f7bcfefce1a6a73f1373a975b5617
当前仓库已经围绕 MVP 主链路组织，重点在于把边界理顺，而不是一次性做很多功能。

- 终端 TUI 交互界面
- Runtime 驱动的 ReAct 主循环
- Provider 抽象与内建 Provider 配置
- Tool Registry 和统一工具协议
- 本地 Session 持久化
- 流式事件回传到 TUI

当前内建工具：

- `filesystem_read_file`
- `filesystem_write_file`
- `filesystem_grep`
- `filesystem_glob`
- `filesystem_edit`
- `bash`
- `webfetch`

当前内建 Provider 配置：

| Provider | 默认模型 | API Key 环境变量 | 说明 |
| --- | --- | --- | --- |
| `openai` | `gpt-4.1` | `OPENAI_API_KEY` | 默认配置 |
| `gemini` | `gemini-2.5-flash` | `GEMINI_API_KEY` | 走 OpenAI-compatible 接口 |
| `openll` | `gpt-4.1` | `AI_API_KEY` | 自定义 OpenAI-compatible 入口 |
| `qiniuyun` | `deepseek/deepseek-v3.2-251201` | `QINIUYUN_API_KEY` | 七牛云 OpenAI-compatible 入口 |

## 快速开始

### 1. 准备环境

- Go `1.25.0`
- 一个可用的模型 API Key
- 可正常联网的终端环境

### 2. 设置 API Key

NeoCode 不会把明文 API Key 写入 `config.yaml`。你可以直接设置系统环境变量，也可以放到 `.env` 文件中。

常见方式：

```powershell
$env:OPENAI_API_KEY="your-api-key"
```

或在项目根目录 / `~/.neocode/.env` 中写入：

```env
OPENAI_API_KEY=your-api-key
```

可选环境变量名取决于你使用的 Provider：

- `OPENAI_API_KEY`
- `GEMINI_API_KEY`
- `AI_API_KEY`
- `QINIUYUN_API_KEY`

你也可以在进入 TUI 后通过 `/apikey` 打开输入框，自定义当前运行时优先读取的 API Key 环境变量名。留空确认会恢复为当前 Provider 的默认环境变量名。

### 3. 启动应用

```bash
go run ./cmd/neocode
```

首次启动时，程序会自动创建默认配置文件：

`~/.neocode/config.yaml`

### 4. 在界面里开始使用

进入 TUI 后，你可以直接输入需求，例如：

- “帮我阅读当前仓库结构并总结”
- “查看 `internal/runtime` 的主循环逻辑”
- “搜索项目里和 provider 相关的实现”

常用 Slash Command：

- `/provider`：切换当前 Provider
- `/model`：切换当前模型
- `/apikey`：设置全局 API Key 环境变量名覆盖

## 新手先理解这 4 层

如果你只想快速看懂项目，先抓住下面这条链路：

`TUI -> Runtime -> Provider / Tools`

### TUI

`internal/tui`

- 负责界面展示、输入处理、Slash Command 和状态切换
- 只消费 Runtime 事件，不直接执行工具，不直接调用厂商 API

### Runtime

`internal/runtime`

- 是整个 Agent 的编排中心
- 负责维护会话、组织上下文、驱动模型调用、执行工具回灌、控制停止条件

### Provider

`internal/provider`

- 负责抹平不同模型厂商之间的协议差异
- 对 Runtime 暴露统一的 `ChatRequest / ChatResponse / ToolCall / StreamEvent`

### Tools

`internal/tools`

- 负责所有可被模型调用的本地能力
- 包括 schema 定义、参数校验、执行、错误包装和结果收敛

一句话记忆：

不要跨层直连。UI 不碰工具执行，Runtime 不写厂商协议细节，工具能力统一进入 `internal/tools`。

## 配置说明

默认配置文件路径：

`~/.neocode/config.yaml`

当前落盘配置是“最小状态配置”，只保存当前选择和通用运行参数，不保存完整 Provider 元数据。

示例：

```yaml
selected_provider: openai
current_model: gpt-4.1
api_key_env_override: CUSTOM_OPENAI_KEY
workdir: /absolute/path/to/workspace
shell: powershell
max_loops: 8
tool_timeout_sec: 20
tools:
  webfetch:
    max_response_bytes: 262144
    supported_content_types:
      - text/html
      - application/xhtml+xml
      - text/plain
      - application/json
      - application/xml
      - text/xml
```

几个关键点：

- `selected_provider`：当前选中的 Provider
- `current_model`：当前 Provider 下使用的模型
- `workdir`：工具默认工作目录；启动后会被规范化为绝对路径
- `shell`：Windows 默认是 `powershell`，其他系统默认是 `bash`
- `max_loops`：Runtime 最大推理轮数
- `tool_timeout_sec`：工具超时时间

说明：

- 示例里的 `workdir` 使用绝对路径是为了贴近真实落盘结果
- 如果你首次启动时还没有手动配置，程序会根据当前工作目录生成默认值

Provider 的 `base_url`、默认模型列表和 `api_key_env` 由代码内建定义提供，不需要你在 YAML 中手动维护；如果你想全局覆盖当前运行时读取的环境变量名，可以设置顶层的 `api_key_env_override`，留空则回退到当前 Provider 的默认值。

当前代码内建的 Provider 包括 `openai`、`gemini`、`openll` 和 `qiniuyun`。其中 `qiniuyun` 预置的模型目录为 `z-ai/glm-5`、`minimax/minimax-m2.5`、`moonshotai/kimi-k2.5` 与 `deepseek/deepseek-v3.2-251201`。

## 工具与安全边界

NeoCode 当前默认遵守这些约束：

- `filesystem` 工具限制在工作目录内
- `bash` 工具有超时控制，避免长时间阻塞
- `webfetch` 限制响应大小和内容类型
- API Key 通过环境变量读取，不写入聊天记录和配置文件

如果你要新增一个可被模型调用的能力，优先放进 `internal/tools`，不要直接写到 `runtime` 或 `tui` 里。

## 仓库结构

```text
.
├── cmd/neocode                # CLI 入口
├── internal/app               # 应用装配与依赖注入
├── internal/config            # 配置加载、保存、校验、并发安全访问
├── internal/provider          # Provider 抽象、驱动注册、厂商适配
├── internal/runtime           # ReAct 主循环、事件、Session 管理
├── internal/tools             # 工具协议、注册表、具体工具实现
├── internal/tui               # Bubble Tea 界面与交互状态机
└── docs                       # 架构与细节设计文档
```

## 开发命令

启动应用：

```bash
go run ./cmd/neocode
```

编译全部包：

```bash
go build ./...
```

运行测试：

```bash
go test ./...
```

格式化代码：

```bash
gofmt -w ./cmd ./internal
```

## 贡献时建议先看

这个仓库最重要的协作原则是：

- 优先保证主链路可运行
- 优先保持模块边界清晰
- 新能力默认沿 `TUI -> Runtime -> Provider / Tool Manager` 接入
- 修改 `config`、`provider`、`runtime`、`tools` 时要同步评估测试

在开始改代码前，建议先阅读仓库根目录的 `AGENTS.md`。

## 文档索引

<<<<<<< HEAD
- `docs/guides/configuration.md`
- `docs/guides/adding-providers.md`
=======
>>>>>>> 93a2f3f92c3f7bcfefce1a6a73f1373a975b5617
- `docs/neocode-coding-agent-mvp-architecture.md`
- `docs/runtime-provider-event-flow.md`
- `docs/config-management-detail-design.md`
- `docs/provider-schema-strategy.md`
- `docs/tools-and-tui-integration.md`
- `docs/session-persistence-design.md`

## 一句话总结

NeoCode 当前不是一个“功能很多”的 Agent，而是一个把主链路、分层和可验证性打磨清楚的 Coding Agent MVP。对新同学来说，最好的阅读顺序是：先跑起来，再看 `README` 的分层说明，最后按需进入 `docs/` 深入细节。
<<<<<<< HEAD

## License

MIT
=======
>>>>>>> 93a2f3f92c3f7bcfefce1a6a73f1373a975b5617
