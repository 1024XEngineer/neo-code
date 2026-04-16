# NeoCode

> 基于 Go + Bubble Tea 的本地 Coding Agent

## NeoCode 是什么

NeoCode 是一个在终端中运行的 AI 编码助手，采用 ReAct（Reason-Act-Observe）循环模式，围绕以下主链路工作：

`用户输入 -> Agent 推理 -> 调用工具 -> 获取结果 -> 继续推理 -> UI 展示`

它适合希望在本地工作流中完成代码理解、修改、调试与自动化操作的开发者。

## 有什么能力

- 终端原生 TUI 交互体验（Bubble Tea）
- Agent 可调用内置工具完成文件与命令相关任务
- 支持 Provider/Model 切换（内建 `openai`、`gemini`、`openll`、`qiniu`）
- 支持上下文压缩（`/compact`），帮助长会话保持可用
- 支持工作区隔离（`--workdir`、`/cwd`）
- 会话持久化与恢复，降低重复沟通成本
- 支持持久记忆查看、显式写入与后台自动提取，保留跨会话偏好与项目事实

## 怎么用（快速开始）

### 1) 环境要求

- Go `1.25+`
- 可用的 API Key（如 OpenAI、Gemini、OpenLL、Qiniu）

### 2) 一键安装

macOS / Linux：

```bash
curl -fsSL https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.sh | bash
```

Windows PowerShell：

```powershell
irm https://raw.githubusercontent.com/1024XEngineer/neo-code/main/scripts/install.ps1 | iex
```

### 3) 从源码运行

```bash
git clone https://github.com/1024XEngineer/neo-code.git
cd neo-code
go run ./cmd/neocode
```

Gateway 子命令（Step 1 骨架）：

```bash
go run ./cmd/neocode gateway
```

指定网络访问面监听地址（默认 `127.0.0.1:8080`，仅允许 Loopback）：

```bash
go run ./cmd/neocode gateway --http-listen 127.0.0.1:8080
```

网络访问面骨架端点（EPIC-GW-04）：

- `POST /rpc`：单次 JSON-RPC 请求入口
- `GET /ws`：WebSocket 流式入口（含心跳）
- `GET /sse`：SSE 流式入口（MVP 默认触发 `gateway.ping`，含心跳）

安全限制：为防止跨站攻击，网关网络面默认开启严格的 Origin 校验。当前仅允许
`http://localhost`、`http://127.0.0.1`、`http://[::1]` 以及 `app://` 前缀来源连入；
非允许来源的跨域调用会被拦截并返回 `403`。
注：上述白名单机制仅针对携带 `Origin` 头的浏览器跨站请求生效。若请求不携带 `Origin` 头
（例如 `cURL`、Postman 或本地后端脚本直连），网关默认放行。

URL Scheme 派发骨架命令（EPIC-GW-02A）：

```bash
go run ./cmd/neocode url-dispatch --url "neocode://review?path=README.md"
```

> `url-dispatch` 会将 `neocode://` URL 转发到本地 Gateway，并输出结构化响应。
>
> 注意：当前 MVP 版本仅支持 `review` 动作，且必须携带 `path` 参数（如 `neocode://review?path=README.md`）；其余动作会在网关侧被拦截拒绝。

设置 API Key 示例（按你使用的 provider 选择）：

```bash
export OPENAI_API_KEY="your_key_here"
export GEMINI_API_KEY="your_key_here"
export AI_API_KEY="your_key_here"
export QINIU_API_KEY="your_key_here"
```

Windows PowerShell：

```powershell
$env:OPENAI_API_KEY = "your_key_here"
$env:GEMINI_API_KEY = "your_key_here"
$env:AI_API_KEY = "your_key_here"
$env:QINIU_API_KEY = "your_key_here"
```

按工作区启动（仅当前进程生效）：

```bash
go run ./cmd/neocode --workdir /path/to/workspace
```

### 4) 首次使用与常用命令

- `/help`：查看命令帮助
- `/provider`：打开 provider 选择器
- `/model`：打开 model 选择器
- `/compact`：压缩当前会话上下文
- `/status`：查看当前会话与运行状态
- `/cwd [path]`：查看或设置当前会话工作区
- `/memo`：查看记忆索引
- `/remember <text>`：保存记忆
- `/forget <keyword>`：按关键词删除记忆
- `& <command>`：在当前工作区执行本地命令

示例输入：

```text
请先阅读当前项目目录结构并给出模块职责摘要
帮我在 internal/runtime 下定位与 tool result 回灌相关逻辑
```

## 配置入口

- 主配置文件：`~/.neocode/config.yaml`
- 自定义 Provider：`~/.neocode/providers/<provider-name>/provider.yaml`

配置原则（用户侧重点）：

- API Key 通过环境变量注入，不写入 `config.yaml`
- `--workdir` 只影响当前运行，不会回写到配置文件

详细配置请参考：[docs/guides/configuration.md](docs/guides/configuration.md)

## 文档导航

- [配置指南](docs/guides/configuration.md)
- [扩展 Provider](docs/guides/adding-providers.md)
- [Runtime/Provider 事件流](docs/runtime-provider-event-flow.md)
- [Session 持久化设计](docs/session-persistence-design.md)
- [Context Compact 说明](docs/context-compact.md)
- [Tools 与 TUI 集成](docs/tools-and-tui-integration.md)
- [MCP 配置指南](docs/guides/mcp-configuration.md)

## 如何参与

欢迎通过 Issue 和 PR 参与共建。

1. 在 [Issues](https://github.com/1024XEngineer/neo-code/issues) 先沟通问题或需求。
2. Fork 仓库并创建功能分支。
3. 完成开发并确保改动聚焦、边界清晰。
4. 本地自检：

   ```bash
   gofmt -w ./cmd ./internal
   go test ./...
   go build ./...
   ```

5. 提交 PR 到主仓库并说明变更目的、影响范围和验证方式。

提交前请确认：

- 不提交明文密钥、个人配置或会话数据
- 不提交无关改动与临时文件

## License

MIT
