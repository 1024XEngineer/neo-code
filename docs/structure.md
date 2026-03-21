### 一、 整体架构设计说明
本架构采用 “契约驱动 + 领域解耦” 的设计思路：

1. 物理隔离解耦 ：后端服务（Server）与终端客户端（TUI）在 internal 目录下物理分离。TUI 严禁调用后端业务逻辑，仅通过 api/proto 定义的契约进行通信。
2. 四层分层模型 ：后端遵循 Transport -> Service -> Domain <- Infra 。利用 依赖倒置（DIP） ，核心业务逻辑（Domain）定义接口，基础设施（Infra）实现接口，彻底规避新手常见的“代码一锅端”和循环依赖。
3. 约束重于灵活 ：通过 internal 目录特性保护核心代码，确保所有组件必须显式依赖接口。这种结构天然支持单元测试（Mocking），且代码流向单一（自顶向下），极大地降低了心智负担。

### 二、 完整项目目录树结构

    - api/                # API 契约：定义前后端通信协议 (.proto, .yaml)
        - proto/            # gRPC/Protobuf 原始定义文件，暂时不使用，保留等待迭代
    - cmd/                # 程序入口：仅负责依赖注入与启动，不含业务逻辑
        - server/           # 后端服务入口 (main.go)
        - tui/              # TUI 客户端入口 (main.go)
    - configs/            # 配置文件：本地开发、生产环境配置 (yaml, toml)
    - docs/               # 文档：架构设计、API 文档、新手上手指南
    - internal/           # 内部私有代码：禁止外部项目引用，核心约束区
        - server/           # 后端业务核心
            - domain/         # 领域层：存放接口定义与核心模型 (实体、抽象)
            - service/        # 应用层：编排业务流程 (调用 Domain 接口)
            - transport/      # 接入层：gRPC/HTTP/LSP 路由与参数解析，暂时不使用，保留等待迭代
            - infra/          # 基础设施：LLM 适配器、数据库、代码仓库实现
        - tui/              # TUI 客户端核心
            - core/           # 状态管理：Bubble Tea Model 与消息循环
            - components/     # UI 组件：可复用的终端视图组件
            - infra/          # 通信实现：后端 API 的 gRPC 客户端封装
        - pkg/              # 内部公共库：仅限本项目内部使用的通用工具
    - pkg/                # 外部公共库：可被其他项目引用的通用工具 (如 Logger)
    - scripts/            # 脚本：编译、Proto 生成、代码质量检查
    - test/               # 集成测试：端到端测试用例 (E2E)
    - go.mod              # 依赖管理文件

### 三、 逐目录详细说明 
1. internal/server/domain (核心领域层)
- 【目录职责】 ：定义项目的“灵魂”，包含核心业务模型（Entity）和外部依赖的接口说明（Interface）。
- 【准入规则】 ：仅允许存放纯 Go 的结构体定义、常量和接口。
- 【禁止规则】 ：绝对禁止引入任何第三方 SDK（如 OpenAI SDK）、数据库驱动或任何其他 internal 目录。
- 【依赖规则】 ： 零依赖 。它是架构的最底层，只能被别人依赖，不能依赖别人。
2. internal/server/infra (基础设施层)
- 【目录职责】 ：负责所有“重活、累活”，实现 Domain 层定义的接口（如 LLM 调用、文件读写）。
- 【准入规则】 ：存放具体的第三方实现代码，如 openai_client.go 、 git_provider.go 。
- 【禁止规则】 ：禁止包含任何业务逻辑判断。它只管按照指令干活并返回结果。
- 【依赖规则】 ：仅依赖 domain 层。 
3. internal/server/service (应用服务层)
- 【目录职责】 ：业务逻辑的“指挥官”，负责组合不同的 domain 接口完成功能。
- 【准入规则】 ：存放具体的业务流程代码（如“修复代码”的逻辑：读取文件 -> 发送给 LLM -> 写入修复）。
- 【禁止规则】 ：禁止出现具体的数据库 SQL 或 HTTP 请求代码。
- 【依赖规则】 ：依赖 domain 。 
4. internal/tui/core (TUI 状态机)
- 【目录职责】 ：基于 Bubble Tea 的 ELM 架构，管理终端交互的全局状态。
- 【准入规则】 ：存放 Update 、 View 、 Model 函数。
- 【禁止规则】 ：禁止直接编写复杂的 UI 渲染代码（应拆分到 components）。
- 【依赖规则】 ：依赖 tui/components 和 tui/infra （API 客户端）。