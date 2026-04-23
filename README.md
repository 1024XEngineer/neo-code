# NeoCode

> 基于 Go + Bubble Tea 的本地 Coding Agent

## NeoCode 是什么？
NeoCode 是一个在终端中运行的 AI 编码助手，采用 ReAct（Reason-Act-Observe）循环模式，围绕以下主链路工作：

`用户输入 -> Agent 推理 -> 调用工具 -> 获取结果 -> 继续推理 -> UI 展示`

它适合希望在本地工作流中完成代码理解、修改、调试与自动化操作的开发者。

## 项目介绍页

- 仓库内置了基于 VitePress 的 GitHub Pages 站点源码，目录为 `www/`
- 启用仓库的 GitHub Pages 并选择 `GitHub Actions` 后，站点将发布到：
  `https://<仓库拥有者>.github.io/neo-code/`
- 本地预览站点可使用：
  ```bash
  cd www
  pnpm install
  pnpm docs:dev
  ```
- 开发服务器启动后，默认从 `http://localhost:5173/neo-code/` 访问首页

## 有什么能力？
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
（例如 `curl`、Postman 或本地后端脚本直连），网关默认放行。

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

Gateway 转发与自动拉起说明：

- `neocode` 默认通过本地 Gateway（优先 IPC）转发 runtime 请求与事件流
- 启动时会先探测本地网关；若未运行会自动后台拉起并等待就绪（无感）
- 若自动拉起后仍不可达或握手失败，会直接报错退出（Fail Fast）

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
- `/skills`：查看当前可用 skills（含当前会话激活标记）
- `/skill use <id>`：在当前会话启用 skill
- `/skill off <id>`：在当前会话停用 skill
- `/skill active`：查看当前会话已激活 skills

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
- TUI 默认通过 Gateway 连接 runtime，启动时会自动探测并在必要时后台拉起网关

详细配置请参考：[docs/guides/configuration.md](docs/guides/configuration.md)

## 内部结构补充

- `internal/context`：负责主会话 system prompt 的 section 组装、动态上下文注入与消息裁剪。
- `internal/runtime`：负责 ReAct 主循环、tool 调用编排、compact 触发与 reminder 注入时机。
- `internal/subagent`：负责子代理角色策略、执行约束与输出契约。
- `internal/promptasset`：负责受版本管理的静态 prompt 模板资产，使用 `go:embed` 编译进程序，供 `context`、`runtime`、`subagent` 读取。

## 文档导航

- [配置指南](docs/guides/configuration.md)
- [扩展 Provider](docs/guides/adding-providers.md)
- [Runtime/Provider 事件流](docs/runtime-provider-event-flow.md)
- [Session 持久化设计](docs/session-persistence-design.md)
- [Context Compact 说明](docs/context-compact.md)
- [Tools 与 TUI 集成](docs/tools-and-tui-integration.md)
- [Skills 设计与使用](docs/skills-system-design.md)
- [MCP 配置指南](docs/guides/mcp-configuration.md)
- [更新与升级](docs/guides/update.md)

## 如何参与

欢迎通过 Issue 和 PR 参与共建。

1. 在 [Issues](https://github.com/1024XEngineer/neo-code/issues) 先沟通问题或需求。
2. Fork 仓库并创建功能分支。
3. 完成开发并确保改动聚焦、边界清晰。
4. 本地自检：
   ```bash
   make docs-gateway-check
   gofmt -w ./cmd ./internal
   go test ./...
   go build ./...
   ```
5. 提交 PR 到主仓库并说明变更目的、影响范围和验证方式。

提交前请确认：
- 不提交明文密钥、个人配置或会话数据
- 不提交无关改动与临时文件

## 在仓库内直接创建 Issue（Skills + 自动化）

仓库提供三类同前缀 skill（位于 `.agents/skills/`）：

- `issue-rfc-proposal`（提案类，RFC 风格）
- `issue-rfc-architecture`（架构类，RFC 风格）
- `issue-rfc-implementation`（实现类，执行单风格）

先安装 skills 到仓库内常见 AI Coding 工具目录：

```bash
make install-skills
```

默认会安装到以下目录（均在仓库内）：

- `.codex/skills`
- `.claude/skills`
- `.cursor/skills`
- `.windsurf/skills`

如需自定义安装目标，可设置环境变量 `SKILL_INSTALL_TARGETS`（冒号分隔目录）：

```bash
SKILL_INSTALL_TARGETS=".codex/skills:.claude/skills" make install-skills
```

Skill 内部调用脚本 `scripts/create_issue.sh` 创建 issue。你也可以直接执行脚本：

```bash
./scripts/create_issue.sh --type proposal --title "统一会话中断恢复语义"
./scripts/create_issue.sh --type architecture --title "Runtime 与 Session 账本边界梳理"
./scripts/create_issue.sh --type implementation --title "补齐流式中断持久化" --labels "bug,priority-high"
```

脚本可选参数：

- `--repo <owner/repo>`：指定目标仓库（默认自动识别当前仓库）
- `--body-file <path>`：自定义 issue 正文文件（不传则使用内置模板）
- `--labels <a,b,c>`：追加标签（逗号分隔）

## 网关运维与安全（GW-06）

- 静默认证（Silent Auth）：
  - 启动 `neocode gateway` 时会自动读取 `~/.neocode/auth.json`。
  - 若凭证不存在或损坏，会自动生成高强度 token 并写回该文件。
  - `url-dispatch` 会自动读取同一 token 并先发送 `gateway.authenticate`，再发送业务请求。
- 认证与授权顺序：`Auth -> ACL -> Dispatch`。
  - 未认证返回 `unauthorized`。
  - 已认证但不允许的方法返回 `access_denied`。
- 运维端点：
  - 免鉴权：`GET /healthz`、`GET /version`
  - 需鉴权：`GET /metrics`、`GET /metrics.json`（`Authorization: Bearer <token>`）
- 关键默认治理参数（可通过 `config.yaml` 的 `gateway.*` 配置）：
  - `max_frame_bytes=1MiB`
  - `ipc_max_connections=128`
  - `http_max_request_bytes=1MiB`
  - `http_max_stream_connections=128`
  - `ipc_read/write_sec=30/30`
  - `http_read/write/shutdown_sec=15/15/2`

详细设计文档：[`docs/gateway-detailed-design.md`](docs/gateway-detailed-design.md)

### Gateway JSON-RPC 方法清单（当前实现）

- `gateway.authenticate`：连接级鉴权握手
- `gateway.ping`：探活
- `gateway.bindStream`：会话流绑定
- `gateway.run`：发起一次运行（Accepted-ACK，异步执行）
- `gateway.compact`：触发会话压缩
- `gateway.cancel`：按 `run_id` 精确取消目标运行（`run_id` 必填）
- `gateway.listSessions`：查询会话摘要列表
- `gateway.loadSession`：加载单个会话详情
- `gateway.resolvePermission`：提交权限审批结果
- `wake.openUrl`：处理 `neocode://` 唤醒请求
- `gateway.event`：网关推送通知事件（notification）

## License

MIT
