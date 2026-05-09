# NeoCode 系统架构文档

**文档版本：** v0.1
**维护者：** NeoCode 团队
**最后更新：** 2026-05-08
**适用系统版本：** main 分支 HEAD
**目标读者：** 架构评审委员会

---

## 1. 文档元信息

本文档用于描述 **NeoCode** 的整体架构设计，包括系统边界、核心模块、关键流程、部署拓扑、安全设计和架构决策记录（ADR）。

本文档面向 **架构评审委员会**，旨在回答以下问题：
- 系统解决什么问题、边界在哪里？
- 核心组件如何划分、如何协作？
- 为什么选择这种架构而不是其他方案？
- 有哪些质量目标、已知风险和演进方向？

本文档 **不包含**：
- 详细 API 字段说明（参见 `docs/reference/gateway-rpc-api.md`）
- 具体部署操作步骤（参见部署手册）
- 用户使用指南（参见官方文档站）

本文档随代码库演进持续更新。当架构决策发生变更时，应在第 15 节追加新的 ADR，并更新受影响的相关章节。

---

## 2. 系统背景与目标

### 2.1 核心定位

NeoCode 不仅是一个对话式 AI 助手，更是一个**本地优先、架构解耦、可被随时唤醒和编排的 AI Coding Agent 基础设施**。

### 2.2 解决的核心问题

当前 AI 编码工具普遍与特定 GUI/IDE 强绑定。开发者若想获得最佳的 AI 辅助编程体验，往往需要放弃自己熟悉的编辑器和工作流，迁移至工具指定的 IDE 生态。同时，这些工具的 AI 能力难以被外部脚本、CI/CD 流水线或即时通讯工具所调用。

NeoCode 解除这种绑定，让具备多步推理与执行能力的 Agent 能够**无缝融入开发者的任意环境、终端以及自动化工作流中**。

### 2.3 目标用户

| 用户角色 | 核心场景 | NeoCode 的价值 |
|----------|----------|---------------|
| 追求编辑器自由的独立开发者/资深工程师 | 习惯使用 Neovim、JetBrains、Emacs 等，不愿为 AI 体验而迁移到特定 IDE | 旁路架构：在终端或后台独立运行，不侵入既有编辑器和工具链 |
| 需要随时响应代码问题的敏捷/分布式团队 | 通勤或开会时遇到线上问题，需要快速查阅和修改代码 | Local Runner + 飞书/IM 接入：手机端通过 IM 直接与工作区代码对话 |
| DevOps 工程师与自动化工作流构建者 | 需要把 AI 接入 CI/CD，通过 RPC 驱动代码库操作 | Gateway JSON-RPC/SSE：将 AI Agent 作为标准后台 Daemon 进行编排 |

### 2.4 与同类产品的差异化

**vs Cursor / Windsurf（重型 IDE 派）**

填补空白：**外挂式协作与零环境侵入**。Cursor 的代价是"换掉你的 IDE"。NeoCode 采用旁路架构，通过 CLI、终端 PTY 代理和后台 Daemon 进程运行在侧，保护开发者既有的肌肉记忆和工具链。

**vs GitHub Copilot（IDE 插件派）**

填补空白：**从"代码补全"跃升为"自主行动闭环"**。Copilot 强依赖当前打开的文件上下文，主要擅长代码补全。NeoCode 是一个拥有执行权限的 Agent（`filesystem`、`git` 检索、`bash` 执行、`MCP` 扩展），能自主完成"看懂需求 → 检索代码库 → 跑脚本验证 → 修改多处文件"的完整循环。

**vs Claude Code（CLI 工具派）**

填补空白：**开源解耦、平台无关与终端诊断**。Claude Code 是闭源且深度绑定单一厂商模型的专属工具。NeoCode 支持多模型接入，终端底层集成 PTY Proxy 诊断代理（自动截获 Shell 报错并原地诊断），原生提供 Gateway 层和飞书 IM 长连接支持，拥有更广阔的集成空间。

### 2.5 量化的核心痛点与解决方案

| 痛点 | 现状描述 | 量化估算 | NeoCode 方案 |
|------|----------|----------|-------------|
| 排障时的"搬运工"式上下文切换 | 终端报错 → 复制 → 切网页大模型 → 复制代码片段 → 等待 → 回 IDE 修改 → 回终端运行 | 每天 1–2 小时 | Shell 诊断代理（`neocode diag`）：自动获取最近异常输出，原地给出原因和建议 |
| 非开发状态下的代码查阅与应急 | 下班后同事询问代码逻辑或突然报 Bug，身边无 IDE，只能凭记忆或找电脑 | 单次切换耗时极大 | Local Runner 反向连接：手机飞书发指令，Agent 操作本地工作区返回答案 |
| 内部工具系统与 AI 割裂 | 大模型无法调用公司内部测试平台、内网 API 文档，AI 只能写代码不能结合基建排查 | 阻碍效率自动化 | Skills 引擎 + MCP：将内部 CLI、数据库查询脚本暴露给大模型，实现自动调用 |

### 2.6 非目标

NeoCode **明确不追求**以下目标：

1. **不做中心化的云端 SaaS 代码托管与多租户平台** — 数据、会话、代码上下文全部留在本地工作区，权限直接继承系统进程权限。
2. **不与单一模型厂商或闭源生态强绑定** — Provider 接口保持开放，允许随时切换到新模型或本地部署的开源模型。
3. **不做完全无人值守的"黑盒程序员"** — 所有操作可打断、可审查，坚持 Human-in-the-loop。
4. **不做重型的代码编辑器分叉** — 不 Fork VS Code 或任何编辑器，UI 定位为 Agent 的独立控制面板与协作客户端。
5. **不做"云端渲染/强依赖公网"的 Web 客户端** — 在离线局域网环境下（搭配本地模型），Web 端和桌面端也能独立拉起并直连本机 Gateway，始终坚持"本地工作区优先"的安全与数据留存策略。

---

## 3. 系统范围与边界

### 3.1 系统职责（In Scope）

| 职责域 | 说明 |
|--------|------|
| 多模型 Provider 适配 | 归一化不同厂商的 Chat API 协议为统一流式事件模型 |
| ReAct 推理循环 | 用户输入 → 上下文构建 → 模型推理 → 工具调用 → 结果回灌 → 循环，直到产出最终回复 |
| 工具执行与安全管理 | 文件读写、Bash 执行（含 Git 语义分类）、代码库检索、Web 抓取、MCP 扩展、Todo/子代理等工具的 schema 暴露、参数校验、权限决策和执行 |
| 多端客户端接入 | 通过 Gateway 统一暴露 JSON-RPC、SSE、WebSocket 接口，支持 TUI、Web、桌面端（Electron）、飞书 Bot 等对等接入 |
| 会话与状态管理 | 会话创建、持久化（SQLite）、历史消息管理、Token 追踪、上下文裁剪（Compact） |
| Skills 系统 | 从本地文件系统加载 SKILL.md，按需激活并注入 System Prompt，为特定任务提供专用行为和流程 |
| 上下文构建与压缩 | 按照会话状态、预算阈值、消息历史动态构建 Provider 请求的 System Prompt 和消息列表 |
| 记忆（Memo）系统 | 跨会话保存用户偏好、项目事实和上下文，通过 LLM 提取结构化记忆 |
| 自我更新 | 通过 Go 自更新机制拉取最新版本 |

### 3.2 明确不在范围内（Out of Scope）

| 不在范围内 | 说明 |
|-----------|------|
| 代码托管（远程仓库） | NeoCode 有本地 Checkpoint 版本快照机制（参见 §10），但不提供远程仓库托管（如 GitHub/GitLab 替代品）；Git 操作通过 Bash 工具的语义分类层安全管控 |
| 自主研发大模型 | NeoCode 是 Agent 框架，不训练或部署自有模型 |
| 项目管理和需求跟踪 | 不替代 Jira、Linear 等工具，但可通过 Todo 工具做任务级编排 |
| 重型 IDE 插件 | 不做 Copilot 式的深度 IDE 集成；但可在 IDE 中嵌入瘦客户端作为 Gateway 的前端面板，复用后端全栈能力 |
| 组织级 RBAC 与多租户 | 鉴权仅限于 Gateway 连接级 Token，不做企业组织架构映射 |

### 3.3 客户端接入架构与外部依赖

NeoCode 的客户端分为两类：**原生客户端**（由 NeoCode 自身提供）和 **第三方客户端**（通过适配器接入）。所有客户端均通过 Gateway 暴露的统一 RPC 接口与 Runtime 通信，Gateway 内部不包含任何端侧特化逻辑。

```
┌──────────────────────────────────────────────────────────────────┐
│                         NeoCode 系统                              │
│                                                                  │
│  原生客户端（NeoCode 内置）           第三方客户端（适配器接入）     │
│  ┌────────┐ ┌────────┐ ┌──────────┐  ┌───────────┐              │
│  │  TUI   │ │  Web   │ │ Desktop  │  │ 飞书 Bot   │  ...         │
│  └───┬────┘ └───┬────┘ └────┬─────┘  └─────┬─────┘              │
│      │           │           │              │                     │
│      │    RPC (JSON-RPC / SSE / WebSocket)  │  Feishu Adapter   │
│      │           │           │              │                     │
│      └───────────┼───────────┼──────────────┘                    │
│                  │           │                                    │
│                  └───────────┘                                    │
│                      │                                            │
│                ┌─────┴──────┐                                     │
│                │  Gateway   │  ← 统一 RPC 边界，无客户端特化逻辑   │
│                └─────┬──────┘                                     │
│                      │                                            │
│                ┌─────┴──────┐                                     │
│                │  Runtime   │                                     │
│                └─────┬──────┘                                     │
│                      │                                            │
│        ┌─────────────┼──────────────┐                             │
│        │             │              │                              │
│   ┌────┴────┐  ┌────┴────┐  ┌─────┴──────┐                       │
│   │Provider │  │ Tools   │  │ Session    │                       │
│   └────┬────┘  └────┬────┘  └────────────┘                       │
│        │             │                                            │
└────────┼─────────────┼────────────────────────────────────────────┘
         │             │
         ▼             ▼
  ┌──────────┐  ┌──────────────┐
  │ 模型厂商  │  │ 外部工具/服务  │
  │          │  │              │
  │ Anthropic│  │ MCP Servers  │
  │ OpenAI   │  │ Git (local)  │
  │ Gemini   │  │ Filesystem   │
  │ DeepSeek │  │ Shell        │
  │ MiniMax  │  │ Web (HTTP)   │
  │ ...      │  │              │
  └──────────┘  └──────────────┘
```

**原生客户端（NeoCode 内置）：**
- **TUI**：终端交互界面（Bubble Tea），通过 RPC 连接 Gateway
- **Web**：React SPA，embed 到二进制中，由 Gateway 提供静态文件服务
- **Desktop**：Electron 壳，内嵌 Web UI，提供系统托盘和原生通知等桌面能力

**第三方客户端接入模式：**
- 第三方软件可通过编写适配器接入 NeoCode，适配器负责将外部事件/消息转换为 Gateway RPC 调用
- **飞书 Bot** 是第三方客户端的典型范例：`Feishu Adapter` 接收飞书开放平台 Webhook → 转换为 Gateway JSON-RPC 请求 → 将 Runtime 回复通过飞书消息 API 返回
- 同模式可扩展至其他 IM 平台（企业微信、钉钉、Slack 等）或自定义系统

**对外依赖（模型与工具层）：**
- **模型厂商 API**：Anthropic、OpenAI、Google Gemini、DeepSeek、MiniMax、Mimo、Qwen、GLM 等（通过 HTTPS）
- **MCP 服务器**：本地 stdio 子进程，提供外部工具扩展
- **Git**：本地 Git 命令行（通过 Bash 工具的语义分类层间接调用，不暴露为独立 `git_*` 工具）
- **Shell**：操作系统原生 Shell（bash/zsh/pwsh 等，用于 `bash` 工具）
- **飞书开放平台 API**：供 Feishu Adapter 使用，用于接收 Webhook 与发送消息回复

### 3.4 边界规则

1. **代码数据不出本地**：所有文件系统操作限制在 `--workdir` 指定目录内，代码内容不经 Gateway 上传至任何云端服务。
2. **模型请求仅含必要上下文**：发送给模型厂商的请求仅包含 System Prompt + 对话历史 + 工具定义，不泄露本地路径、环境变量或密钥。
3. **Gateway 是唯一跨边界通道**：所有客户端（原生或第三方）必须通过 Gateway 的 RPC 接口与 Runtime 通信，不允许直连。第三方接入时，适配器负责协议转换，Gateway 不感知客户端来源。

---

## 4. 架构目标与质量属性

以下按优先级从高到低排列。

### 4.1 安全性（Security）— 防越权与防破坏的底线

作为拥有文件读写和 Bash 执行权限的本地 Agent，任何沙箱逃逸或不可控行为都是毁灭性的。

| 指标 | 目标值 | 验证方式 |
|------|--------|----------|
| 工作区绝对隔离 | 0 逃逸：`filesystem_*` 和 `codebase_*` 的读写 100% 拦截在工作目录边界内，阻断向上的路径穿越（如 `../../etc/passwd`）和逃逸级 Symlink 解析 | 安全策略引擎单元测试 + 渗透测试用例 |
| 密钥零泄漏 | 日志、调试输出、配置快照中 API Key 明文留存率为 0 | CI 扫描规则 + 代码审查 |
| 执行阻断与超时兜底 | Bash 工具硬性超时（默认 60s），100% 拦截交互式阻塞命令（如 `vim`、`top`） | 工具执行单元测试 |

### 4.2 可测试性（Testability）— 对抗 AI 不确定性的锚

大模型的输出不可预测，因此框架本身的链路（流式解析、工具回调、状态机流转）必须极其健壮且可被快速验证。

| 指标 | 目标值 |
|------|--------|
| 核心模块测试覆盖率 | `runtime`、`gateway`、`tools`、`provider` 达到 100%（硬性目标） |
| Mock 测试速度 | 在不挂载真实模型 API 的情况下，数千个核心引擎单测在 **< 5 秒**内完成 |
| 物理交互组件接口化 | 文件系统、Bash 进程、API Provider 全部基于 Interface，支持 Mock 注入 |

### 4.3 可扩展性（Extensibility）— 生态兼容的核心引擎

系统需要适配不断涌现的新模型、新的前端客户端以及外部团队自定义的诊断工具。

| 指标 | 目标值 |
|------|--------|
| Provider 零侵入 | 接入全新大模型时，`runtime` 和 `gateway` 业务代码改动行数为 **0** |
| MCP 热插拔 | 挂载新外部 Skill 或工具 Schema 到模型上下文的生效延迟 **< 1 秒** |
| 多端无缝挂载 | Gateway 同时支撑 3 种以上异构客户端（TUI、React Web、Electron、IM Bot），Gateway 内部无任何端侧特化硬编码 |

### 4.4 可观测性（Observability）— 黑盒执行的破局点

开发者必须能一眼看穿 AI 在"想什么"、"读了什么文件"、"为什么报错"。

| 指标 | 目标值 |
|------|--------|
| Token 消耗透出 | 每次 Run Loop 100% 暴露 Input/Output/Cache Tokens 明细 |
| Tool Call 回传完整性 | Tool Call 的确切入参及回传原始结果完整透出（不受输出截断影响） |
| 全链路追踪 | 所有事件共享统一 `SessionID` + `RunID`，从客户端点击到 Tool 执行可一键溯源 |

### 4.5 性能（Performance）— 流畅的结对编程体验

虽然 AI 推理耗时由云端 API 主导，但本地框架的组装与工具执行必须无感。

| 指标 | 目标值 |
|------|--------|
| 本地代码检索 | 基于 Tree-sitter 的符号提取和路径匹配，单次引擎执行 **< 200 毫秒** |
| 大上下文流转 | 单会话稳定支持 **10 万 Token 以上** 长上下文；Compact 压缩触发耗时 **< 100 毫秒**，且不遗失 System Prompt 和当前执行状态 |
| 工具并行调度 | Runtime 支持同时调度最多 4 个独立工具调用（并行度可配置） |

---

## 5. 约束与设计原则

### 5.1 技术栈约束

| 层面 | 选型 | 约束来源 |
|------|------|----------|
| 语言 | Go 1.25+ | 团队能力、编译为单一二进制、跨平台 |
| TUI 框架 | Charmbracelet Bubble Tea + Lipgloss | Go 生态最成熟的 TUI 框架 |
| 数据库 | SQLite（modernc 纯 Go 实现） | 零依赖本地持久化，无需外部数据库进程 |
| 配置管理 | Viper + YAML | Go 生态标准方案 |
| CLI 框架 | Cobra | Go 生态标准方案 |
| 自更新 | go-selfupdate | 支持跨平台二进制差分更新 |
| Web UI | React + Vite（embed 到 Go binary） | Web 端嵌入，启动时由 Gateway 提供静态文件服务 |
| 桌面端 | Electron | 跨平台桌面壳，内嵌 Web UI |

### 5.2 团队与组织约束

