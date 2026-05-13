## 2. 系统背景与目标


### 2.1 核心定位

NeoCode 是一个**本地优先、架构解耦、可被随时唤醒和编排的 AI Coding Agent 基础设施**。

2025–2026 年，AI 编码工具已经从"代码补全"进化到"Agent 自主行动"——Claude Code、Codex CLI、OpenCode 等终端 CLI 工具已经证明了开发者不需要换 IDE 就能获得 Agent 级的 AI 辅助。MCP 和 Skills/Hooks 机制已成为 Agent 扩展生态的通用语言。多模型接入也不再是新鲜事。

在这个背景下，NeoCode 的定位不是"发明一个新品类"，而是在已有的 Agent 基础设施共识之上，**将这些能力按照自己的设计哲学重新组装**——追求**更强的架构解耦**、**更多的客户端形态**、**更开放的多模型自由**，并通过 Gateway 统一 RPC 边界将这些能力暴露给任何想接入的客户端。

### 2.2 NeoCode 的设计信念

NeoCode 的设计基于以下几条核心判断：

1. **AI Agent 不应该是编辑器的附属功能。** 它应该是一个独立的"结对编程进程"——在你写代码时安静运行在终端或后台，在你离开工位时仍然可以通过 IM 唤醒。所以 NeoCode 选择**"旁路架构"**而非"IDE 插件架构"（这一判断与 Claude Code、Codex CLI、OpenCode 一致——它们是 CLI 工具，不是 IDE 插件。但 NeoCode 进一步将**"独立进程"**推到极致：提供独立的 Gateway Daemon、独立的 Runner，以及可被第三方适配器接入的 RPC 接口）。

2. **开发者不应该为 AI 体验而被锁定在一个模型或一个厂商上。** 多模型支持已是行业标配（Claude Code 支持多模型、Codex 原生多模型、OpenCode 也支持切换 provider）。NeoCode 的做法是让 Provider 成为架构层面的**一等公民**——不是"配置项切换"，而是**独立的插件化接口层**，新增模型的改动严格收敛在 Provider 包内，上层零改动。

3. **AI Agent 的能力边界应该由使用者决定。** MCP 和 Skills/Hooks 是 Codex、Claude Code 等行业先行者开创的扩展机制。NeoCode 完整支持这些开放协议，并进一步将它们**整合**到 Gateway 安全边界内——所有工具（包括 MCP 挂载的外部工具）**统一经过** Security Engine 的权限裁决，确保扩展能力不侵蚀安全底线。

4. **客户端形态应该自由演化。** 终端 TUI 是核心体验（与 Claude Code/Codex CLI/OpenCode 同类），但不应是唯一入口。NeoCode 的 Gateway 将 Agent 能力通过标准 JSON-RPC/SSE/WebSocket 暴露，使得 Web 端、桌面端、飞书 Bot、CI/CD 脚本都是**对等的一等公民客户端**——不需要"终端优先再做 Web 适配"。

### 2.3 目标用户

| 用户角色 | 核心场景 | 是否独创 | NeoCode 的思考 |
|----------|----------|----------|---------------|
| 追求编辑器自由的独立开发者/资深工程师 | 习惯 Neovim/JetBrains/Emacs，不愿为 AI 迁移到特定 IDE | 人有我有：Claude Code、Codex CLI、OpenCode 均已解决"不换 IDE 用 AI" | 旁路架构与它们一致。NeoCode 的不同：Gateway 作为一等公民设计——TUI/Web/Desktop 完全对等，不是"先有 CLI 再补 Web"；多模型切换是架构级的 Provider 插件化，不是配置切换 |
| 需要随时响应代码问题的敏捷/分布式团队 | 通勤或开会时遇到线上问题，快速查阅和修改代码 | 创意集成，做的更好：Claude Code 有 headless 模式可做基础集成；NeoCode 将 IM 接入作为一等公民设计 | Local Runner 反向连接 + 飞书 Adapter 原生集成：Runner 主动连 Gateway（无需公网 IP），飞书 Bot 作为对等客户端接入 Gateway RPC。这不是"CLI 工具加了个 IM 通知"，而是 IM 就是一个完整的 Agent 交互界面 |
| DevOps 工程师与自动化工作流构建者 | 需要把 AI 接入 CI/CD，通过 RPC 驱动代码库操作 | 人有我有：Codex/Claude Code 可做脚本集成 | NeoCode 的 Gateway 从设计第一天就是独立的 RPC 服务（`neocode gateway` 子命令），不是 CLI 的附属模式。JSON-RPC 协议使得 Python/Bash/Node.js 脚本都能平等接入 |

### 2.4 与同类产品的差异化

NeoCode 不声称自己发明了 AI Coding Agent 的基础范式——ReAct 循环、MCP 扩展、流式推理、上下文压缩这些都是行业的共同财富。以下按照"行业现状"和"NeoCode 的差异化设计"两层来对比。

**行业现状（2026 年 AI 编码工具概览）：**

| 产品 | 类型 | 已有能力 |
|------|------|----------|
| **Claude Code** | CLI Agent（闭源） | ReAct Agent、MCP、Skills/Hooks、多模型、上下文压缩、Plan Mode、终端 TUI |
| **Codex CLI** | CLI Agent（开源） | ReAct Agent、MCP、多模型、Skills、沙箱执行、终端 TUI |
| **OpenCode** | CLI Agent（开源） | ReAct Agent、MCP、多模型、终端 TUI、可自定义 provider |
| **Cursor / Windsurf** | IDE 分叉（闭源） | IDE 深度集成、Agent 模式、代码补全、内联编辑 |
| **GitHub Copilot** | IDE 插件 | 代码补全、Agent 模式（Copilot Chat）、MCP |

