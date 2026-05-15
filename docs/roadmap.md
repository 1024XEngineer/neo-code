# NeoCode 未来演进路线图

## 17. 未来演进

以下演进方向的优先级判断基于一个核心问题：**"这项改进对 NeoCode 的独特价值（本地优先、多端接入、多模型自由、Human-in-the-loop 安全）有多大推动？"**

### 17.1 短期（已在进行或立即需要的改进）

| 方向 | 理由 | 优先级 |
|------|------|--------|
| **传输层全 HTTP 化** | 当前 `transport/` 中残留的 Unix socket / Named pipe 逻辑增加了双平台代码路径和维护负担。统一到 HTTP JSON-RPC 后：第三方客户端接入更简单（只需要发 HTTP POST，不需要理解 Unix socket 地址规则）、Windows 和 Linux/macOS 的客户端连接逻辑完全一致 | 高——正在进行 |
| **Gateway 大文件拆分** | `bootstrap.go` 超 1600 行，包含帧路由、认证、session CRUD、RPC 处理、流绑定等所有 Gateway 逻辑。5 人团队每人负责不同模块，但 Gateway 的改动集中在同一大文件中 → 持续的合并冲突。按功能域拆分为 `auth_handler.go`、`session_handler.go`、`stream_handler.go` 后，各自改自己的文件 | 高——直接影响并行开发效率 |
| **Runner 工具并行执行** | Runner 当前串行处理 Gateway 下发的工具请求。在"手机飞书下指令 → 工位 Runner 执行"场景中，模型经常一次产出多个独立的 tool call（如同时读 3 个文件），串行执行导致不必要的延迟。改为并行执行可显著改善远程场景的响应体验 | 中——核心差异化场景的性能瓶颈 |
| **Compact 配置收敛** | MicroCompact 的 `MicroCompactConfig`、Full Compact 的 `CompactConfig`、预算阈值在 `RuntimeConfig` 中分散定义。调整上下文压缩策略时需要理解三个不同的配置入口，容易产生不一致的配置。收敛为单一 `CompactPolicy` 结构体 | 中——降低调优门槛 |

### 17.2 中期（巩固和放大现有差异化优势）

| 方向 | 理由 |
|------|------|
| **第三方客户端生态增强** | 飞书 Adapter 已证明"适配器接入 Gateway → 复用全栈 Agent 能力"的模式可行。下一步是降低第三方适配器的编写成本：提供官方 SDK（Go/Python/Node.js）封装 `gateway.authenticate` → `gateway.run` → SSE 事件消费的完整流程。这直接放大 NeoCode 最大的差异化优势——"任何客户端都能接入的 AI Agent 基础设施" |
| **Skills 和 MCP 体验改善** | 当前 Skills 需要手动在目录中放置 `SKILL.md` 文件，MCP Server 需要手动编辑 JSON 配置。这些操作的受众是开发者，但不是所有开发者都愿意读 YAML。方向：`neocode skill add <url>` 一键安装 Skill；`neocode mcp add <command>` 自动生成 MCP 配置。降低扩展成本直接提升生态壁垒 |
| **Checkpoint 可视化与选择性恢复** | Checkpoint 已在后台静默创建，但用户缺少手段查看"AI 在上一轮改了什么、为什么改"。方向：在 TUI/Web 端展示 `end_of_turn` Checkpoint 的 Diff 预览，支持用户选择"回滚到上一步"或"只回滚某个文件"。这增强 Human-in-the-loop 的安全信任感 |
| **安全策略预置模板** | 当前 Security Engine 的策略规则完全由用户自定义（或使用默认 `ask` 兜底）。多数用户不会写策略规则。方向：预置 3 套模板（"宽松——信任所有工作区操作"、"标准——敏感文件需确认"、"严格——任何写入操作需确认"），用户一键切换。安全能力如果门槛太高，等于没有 |
| **模型切换体验深化** | Provider 零侵入接入已实现（ADR-002），但用户切换模型时的体验仍粗糙：不知道新模型在"代码修改"场景的实际表现、不知道它的上下文窗口和工具调用能力。方向：为每个 Provider 提供能力画像（context window、tool calling 支持、已知局限），在切换时展示 |

### 17.3 长期（战略级方向）

| 方向 | 理由 |
|------|------|
| **无头模式（Headless Agent）** | NeoCode 的核心能力（ReAct 推理 + 工具执行）当前主要通过交互式客户端消费。但对于 CI/CD 场景——"当 PR 被标记为 `neocode-review` 时自动运行代码审查 Agent"——需要一个无交互的批处理模式。技术上 Runtime 已支持，缺少的是：非交互式权限策略（`allow`/`deny` 无 `ask`）、简洁的结果输出格式。这是从"开发者工具"走向"基础设施"的关键一步 |
| **Runner 能力谱系** | 当前 Runner 是一个"全有或全无"的远程执行代理——注册后就能执行所有工具。但不同场景需要不同权限：CI Runner 可能只需要 `bash`（跑测试）；代码审查 Runner 可能只需要 `filesystem_read` + `codebase_search`。方向：Runner 注册时声明自己的能力谱系（tool list + path allowlist），Gateway 按需路由 |
| **会话可移植性** | 当前会话数据绑定在本地 SQLite。如果开发者在工位电脑上开了一个长会话调试问题，回家后想在笔记本上继续，需要手动迁移 `session.db`。方向：可选的会话导出/导入（标准化格式），或可插拔的远程 Session Store 后端。这直接服务于"随时随地连线本地代码库"的价值主张 |

### 17.4 刻意不做的方向

以下方向经常被提及，但**故意不作为**演进目标：

| 方向 | 不做理由 |
|------|----------|
| **自研模型或模型微调** | NeoCode 是 Agent 框架，不是模型厂商。见 §2.6 非目标 #2 |
| **云端 SaaS 托管** | 代码留在本地是最核心的安全承诺。见 §2.6 非目标 #1 |
| **重型 IDE 插件（Copilot 模式）** | 旁路架构是核心差异化。见 §2.6 非目标 #4 |
| **微服务化拆分** | 单机场景下分布式是负资产。见 ADR-004 |
| **引入消息队列（Kafka/RabbitMQ）** | 零外部依赖是强约束。见 ADR-005 的推理链 |
| **图数据库 / 向量数据库** | 代码库理解（Tree-sitter AST + Grep + Glob）在代码领域远超向量检索的准确度，且不需要额外的数据库运维 |

### 17.5 可替换模块

以下模块在设计时就考虑了被替换的可能性——这是分层架构（§7.1）和接口优先原则（§5.4 原则 5）的直接成果：

| 模块 | 可替换原因 | 替换成本 |
|------|-----------|----------|
| **Provider 实现** | 仅需实现 2 方法 interface | 低——新增 Go 包 + 配置即可 |
| **Authenticator** | `TokenAuthenticator` interface | 低——实现验证逻辑即可 |
| **工具（单个）** | `Executor` interface | 低——注册到 Registry 即可 |
| **Skills 来源** | `SourceLayer` 机制 | 低——新增目录即可 |
| **Web UI** | 独立于后端逻辑，纯 RPC 通信 | 中——需重写 UI 层，Gateway API 不变 |
| **Session Store 后端** | `Store` interface | 中——理论上可替换为其他存储，但需重新评估 ADR-005 的零依赖约束 |
| **Gateway 传输协议** | `transport.Listener` interface | 中——需实现新协议适配 |
| **Runtime（整个）** | 通过 Gateway RPC 隔离 | 高——理论上可用非 Go 实现，但触及系统根基 |

---