| 约束项 | 说明 |
|--------|------|
| 团队规模 | 5 名核心开发者 |
| 模块分工 | TUI/Gateway 适配层（1 人）、Provider/Runtime/上下文压缩/网站（1 人）、Tools/Security/Hook/飞书适配器（1 人）、Session/Context/Memo/PromptAsset/Web/App（1 人）、Gateway/URL Scheme/Shell诊断/CLI/CI/发布/安装脚本（1 人） |
| 审查流程 | 组内 peer review，简单审查后合入 |
| 发布节奏 | 按需发布，通过 goreleaser 自动构建 |
| 代码规范 | 参见 `AGENTS.md`：严格 UTF-8 编码、Go 惯用风格、TAB 缩进、中文注释、单行约 120 字符 |

### 5.3 部署与平台约束

| 约束项 | 说明 |
|--------|------|
| 目标平台 | Windows、macOS、Linux 三大桌面平台全部支持；用户分布以 Windows 为主 |
| 部署形态 | 单一二进制文件（`neocode`），提供 CLI 交互、Gateway 服务 (`neocode gateway`)、HTTP URL Scheme 唤醒 Daemon (`neocode daemon`)、Web UI、Local Runner (`neocode runner`) 等全部子命令 |
| 网络环境 | 无特殊限制：在线环境通过 HTTPS 调用模型 API；离线环境可搭配本地模型使用，Web 端和桌面端也可在局域网环境下直连本机 Gateway |
| 数据目录 | 默认 `~/.neocode/`（可配置），存放配置文件、SQLite 会话数据库、Skill 缓存、自更新下载等 |
| 进程间通信 | 客户端与 Gateway 通过 JSON-RPC / SSE / WebSocket 等 RPC 协议通信；底层传输层正在从 IPC（Unix domain socket / named pipe）向全 RPC 方案迁移 |

### 5.4 设计原则

以下原则提炼自项目 AGENTS.md 并经架构评审确认，每条原则在 NeoCode 中有其具体的设计动机。

#### 原则 1：分层隔离

上层只依赖下层契约，不依赖下层实现细节。`tui` 不感知 provider 协议，`runtime` 不感知具体模型厂商字段。

**动机：** 团队五人各守一层，分层隔离使得各层可独立开发、测试和替换，减少并行开发中的冲突。同时，Provider 的零侵入可扩展性（§4.3）直接依赖此原则。

#### 原则 2：能力入口收敛

任何模型可调用的能力，必须经过 `internal/tools` 的 Schema + Execute 协议，不允许在 `runtime` 或 `tui` 中内嵌工具逻辑。

**动机：** 安全性（§4.1）要求所有工具执行经过统一的安全策略引擎（权限决策、工作区边界检查）。如果工具逻辑分散在各处，安全审计将不可行。

#### 原则 3：状态集中

会话状态、消息历史、工具调用记录由 `runtime/session` 统一管理，不分散到 UI 或其他消费层。

**动机：** 多端（TUI/Web/Desktop/飞书）共享同一 Runtime 实例时，状态若分散会导致一致性问题。集中管理保证了全链路可观测性（§4.4）中 SessionID/RunID 的全局统一。

#### 原则 4：配置先行

环境差异项（超时、路径、模型名、输出限制）优先通过配置注入，不硬编码。

**动机：** 支持多模型自由切换和多部署环境（本地/内网/离线）的必然要求。配置的外部化也保证了密钥零泄漏（密钥仅存于环境变量，不入配置文件）。

#### 原则 5：接口优于实现

核心抽象上的导出类型、函数、接口优先面向接口编程，不暴露具体厂商结构。

**动机：** 可测试性（§4.2）要求所有物理世界交互组件（文件系统、Bash、模型 API）可被 Mock。接口化是实现亚秒级 Mock 测试的前提。

---

## 6. 系统上下文视图

本节描述 NeoCode 与外部世界的交互关系，即 C4 模型中的 Level 1（系统上下文图）。

![System Context Diagram](diagrams/system-context.svg)

**图 6-1：NeoCode 系统上下文图。** 此图展示系统的四类调用方（终端开发者、Web/桌面用户、IM 用户、CI/CD 流水线）和五类外部依赖（模型厂商 API、MCP 服务器、本地 Git、操作系统 Shell、飞书开放平台）。为便于理解，系统边界内部标注了核心模块的逻辑分组，但不展开内部交互细节——详见 §7 容器图。

### 6.1 调用方（Actor）分析

| 调用方 | 接入方式 | 是否原生客户端 | 典型交互模式 |
|--------|----------|---------------|-------------|
| 终端开发者 | `neocode` CLI / TUI → Gateway RPC | 是 | 长会话交互，持续多轮对话 + 工具执行 |
| Web/桌面用户 | Web UI / Electron → Gateway RPC | 是 | 长会话交互，UI 面板展示实时流式输出 |
| IM 用户（飞书） | 飞书消息 → Feishu Adapter → Gateway RPC | 否（第三方适配器） | 短任务驱动："查代码" "修 Bug" "跑诊断" |
| CI/CD 流水线 | 脚本 → Gateway JSON-RPC | 否（自动化调用方） | 无状态单次调用：代码审查、自动修复、批量操作 |

### 6.2 外部系统依赖方向

- **传出依赖**（NeoCode → 外部）：模型 API 调用、MCP 子进程启动、Git 信息查询、Shell 命令执行——均为 NeoCode 主动发起
- **传入依赖**（外部 → NeoCode）：飞书开放平台 Webhook → Feishu Adapter——为外部系统回调触发
- **被动资源**：本地文件系统和 Git 仓库——由 NeoCode 通过工具层读写，不直接向 NeoCode 发送请求

---

## 7. 整体架构设计

### 7.0 架构哲学：根本设计张力与选择

架构评审的核心问题不是"系统有哪些模块"，而是"面对各种约束和权衡，**为什么做出了这些选择而不是那些**"。本节直接回答 NeoCode 架构中最根本的五个设计张力。

#### 张力 1：微服务 vs 单体 —— 为什么选择"强边界单体"？

NeoCode 的所有核心模块（Gateway、Runtime、Provider、Tools、Session）**运行在同一进程中**，通过 Go interface 解耦，而非通过网络调用。

| 方案 | 适合场景 | NeoCode 为何不选 |
|------|----------|-----------------|
| **微服务**（每模块独立进程，gRPC/消息队列通信） | 多团队独立部署、独立扩缩容、异构技术栈 | NeoCode 在用户本地机器上运行（单机单用户），微服务引入的序列化开销、网络延迟、运维复杂度都是**净成本而非收益**。一个开发者不会为 AI Agent 启动 5 个 Docker 容器 |
| **纯单体**（模块间直接调用，无接口边界） | 小团队快速迭代 | 违背 §4.3 可扩展性：新增 Provider 需要改 Runtime 代码；违背 §5.2 五人并行分工 |
| **强边界单体（当前选择）** | 需要模块独立演进但不需要独立部署 | 同一进程内通过 interface 解耦，享受编译时类型安全 + 零网络延迟 + 单一二进制部署；当某个模块确实需要独立伸缩时（如 Runner），才拆分为独立进程 |

**核心逻辑：** NeoCode 的部署约束（单机单用户本地运行）决定了分布式架构是负资产；NeoCode 的演进约束（多人并行开发 + 多模型自由切换）决定了纯单体不可行。**强边界单体是在这两个约束下的最优解。**

#### 张力 2：事件驱动 vs 纯同步调用 —— 为什么在进程内使用事件模型？

Runtime 内部使用 Go channel 驱动的异步事件流，而非同步 callback 或返回大对象。

| 方案 | 适用场景 | NeoCode 为何不选 |
|------|----------|-----------------|
| **纯同步**（Run 方法阻塞等待完成，返回完整结果） | 批处理、请求-响应模式 | AI 推理是流式的（token 逐个产出）且可能持续数十秒到数分钟。纯同步在推理完成前客户端完全黑屏，无法支持中途取消或实时权限审批 |
| **独立消息队列**（RabbitMQ/Kafka） | 跨进程/跨服务异步通信 | NeoCode 在单进程中运行，引入外部消息中间件违反"零外部依赖"的部署约束 |
| **进程内事件驱动（当前选择）** | 需要流式输出 + Human-in-the-loop 的单进程系统 | Go channel 天然支持 goroutine 安全通信；StreamRelay 实现广播（同一事件推送到多个客户端连接）；需要暂停执行等用户决策时（`permission_request`），Runtime 通过 channel 等待而不阻塞其他 goroutine |

**核心逻辑：** AI Agent 的推理过程本质上是异步的、流式的、可被中途干预的。事件驱动模型不是"过度设计"，而是**对 AI 交互本质的忠实映射**。

#### 张力 3：Go vs 其他语言 —— 为什么选 Go 而不是 Python/TypeScript/Rust？

| 方案 | NeoCode 场景下的关键差异 |
|------|------------------------|
| **Python** | AI 生态最丰富，但 (1) 运行时依赖重、分发困难（用户需要 Python 解释器）；(2) GIL 限制并发工具执行；(3) 动态类型在 5 人并行开发 + 100% 覆盖率目标下类型安全成本高 |
| **TypeScript/Node.js** | 前端生态统一（Web UI 已是 TS），但 (1) 单线程模型限制工具并行执行；(2) 原生性能不如编译语言（Tree-sitter、文件系统操作）；(3) 分发同样需要运行时 |
| **Rust** | 性能和安全性最优，但 (1) 学习曲线在 5 人团队中不均衡；(2) 迭代速度受编译时间影响；(3) AI/LLM SDK 生态（Anthropic、OpenAI、Google）不如 Go 成熟 |
| **Go（当前选择）** | 编译为单一静态二进制（零依赖分发）、goroutine 天然支持并行工具执行、静态类型保障 5 人并行开发时的接口契约、AI SDK 生态成熟（Anthropic、OpenAI Go SDK）、跨平台编译无痛 |

**核心逻辑：** 选择 Go 不是因为"Go 是最好的语言"，而是因为在 NeoCode 的具体约束下——**单一二进制分发、并行工具执行、5 人并行开发、跨平台**——Go 是这四个约束的**最大公约数**。

#### 张力 4：SQLite vs 外部数据库 —— 为什么用嵌入式数据库？

| 方案 | NeoCode 为何不选 |
|------|-----------------|
| **PostgreSQL / MySQL** | 需要用户安装和运行数据库服务 → 违反"零外部依赖"的部署约束 |
| **Redis** | 主要用于缓存，NeoCode 的会话数据需要持久化而非纯缓存；额外进程依赖 |
| **纯文件存储（JSON/YAML）** | 并发写入不安全；无法支持原子事务（Compact 替换消息列表时尤为危险）；查询（按 SessionID 过滤、按时间排序、过期清理）需要全量加载 |
| **SQLite（当前选择）** | 嵌入式（与 Go binary 链接）、ACID 事务（保证 Compact 原子替换）、单文件存储（备份简单）、modernc 纯 Go 实现零 CGO 依赖 |

**核心逻辑：** AI 编码 Agent 的会话数据量级（单会话最多 8192 条消息）完全在 SQLite 的舒适区内。引入外部数据库的运维成本与系统实际数据规模**严重不匹配**。

#### 张力 5：JSON-RPC vs gRPC/REST —— 为什么选 JSON-RPC 2.0？

| 方案 | NeoCode 场景下的关键差异 |
|------|------------------------|
| **gRPC** | 需要 protobuf 编译步骤 → 第三方客户端接入门槛高（需要 `.proto` 文件 + 代码生成）；调试需要专用工具 |
| **REST** | 资源建模适合 CRUD，但 NeoCode 的操作是**动词型**的（`gateway.run`、`gateway.cancel`），硬映射到 REST 资源语义不自然；SSE 做流式输出需要额外约定 |
| **JSON-RPC 2.0（当前选择）** | 极简协议（method + params + id）、任何能发 JSON 的客户端都能接入、配合 SSE/WS 做流式输出、人类可读易调试 |

**核心逻辑：** Gateway 的核心目标是"让任何客户端都能平等接入"。JSON-RPC 2.0 是**协议门槛最低**的选择——第三方写一个飞书适配器只需发 JSON，不需要 protobuf 编译器。

---

### 7.1 架构风格：分层 + 事件驱动

在上述五大设计张力的约束下，NeoCode 的架构风格自然收敛为 **分层架构（Layered Architecture）** + **进程内事件驱动（Event-Driven）**。

**分层架构的动机：**
- 团队五人各守一层（见 §5.2），严格接口边界使各层可独立开发、测试和替换
- Provider 层和 Client 层的可替换性（见 §4.3）直接依赖分层边界
- 安全审计（见 §4.1）要求工具执行路径可预测、可审计——分层保证了安全策略引擎的唯一入口

**事件驱动的动机：**
- 模型推理是流式、异步的过程：Provider 通过 channel 推送 `StreamEvent`，Runtime 消费并转换为 `RuntimeEvent`。客户端通过 JSON-RPC 发起请求，Gateway 同步返回 ack；对于长时间运行的 run/ask 操作，Gateway 通过 StreamRelay 将 Runtime 事件以 SSE 或 WebSocket 推送至订阅客户端
- 多客户端并发：Gateway 通过流中继（Stream Relay）机制，将同一 Runtime 事件广播到多个订阅连接
- Human-in-the-loop 权限审批：工具执行遇到需用户决策的操作时，Runtime 暂停并向 Gateway 发送 `permission_request` 事件，等待客户端通过 JSON-RPC（`gateway.resolve_permission`）回复

### 7.2 容器视图（C4 Level 2）

> **容器 = 可独立部署/运行的最小单元。** 在 NeoCode 中，CLI、Gateway Daemon、Runner 通过同一二进制文件的不同子命令启动；Web UI 嵌入在 Gateway 进程内。

```mermaid
graph TD
    %% --- 客户端 ---
    subgraph Client["客户端 (独立进程或嵌入)"]
        direction LR
        TUI["TUI<br/>Bubble Tea CLI"] ~~~ WEB["Web<br/>React SPA"] ~~~ DT["Desktop<br/>Electron"] ~~~ FS["飞书 Bot<br/>via Adapter"]
    end

    %% --- 核心 ---
    GW["Gateway<br/>══════════════<br/>JSON-RPC · SSE · WS<br/>Stream Relay<br/>Token Auth"]
    RT["Runtime<br/>══════════════<br/>ReAct 循环编排<br/>权限审批 · Compact 调度<br/>事件派发 · Hook 管理"]

    %% --- 能力层 ---
    subgraph Capability["能力层 (同一进程内的模块边界)"]
        direction LR
        PR["Provider<br/>多模型适配"]
        TL["Tools<br/>工具执行 + 安全引擎"]
        CTX["Context<br/>Prompt 构建 + 压缩"]
        SESS["Session<br/>SQLite 持久化"]
    end

    %% --- 支撑 ---
    subgraph Support["支撑 (同一进程)"]
        direction LR
        SK["Skills<br/>SKILL.md 加载"]
        CFG["Config<br/>配置管理"]
    end

    %% --- 外部 ---
    subgraph External["外部系统"]
        direction LR
        RN["Local Runner<br/>独立进程 · 远程/本机"]
        LLM["模型厂商 API<br/>Anthropic · OpenAI · Gemini · ..."]
    end

    DB[("SQLite<br/>~/.neocode/")]

    %% --- 连线 ---
    Client -->|"JSON-RPC"| GW
    GW -->|"同步调用"| RT
    RT --> Capability
    RT --> Support
    RT --> DB
    GW -.->|"SSE / WS 事件流"| Client
    GW -.->|"WebSocket<br/>tool_exec + heartbeat"| RN
    RT -.->|"HTTPS 流式"| LLM

    %% --- 样式 ---
    style GW fill:#fb923c,stroke:#fb923c,color:#0f172a
    style RT fill:#34d399,stroke:#34d399,color:#0f172a
    style PR fill:#a78bfa,stroke:#a78bfa,color:#0f172a
    style TL fill:#fbbf24,stroke:#fbbf24,color:#0f172a
    style CTX fill:#60a5fa,stroke:#60a5fa,color:#0f172a
    style SESS fill:#60a5fa,stroke:#60a5fa,color:#0f172a
    style SK fill:#60a5fa,stroke:#60a5fa,color:#0f172a
    style CFG fill:#60a5fa,stroke:#60a5fa,color:#0f172a
    style DB fill:#94a3b8,stroke:#94a3b8,color:#0f172a
    style RN fill:#fb7185,stroke:#fb7185,color:#0f172a
    style LLM fill:#94a3b8,stroke:#94a3b8,color:#0f172a
    style Client fill:#1e293b,stroke:#22d3ee,color:white
    style Capability fill:#1e293b40,stroke:#334155,color:white
    style Support fill:#1e293b40,stroke:#334155,color:white
    style External fill:#1e293b40,stroke:#475569,color:white
```

**图 7-1：NeoCode 容器图（C4 Level 2）。** 实线 = 同步调用；虚线 = 异步事件/外部通信。颜色：橙 = 协议边界、绿 = 编排中枢、紫 = 适配层、黄 = 工具层、蓝 = 支撑层、红 = 独立进程、灰 = 外部系统/数据存储。