**NeoCode 的差异化：不是"别人没有我有"，而是"别人有，我按自己的设计哲学重新组装，并在几个方向上做得更深"。**

| 能力维度 | 行业现状 | NeoCode 的设计选择 |
|----------|----------|-------------------|
| **架构模型** | Claude Code/Codex CLI/OpenCode 是 CLI 优先的单体 Agent | Gateway 作为独立的 RPC 服务层：Agent 能力通过 JSON-RPC/SSE/WS 对外暴露，所有客户端（包括 TUI）都通过同一套 RPC 协议接入。这使得"把 Agent 作为基础设施"从第一天就是架构事实，不是后期改造 |
| **多模型支持** | Codex、OpenCode、Claude Code 均支持多模型切换 | Provider 是架构级的一等公民：不是"配置项"，而是独立的插件化接口层（2 方法 interface）。新增模型不需要改 Runtime 或 Gateway 一行代码 |
| **多客户端** | Claude Code 有 VS Code 扩展、Codex 有 Web UI | NeoCode 的 TUI/Web/Desktop 三者完全对等——没有"先做 CLI 再适配 Web"的技术债。第三方客户端（如飞书 Bot）通过适配器接入，与原生客户端在 Gateway 视角完全一致 |
| **IM 接入** | Claude Code 可通过 API 做基础集成 | NeoCode 将飞书 Bot 作为一等公民客户端设计——完整的 Agent 交互（run、ask、permission_request、流式回复）都可以在 IM 中完成。Local Runner 反向连接使得 IM 指令能操作工位电脑的代码库 |
| **安全模型** | Claude Code 有权限系统（ask/allow/deny） | NeoCode 在此基础上增加：策略引擎（按 Priority 排序的规则匹配）、工作区沙箱（路径穿越/Symlink 检测）、敏感路径自动检测（`.env`、`.ssh`、密钥文件）、Runner 的 Capability Token 签名——四层纵深防御 |
| **部署形态** | Claude Code 为单二进制、Codex 需要 Node.js 运行时 | Go 静态编译单二进制（`CGO_ENABLED=0`），零运行时依赖。同一二进制提供 CLI、Gateway Daemon、Runner、HTTP Daemon 全部子命令 |
| **可扩展性** | Claude Code/Codex 均有 MCP 和 Hooks/Skills | NeoCode 完整支持 MCP + Skills + Hooks，并明确区分三种扩展路径（有代码工具、零代码 MCP、零代码 Skills），在 §7.6 中集中定义了 7 个扩展点的接口契约和 4 条不可扩展边界 |

### 2.5 核心痛点与 NeoCode 的方案

以下三个痛点并非 NeoCode 独自发现——它们是 AI 编码工具行业公认的摩擦点。这里记录 NeoCode 针对每个痛点的**具体设计方案**。

| 痛点 | 量化估算 | 行业中已有的解决思路 | NeoCode 的方案 |
|------|----------|---------------------|---------------|
| 终端→AI→编辑器频繁切换 | 每天 1–2 小时 | Claude Code/Codex CLI/OpenCode 均通过终端内 Agent 解决 | PTY Proxy 诊断代理：不只是"在终端里跑 AI"，而是自动截获终端最近一次命令的异常输出（编译错误、运行时 panic），原地附加 AI 诊断，不需要离开终端 |
| 非工位状态下的代码查阅与应急 | 单次切换耗时极大 | Claude Code 的 headless 模式可做基础集成 | Local Runner 反向连接 + 飞书 Adapter 原生集成：Runner 主动连 Gateway（无需公网 IP/入站端口），飞书 Bot 作为对等客户端接入，手机端发指令 → 工位电脑执行 → 结果回传 IM |
| 将公司内部工具接入 AI 工作流 | 传统 AI 工具无法调用内网基建 | Claude Code/Codex/OpenCode 率先引入了 MCP 和 Skills 机制 | NeoCode **完整支持 MCP + Skills**（这是行业标准，不是 NeoCode 独创）。NeoCode 的增量在于：外部的 MCP 工具统一经过 Security Engine 的权限裁决（每次调用前经过 PolicyEngine + WorkspaceSandbox），确保"扩展能力"不变成"安全漏洞"；Skills 的 project/global 双层加载 + 会话级激活，使其更易于在团队中管理和复用 |


### 5.2 团队与组织约束

| 约束项 | 说明 |
|--------|------|
| 团队规模 | 5 名核心开发者 |
| 模块分工 | TUI/Gateway 适配层（1 人）、Provider/Runtime/上下文压缩/网站（1 人）、Tools/Security/Hook/飞书适配器（1 人）、Session/Context/Memo/PromptAsset/Web/App（1 人）、Gateway/URL Scheme/Shell诊断/CLI/CI/发布/安装脚本（1 人） |
| 审查流程 | 组内 peer review，简单审查后合入 |
| 发布节奏 | 按需发布，通过 goreleaser 自动构建 |
| 代码规范 | 参见 `AGENTS.md`：严格 UTF-8 编码、Go 惯用风格、TAB 缩进、中文注释、单行约 120 字符 |

