# NeoCode Agent Rules

本文件是本仓库的 AI 协作规则。任何 AI 在本项目中进行改写、续写、重构、修复、补测试或补文档时，都应优先遵守本文件。

## 1. 任务目标
- 本仓库的目标是实现 `NeoCode Coding Agent MVP`。
- 当前主链路必须始终围绕以下闭环保持可用：
  `用户输入 -> Agent 推理 -> 调用工具 -> 获取结果 -> 继续推理 -> UI 展示`
- 做改动时，优先保证主链路可运行、模块边界清晰、实现可验证。

## 2. 最高优先级规则
- 不要为了“可能兼容旧版本”破坏当前架构；若新设计已确定，优先直接切换到新实现。
- 不要跨层直连；新功能默认沿 `TUI -> Runtime -> Provider / Tool Manager` 主链路设计。
- 不要把模型厂商差异泄漏到 `runtime`、`tui` 或上层调用方。
- 不要在 `runtime` 或 `tui` 里直接写工具执行逻辑；所有可被模型调用的能力必须进入 `internal/tools`。
- 不要把会话状态、消息历史、工具调用记录散落到 UI；这些状态优先由 `runtime` 管理。
- 不要把明文 API Key 写入 YAML、样例配置、测试快照或提交内容。
- 修改 `config`、`provider`、`runtime`、`tools` 时，默认应同时评估并补充测试。

## 3. 代码结构与职责边界

### 3.1 关键目录
- `cmd/neocode`：CLI 入口。
- `internal/app`：应用装配与 bootstrap，负责连接 config、provider、tools、runtime、tui。
- `internal/config`：配置模型、YAML 加载、环境变量管理、配置校验和并发安全访问。
- `internal/provider`：provider 抽象、领域模型和各厂商适配器。
- `internal/runtime`：ReAct 主循环、事件流、Prompt 编排、Session 持久化。
- `internal/tools`：工具契约、注册表、参数校验和具体工具实现。
- `internal/tui`：Bubble Tea 状态机、渲染层、Slash Command 和事件桥接。
- `docs`：架构、配置、事件流、会话持久化等说明文档。

### 3.2 模块职责
- `app` 只负责装配和依赖注入，不承载业务规则。
- `config` 负责 provider 列表、当前 provider、当前 model、workdir、shell 的管理和校验。
- `provider` 只处理模型协议差异、请求组装、响应解析、流式输出、超时与重试。
- `runtime` 负责会话、消息上下文、tool schema 传递、tool call 识别、tool result 回灌和停止条件。
- `tools` 负责统一的 `schema + execute + result` 协议，以及参数校验、错误包装和输出格式收敛。
- `tui` 只消费 runtime 事件并负责展示，不直接调用 provider，不直接执行 tools。

## 4. AI 修改代码时的执行流程
- 先定位改动所属模块，再检查是否会破坏职责边界。
- 优先做最小闭环改动，避免无关重构。
- 如果新增能力会被模型调用，先设计 `tools` 抽象，再接入 `runtime`。
- 如果改动涉及 provider 协议，差异收敛在 `internal/provider` 内，不向外扩散特定厂商字段。
- 如果改动涉及配置，统一经由 `ConfigManager` 或现有配置管理入口处理。
- 如果新增目录、命令、配置项或模块职责，必须同步更新 `README.md`、`docs/` 或本文件。

## 5. 测试规则
- 测试文件命名为 `*_test.go`，测试函数命名为 `TestXxx`。
- 优先覆盖以下边界：
  - 配置校验
  - provider 请求/响应转换
  - tool 参数校验
  - runtime loop 停止条件
  - 事件派发
- 修改 `runtime` 时，重点覆盖：
  - 最大轮数停止
  - tool result 回灌
  - 最终响应输出
  - 错误事件派发
- 修改 `tools` 时，重点覆盖：
  - schema 校验
  - 超时控制
  - 错误包装
  - 工作目录限制
- 修改 `provider` 时，重点覆盖：
  - 请求组装
  - tool call 解析
  - 异常响应处理
  - 认证或限流错误映射

## 6. 编码与实现规则
- 遵循 Go 惯用风格。
- 包名使用短小、全小写名词。
- 导出标识符使用 `PascalCase`，未导出使用 `camelCase`。
- 使用制表符缩进，尽量将单行控制在约 120 字符内。
- 核心抽象上的导出类型、函数、接口应补充清晰注释，尤其是 `provider`、`runtime`、`tools`。
- 避免硬编码路径、URL、模型名、超时、输出长度限制和环境差异项，优先通过配置、参数或具名常量注入。
- 优先写清晰、可替换的实现，不要为了“未来可能需要”提前引入复杂泛化。

## 7. 配置与安全规则
- 配置文件默认路径为 `~/.neocode/config.yaml`。
- 配置中只保存环境变量名，不保存明文 API Key。
- `selected_provider`、`current_model`、`workdir`、`shell` 必须在启动阶段完成校验，非法配置应尽早失败。
- `filesystem` 工具默认限制在工作目录内，禁止越界访问。
- `bash` 工具必须限制超时、输出长度，并避免交互式阻塞命令。
- `webfetch` 工具必须限制协议范围和响应大小，防止拉取不受控内容。
- 本地运行数据、会话数据和真实密钥不得默认入库；相关目录和配置文件应加入 `.gitignore`。

## 8. 文档规则
- 修改说明性文档时，沿用目标文档当前已有的主语言。
- 当前文档如果以中文为主，则继续使用中文；若以英文为主，则继续使用英文。
- 仅在代码标识符、命令、路径、协议名、库名等不可避免的场景下保留英文原词。
- 文档必须反映真实实现；如果实现和文档冲突，修代码或修文档时至少修正其中一个，不要放任失真。

## 9. 提交前最低检查
- 确认改动没有破坏 `TUI / Runtime / Provider / Tools / Config` 的职责分工。
- 确认新增能力已经接到正确层级，而不是临时跨层实现。
- 运行必要的格式化和测试。
- 检查 `git status`，确保没有无关文件、密钥、本地配置或临时数据混入。

## 10. 常用命令
- 启动应用：`go run ./cmd/neocode`
- 编译全部包：`go build ./...`
- 运行测试：`go test ./...`
- 格式化代码：`gofmt -w ./cmd ./internal`

## 11. 一句话原则
- 任何 AI 在此仓库中工作时，都应优先保持主链路可用、边界清晰、配置安全、测试可验证，而不是追求无关的大改或过度设计。