**关键拓扑特征：**
- Gateway、Runtime、Provider、Tools、Session、Context、Skills、Config **共享同一进程**，通过 Go interface 解耦而非网络调用
- Local Runner 是**唯一可能跨越物理机边界**的容器（通过 WebSocket 反向连接 Gateway）
- Web UI 的静态资源嵌入在 Gateway 二进制中，不独立部署
- SQLite 是唯一的持久化存储，无需外部数据库进程

### 7.3 层间依赖规则

```mermaid
graph TB
    L0["第 0 层：客户端<br/>TUI / Web / Desktop / IM Adapter / CI/CD"]
    L1["第 1 层：Gateway<br/>协议路由 + 流中继 + 连接认证"]
    L2["第 2 层：Runtime<br/>ReAct 循环编排 + 权限决策 + 事件派发"]
    L3A["Provider<br/>模型适配"]
    L3B["Tools<br/>工具执行"]
    L3C["Context<br/>Prompt 构建"]
    L3D["Session<br/>状态持久化"]
    L4A["Skills<br/>可插拔行为注入"]
    L4B["Runner<br/>远程工具执行代理"]

    L0 -->|"依赖接口"| L1
    L1 -->|"依赖接口"| L2
    L2 -->|"依赖接口"| L3A
    L2 -->|"依赖接口"| L3B
    L2 -->|"依赖接口"| L3C
    L2 -->|"依赖接口"| L3D
    L3A -.->|"可选扩展"| L4A
    L3B -.->|"可选扩展"| L4B
```

**依赖铁律：** 上层只依赖下层契约（接口），绝不依赖具体实现。实线箭头 = 编译时依赖，虚线箭头 = 运行时可选绑定。

**跨层禁止事项（提炼自 AGENTS.md）：**
- `tui` 不得直接调用 `provider` 或 `tools`
- `runtime` 不得内嵌具体厂商字段或工具执行逻辑
- `gateway` 不得包含客户端特化逻辑（所有客户端通过统一 RPC 接入）
- 模型厂商差异不得泄漏到 `runtime` 或上层

### 7.4 核心设计决策

#### 决策 1：Gateway 作为唯一 RPC 边界

所有客户端（原生和第三方）必须通过 Gateway 与 Runtime 通信。Gateway 内部通过 `Action` 路由表将 JSON-RPC 请求帧分发到对应的处理器（`run`、`ask`、`cancel`、`resolve_permission` 等），并通过 `StreamRelay` 将 Runtime 的异步事件广播到订阅连接。

**选择理由：**
- 多端对等接入：TUI、Web、Desktop、IM Bot 在 Gateway 视角完全一致
- 安全收敛：认证、授权、速率限制集中在 Gateway 层，不需要每个客户端独立实现
- 协议统一：未来新增协议（如 gRPC）只需在 Gateway 增加 transport handler

**替代方案对比：**

| 方案 | 优点 | 缺点 | 为何不选 |
|------|------|------|----------|
| 各客户端直连 Runtime | 无中间层延迟 | 每个客户端需独立实现认证/鉴权/重连；Runtime 需理解多种传输协议；新增客户端成本高 | 违背"客户端对等"原则，安全面分散 |
| 纯 HTTP REST | 生态成熟、调试方便 | 流式推理结果需客户端轮询或长轮询，延迟高，实现不优雅 | AI 推理天然是流式的，SSE/WS 更适合 |
| **Gateway 统一 RPC** | 安全收敛、客户端对等、流式原生支持 | 增加一跳网络延迟（本地 loopback 可忽略） | ✅ 当前选择 |

#### 决策 2：Provider 作为一等公民的插件化抽象

`Provider` 接口仅定义两个方法：`EstimateInputTokens` 和 `Generate`（通过 channel 推送流式事件）。所有模型厂商差异（请求组装、响应解析、工具调用格式转换）收敛在各自的 Provider 实现中。

**选择理由：**
- 零侵入新增模型：现有已接入的 Provider 实现包括 Anthropic、OpenAI Compat（Qwen/GLM/通用）、Gemini、DeepSeek、MiniMax、Mimo
- 测试友好：Runtime 测试只需注入 Mock Provider，不依赖真实 API

**替代方案对比：**

| 方案 | 优点 | 缺点 | 为何不选 |
|------|------|------|----------|
| 统一的内部模型协议，由 Gateway 做协议转换 | Provider 实现更简单 | Gateway 成为瓶颈：每种新模型的流式格式差异需在 Gateway 处理；Gateway 职责膨胀 | 违反"Gateway 不感知模型差异"的边界原则 |
| 每个客户端自行集成模型 SDK | 无中间损耗 | 模型切换需更新所有客户端；安全密钥分散管理；无法做统一的 Token 预算管理 | 安全性和可维护性灾难 |
| **Provider 插件化** | 新增模型零侵入上层；Runtime 无厂商感知；测试可 Mock | 每个新厂商需写适配代码 | ✅ 当前选择 |

#### 决策 3：事件驱动的异步工具执行

Runtime 在 ReAct 循环中收到模型的 tool call 后，并行调度工具执行（默认并发度 4），并将结果回灌到消息历史。工具执行结果、权限请求、流式文本增量全部通过统一 `RuntimeEvent` channel 发出。

**选择理由：**
- Human-in-the-loop：`permission_request` 事件可被 Gateway 拦截，暂停执行等待用户决策
- 实时流式透出：文本增量事件经 SSE 流式推送到客户端，用户可见 AI "打字"过程
- 全链路可追踪：所有事件共享 `SessionID + RunID`

**替代方案对比：**

| 方案 | 优点 | 缺点 | 为何不选 |
|------|------|------|----------|
| 同步回调（工具执行完再统一返回） | 实现简单 | 工具执行期间客户端完全黑屏；不支持 Human-in-the-loop；不支持并行工具执行 | 用户体验差，无法满足 §4.4 可观测性要求 |
| 纯轮询（客户端定时查询执行状态） | 无长连接需求 | 延迟高、带宽浪费、无法支持实时权限审批 | 不适合流式推理场景 |
| **事件驱动 + Channel** | 实时流式、Human-in-the-loop、并行工具 | 需要客户端支持 SSE/WS 长连接 | ✅ 当前选择 |

---

### 7.5 关键角色与职责

以下从架构视角定义系统中的关键"角色"——这里说的不是代码中的类或模块，而是在运行时协作中承担明确职责的逻辑参与者。一个 Go 包可能承载多个角色；一个角色也可能跨多个包协作完成。

```mermaid
graph LR
    Client["客户端<br/>Client"] -->|"JSON-RPC"| Router["协议路由器<br/>Gateway"]
    Router -->|"事件流"| Orch["推理编排器<br/>Runtime"]

    Orch -->|"Generate()"| Adapter["模型适配器<br/>Provider"]
    Orch -->|"Execute()"| Executor["工具执行器<br/>Tools"]
    Orch -->|"Build()"| CtxBuilder["上下文构建器<br/>Context"]
    Orch -->|"Load/Save"| StateMgr["状态管理者<br/>Session"]

    Executor -->|"Check()"| Guard["安全守卫<br/>Security Engine"]
    Adapter -.->|"HTTPS"| LLM["外部模型厂商"]
    Executor -.->|"WebSocket"| RemoteAgent["远程执行代理<br/>Runner"]

    CtxBuilder -->|"激活/注入"| SkillInjector["技能注入器<br/>Skills"]
    Router -->|"Get/Update"| ConfigCoord["配置协调者<br/>Config"]
    Orch -->|"Get/Update"| ConfigCoord

    style Router fill:#fb923c,stroke:#fb923c,color:#0f172a
    style Orch fill:#34d399,stroke:#34d399,color:#0f172a
    style Adapter fill:#a78bfa,stroke:#a78bfa,color:#0f172a
    style Executor fill:#fbbf24,stroke:#fbbf24,color:#0f172a
    style Guard fill:#fb7185,stroke:#fb7185,color:#0f172a
    style CtxBuilder fill:#60a5fa,stroke:#60a5fa,color:#0f172a
    style StateMgr fill:#60a5fa,stroke:#60a5fa,color:#0f172a
    style SkillInjector fill:#60a5fa,stroke:#60a5fa,color:#0f172a
    style RemoteAgent fill:#fbbf24,stroke:#fbbf24,color:#0f172a
    style ConfigCoord fill:#60a5fa,stroke:#60a5fa,color:#0f172a
```

**图 7-3：关键角色关系图。** 实线箭头 = 同步调用依赖；虚线箭头 = 异步/外部通信。颜色：橙=协议边界、绿=编排中枢、紫=适配层、黄=执行层、红=安全、蓝=支撑角色。

| 角色 | 职责 | 关键约束 | 承载模块 |
|------|------|----------|----------|
| **协议路由器** | 将客户端 JSON-RPC 请求路由至正确的处理器；将 Runtime 异步事件中继至订阅的客户端连接；执行连接级认证与 Token 校验 | 不得包含任何客户端特化逻辑；不感知模型厂商差异 | Gateway |
| **推理编排器** | 驱动 ReAct 循环：调度上下文构建 → 模型推理 → 工具执行 → 结果回灌；管理 Token 预算与 Compact 触发；协调权限审批的暂停/恢复 | 不直接执行工具逻辑；不内嵌厂商字段；不跨层直连客户端 | Runtime |
| **模型适配器** | 归一化不同厂商的 Chat API 为统一的 `Generate()` + `EstimateInputTokens()` 接口；将厂商特定的流式响应格式转换为标准 `StreamEvent` | 厂商差异不得泄漏到 Runtime；每个 Adapter 独立部署、独立测试 | Provider |
| **工具执行器** | 暴露工具的 Schema 供模型选择；校验参数并执行工具调用；在每次执行前经过安全守卫的权限裁决 | 所有模型可调用的能力必须收敛于此角色；不可在 Runtime 或客户端中绕过 | Tools (Manager) |
| **安全守卫** | 基于策略规则（Priority 排序）裁决每个操作的 allow/deny/ask 决策；校验工作区边界（路径穿越检测、Symlink 解析）；管理会话级权限记忆 | 必须位于工具执行的关键路径上，不可被绕过 | Security Engine |
| **上下文构建器** | 按会话状态 + 预算阈值动态组装 System Prompt 和消息列表；执行上下文压缩（MicroCompact / Full Compact / Trim） | 压缩时不得丢失 System Prompt 和 Pin 标记的关键消息；组装顺序必须稳定 | Context |
| **状态管理者** | 持久化会话消息历史（SQLite）；管理 Checkpoint 快照的创建/恢复/修剪；执行过期会话的自动清理 | 单会话并发写必须串行化（sessionLock）；消息追加必须原子化 | Session |
| **技能注入器** | 从文件系统扫描 SKILL.md；管理会话级 Skill 激活状态；按激活列表将 Skill Prompt 注入 System Prompt 的技能段落 | project 层覆盖 global 层（同名去重）；单文件大小限制 1MB | Skills |
| **远程执行代理** | 在远程/本机独立进程中接收 Gateway 的工具执行请求；校验 Capability Token；在本地完成工具执行并返回结果 | 必须主动连接 Gateway（反向连接）；不可开放入站端口；受 WorkdirAllowlist 限制 | Runner |
| **配置协调者** | 管理配置文件加载、校验、热更新和持久化；维护跨会话的 Provider/Model 选择状态（`state.Service`）；协调多端的 Provider 切换一致性 | 密钥仅通过环境变量引用，永不入配置文件；配置变更通过回调通知下游 | Config |

### 7.6 可扩展性设计

NeoCode 的架构在多处预留了受控的扩展点。本节集中描述：**哪里可以扩展、怎么扩展、哪里不可以扩展以及为什么。**

#### 7.6.1 扩展点总览

| 扩展点 | 扩展什么 | 接口/契约 | 生效范围 | 侵入性 |
|--------|----------|----------|----------|--------|
| **Provider** | 新增模型厂商（如接入新的 LLM 服务） | 实现 `Provider` interface（2 方法）：`EstimateInputTokens` + `Generate` | 全局 | 仅需在 `provider/` 下新增包，上层零改动 |
| **Tools** | 新增模型可调用的工具能力 | 实现 `Executor` interface：`Name()` + `ListAvailableSpecs()` + `Execute()` + `Supports()`，注册到 `Registry` | 全局 | 工具 schema 自动进入模型上下文 |
| **MCP** | 动态挂载外部工具（无需写 Go 代码） | MCP stdio 协议（JSON-RPC 子进程） | 会话级或全局 | 零代码：配置 MCP server 路径即可 |
| **Skills** | 注入专用行为 Prompt（不改变工具列表） | 在指定目录下放置 `SKILL.md` 文件（YAML frontmatter + Markdown body） | 会话级（按需激活） | 零代码：文件即 Skill |
| **Client** | 新增客户端类型（如企业微信、Slack、自定义脚本） | 实现适配器：接收外部事件 → 转换为 Gateway JSON-RPC 请求 → 接收 SSE/WS 事件 → 转换为目标格式 | 全局 | Gateway 零改动；适配器独立进程 |
| **Hook** | 在 Runtime 生命周期节点注入自定义行为（如合规检查、自定义日志） | 在 hooks 配置目录下放置可执行文件；支持 `PreToolUse`、`PostToolUse`、`PreCompact`、`SessionStart`、`UserPromptSubmit` 等 hook point | 会话级或全局 | 零侵入：Hook 以子进程运行，通过 stdin/stdout JSON 通信 |
| **Transport** | 新增 Gateway 传输协议（如 gRPC、QUIC） | 实现 `transport.Listener` interface | 全局 | Gateway handler 逻辑不变，仅新增 transport |

#### 7.6.2 扩展机制详解

**Provider 扩展（最常用的扩展点）：**

```
新增模型厂商需要的步骤：
1. 在 internal/provider/ 下创建新包
2. 实现 Provider interface（EstimateInputTokens + Generate）
3. 将厂商特定的流式响应格式转换为统一的 StreamEvent
4. 在配置文件中添加 provider 条目（name + driver + base_url + api_key_env + models）
→ 完成。Runtime 和 Gateway 代码零改动。

为什么接口只有 2 个方法？
- 接口越大，实现成本越高，厂商差异泄漏的风险越大
- "估算 Token 数"和"发起推理"是模型调用的最小完备集
- 其他可变行为（重试策略、超时、模型发现）通过 RuntimeConfig 注入，不进入 interface
```

**工具扩展（有代码 vs 无代码两条路径）：**

| 路径 | 适用场景 | 成本 |
|------|----------|------|
| 实现 `Executor` interface（Go 代码） | 需要深度系统集成的新工具（如 Tree-sitter 代码分析） | 写 Go 代码 + 注册 |
| MCP stdio 子进程（零 Go 代码） | 外部团队的工具、已有 CLI 工具包装 | 配置 JSON 声明 server 路径 |

**客户端扩展（适配器模式）：**

```
第三方接入 NeoCode 的最小合约：
1. 能够发送 JSON-RPC 2.0 请求到 Gateway 的 /rpc 端点
2. 能够接收 SSE 或 WebSocket 事件流
3. (可选) 实现 gateway.authenticate 获取 subject_id

飞书 Adapter 就是按这个合约实现的第一个第三方客户端。
任何能发 HTTP POST + 解析 JSON 的环境（Python 脚本、Shell curl、Node.js 服务）
都可以成为 NeoCode 客户端。
```

#### 7.6.3 不可扩展的边界

以下设计决策是**刻意的不可扩展点**，修改它们意味着改变了系统的基本架构假设：

| 边界 | 为什么不可扩展 |
|------|---------------|
| **Gateway 是唯一的 RPC 入口** | 如果客户端绕开 Gateway 直连 Runtime，安全认证、流中继、客户端对等性全部失效。这是一个架构约束，不是技术限制 |
| **工具执行必经 Security Engine** | 如果某个工具绕过 PolicyEngine + WorkspaceSandbox，整个安全模型崩溃。工具执行路径不可旁路 |
| **Provider 差异不出 Provider 层** | 如果 Anthropic 的工具调用格式泄漏到 Runtime，新增 DeepSeek Provider 时就需修改 Runtime 代码。这是分层隔离的底线 |
| **配置文件不存明文密钥** | API Key 通过环境变量引用而非存入 YAML。如果开放此限制，密钥泄漏风险将从"单个环境变量"扩散到"配置文件 + 备份 + 版本控制" |

---

## 8. 核心模块设计

以下选取 9 个最具架构意义的模块，按层从上到下逐一描述。

### 8.1 Gateway（协议路由与多端接入边界）

| 属性 | 描述 |
|------|------|
| **模块职责** | 对外暴露统一的 RPC 接口（JSON-RPC / SSE / WebSocket），将客户端请求路由至 Runtime，并将 Runtime 的异步事件中继回客户端 |
| **输入** | 客户端 JSON-RPC 请求帧（`gateway.run`、`gateway.ask`、`gateway.cancel`、`gateway.resolve_permission` 等 action） |
| **输出** | JSON-RPC 响应帧（ack/error）、SSE 流式事件、WebSocket 双向消息 |
| **依赖** | `Runtime` 接口（下行依赖）、`Auth` 接口（Token 认证）、`transport`（底层连接传输） |
| **对外接口** | 通过 `transport` 包暴露 Unix domain socket（macOS/Linux）或 Windows named pipe，正迁移至全 RPC 方案 |
| **核心内部逻辑** | `dispatchRequestFrame`：将请求帧按 Action 路由到注册的 handler；`StreamRelay`：pub/sub 模式管理客户端订阅，将 Runtime 事件广播到匹配的 Session/Run 连接 |
| **设计理由** | 作为独立的协议路由层存在，而非嵌入 Runtime 或客户端：使得客户端实现对等化（任何能发 JSON-RPC 的程序都是客户端），且 Runtime 不需要理解任何传输协议 |
| **错误处理** | 未认证连接返回 `unauthorized`；不支持 action 返回 `unsupported_action`；Runtime 不可用时返回 `runtime_unavailable`；所有错误统一用 FrameError 包装 |
| **扩展点** | 新增 action handler 只需注册到 `registry`；新增传输协议（如 gRPC）只需实现 `transport.Listener` |

**架构价值论证：** 如果系统中没有 Gateway 这个独立的协议路由层会怎样？每个客户端（TUI/Web/Desktop/飞书）都需要在 Runtime 中实现各自的认证、传输、流中继逻辑。新增一种客户端类型意味着修改 Runtime——这违反了开闭原则。更严重的是，安全认证逻辑分散在 N 个客户端的实现中，一旦发现漏洞需要修复 N 处。Gateway 的存在将 N×M 的复杂度（N 个客户端 × M 个能力）降低为 N+M。这是 NeoCode 架构中**投资回报率最高的单一设计决策**。

`[代码位置: internal/gateway/ — bootstrap.go (帧路由), network_server.go (HTTP/WS/SSE), stream_relay.go (事件广播), contracts.go (类型契约)]`

### 8.2 Runtime（ReAct 循环与会话编排）

| 属性 | 描述 |
|------|------|
| **模块职责** | 编排完整的 Agent 推理循环：接收用户输入 → 构建上下文 → 调用 Provider 推理 → 解析工具调用 → 执行工具 → 回灌结果 → 循环，直到模型产出最终文本回复 |
| **输入** | `PrepareInput`（文本 + 图片 + 会话上下文）、`AskInput`（轻量问答）、`SystemToolInput`（系统级工具调用） |
| **输出** | `RuntimeEvent` channel（`run_progress` / `run_done` / `run_error` / `ask_chunk` / `ask_done` / `ask_error` 等事件类型） |
| **依赖** | `Provider`（模型推理）、`tools.Manager`（工具执行）、`context.Builder`（Prompt 构建）、`session.Store`（会话持久化）、`security.PolicyEngine`（权限决策） |
| **对外接口** | `Runtime` interface：`Submit`、`Run`、`Ask`、`Compact`、`CancelActiveRun`、`ResolvePermission`、`Events()`、`ListSessions`、`LoadSession` 等 |
| **核心内部逻辑** | ReAct Loop：`prepareContext()` → `provider.Generate()` → 解析 `tool_call` → `executeTools()`（并行，默认并发度 4） → 回灌 `tool_result` → 循环；Compact 调度器在 Token 预算达到阈值时自动触发上下文压缩 |
| **设计理由** | Runtime 是系统的神经中枢——将所有能力（推理、工具、安全、记忆）编排为统一循环，但自身不实现任何具体能力。这种"指挥不执行"的设计使得每个子能力可独立演进 |
| **错误处理** | Provider 返回错误时根据重试策略自动重试（`generate_max_retries`）；工具执行超时或失败时返回 error 类型的 tool_result（不回灌异常退出循环）；不可恢复错误以 `run_error` 事件发出 |
| **扩展点** | Hook 系统（`runtime/hooks`）：在关键生命周期节点（`PreToolUse`、`PostToolUse`、`PreCompact` 等）注入自定义行为；`approval` 子包提供计划审批能力 |

**架构价值论证：** Runtime 的设计遵循"指挥不执行"原则——它知道推理循环的每个步骤何时应该发生，但不知道每个步骤的具体实现细节。这意味着 Runtime 本身不包含任何模型厂商字段、工具执行代码或上下文构建规则。当需要支持一种新模型、新增一个工具或优化 Compact 策略时，修改范围被严格限制在对应的子模块内。如果 Runtime 内嵌了工具逻辑（例如直接在 Runtime 中写文件读写），那么新增一个工具就需要修改 Runtime——这在 5 人并行开发的团队中意味着持续的合并冲突和回归风险。Runtime 的"薄编排层"设计是用**初期的接口抽象成本换取长期的并行演进能力**。

`[代码位置: internal/runtime/ — runtime.go (接口定义), run.go (ReAct 主循环), compact.go (压缩调度), session_scheduler.go (并发控制)]`

### 8.3 Provider（多模型厂商协议适配）

| 属性 | 描述 |
|------|------|
| **模块职责** | 归一化不同模型厂商的 Chat API 为统一接口：`EstimateInputTokens` + `Generate(ctx, req, events chan)` |
| **输入** | `GenerateRequest`（SystemPrompt + Messages + Tools + 运行时配置） |
| **输出** | 通过 `chan StreamEvent` 推送流式事件（`text_delta`、`tool_call_start`、`tool_call_args`、`tool_call_end`、`usage`、`error`） |
| **依赖** | 无（独立层，不依赖 NeoCode 其他模块，仅依赖厂商 SDK） |
| **对外接口** | `Provider` interface（2 个方法） |
| **核心内部逻辑** | 每个 Provider 实现负责：请求参数映射（模型名、温度、max_tokens 等）→ HTTP 调用 → 流式响应解析 → 统一 `StreamEvent` 格式输出；`CatalogInput` + 模型发现机制支持动态获取厂商可用模型列表 |
| **设计理由** | 作为一等公民插件化存在：新增模型厂商只需写一个 adapter，Runtime 和 Gateway 零改动。统一使用 channel 而非 callback，与 Go 的并发模型一致 |
| **错误处理** | 认证错误（401/403）→ 包装为 `AuthError`；限流错误（429）→ 包装为 `RateLimitError` 供 Runtime 决定重试策略；流式中断 → `StreamError` 事件 |
| **扩展点** | `openaicompat` 子包提供 OpenAI 兼容协议的通用适配器，新厂商若兼容 OpenAI API 格式，只需配置对应 base URL + API Key 即可接入 |

`[代码位置: internal/provider/ — contracts.go (Provider interface, RuntimeConfig), chat_endpoint.go, chat_api_mode.go; 实现: anthropic/, openaicompat/ (含 qwen/glm), gemini/, deepseek/, minimax/, mimo/]`

### 8.4 Tools（工具执行与安全策略引擎）

| 属性 | 描述 |
|------|------|
| **模块职责** | 提供模型可调用的全部能力的 Schema 暴露、参数校验、安全决策和执行，是工具能力的唯一入口 |
| **输入** | `ToolCallInput`（tool_name + arguments JSON + session context） |
| **输出** | `ToolResult`（content + is_error + metadata） |
| **依赖** | `security`（权限策略引擎）、操作系统资源（文件系统、Shell、网络） |
| **对外接口** | `Manager` interface：`ListAvailableSpecs`、`Execute`、`RememberSessionDecision`；`Executor` interface：每个工具实现此接口 |
| **核心内部逻辑** | 执行路径：`Manager.Execute()` → 权限检查（`security.PolicyEngine.Check()`） → 若决策为 `ask` 则返回 `PermissionDecisionError` 等待 Runtime 审批 → 若通过则委托给具体 `Executor` → 结果裁剪与格式化；`factsEnrichingExecutor` 包装层在工具结果上自动补齐受信的结构化事实 |
| **设计理由** | 将"哪些能力可被 AI 调用"和"每次调用如何安检"收敛为统一入口：安全审计只需检查这一个模块。所有物理世界交互（文件、Bash、网络）通过 interface 抽象，测试时可完全 Mock |
| **错误处理** | 权限拒绝 → `PermissionDecisionError`（Runtime 等待用户决策）；超时 → 按工具类型返回截断结果 + warning；工具自身异常 → 包装为 error 类型的 ToolResult（不中断 ReAct 循环） |
| **扩展点** | 新增工具：实现 `Executor` interface → 注册到 `Registry` → Schema 自动进入模型上下文；MCP 工具通过 `mcp/` 子包动态挂载（stdio 子进程协议） |

`[代码位置: internal/tools/ — manager.go (Manager/Executor 接口), registry.go; 实现: bash/, filesystem/, codebase/, webfetch/, mcp/, memo/, spawnsubagent/, todo/, diagnose/; 安全引擎: internal/security/ — policy.go (PolicyEngine), workspace.go (WorkspaceSandbox), types.go (Action/Decision)]`

#### 8.4.1 已有工具清单（Inferred from `internal/tools/`）

| 工具名 | 分类 | 说明 |
|--------|------|------|
| `bash` | 系统执行 | 执行 Shell 命令，含 Git 语义分类（只读/远端操作/破坏性） |
| `filesystem_read_file` | 文件系统 | 读取文件内容 |
| `filesystem_write_file` | 文件系统 | 写入/创建文件 |
| `filesystem_edit` | 文件系统 | 基于字符串精确替换的原地编辑 |
| `filesystem_glob` | 文件系统 | 文件名模式匹配 |
| `filesystem_grep` | 文件系统 | 文件内容正则搜索 |
| `filesystem_copy_file` | 文件系统 | 复制文件 |
| `filesystem_move_file` | 文件系统 | 移动/重命名文件 |
| `filesystem_create_dir` | 文件系统 | 创建目录 |
| `filesystem_delete_file` | 文件系统 | 删除文件 |
| `filesystem_remove_dir` | 文件系统 | 删除目录 |
| `codebase_read` | 代码库 | 读取代码文件（含语义增强） |
| `codebase_search_text` | 代码库 | 基于文本搜索代码库 |
| `codebase_search_symbol` | 代码库 | 基于 Tree-sitter 的跨语言符号搜索 |
| `webfetch` | 网络 | 获取 URL 内容（限制协议与响应大小） |
| `todo_write` | 任务管理 | 创建/更新 Todo 列表 |
| `memo_list` / `memo_remember` / `memo_recall` / `memo_remove` | 记忆 | 跨会话结构化记忆管理 |
| `diagnose` | 诊断 | 分析终端异常输出并给出建议 |
| `spawn_sub_agent` | 编排 | 创建子代理处理独立子任务 |
| MCP 工具（动态） | 扩展 | 通过 MCP stdio 协议挂载的外部工具 |

### 8.5 Session（会话领域模型与 SQLite 持久化）

| 属性 | 描述 |
|------|------|
| **模块职责** | 管理会话的完整生命周期：创建、持久化、查询、过期清理；存储消息历史、Token 统计、Todo 列表、Plan 快照和 Skills 激活状态 |
| **输入** | Session ID + 消息记录 + Token 计数增量 + 状态变更（Skill 激活 / Todo 更新 / Plan 快照） |
| **输出** | `Session` 聚合根、`Summary` 列表视图、`CheckpointRecord` 代码快照记录 |
| **依赖** | SQLite（modernc 纯 Go 实现，零 CGO） |
| **对外接口** | `Store` interface：`CreateSession`、`LoadSession`、`AppendMessages`、`UpdateSessionHead`、`CleanupExpiredSessions` 等；`CheckpointStore` interface（参见下文 8.5.1） |
| **核心内部逻辑** | 会话数据分两层存储：`SessionHead`（轻量头状态，高频更新）和 `Messages`（消息表，顺序追加）；单会话最多保留 8192 条消息，超出自动裁剪最旧条目；过期会话（30 天未更新）自动清理 |
| **设计理由** | 集中式会话管理确保多端共享同一 Runtime 时状态一致。SQLite 选型避免外部数据库依赖，维持"单一二进制"部署形态 |
| **错误处理** | `ErrSessionNotFound` → Runtime 创建新会话；`ErrSessionAlreadyExists` → 并发冲突处理；数据库写入失败 → 包装为内部错误，上层决定重试或降级 |

`[代码位置: internal/session/ — store.go (Store interface), sqlite_store.go (SQLite 实现), id.go (ID 生成), asset_store.go; internal/checkpoint/ — checkpoint_manager.go (CheckpointStore), bash_capture.go]`

#### 8.5.1 Checkpoint（代码版本快照）

NeoCode 实现了轻量级的本地代码版本快照系统。每次 AI 执行写操作前（`pre_write`）、Plan Mode 切换时、上下文压缩时（`compact`），系统会自动创建代码快照（Checkpoint），记录当时的文件状态。快照数据存储在 Session SQLite 数据库中，支持恢复（Restore）和过期修剪（Prune）。这套机制与 Git 并存：有 `.git` 目录时优先使用 Git 的版本追踪；无 `.git` 时 Checkpoint 提供独立的安全网。

### 8.6 Context（Prompt 构建与上下文压缩）

| 属性 | 描述 |
|------|------|
| **模块职责** | 按照会话状态、预算阈值和激活 Skills 动态构建发送给 Provider 的完整 Prompt（System Prompt + 消息列表），并在触发压缩阈值时执行上下文裁剪 |
| **输入** | `Session`（含消息历史、Todo、Plan、Skills、AgentMode 等）、`MicroCompactConfig`（压缩策略配置） |
| **输出** | 组装完成的 `GenerateRequest`（SystemPrompt + Messages），供 Runtime 直接传递给 Provider |
| **依赖** | `repository`（代码库上下文）、`rules`（外部规则文件）、`promptasset/templates`（模板资源）、`session`（会话状态读取） |
| **对外接口** | `Builder` interface：`Build(ctx, session)` → Prompt sections |
| **核心内部逻辑** | System Prompt 组装顺序：`corePrompt` → `capabilities` → `rules` → `taskState` → `planModeContext` → `todos` → `skillPrompt` → `repositoryContext` → `systemState`。每条 section 独立构建、独立配置是否参与 Compact 裁剪。Compact 策略：基于 Token 预算动态修剪历史消息（保留 System Prompt + 最近 N 轮消息 + 关键工具调用结果）。MicroCompact：在单次额度即将耗尽时，对单条消息的工具结果进行摘要化压缩 |
| **设计理由** | 独立的 Context 层将"AI 看到了什么上下文"与 Runtime 推理循环解耦，支持多种 Compact 策略（全量压缩、微压缩、预算感知）独立演进，Runtime 不关心上下文构建细节 |
| **错误处理** | Token 估算偏差 → 预算超限时自动触发 Compact 补救；模板文件缺失 → 降级使用最小 Prompt（仅 systemState）；规则文件加载失败 → 跳过该 section，不阻断 Prompt 构建 |
| **扩展点** | `SectionSource` interface 允许注入新的 Prompt section（如自定义分析报告）；`MicroCompactConfig` 支持按工具类型配置不同的压缩策略 |

`[代码位置: internal/context/ — builder.go (Builder interface, DefaultBuilder), compact_prompt.go, microcompact.go, trim.go, trim_policy.go; internal/context/compact/ — planner.go (Compact Planner)]`

### 8.7 Skills（可插拔行为注入引擎）

| 属性 | 描述 |
|------|------|
| **模块职责** | 从本地文件系统扫描和加载 `SKILL.md` 文件，按会话级激活状态将对应 Prompt 注入 System Prompt，实现特定任务的专用行为和流程 |
| **输入** | 本地目录（project 级 `.claude/skills/` 或 global 级 `~/.claude/skills/`）+ 会话激活列表 |
| **输出** | 已解析的 `Descriptor` 列表（含 name、description、source layer、prompt body、allowed tools、model 等元数据） |
| **依赖** | 文件系统（读取 `SKILL.md`） |
| **对外接口** | `LocalLoader`：`Load()` → 按目录递归扫描 `SKILL.md`；`ActivationMgr`：管理会话级 Skill 激活状态 |
| **核心内部逻辑** | 两层来源（project > global）+ 去重规则（同 name 时 project 覆盖 global）；Skill 文件大小限制（默认 1MB）；激活的 Skill 其 prompt body 通过 Context 的 `skillPromptSource` 注入 System Prompt 的技能段落 |
| **设计理由** | Skills 机制是 NeoCode 可扩展性的关键：不修改任何 Go 代码即可为 Agent 添加专用行为（如"PDF 处理"、"飞书消息格式化"、"特定框架的代码生成规范"）。与 MCP 互补：Skills 扩展 System Prompt（指示 AI "怎么做"），MCP 扩展工具列表（赋予 AI "能做的新能力"） |
| **错误处理** | 单个 Skill 文件解析失败 → 跳过该 Skill，不影响其他 Skill 加载；Skill 文件超过大小限制 → 返回 `errSkillFileTooLarge`，该 Skill 不可用但不会阻断启动 |
| **扩展点** | 通过 `SourceLayer` 机制支持多层来源（user / project / global）；通过 `validateDescriptor` 注入自定义校验逻辑 |

`[代码位置: internal/skills/ — loader.go (LocalLoader, Descriptor), filter.go (激活管理)]`

### 8.8 Runner（远程工具执行代理）

| 属性 | 描述 |
|------|------|
| **模块职责** | 作为独立进程运行在远程/本机，通过 WebSocket 主动连接 Gateway，接收工具执行请求并在本地完成执行，将结果返回给 Gateway |
| **输入** | `ToolExecutionRequest`（通过 WebSocket 从 Gateway 接收，含 tool_name、arguments、capability_token） |
| **输出** | `ToolExecutionResult`（通过 WebSocket 返回 Gateway，含 content、is_error） |
| **依赖** | `tools.Manager`（本地工具执行）、`security.CapabilityToken`（子代理能力令牌校验） |
| **对外接口** | `neocode runner` CLI 命令 + WebSocket 双向消息协议 |
| **核心内部逻辑** | 启动时与 Gateway 建立 WebSocket 长连接 → 发送 `register_runner` 注册 → 维持心跳（默认 10s 间隔） → 收到工具执行请求 → `capSigner.VerifyCapabilityToken()` 校验令牌 → `toolMgr.Execute()` 执行工具 → 返回结果；断线时自动重连（指数退避：500ms ~ 10s） |
| **设计理由** | Runner 是 NeoCode 独特拓扑的核心差异化能力：实现"手机飞书发指令 → Gateway 在云端/本地 → Runner 在工位电脑上执行工具"的反向代理模式。与 Gateway 通过 WebSocket 主动连接（而非 Gateway 连接 Runner），使得 Runner 可位于 NAT/防火墙后，无需开放入站端口 |
| **错误处理** | `ErrCapabilityTokenRequired` / `ErrCapabilityTokenExpired` / `ErrCapabilitySignatureInvalid` → 拒绝执行；`ErrRunnerStopped` → 优雅退出，不泄露资源；WebSocket 断连 → 自动重连，重连期间收到的请求会在重连后重试 |
| **扩展点** | 通过 `WorkdirAllowlist` 限制 Runner 可访问的工作目录范围；通过 `CapSigner` 签名机制控制子代理的能力边界 |

`[代码位置: internal/runner/ — runner.go (Runner 主逻辑), types.go (ToolExecutionRequest/Result), capability.go (CapSigner)]`

### 8.9 Config（配置管理与运行时状态）

| 属性 | 描述 |
|------|------|
| **模块职责** | 管理配置文件的加载、校验、热更新和持久化；维护 Provider 选择状态（当前使用的 Provider/Model 映射）作为运行时协调中枢 |
| **输入** | YAML 配置文件（`~/.neocode/config.yaml`）、环境变量、CLI 参数 |
| **输出** | 不可变配置快照（`Config` 结构体）、Provider 选择状态变更通知 |
| **依赖** | Viper（配置解析）、文件系统（读写配置文件）、`ProviderIdentity`（厂商身份校验） |
| **对外接口** | `Manager`：`Load(ctx)` → 加载并校验配置；`Get()` → 获取线程安全的配置快照（copy-on-read）；`Update(ctx, mutateFn)` → 原子更新配置并持久化；`Save(ctx)` → 落盘当前配置；`state.Service`：管理 `selected_provider` / `current_model` / `current_thinking` 等跨会话选择状态，支持变更回调 |
| **核心内部逻辑** | 配置加载流程：读取 YAML → 解析 Provider 列表 → 校验（名称唯一性、必填字段、超时参数范围） → 归一化（补齐默认值） → 写入 `ConfigManager`。Provider 选择状态通过 `state.Service` 管理：切换 Provider/Model 时自动校验新选择的合法性（Provider 是否存在、Model 是否在目录中），变更后触发回调通知下游（如 Gateway 同步更新会话元数据）；`AcquireProviderCreateLock()` 提供 Provider 实例创建的互斥锁，防止并发创建重复连接 |
| **设计理由** | Config 是实现"配置先行"原则（§5.4 原则 4）的工程载体：所有环境差异项（超时、模型名、路径、Shell）通过配置注入而非硬编码。`state.Service` 将运行时选择状态集中管理，解决了多端并发切换 Provider/Model 的一致性问题——任何一端切换，其他端通过 Gateway 同步感知 |
| **错误处理** | 配置文件不存在 → 使用 `StaticDefaults()` 骨架 + 校验失败提示用户编辑；Provider 名重复 → 启动时立即失败（fail-fast）；非法的 selected_provider / current_model → 启动时校验拒绝，回退到默认值；文件写入失败 → 返回错误，内存状态不回滚 |
| **扩展点** | 新增配置项：在 `Config` 结构体中添加字段 → 在 `ApplyDefaults` 中补齐默认值 → 在 `ValidateSnapshot` 中添加校验规则；新增 Provider 选择维度（如 thinking 模式）通过 `state.Service` 扩展而不影响现有逻辑 |

`[代码位置: internal/config/ — config.go (Config 结构体, StaticDefaults, ValidateSnapshot), manager.go (Manager: Load/Get/Update/Save); internal/config/state/ — service.go (Provider/Model 选择状态)]`

---

## 9. 核心流程与动态视图

以下选取 5 个最具架构意义的运行时流程，逐一描述触发条件、参与组件、关键步骤和异常路径。

### 9.1 主 ReAct 推理循环（Run Flow）

**触发条件：** 客户端通过 Gateway 发送 `gateway.run` 请求，由 Runtime.Submit / Runtime.Run 入口进入。

**参与组件：** Gateway → Runtime → Context Builder → Provider → Tools Manager → Security Engine → Session Store

**流程：**

```mermaid
sequenceDiagram
    participant Client as 客户端
    participant GW as Gateway
    participant RT as Runtime
    participant CTX as Context
    participant PR as Provider
    participant TL as Tools

    Client->>GW: gateway.run (JSON-RPC)
    GW->>RT: PrepareInput → Run
    RT->>CTX: Build(session)
    CTX-->>RT: [SystemPrompt + Messages]
    RT->>PR: Generate(req)
    PR-->>RT: [StreamEvent: text_delta, tool_call...]
    RT-->>GW: emit(run_progress)
    GW-->>Client: [SSE: progress event]

    alt 模型产出 tool_calls
        RT->>TL: Execute(toolCalls) [并行≤4]
        TL-->>RT: [ToolResult]
        RT->>RT: 回灌 tool_result 到消息历史
        RT->>PR: Generate(req) [继续]
        PR-->>RT: [StreamEvent ...]
    else 模型产出最终文本
        RT-->>GW: emit(run_done)
        GW-->>Client: [SSE: run_done]
    end
```

**图 9-1：ReAct 推理循环时序图。** 虚线箭头 = 异步事件；实线箭头 = 同步调用。工具执行在独立 goroutine 中并行调度。

**步骤详解：**

1. **输入归一化**（`PrepareInput` → `UserInput`）：Gateway 将客户端 JSON-RPC 请求转换为领域模型，附加 RunID、SessionID、Workdir、CapabilityToken 等元数据
2. **会话加载与加锁**（`loadOrCreateSession`）：从 SQLite 加载会话；同一 SessionID 的并发 Run 通过 `sessionLock` 串行化（不同会话可并行）
3. **Hook 执行**：触发 `SessionStart` 和 `UserPromptSubmit` 生命周期钩子；若 Hook 返回 `Blocked`，Run 终止并返回阻断原因
4. **用户消息追加**：将用户输入 Parts 作为 `user` 角色消息 append 到会话消息列表
5. **主循环（ReAct Loop）**：
   - `prepareTurnBudgetSnapshot`：检查 Token 预算，若超阈值触发 Compact（参见 §9.2）
   - Context Builder 组装完整 Prompt（参见 §8.6）
   - Provider.Generate 发起流式推理，通过 channel 推送 `StreamEvent`
   - 解析模型回复中的 `tool_calls`：若无工具调用 → 循环结束，产出最终文本
   - 并行执行工具（`executeTools`，默认并发度 4）：每个工具调用经过 Security Engine → Executor → 结果回灌
   - 循环回到步骤 5a，直到模型产出纯文本回复或达到 `max_turns` 上限
6. **终止处理**：发送 `run_done` 事件（含 Token Usage 汇总、Diff 摘要）；更新 Resume Checkpoint；释放会话锁

**异常路径：**

| 异常 | 处理策略 |
|------|----------|
| `max_turns` 达到上限（默认由配置控制） | 发送 `run_error` + `max_turn_limit` 原因；最后一次推理结果仍保留在会话中 |
| Provider 返回错误（网络/限流/认证） | 按 `generate_max_retries` 配置自动重试（在 turn 内最多 `max_attempts` 次）；不可恢复时 `run_error` |
| 工具执行超时 | 返回 error 类型的 ToolResult 回灌给模型（不中断循环），让模型决定如何处理 |
| 用户手动取消（`CancelActiveRun`） | context 取消传播 → `run_error` + `cancelled` 原因 |
| 循环检测（重复工具调用签名） | 若连续 3 轮工具签名相同，注入自愈提醒 Prompt（`NoProgressReminder`）引导模型改变策略 |

### 9.2 上下文压缩流程（Compact Flow）

**触发条件：** 每轮推理前 `prepareTurnBudgetSnapshot` 检测到 Token 消耗接近预算阈值（基于 Provider 的 `EstimateInputTokens` 估算 + 配置的 `compact_trigger_ratio`）。

**参与组件：** Runtime → Context Builder → MicroCompact → Compact Runner (Provider) → Session Store

**流程：**

```mermaid
sequenceDiagram
    participant RT as Runtime
    participant CC as Context/Compact
    participant SS as Session Store

    RT->>CC: BudgetSnapshot(session)
    CC-->>RT: needsCompact = true

    RT->>CC: Compact(input)
    CC->>CC: MicroCompact: 对可压缩 tool_result 摘要化
    alt MicroCompact 不足
        CC->>CC: Full Compact: CompactRunner.Generate() 生成结构化摘要
    end
    CC->>CC: Trim: 裁剪最旧消息（保留 System Prompt + Pin 标记）

    RT->>SS: SaveCompacted()
    SS->>SS: ReplaceTranscript() 原子替换消息列表 + 更新 SessionHead
```

**两级压缩策略：**

| 级别 | 触发条件 | 操作 | 对上下文的影响 |
|------|----------|------|---------------|
| **MicroCompact** | 单次 Tool Call 结果过大，导致本轮预算紧张 | 对单个 tool_result 内容摘要化（保留关键输出，丢弃冗长中间日志） | 仅影响当前工具结果，不改变历史 |
| **Full Compact** | MicroCompact 后仍超预算，或累计历史消息过多 | 将历史消息中可压缩的部分通过 LLM 生成结构化摘要，替换原始消息 | 历史消息被摘要替代，System Prompt + 最近 N 轮保留 |

**关键不变量：**
- System Prompt（corePrompt + capabilities + rules + skillPrompt）永不参与压缩
- Pin 标记的消息（如用户原始问题、Plan 批准记录）不被压缩
- 压缩后的消息列表通过 `ReplaceTranscript` 原子替换（单事务），确保不会出现半压缩状态

### 9.3 权限决策流程（Permission Resolution Flow）

**触发条件：** 工具执行前，Tools Manager 调用 Security Engine 检查操作权限；若决策为 `ask`，Runtime 暂停执行并等待用户决策。

**参与组件：** Runtime → Tools Manager → Security Engine（PolicyEngine + WorkspaceSandbox）→ Gateway → 客户端

**流程：**

```mermaid
sequenceDiagram
    participant RT as Runtime
    participant TM as Tools Manager
    participant SE as Security Engine
    participant GW as Gateway
    participant Client as 客户端

    RT->>TM: Execute(toolCall)
    TM->>SE: Check(action)
    SE->>SE: 匹配策略规则（Priority 降序）
    SE-->>TM: Decision: ask
    TM-->>RT: PermissionDecisionError
    RT->>GW: emit(permission_request)
    GW-->>Client: [SSE: permission_request]
    Client->>GW: resolve_permission (JSON-RPC)
    GW->>RT: ResolvePermission(allow_once / allow_session / reject)
    alt allow_once 或 allow_session
        RT->>TM: Execute(toolCall) [重试]
        opt allow_session
            TM->>TM: RememberSessionDecision(PermissionFingerprint)
        end
    else reject
        RT->>RT: 返回 error ToolResult
    end
```

**图 9-3：权限决策时序图。** 关键特征：Runtime 在 `ask` 状态下暂停执行，等待客户端通过 JSON-RPC 回传决策。

**安全决策链：**

```mermaid
flowchart TD
    A["Action<br/>(ToolName + Resource + Target)"] --> B["PolicyEngine.Check()"]
    B -->|"匹配规则（按 Priority 降序）"| C{"命中规则?"}
    C -->|"是"| D["返回 Rule.Decision"]
    C -->|"否"| E["返回 defaultDecision<br/>(通常为 ask)"]
    D --> F["WorkspaceSandbox.Check()"]
    E --> F
    F -->|"路径穿越检测 (../、symlink)"| G{"安全?"}
    G -->|"否"| H["deny"]
    G -->|"是"| I["工作区边界检查"]
    I -->|"越界"| J["deny<br/>(生成 safe 候选路径)"]
    I -->|"通过"| K["allow"]
```

**图 9-4：安全决策链。** 两阶段检查：先过策略引擎（PolicyEngine），再过沙箱（WorkspaceSandbox）。任一阶段拒绝即终止。

**三类决策含义：**

| 决策 | 含义 | 后续行为 |
|------|------|----------|
| `allow` | 安全策略明确放行 | 直接执行 |
| `deny` | 安全策略明确拒绝 | 返回 error ToolResult，不询问用户 |
| `ask` | 需用户判断 | Runtime 暂停，发送 `permission_request` 事件，等待用户通过 `resolve_permission` 回复 |

**会话级记忆：** 用户选择 `allow_session` 后，Manager 调用 `RememberSessionDecision` 将决策持久化到会话权限记忆表，该会话后续同类操作自动放行（基于 `PermissionFingerprint` 匹配）。

### 9.4 Runner 远程工具执行流程

**触发条件：** Gateway 收到工具执行请求，且该 Runner 已注册并空闲，Gateway 将请求通过 WebSocket 转发至远程 Runner 执行。

**参与组件：** Gateway → (WebSocket) → Runner → Tools Manager → Security Engine

**流程：**

```mermaid
sequenceDiagram
    participant GW as Gateway
    participant RN as Runner (远程)
    participant TL as Tools/文件系统

    GW->>GW: 选择已注册的 Runner
    GW->>RN: WebSocket: tool_exec_request
    RN->>RN: capSigner.Verify(token)
    RN->>TL: toolMgr.Execute()
    TL-->>RN: [ToolResult]
    RN->>GW: WebSocket: tool_exec_result

    loop 每 N 秒
        GW->>RN: WebSocket: heartbeat
        RN->>GW: WebSocket: heartbeat_ack
    end
```

**关键机制：**

| 机制 | 说明 |
|------|------|
| **反向连接** | Runner 主动连接 Gateway（而非 Gateway 连接 Runner），因此 Runner 可位于 NAT/防火墙后 |
| **心跳保活** | 默认 10s 间隔；超时未收到 ack 则判定断连，触发重连 |
| **自动重连** | 指数退避（500ms → 1s → 2s → ... → 10s max） |
| **Capability Token** | 每个工具执行请求附带签名令牌，校验 Runner 是否有权执行该工具及访问目标路径 |
| **工作区白名单** | `WorkdirAllowlist` 限制 Runner 只能访问指定目录，即使 Capability Token 签名有效 |

### 9.5 会话生命周期

**触发条件：** 用户首次发起对话（创建） / 每次 Run（加载 + 追加） / 系统定时任务（清理）。

**状态流转：**

```mermaid
stateDiagram-v2
    [*] --> Empty: CreateSession()
    Empty --> Active: LoadSession()
    Active --> Updated: AppendMessages()
    Updated --> Updated: AppendMessages()
    Updated --> Compacted: Compact()
    Active --> Compacted: Compact()
    Active --> Expired: 30天无更新
    Updated --> Expired: 30天无更新
    Compacted --> Expired: 30天无更新
    Expired --> [*]: CleanupExpiredSessions()
```

**图 9-2：会话生命周期状态机。** Active/Updated/Compacted 均为活跃状态；30 天阈值通过 `DefaultSessionMaxAge` 配置。

**数据生命周期：**

| 数据类型 | 存储位置 | 生命周期 |
|----------|----------|----------|
| 会话消息历史 | SQLite `messages` 表 | 单会话最多 8192 条；超出自动裁剪最旧消息；30 天未更新则整体清理 |
| 会话头状态 | SQLite `sessions` 表 | 与会话同生命周期 |
| Checkpoint 快照 | SQLite `checkpoint_records` 表 | 每个 Run 内保留；自动修剪（可配置保留数） |
| 权限记忆 | SQLite session_permission 表 | 跟随会话，会话删除时清理 |
| 代码变更 PerEdit | Checkpoint 文件系统存储 | Run 结束时捕获 Diff 摘要，关联 Checkpoint ID 引用 |
| 配置文件 | `~/.neocode/config.yaml` | 持久保留，手动修改 |

### 9.6 端到端场景走查：一次典型的 AI 代码修改

以下用一个具体的用户故事串联 §9.1-§9.5 的五个流程，展示各模块在真实场景中如何协作。

> **场景：** 用户在工作区中打开 NeoCode CLI，输入：*"帮我在 auth.go 的 Login 函数里加一个登录失败次数限制，超过 5 次就锁定 30 秒"*

```mermaid
sequenceDiagram
    actor User as 用户 (CLI)
    participant GW as Gateway
    participant RT as Runtime
    participant CTX as Context
    participant PR as Provider
    participant TL as Tools
    participant SE as Security
    participant SS as Session

    Note over User,SS: ══════════ 第 1 轮：理解需求 ══════════

    User->>GW: run "帮我在 auth.go 加登录限流"
    GW->>RT: PrepareInput → Run (RunID=r1)
    RT->>SS: LoadSession / LockSession
    RT->>CTX: Build(session) → SystemPrompt + Messages
    CTX-->>RT: [Prompt: 含 codebase 上下文]
    RT->>PR: Generate(prompt)
    PR-->>RT: [StreamEvent: tool_call: codebase_read("auth.go")]
    RT->>TL: Execute(codebase_read, "auth.go")
    TL->>SE: Check(read, "auth.go")
    SE-->>TL: allow
    TL-->>RT: [auth.go 内容: func Login()...]
    RT->>RT: 回灌 tool_result 到消息历史

    Note over User,SS: ══════════ 第 2 轮：写代码 + 权限决策 ══════════

    RT->>PR: Generate(继续)
    PR-->>RT: [StreamEvent: tool_call: filesystem_edit("auth.go", old_str, new_str)]
    RT->>TL: Execute(filesystem_edit, "auth.go")
    TL->>SE: Check(write, "auth.go")
    SE-->>TL: ask (首次写入此文件)
    TL-->>RT: PermissionDecisionError
    RT->>GW: emit(permission_request)
    GW-->>User: [SSE: "是否允许编辑 auth.go？"]
    User->>GW: resolve_permission(allow_session)
    GW->>RT: ResolvePermission(allow_session)
    RT->>TL: Execute(filesystem_edit, "auth.go") [重试]
    TL->>SE: Check(write, "auth.go")
    SE-->>TL: allow (会话记忆命中)
    TL->>TL: RememberSessionDecision → 后续同文件编辑自动放行
    TL-->>RT: [编辑成功]

    Note over User,SS: ══════════ 第 3 轮：编译验证 ══════════

    RT->>PR: Generate(继续)
    PR-->>RT: [StreamEvent: tool_call: bash("go build ./...")]
    RT->>TL: Execute(bash, "go build ./...")
    TL->>SE: Check(bash, "go build")
    SE-->>TL: allow (只读命令)
    TL-->>RT: [编译通过或报错]

    Note over User,SS: ══════════ 终结 ══════════

    RT->>PR: Generate(继续)
    PR-->>RT: [final text: "已在 auth.go 中添加..."]

    opt 若 Token 预算接近阈值
        RT->>CTX: Compact(session)
        CTX->>CTX: MicroCompact(tool_results) → FullCompact(history)
        CTX->>SS: ReplaceTranscript()
    end

    RT->>SS: AppendMessages + UpdateSessionHead
    RT->>SS: CreateCheckpoint(end_of_turn)
    RT->>GW: emit(run_done {usage, diff_summary})
    GW-->>User: [SSE: run_done + Diff 摘要]
```

**图 9-5：端到端场景走查时序图。** 此图将五个核心流程（ReAct 循环、权限决策、Compact、Checkpoint、会话持久化）串联为一个完整的用户故事。关键观察：

1. **多轮推理是自然的：** 模型先在轮 1 读文件理解现状，轮 2 写修改，轮 3 跑编译验证——这是 AI Agent 的标准行为模式，不是设计缺陷
2. **权限决策在关键路径上：** `ask → permission_request → resolve_permission` 的暂停-恢复循环是 Human-in-the-loop 的工程实现，在第 2 轮中首次写文件时触发
3. **会话记忆消除重复审批：** 用户选择 `allow_session` 后，同会话内同文件编辑自动放行——这是 Security Engine 的 PermissionFingerprint 机制
4. **安全性在工具层而非 Runtime 层：** 每次工具执行前都经过 Security Engine，Runtime 不需要知道哪些操作是"安全的"
5. **Compact 和 Checkpoint 是隐式的：** 用户不感知 Compact（Token 预算达到阈值时自动触发）和 Checkpoint（每轮 `end_of_turn` 自动创建），但它们是系统安全网的关键组成部分

---

## 10. 数据与状态管理

### 10.1 核心数据模型

**会话聚合根（Session）：**

```
Session
├── ID, Title, Provider, Model          ← 身份与归属
├── CreatedAt, UpdatedAt                ← 时间戳
├── Workdir                             ← 运行时工作目录
├── TaskState                           ← 任务状态快照
│   ├── Summary, Goal, Constraints
│   ├── Architecture, Design
│   └── Progress, NextSteps
├── AgentMode                           ← 当前模式（default / plan）
├── Messages[]                          ← 对话历史（关联数据）
│   ├── UserMessage / AssistantMessage / ToolResult
│   ├── 每条含 ContentPart[]（text / tool_call / tool_result）
│   └── TokenUsage（input / output / cache）
├── ActivatedSkills[]                   ← 已激活的 Skill 列表
├── Todos[]                             ← Todo 列表
│   └── TodoItem: ID, Content, Status, Priority
├── CurrentPlan                         ← 当前 Plan 快照
│   └── PlanArtifact: Revision, Status, Steps, ...
├── TokenInputTotal / TokenOutputTotal  ← 累计 Token 消耗
└── HasUnknownUsage                     ← 是否含未统计的用量
```

**工具调用与结果模型：**

```
ToolCall（模型产出）                ToolResult（系统返回）
├── ID                              ├── ToolCallID（对应）
├── Name（工具名）                   ├── Content（文本结果）
├── Arguments（JSON）                ├── IsError
└── Type（function）                 └── Metadata（结构化事实）
```

### 10.2 关键状态机

#### 10.2.1 任务状态（TaskState）

TaskState 是会话级别的"任务理解"快照，由模型在推理过程中填充和更新。Runtime 在每轮开始时将当前 TaskState 注入 System Prompt 的 `taskState` 段落，确保模型理解当前上下文阶段。

TaskState 本身不强制执行状态转换——它是一个模型填充的数据结构，而非工作流引擎。转换完全由模型决定（在当前轮次的 System Prompt 中看到当前 TaskState 后自行决定是否更新）。

#### 10.2.2 Checkpoint 状态机

```mermaid
stateDiagram-v2
    [*] --> creating: 触发快照创建
    creating --> available: 写入完成（原子事务）
    creating --> broken: 写入异常
    available --> restored: RestoreCheckpoint()
    available --> pruned: PruneExpiredCheckpoints()
    restored --> pruned: 修剪策略触发
    broken --> [*]
    pruned --> [*]
```

触发创建的场景：`pre_write`（写操作前）、`compact`（压缩前）、`plan_mode`（Plan 模式切换）、`manual`（用户手动）、`end_of_turn`（每轮结束）、`pre_restore_guard`（恢复前的安全快照）。创建和写入在同一 SQLite 事务中完成，确保不会出现"半写入"的快照记录。

#### 10.2.3 权限决策状态

```mermaid
stateDiagram-v2
    [*] --> SecurityCheck: 工具执行请求

    state SecurityCheck {
        [*] --> PolicyMatch: Action.Validate()
        PolicyMatch --> Allow: 策略明确放行
        PolicyMatch --> Deny: 策略明确拒绝
        PolicyMatch --> Ask: 无匹配规则 → 询问用户
    }

    SecurityCheck --> Execute: allow
    SecurityCheck --> Reject: deny
    SecurityCheck --> WaitUser: ask

    WaitUser --> AllowOnce: 用户选 allow_once (不记忆)
    WaitUser --> AllowSession: 用户选 allow_session (持久化 PermissionFingerprint)
    WaitUser --> Reject: 用户选 reject

    AllowOnce --> Execute
    AllowSession --> Execute
```

### 10.3 并发控制策略

| 场景 | 机制 | 说明 |
|------|------|------|
| 同一会话并发 Run | `sessionLock`（mutex per sessionID） | 后续同会话 Run 排队等待；不同会话可并行 |
| Config 读写 | `Manager.mu`（RWMutex） + copy-on-read | `Get()` 返回配置快照的深拷贝，写入通过 `Update(mutateFn)` 原子执行 |
| Provider 实例创建 | `providerCreateMu`（Mutex） | 防止并发切换 Provider 时创建重复连接 |
| WebSocket 并发写 | `Runner.writeMu`（Mutex） | 保护 WebSocket 连接级别的并发写入 |
| 工具并行执行 | goroutine + WaitGroup | Runtime 在 ReAct Loop 中并行调度最多 N 个工具（可配置），每个工具在独立 goroutine 中执行 |

### 10.4 数据一致性保证

| 操作 | 一致性级别 | 实现方式 |
|------|-----------|----------|
| 消息追加 | 原子追加 | SQLite 单事务：`INSERT INTO messages` + `UPDATE sessions` |
| Compact 替换 | 原子替换 | `ReplaceTranscript` 在单事务内删除旧消息 + 插入新摘要 + 更新 SessionHead |
| Checkpoint 创建 | 原子写入 | 单事务：`INSERT checkpoint_record` → `INSERT session_cp` → `UPDATE record SET status=available` |
| 配置更新 | 内存原子 + 持久化 | `Manager.Update(mutateFn)` 先在内存应用变更，再整体 `Save()` 落盘 |
| 跨进程状态（Gateway ↔ 客户端） | 最终一致 | Gateway 通过 StreamRelay 广播 Runtime 事件；客户端通过 SSE/WS 订阅更新 |

---

## 11. 接口与集成

### 11.1 通信模式总览

```
┌──────────────────────────────────────────────────────────────────┐
│                        通信协议分层                                │
│                                                                  │
│  请求 / 响应（同步）                                               │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ 协议: JSON-RPC 2.0                                        │   │
│  │ 传输: HTTP POST（loopback 或网络）                           │   │
│  │ 方向: 客户端 → Gateway → Runtime                           │   │
│  │ 典型 action: run / ask / cancel / resolve_permission      │   │
│  │                                                           │   │
│  │ 请求帧格式:                                                 │   │
│  │ { "jsonrpc": "2.0", "method": "gateway.run",              │   │
│  │   "id": "req-1", "params": {...} }                        │   │
│  │                                                           │   │
│  │ 响应帧格式:                                                 │   │
│  │ { "jsonrpc": "2.0", "type": "ack", "action": "pong",     │   │
│  │   "request_id": "req-1", "payload": {...} }               │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  事件流（异步）                                                    │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ 协议: SSE (Server-Sent Events) 或 WebSocket (双向消息)     │   │
│  │ 方向: Runtime → Gateway (StreamRelay) → 客户端             │   │
│  │ 典型事件: run_progress / run_done / run_error /            │   │
│  │          permission_request / ask_chunk / ask_done         │   │
│  │                                                           │   │
│  │ SSE 端点: GET /sse?session_id=...&run_id=...              │   │
│  │ WebSocket 端点: ws://localhost:PORT/ws                     │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  消息队列（内部）                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ 机制: Go channel (StreamRelay 内部 pub/sub)                 │   │
│  │ 方向: Runtime → StreamRelay → 所有订阅连接                   │   │
│  │ 容量: DefaultStreamQueueSize（每个连接独立缓冲）              │   │
│  └──────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────┘
```

### 11.2 同步 vs 异步交互

| 交互类型 | 同步/异步 | 超时 | 说明 |
|----------|----------|------|------|
| `gateway.run` | 异步 | 无直接超时（30 min Runtime 硬超时） | Gateway 立即返回 ack；后续通过 SSE/WS 推送 run_progress / run_done |
| `gateway.ask` | 异步 | 同上 | 轻量问答，类似 run 但流程更短 |
| `gateway.cancel` | 同步 | 10s | 向 Runtime 发取消信号，等待确认 |
| `gateway.ping` | 同步 | 3s | 连接探活 |
| `gateway.authenticate` | 同步 | 5s | Token 认证 |
| `gateway.resolve_permission` | 同步 | 10s | 用户权限决策，Runtime 在等待此回复时处于暂停状态 |
| Runner → Gateway | 异步 | 30s（请求级） | WebSocket 双向消息 |

### 11.3 认证模型

```
┌─────────────────────────────────────────────────────────┐
│                    认证体系                              │
│                                                         │
│  本地模式（无 Authenticator）                             │
│  ┌─────────────────────────────────────────────────┐   │
│  │ 场景: neocode CLI / TUI (本地 loopback RPC)      │   │
│  │ 身份: auth.DefaultLocalSubjectID = "local_admin" │   │
│  │ 鉴权: gateway.authenticate 自动通过（空 Token）    │   │
│  └─────────────────────────────────────────────────┘   │
│                                                         │
│  网络模式（有 Authenticator）                             │
│  ┌─────────────────────────────────────────────────┐   │
│  │ 场景: Web / Desktop / Runner / Feishu Adapter   │   │
│  │ 身份: Authenticator.ResolveSubjectID(token)      │   │
│  │ 鉴权: 客户端先发送 gateway.authenticate 获取       │   │
│  │       subject_id；后续请求携带此身份               │   │
│  └─────────────────────────────────────────────────┘   │
│                                                         │
│  Runner 额外层                                          │
│  ┌─────────────────────────────────────────────────┐   │
│  │ Capability Token: 签名令牌校验 Runner 有权         │   │
│  │ 执行哪些工具、访问哪些路径                          │   │
│  │ WorkdirAllowlist: Runner 配置级目录白名单          │   │
│  └─────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
```

### 11.4 超时、重试与幂等性

| 机制 | 配置项 | 默认值 | 作用域 |
|------|--------|--------|--------|
| 工具执行超时 | `tool_timeout_sec` | 20s | 单个工具调用（bash 工具可单独配置） |
| 模型首包超时 | `generate_start_timeout_sec` | 90s | Provider.Generate 首次响应 |
| 模型空闲超时 | `generate_idle_timeout` | 由 Provider 决定 | 流式响应中连续无数据 |
| 模型重试 | `generate_max_retries` | 由 Provider 决定 | 同一 turn 内失败重试 |
| Runtime 硬超时 | `defaultRuntimeOperationTimeout` | 30 min | 单次 Run 的最大时长 |
| Runner 请求超时 | `RequestTimeout` | 30s | Runner 侧工具执行等待 |
| Runner 重连 | `ReconnectBackoffMin` / `Max` | 500ms ~ 10s | WebSocket 断连后自动重连 |

**幂等性保障：**
- 每次 Run 生成唯一 `RunID`（客户端或 Gateway 生成），同一 RunID 的重复 submit 被 Gateway 去重
- 工具执行不保证幂等（如 Bash、WriteFile 天然非幂等），由模型在 Prompt 指导下自行判断
- Session 消息追加通过 SQLite 事务保证不重复写入
- Checkpoint 通过 `CheckpointID` 去重

### 11.5 错误分类

参考 Gateway 错误编目（`docs/reference/gateway-error-catalog.md`）：

| 错误类别 | 错误码前缀 | 典型场景 | HTTP 映射 |
|----------|-----------|----------|-----------|
| 认证错误 | `unauthorized` | 无效 Token、未认证连接 | 401 |
| 参数校验 | `invalid_params` | 缺失必填字段、类型错误 | 400 |
| 不支持操作 | `unsupported_action` | 未注册的 action | 400 |
| 运行时错误 | `runtime_unavailable` | Runtime 进程异常 | 503 |
| 会话错误 | `session_not_found` | SessionID 不存在 | 404 |
| 内部错误 | `internal_error` | Gateway 自身异常 | 500 |
| 超时 | `timeout` | 操作超时 | 504 |
| 权限拒绝 | `permission_denied` | 安全策略拒绝 | 403 |

### 11.6 版本兼容策略

| 层面 | 策略 |
|------|------|
| **Gateway RPC API** | JSON-RPC 2.0 协议版本固定为 `"2.0"`；新增 action 不影响旧客户端；废弃 action 保留一个版本过渡期 |
| **Session 数据库 Schema** | `sqliteSchemaVersion` 管理（当前 v7）；启动时自动迁移（`MigrateSchema`） |
| **配置文件** | 向后兼容：新增字段有默认值；废弃字段在加载时忽略并 warn；`StaticDefaults()` 保证旧配置可启动 |
| **Provider 接口** | `Provider` interface 仅 2 个方法，极简稳定；新增可选能力通过 `GenerateRequest` 结构体字段扩展（`omitempty` JSON tag） |
| **Web UI** | 嵌入到 Go binary 中（`web/dist/`），与二进制同版本发布；不独立部署 |
| **自更新** | `go-selfupdate` 机制；二进制整体替换；配置文件不自动迁移（向后兼容保证可用） |

---

## 12. 部署视图

### 12.1 产物矩阵

NeoCode 通过 goreleaser 构建两个二进制产物（Inferred from `.goreleaser.yaml`）：

| 产物 | 入口 | 说明 |
|------|------|------|
| `neocode` | `cmd/neocode/main.go` | 完整 CLI：TUI 交互、Gateway 服务端 (`neocode gateway`)、HTTP Daemon (`neocode daemon`)、Local Runner (`neocode runner`)、Shell 诊断 (`neocode diag`) |
| `neocode-gateway` | `cmd/neocode-gateway/main.go` | 独立 Gateway 二进制：仅包含 Gateway 服务端，适合在服务器上长期运行为守护进程 |

**构建目标矩阵：**

| OS | Arch |
|----|------|
| Linux | amd64, arm64 |
| macOS (darwin) | amd64, arm64 |
| Windows | amd64, arm64 |

所有产物均以 `CGO_ENABLED=0` 静态编译，无系统依赖。Web UI 静态资源（React build 产物）嵌入在 `neocode` 二进制中。

### 12.2 部署拓扑

```
┌─────────────────────────────────────────────────────────────────────┐
│                        单机部署（开发者工作站）                        │
│                                                                     │
│  ┌──────────┐    ┌───────────┐    ┌──────────────┐                 │
│  │neocode   │    │neocode    │    │neocode       │                 │
│  │CLI / TUI │    │gateway    │    │daemon        │                 │
│  │          │───▶│(JSON-RPC) │    │(HTTP :18921) │                 │
│  │          │    │           │    │              │                 │
│  └──────────┘    └─────┬─────┘    └──────┬───────┘                 │
│                        │                 │                          │
│                        │           ┌─────┴────────┐                │
│                        │           │neocode:// URL │                │
│                        │           │Scheme 唤醒    │                │
│                        │           └──────────────┘                │
│                        │                                            │
│            ┌───────────┴───────────┐                                │
│            │   ~/.neocode/          │                               │
│            │   ├── config.yaml      │                               │
│            │   ├── session.db       │                               │
│            │   ├── checkpoint/      │                               │
│            │   └── skills/          │                               │
│            └───────────────────────┘                                │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│                    分布式部署（Runner 反向连接）                       │
│                                                                     │
│  工位电脑 A (NAT/防火墙后)              服务器 / 云主机                │
│  ┌──────────────────────┐            ┌──────────────────────┐       │
│  │ neocode runner       │──WS──────▶│ neocode gateway      │       │
│  │ (工具执行)            │  主动连接  │ (RPC + 事件中继)      │       │
│  └──────────────────────┘            └──────────────────────┘       │
│                                               ▲                      │
│  手机 / 远程                                   │                     │
│  ┌──────────────────────┐                      │                     │
│  │ 飞书 / Web / CLI     │──HTTPS/JSON-RPC─────┘                    │
│  └──────────────────────┘                                           │
└─────────────────────────────────────────────────────────────────────┘
```

**图 12-1：部署拓扑。** 单机模式（上）：所有组件在同一台机器上，通过本地 loopback RPC 通信。分布式模式（下）：Local Runner 主动连接 Gateway，使远程客户端可以通过 Gateway 驱动 Runner 所在机器的工具执行。

### 12.3 安装与分发

| 渠道 | 说明 |
|------|------|
| **Shell 脚本** | `curl -fsSL <url>/install.sh \| bash`（macOS/Linux），支持 `--flavor full\|gateway` |
| **PowerShell** | `irm <url>/install.ps1 \| iex`（Windows） |
| **自更新** | `neocode update` 命令通过 `go-selfupdate` 拉取最新 GitHub Release，校验 checksum 后原地替换二进制 |
| **Electron 桌面端** | 通过 `electron-builder` 打包为 `.dmg`（macOS）/ `.exe` installer（Windows）/ `.AppImage`（Linux） |
| **手动下载** | GitHub Releases 页面下载对应平台的 `.tar.gz` / `.zip` |

### 12.4 环境隔离

| 环境 | 数据目录 | 说明 |
|------|----------|------|
| 默认 | `~/.neocode/` | 所有数据（配置、会话、Checkpoint、Skills 缓存）的根目录 |
| `NEOCODE_HOME` 覆盖 | `$NEOCODE_HOME/` | 通过环境变量切换数据目录，支持多 Profile 隔离 |
| 工作目录 | `--workdir` / `workdir` 配置项 | 限制工具的文件系统访问边界 |

### 12.5 扩缩容考量

| 维度 | 限制 | 说明 |
|------|------|------|
| 单机并发会话 | 无进程级上限 | 不同会话通过 `sessionLock` 并行执行；同会话串行化 |
| Gateway 连接数 | `DefaultNetworkMaxStreamConnections`（可配置） | 超过上限时新连接收到 `too_many_connections` 错误 |
| Session 消息量 | 单会话最多 8192 条 | 超限自动裁剪最旧消息 |
| Runner 数量 | 无硬限制 | 多个 Runner 可注册到同一 Gateway，Gateway 按策略选择执行节点 |
| 数据库容量 | SQLite 单文件 | 30 天过期会话自动清理，Checkpoint 自动修剪 |

---

## 13. 安全设计

### 13.1 安全模型总览

NeoCode 的安全设计遵循**纵深防御**原则：不依赖单一安全机制，而是在多个层面上独立校验。

```
外部输入（用户/IM/CI）
        │
        ▼
┌──────────────────┐  第 1 层：认证
│ Gateway Auth     │  Token 校验 → subject_id
│ (本地/网络模式)    │  未认证连接仅允许 ping + authenticate
└────────┬─────────┘
         │
         ▼
┌──────────────────┐  第 2 层：ACL 授权
│ Gateway ACL      │  method × source 白名单
│ (每连接级)         │  未授权 method → acl_denied
└────────┬─────────┘
         │
         ▼
┌──────────────────┐  第 3 层：工具级安全策略
│ Security Engine  │  PolicyEngine（规则匹配）
│ (每次调用)         │  + WorkspaceSandbox（路径校验）
└────────┬─────────┘  + CapabilityToken（Runner 权限）
         │
         ▼
┌──────────────────┐  第 4 层：操作系统约束
│ OS 级隔离         │  进程权限 = 当前用户
│                  │  文件系统权限 = OS ACL
└──────────────────┘  网络边界 = 本机 loopback
```

### 13.2 认证（Authentication）

所有客户端（无论 TUI、Web、Desktop 还是第三方）统一通过 **JSON-RPC `gateway.authenticate`** 方法完成认证。Gateway 侧根据是否配置了 `Authenticator` 决定如何处理认证请求：

| 模式 | Gateway 配置 | 认证行为 | 典型场景 |
|------|-------------|----------|----------|
| **本地模式** | 未配置 `Authenticator` | `gateway.authenticate` 可直接调用（允许空 Token），Gateway 自动授予 `local_admin` 身份 | 开发者在自己机器上启动 CLI，Gateway 默认无 Authenticator |
| **网络模式** | 配置了 `Authenticator` | 客户端必须提供有效 Token → Authenticator 解析为 `subject_id` → 连接携带此身份发起后续请求 | Web UI、Desktop、Runner、飞书 Bot 等通过 HTTP 连接 Gateway 的场景 |

**关键安全属性：**
- Gateway 在本地模式下仅接受 loopback 地址连接（`127.0.0.1`），不可从外部网络访问
- 网络模式下的 `Authenticator` 是可插拔接口：内置 static token 实现，可替换为 OAuth2/JWT/LDAP 等
- `subject_id` 在连接生命周期内不变，作为所有操作的审计主体标识
- 所有客户端使用**同一套 RPC 协议**完成认证，不存在"IPC 免认证旁路"——这是 Gateway 作为统一安全边界的基础

### 13.3 授权（Authorization）

**Gateway ACL（连接级）：** 每个连接在认证后获得 ACL profile，控制允许调用的 RPC method 列表。未在 ACL 白名单中的 method 返回 `acl_denied`。

**Security Engine（操作级）：** 每次工具执行前经过两阶段检查：

| 阶段 | 组件 | 检查内容 |
|------|------|----------|
| 策略匹配 | `PolicyEngine.Check()` | 按 Priority 降序遍历 `PolicyRule` 列表；匹配条件包括 ActionType、Resource（工具名）、TargetPrefix（路径前缀）、HostPatterns（URL 域名）等；命中返回 `allow` / `deny` / `ask` |
| 沙箱校验 | `WorkspaceSandbox.Check()` | 路径穿越检测（`../`、Symlink 逃逸）→ `deny`；工作目录边界检查 → 越界时自动生成 safe 候选路径 |

**敏感路径自动检测：**

Security Engine 内置敏感路径特征库，无需配置即可检测：
- 目录关键词：`secrets`、`.ssh`、`.gnupg`、`.aws`、`.config`
- 文件名模式：`.env`、`*.secret`、`*.token`、`*.key`、`*.pem`、`id_rsa`、`id_ed25519` 等
- 命中敏感路径的操作自动升级为 `deny`（即使 PolicyRule 未显式配置）

### 13.4 密钥管理

| 原则 | 实现 |
|------|------|
| **不入配置文件** | `api_key_env` 配置项仅存储环境变量名，不存储密钥值 |
| **仅在内存中使用** | `APIKeyResolver(envName)` 在 Provider 发起请求前才从环境变量读取 |
| **不入日志** | 日志、调试输出、配置快照序列化时排除 `api_key_env` 的值域 |
| **不通过 Gateway 传输** | Provider 调用直接从 Runtime 进程发起，密钥不经过 Gateway 的 RPC 通道 |
| **Runner 隔离** | Runner 的 Capability Token 使用 HMAC-SHA256 签名，包含工具白名单和路径白名单，有时效性 |

### 13.5 输入校验

| 校验点 | 机制 |
|--------|------|
| **JSON-RPC 参数** | Gateway 在 dispatch 前对每个 action 的 params 做结构校验；必填字段缺失 → `invalid_params` |
| **工具参数** | 每个 `Executor` 在 `Execute()` 中独立校验参数；bash 工具检测交互式命令（`vim`、`top` 等）并拒绝执行 |
| **文件路径** | 所有路径先经 `ResolveWorkspacePath()` 归一化（解析相对路径、Symlink），再经 Sandbox 校验 |
| **URL** | `webfetch` 工具限制协议（仅 http/https）、限制响应大小（可配置）、禁止访问内网地址（可配置） |
| **消息内容** | 用户输入 Parts 在 Runtime 入口做基本合法性校验（非空、格式正确） |

### 13.6 审计追踪

| 审计要素 | 记录方式 |
|----------|----------|
| **主体标识** | 每个请求携带 `subject_id`，贯穿 Gateway → Runtime → Session 全链路 |
| **请求追踪** | `SessionID + RunID` 唯一标识一次完整的用户交互；所有事件（run_progress、tool_call、permission_request）附带这两个 ID |
| **操作审计** | 工具执行前 Security Engine 的 Check 调用记录 Action 详情（ToolName、Resource、TargetType、Target、NormalizedIntent） |
| **权限记忆** | `allow_session` 决策持久化到 SQLite，可追溯"谁在何时对什么操作授权了会话级放行" |
| **指标暴露** | GatewayMetrics 提供 `auth_failures_total`（认证失败）、`acl_denied_total`（ACL 拒绝）等安全相关计数器 |

---

## 14. 可观测性与运维支持

### 14.1 指标（Metrics）

Gateway 内置 Prometheus 指标收集器（`GatewayMetrics`，Inferred from `internal/gateway/metrics.go`），同时提供 Prometheus 格式和 JSON 格式的指标端点。

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `gateway_requests_total` | Counter | source, method, status | RPC 请求总量，按来源、方法、状态分组 |
| `gateway_auth_failures_total` | Counter | source, reason | 认证失败总量 |
| `gateway_acl_denied_total` | Counter | source, method | ACL 拒绝总量 |
| `gateway_connections_active` | Gauge | channel | 当前活跃流连接数（按 WS/SSE 通道） |
| `gateway_stream_dropped_total` | Counter | reason | 流连接剔除总量 |

**指标端点：**

| 端点 | 格式 | 说明 |
|------|------|------|
| `GET /metrics` | Prometheus text | Prometheus scrape 目标 |
| `GET /metrics.json` | JSON | 供 UI 面板或自定义监控消费 |
| `GET /healthz` | `{"status":"ok"}` | 存活探针 |

### 14.2 日志

| 层面 | 机制 | 说明 |
|------|------|------|
| Runtime | Go `log.Printf` | 关键生命周期事件（Checkpoint 创建/恢复失败、Compact 触发、Hook 执行）使用 `log.Printf` 输出到 stderr |
| Gateway | Go `log.Printf` + `http.Handler` 错误包装 | 连接异常、认证失败、流中继错误 |
| Runner | 可配置 `*log.Logger` | 默认输出到 stderr，前缀 `runner: ` |

**日志安全约束：**
- 不得在日志中输出 API Key 明文
- 不得在日志中输出用户消息内容（隐私）
- 工具调用参数中的敏感路径名（如 `.env`）在日志中以归一化相对路径替代绝对路径

### 14.3 分布式追踪

NeoCode 不使用外部分布式追踪系统（Jaeger/Zipkin），而是通过 **应用级标识符串联** 实现端到端可追踪性：

```
SessionID（会话级） + RunID（单次运行级）
     │                      │
     └──────────────────────┘
              │
    贯穿所有层：
    Client → Gateway → Runtime → Provider → Tools → Session Store
```

**附加追踪信息：**
- `TaskID` + `AgentID`：子代理调用链追踪
- `RequestID`：单次 JSON-RPC 请求-响应匹配
- `ToolCallID`：模型工具调用与执行结果的关联

### 14.4 健康检查

| 探针 | 端点 | 说明 |
|------|------|------|
| **HTTP Daemon 存活** | `GET /healthz` → 200 | Daemon 进程存活 + HTTP 服务正常 |
| **Gateway 存活** | `GET /healthz` → 200 | Gateway 进程存活 + HTTP 服务正常 |
| **Gateway 就绪** | `gateway.ping` JSON-RPC | 全链路可达（客户端 → Gateway → Runtime） |

### 14.5 告警建议

基于 Gateway 暴露的 Prometheus 指标，推荐配置以下告警规则：

| 告警 | 条件 | 严重度 |
|------|------|--------|
| 认证失败率异常 | `rate(gateway_auth_failures_total[5m]) > 0.1` | Warning |
| ACL 拒绝突增 | `rate(gateway_acl_denied_total[5m]) > 5` | Warning |
| 流连接数接近上限 | `gateway_connections_active > max * 0.8` | Warning |
| 流连接异常剔除 | `rate(gateway_stream_dropped_total[5m]) > 0` | Critical |
| Gateway 不可达 | `gateway.ping` 无响应或 `/healthz` 非 200 | Critical |

### 14.6 运维诊断工具

| 工具 | 用途 |
|------|------|
| `neocode diag` | Shell 诊断代理：自动获取终端最近一次命令的异常输出，调用 LLM 分析原因并给出建议 |
| `neocode daemon status` | 查看 HTTP Daemon 运行状态与自启动安装状态 |
| `neocode gateway --http-listen <addr>` | 显式指定 Gateway HTTP 监听地址，供调试时暴露到非 loopback 接口 |
| Session Log Viewer | Runtime 内部将关键会话事件写入 `log-viewer/` 目录，供离线排查 |

---

## 15. 架构决策记录（ADR）

以下记录 NeoCode 架构演进过程中的关键决策，遵循 ADR 标准格式（Context → Alternatives → Decision → Consequences）。

### ADR-001：Gateway 作为唯一 RPC 边界

**状态：** Accepted

**背景：** 系统需要支持多种客户端（TUI、Web、Desktop、飞书 Bot、CI/CD 脚本）。若每个客户端独立接入 Runtime，认证、授权、流式中继将在 N 个客户端中重复实现。

**替代方案：**

| 方案 | 评估 |
|------|------|
| 各客户端直连 Runtime | 安全逻辑分散、新客户端接入成本高、Runtime 需理解传输协议 |
| 按客户端类型分别建 Gateway | 仍有重复逻辑，且客户端类型增加时需要新增 Gateway |
| **单一 Gateway 统一 RPC** | 安全收敛、客户端对等化、流式中继集中管理 |

**决策：** 所有客户端必须通过 Gateway 的 JSON-RPC 接口与 Runtime 通信。Gateway 是系统唯一的跨边界通道。

**后果：**
- 变得更容易：新增客户端类型只需实现 JSON-RPC 客户端；安全审计只需检查 Gateway 一个入口
- 变得更困难：Gateway 成为单点故障（通过本地自动拉起 + 健康检查 + 快速重启缓解）
- 需要关注：Gateway 不得包含任何客户端特化逻辑——这是架构铁律，违反将侵蚀客户端对等性

### ADR-002：Provider 插件化（2 方法接口）

**状态：** Accepted

**背景：** AI 模型市场快速变化，新模型不断涌现。系统必须支持随时切换到新模型而不修改上层代码。

**替代方案：**

| 方案 | 评估 |
|------|------|
| 统一内部模型协议，Gateway 做转换 | Gateway 职责膨胀，每种新模型的流式格式差异需在 Gateway 处理 |
| 每个客户端自行集成模型 SDK | 模型切换需更新所有客户端，密钥分散管理 |
| **Provider interface（2 方法 + channel）** | 上层零改动接入新模型，厂商差异收敛在 Provider 包内 |

**决策：** `Provider` interface 仅定义 `EstimateInputTokens` + `Generate(ctx, req, events chan)`。所有厂商差异收敛在各自的 Provider 实现中。

**后果：**
- 变得更容易：新增模型只需写 adapter；测试只需注入 Mock Provider
- 变得更困难：Provider interface 极度简洁，但某些厂商的高级特性（如 thinking、caching）需要统一抽象层来表达
- 需要关注：接口不可膨胀——每次新增方法需严格审查是否值得破坏所有已有 Provider 实现

### ADR-003：事件驱动的异步工具执行

**状态：** Accepted

**背景：** AI 推理是流式的（token 逐个产出），可能持续数十秒到数分钟。纯同步调用在推理完成前客户端完全黑屏，且无法支持中途取消或实时权限审批。

**替代方案：**

| 方案 | 评估 |
|------|------|
| 同步回调（Run 阻塞等待完成） | 用户体验差，无法中途干预 |
| 客户端轮询（定时查询执行状态） | 延迟高、带宽浪费、权限审批需实时交互 |
| **进程内事件驱动（Go channel）** | 流式实时、Human-in-the-loop、支持并行工具执行 |

**决策：** 推理结果、工具调用、权限请求、Token 用量全部通过 `RuntimeEvent` channel 异步发出，由 Gateway StreamRelay 中继到客户端。

**后果：**
- 变得更容易：Human-in-the-loop（`permission_request` 暂停等待用户决策）；流式文本透出（用户可见 AI "打字"过程）；全链路追踪（SessionID + RunID 贯穿所有事件）
- 变得更困难：客户端需要支持 SSE/WebSocket 长连接；事件顺序一致性需在 Runtime 层保证
- 需要关注：channel buffer 满时的背压策略（当前为丢弃 + drop 计数）

### ADR-004：强边界单体架构

**状态：** Accepted

**背景：** NeoCode 运行在开发者本地机器上（单机单用户），同时由 5 人团队并行开发，需要模块独立演进但不需独立部署。

**替代方案：**

| 方案 | 评估 |
|------|------|
| 微服务（每模块独立进程） | 单机场景下序列化开销、网络延迟、运维复杂度都是净成本 |
| 纯单体（模块间直接调用，无接口边界） | 无法支持 5 人并行开发和 Provider 零侵入可扩展性 |
| **强边界单体（interface 解耦）** | 编译时类型安全 + 零网络延迟 + 单一二进制部署 |

**决策：** 核心模块在同一进程中通过 Go interface 解耦，享受单体部署的简单性同时保持模块边界的严格性。仅当模块确实需要跨越物理机边界时（Runner），才拆分为独立进程。

**后果：**
- 变得更容易：单一二进制分发（`CGO_ENABLED=0` 静态编译）；无外部服务依赖；调试简单（单进程内追踪）
- 变得更困难：模块间耦合只能通过接口契约约束（无法通过网络隔离强制执行）；单进程内存压力（需关注 Compact 和内存泄漏）
- 需要关注：若未来出现多用户共享同一 NeoCode 实例的场景，可能需要重新审视此决策

### ADR-005：SQLite 作为唯一持久化存储

**状态：** Accepted

**背景：** 会话数据（消息历史、Token 统计、Checkpoint 快照）需要可靠持久化，但单机场景不需要外部数据库。

**替代方案：**

| 方案 | 评估 |
|------|------|
| PostgreSQL / MySQL | 需要用户安装和运行数据库服务 → 违反零依赖部署 |
| 纯文件存储（JSON/YAML） | 并发写不安全、无法原子事务（Compact 替换时危险）、查询需全量加载 |
| **SQLite（modernc 纯 Go）** | ACID 事务、零外部依赖、单文件存储、开箱即用 |

**决策：** 所有持久化（会话、Checkpoint、权限记忆）使用 SQLite，通过 modernc 纯 Go 实现消除 CGO 依赖。

**后果：**
- 变得更容易：单文件备份（复制 `session.db`）；原子事务（Compact 替换消息列表、Checkpoint 创建）
- 变得更困难：写并发受限（SQLite 单 writer），同会话并发写需显式加 `sessionLock`
- 需要关注：数据库文件大小增长（8192 条消息/会话 × 多会话），通过 30 天过期清理 + Checkpoint 自动修剪控制

### ADR-006：JSON-RPC 2.0 作为 RPC 协议

**状态：** Accepted

**背景：** Gateway 需要一种通用协议，使得任何客户端（从 Go TUI 到 Python 脚本到飞书服务端）都能平等接入。

**替代方案：**

| 方案 | 评估 |
|------|------|
| gRPC | 需要 protobuf 编译步骤 → 第三方接入门槛高；调试需专用工具 |
| REST | 资源建模适合 CRUD，NeoCode 的操作是动词型的（`gateway.run`、`gateway.cancel`），硬映射不自然 |
| **JSON-RPC 2.0** | 极简协议、人类可读、任何能发 HTTP POST 的环境都能接入 |

**决策：** 客户端-Gateway 间使用 JSON-RPC 2.0 作为请求/响应协议。流式事件通过 SSE 或 WebSocket 推送。

**后果：**
- 变得更容易：第三方接入成本最低（发 JSON 即可）；调试简单（可抓包查看明文）；与 SSE/WS 配合自然
- 变得更困难：无强类型 schema（对比 protobuf）；错误格式需自行规范化（`FrameError` + `GatewayRPCError`）
- 需要关注：JSON-RPC 的 batch 和 notification 语义当前不使用，避免引入复杂度

### ADR-007：Runner 反向连接模型

**状态：** Accepted

**背景：** "手机飞书发指令 → 工位电脑执行代码"的场景要求 Runner 能穿过 NAT/防火墙接收指令。

**替代方案：**

| 方案 | 评估 |
|------|------|
| Gateway 主动连接 Runner | Runner 需开放入站端口 → 企业网络通常不允许 |
| VPN/隧道统一网络 | 增加运维成本，不适合"即装即用"体验 |
| **Runner 主动连接 Gateway（反向连接）** | Runner 位于 NAT 后也可用，无需开放入站端口 |

**决策：** Runner 启动后通过 WebSocket 主动连接 Gateway 并注册自身。Gateway 将工具执行请求通过该 WebSocket 连接下发给 Runner。

**后果：**
- 变得更容易：Runner 可在任何网络环境下运行（仅需出站 HTTPS）；即装即用，零网络配置
- 变得更困难：Gateway 需管理 Runner 注册/心跳/断连重连；重连期间的工具请求需排队
- 需要关注：WebSocket 连接安全（通过 Capability Token 签名校验工具权限和路径白名单）

### ADR-008：Checkpoint 本地代码版本快照

**状态：** Accepted

**背景：** AI Agent 的写操作（文件编辑、删除）可能出错。需要一个轻量级的回滚安全网，且不能依赖用户已初始化 Git。

**替代方案：**

| 方案 | 评估 |
|------|------|
| 仅依赖 Git（`git stash` / `git checkout`） | 非 Git 仓库无法使用；AI 修改粒度远细于 commit |
| 全量文件备份 | 大仓库空间开销不可接受；恢复粒度粗 |
| **Checkpoint 快照（SQLite 记录 + 文件存储）** | 细粒度、自动触发、与 Git 并存、轻量 |

**决策：** 在每次写操作前（`pre_write`）、每轮结束（`end_of_turn`）、上下文压缩前（`compact`）自动创建 Checkpoint 快照。有 `.git` 时优先用 Git 版本追踪，无 `.git` 时 Checkpoint 提供独立安全网。

**后果：**
- 变得更容易：用户无需任何操作即有安全网；支持选择性恢复；自动修剪避免空间膨胀
- 变得更困难：需管理 Checkpoint 生命周期（creating → available → restored/pruned）；大文件（二进制、vendor）的快照效率需持续优化
- 需要关注：快照频率与磁盘空间的平衡（通过 `maxAutoKeep` 和过期修剪控制）

---

## 16. 风险、局限与技术债

好的架构文档必须诚实。以下逐项记录当前架构中的已知风险、临时妥协和技术债务，以及每项的缓解计划。

### 16.1 架构风险

| 风险 | 严重度 | 描述 | 缓解措施 |
|------|--------|------|----------|
| **Gateway 单点故障** | 中 | 所有客户端依赖 Gateway 作为唯一入口；Gateway 进程异常时整个系统不可用 | 客户端内置自动拉起（auto-spawn）；本地 loopback 部署下 Gateway 与 CLI 同生命周期；网络模式下建议部署多个 Gateway 实例（当前未实现） |
| **模型行为不可预测** | 高 | 底层模型升级或切换时，Agent 的行为可能发生微妙变化（推理深度、工具选择偏好、错误处理风格），且这种变化难以通过自动化测试捕获 | Provider 层契约极简（2 方法），限制厂商差异扩散；100% 覆盖率的框架层测试确保框架逻辑不受影响；实际模型行为通过验收测试（`runtime/acceptance/`）做抽样验证 |
| **SQLite 写并发瓶颈** | 低 | 同会话的所有写操作（追加消息、更新状态、Compact 替换）串行执行；当需要跨多个会话做批量分析时单 writer 限制成为瓶颈 | 同会话并发写已通过 `sessionLock` 串行化，不同会话可并行；当前用户场景（单用户、顺序交互）下不构成实际瓶颈；若未来需要批量跨会话操作，可通过读写分离（读可并发）缓解 |
| **上下文窗口天花板** | 中 | 即使有 Compact，模型原生的 context window 有硬限制（如 Claude 200K、GPT-4 128K）；对于超长会话，最终仍会达到无法继续的临界点 | Compact 两级策略（Micro + Full）最大化利用现有窗口；`max_turns` 限制防止无限循环；长期来看需借助模型厂商的 context window 增长 |
| **TOCTOU 路径竞态** | 低 | 文件系统操作在 Security Engine 校验通过后、实际读写前，目标路径的状态可能被外部进程改变（symlink 替换攻击） | 当前在校验时 resolve symlink，但存在微小的时间窗口；现代 OS 的 `O_NOFOLLOW` 等标志可进一步缓解；实际攻击面极小（本地单用户场景） |

### 16.2 已知局限

| 局限 | 影响 | 讨论 |
|------|------|------|
| **单机单用户模型** | 不支持多用户共享同一 NeoCode 实例 | 这是刻意的设计选择（见 ADR-004）。多用户场景可通过每个用户运行自己的 Gateway 实例 + 共享 Runner 来解决，不需要多租户 |
| **无分布式追踪** | SessionID/RunID 仅在 NeoCode 内部可追踪，无法与外部的 APM（如 Datadog、Jaeger）关联 | 当前通过标准化日志格式（SessionID+RunID 前缀）做手动关联，未引入分布式追踪 SDK。如果未来部署复杂度提升，可在 Gateway 层注入 OpenTelemetry context |
| **纯 Go 生态** | 工具和 Skill 的执行受限于 Go 生态；无法直接调用 Python/Node.js 库 | MCP 协议（stdio 子进程）提供了语言无关的扩展通道。Python/Node.js 工具可通过 MCP server 接入 |
| **Web UI 嵌入分发** | Web 端 patch 只能随二进制更新，不支持独立热更新 | 这是单二进制部署的代价。对于需要频繁更新 UI 的场景，可将 Web 端独立部署（当前已支持 `neocode-gateway` 独立二进制 + 反向代理静态资源） |
| **无插件市场/发现机制** | Skills 和 MCP server 的获取依赖用户手动配置，没有中心化的发现和安装渠道 | 当前的 Skills 设计优先保证离线可用性和零信任（文件即 Skill）。发现机制可在此基础上叠加 |

### 16.3 技术债清单

| 技术债 | 位置 | 影响 | 建议处理时机 |
|--------|------|------|-------------|
| **底层传输层 IPC 残留** | `internal/gateway/transport/` — Unix domain socket / Named pipe | 客户端连接路径复杂（需判断平台选 socket 类型），迁移到全 HTTP 后可消除 | 短期——已在迁移计划中 |
| **`runtime/run.go` 单文件过长** | ReAct 主循环逻辑集中在 `run.go` (~400 行) 和 `runtime.go` (~540 行) | 新成员理解核心循环需要较长时间；修改风险集中在少数大文件中 | 中期——可按阶段拆分（pre-processing / loop body / termination） |
| **Compact 策略配置分散** | MicroCompact 配置在 `MicroCompactConfig`，Full Compact 在 `CompactConfig`，部分阈值在 `RuntimeConfig` | 调整上下文管理策略需要理解三处配置 | 中期——收敛为统一的 `CompactPolicy` 结构体 |
| **Gateway Bootstrap 单文件** | `bootstrap.go` 超过 1600 行，包含帧路由、认证、session CRUD、RPC 处理 | 单体文件难以定位和维护 | 中期——拆分为 `session_handler.go`、`rpc_handler.go`、`auth_handler.go` |
| **Acceptance 测试耗时长** | `runtime/acceptance/` 的端到端测试依赖真实模型 API | CI 成本高、不稳定（网络波动导致 flaky） | 长期——增加录制/回放（VCR）模式，CI 中默认使用录制的 fixture |

---

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

## 18. 附录

### 18.1 术语表

| 术语 | 定义 |
|------|------|
| **ReAct Loop** | Reasoning + Acting 循环：模型推理 → 解析工具调用 → 执行工具 → 回灌结果 → 继续推理，直到产出最终文本回复 |
| **Compact** | 上下文压缩：当对话历史累积到接近 Token 预算上限时，自动将历史消息摘要化或裁剪，以释放上下文空间 |
| **MicroCompact** | 轻量级压缩：仅对单个 tool_result 内容做摘要化，不改变消息列表结构。是 Compact 的第一阶段 |
| **StreamRelay** | 流式中继：Gateway 内部将 Runtime 的异步事件按 SessionID/RunID 广播到所有订阅客户端连接的 pub/sub 机制 |
| **Checkpoint** | 代码版本快照：AI 执行写操作前自动创建的文件状态快照，支持恢复和 Diff 查看 |
| **Human-in-the-loop** | 人机协作模式：AI 在执行可能危险的操作（如写文件、执行 Bash）前暂停，等待人类审批 |
| **Capability Token** | 能力令牌：Runner 执行工具时携带的 HMAC 签名令牌，限定允许的工具列表、路径范围和有效期 |
| **Skill** | 技能：通过 SKILL.md 文件定义的专用行为 Prompt，在会话中按需激活并注入 System Prompt |
| **MCP** | Model Context Protocol：通过本地 stdio 子进程动态挂载外部工具的开放协议 |
| **Provider** | 模型厂商适配器：实现统一 Generate 接口、封装厂商特定协议和流式格式的插件化组件 |
| **Gateway** | 协议路由层：系统唯一的 RPC 入口，负责客户端认证、请求路由和事件流中继 |
| **Runner** | 远程工具执行代理：独立进程，通过 WebSocket 主动连接 Gateway，在本地执行 Gateway 下发的工具请求 |
| **ADR** | Architecture Decision Record：架构决策记录，记录背景、替代方案、决策和后果 |

### 18.2 参考文档

| 文档 | 路径 | 说明 |
|------|------|------|
| Gateway RPC API 参考 | `docs/reference/gateway-rpc-api.md` | 完整的 JSON-RPC method、params、error code 定义 |
| Gateway 错误编目 | `docs/reference/gateway-error-catalog.md` | 所有 Gateway 错误码的语义和 HTTP 映射 |
| Gateway 兼容性 | `docs/reference/gateway-compatibility.md` | 跨版本兼容性保证 |
| TUI-Gateway 契约矩阵 | `docs/reference/tui-gateway-contract-matrix.md` | TUI 与 Gateway 间的协议契约 |
| Provider 接入指南 | `docs/guides/adding-providers.md` | 如何新增模型厂商 |
| Gateway 集成指南 | `docs/guides/gateway-integration-guide.md` | 第三方客户端接入指南 |
| MCP 配置指南 | `docs/guides/mcp-configuration.md` | MCP Server 配置详解 |
| 飞书适配器指南 | `docs/guides/feishu-adapter.md` | 飞书 Bot 接入配置 |
| Context Compact 详解 | `docs/context-compact.md` | Compact 策略和预算管理的实现细节 |
| Runtime 事件流 | `docs/runtime-provider-event-flow.md` | Runtime 与 Provider 间的事件协议 |
| Stop Reason 决策 | `docs/stop-reason-and-decision-priority.md` | 停止原因和决策优先级 |
| Skills 系统设计 | `docs/skills-system-design.md` | Skills 系统的详细设计 |
| Gateway 详细设计 | `docs/gateway-detailed-design.md` | Gateway 内部实现细节 |
| 开发规范 | `AGENTS.md` | 项目 AI 协作规则、模块边界、编码规范 |

### 18.3 变更日志

| 版本 | 日期 | 变更 |
|------|------|------|
| v0.1 | 2026-05-09 | 初始完整版本。涵盖全部 18 节：系统背景与目标、范围与边界、质量属性、约束与原则、系统上下文、整体架构、核心模块设计（9 个模块）、核心流程（5 个流程 + 端到端走查）、数据与状态管理、接口与集成、部署视图、安全设计、可观测性、8 条 ADR、风险与局限、未来演进、附录 |

