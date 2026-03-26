# Repository Guidelines

## Project Positioning
- 本仓库面向 `NeoCode Coding Agent MVP`，目标是先跑通最小闭环：
  `用户输入 -> Agent 推理 -> 调用工具 -> 获取结果 -> 继续推理 -> UI 展示`。
- 当前实现应围绕五个核心模块展开：`provider`、`tui`、`tools`、`config`、`runtime`。
- 设计优先级是先保证主链路可用、边界清晰、便于扩展，不为历史实现做兼容性妥协。

## Architecture Principles
- `Runtime` 是唯一编排中心：负责会话上下文、Prompt 组装、模型调用、工具执行编排和事件分发。
- `TUI` 只负责交互和渲染，不直接调用 `provider`，也不直接执行 `tools`。
- 所有副作用操作统一收敛到边界层：`provider`、`tools`、`config`。
- 面向接口设计，优先定义稳定抽象，再补充具体实现，确保后续新增 provider 或工具时无需推翻主链路。
- MVP 阶段优先选择简单、可验证、可替换的方案，避免过早抽象和过度设计。

## Expected Project Structure
- `cmd/neocode/main.go`：应用入口，负责启动配置加载、依赖注入和 TUI。
- `internal/app/`：应用装配与 bootstrap，连接 config、provider、tools、runtime、tui。
- `internal/config/`：配置模型、加载器、校验逻辑，负责从 `~/.neocode/config.yaml` 生成运行时只读配置。
- `internal/provider/`：模型调用抽象及实现，按 provider 拆分子目录，例如 `openai/`、`anthropic/`、`gemini/`。
- `internal/runtime/`：Agent loop 核心，包括 `runtime.go`、`executor.go`、`prompt_builder.go`、`session_store.go`、`events.go`。
- `internal/tools/`：工具协议、注册表、参数校验与执行框架，内置工具建议拆分为 `filesystem/`、`bash/`、`webfetch/`。
- `internal/tui/`：Bubble Tea 应用层，包括状态、键位、组件和视图。
- `docs/`：架构说明、交互设计、安全边界、开发计划等文档。

## Module Responsibilities
- `provider`：只处理模型协议差异、请求组装、响应解析、流式输出、超时与重试，不承担 UI 或工具逻辑。
- `runtime`：维护会话、构造消息上下文、传递 tool schema、识别 tool call、回灌 tool result，并决定何时停止循环。
- `tools`：提供统一的 `schema + execute + result` 协议，负责参数校验、错误包装和输出格式收敛。
- `config`：管理 provider 列表、当前 provider、当前 model、workdir、shell，并在启动时完成校验。
- `tui`：消费 runtime 事件，展示消息、会话列表、工具执行状态、当前 provider/model/workdir。

## Development Rules
- 新功能开发默认沿 `TUI -> Runtime -> Provider / Tool Manager` 主链路思考，避免跨层直连。
- 任何需要被模型消费的工具，都必须先进入 `tools` 抽象层，不能在 `runtime` 或 `tui` 中直接嵌入命令执行逻辑。
- 任何与模型厂商相关的差异都应留在 `provider` 实现内，不向上泄漏协议细节。
- 会话状态、消息历史、工具调用记录等运行信息，应优先由 `runtime` 管理，而不是散落在 UI 层。
- 新增目录、命令、配置项或模块职责时，必须同步更新 `README.md`、`docs/` 或本文件，避免文档落后于实现。
- 当前处于 MVP / 开发阶段时，默认不为旧数据格式、旧配置结构、旧接口行为做兼容性妥协；若新设计已经确定，应直接切换到新实现，并让旧格式尽早失败，而不是继续叠加兼容分支。
- 如确需兼容旧行为、迁移历史数据或保留过渡层，必须是明确需求驱动；否则优先保持主链路、数据结构和模块边界简洁可控。
- 修改 `README.md`、`docs/`、注释或其他说明性文档时，必须沿用目标文档当前已有的主语言；若文档当前以中文为主，则继续使用中文，若以英文为主，则继续使用英文，避免无必要的中英文混写。
- 仅在代码标识符、命令、路径、协议名、库名等不可避免的场景下保留原文术语；说明性正文、标题、列表项和新增段落应保持语言一致。

## Build, Test, and Development Commands
- `go build ./...`：编译全部包；提交前至少保证可编译。
- `go test ./...`：执行全部测试；修改 `runtime`、`provider`、`tools`、`config` 后必须重新运行。
- `go run ./cmd/neocode`：从仓库根目录启动本地 MVP 应用。
- `gofmt -w <file>` 或 `go fmt ./...`：格式化 Go 代码。
- 如引入 `goimports`，在提交前执行，保持 import 有序且无冗余。

## Coding Style & Naming Conventions
- 遵循惯用 Go 风格：包名使用短小、全小写名词；导出标识符使用 `PascalCase`，未导出使用 `camelCase`。
- 使用制表符缩进，尽量将行长度控制在约 120 字符内。
- 导出类型、函数、接口需要补充完整注释，尤其是 `provider`、`runtime`、`tools` 等核心抽象。
- 避免硬编码路径、URL、模型名、超时、输出长度限制和环境差异项，优先通过配置、参数或具名常量注入。
- 优先写清晰、可替换的实现，不用为了“可能以后需要”提前引入复杂泛化。

## Testing Guidelines
- 测试文件命名为 `*_test.go`，测试函数命名为 `TestXxx`。
- 优先为以下边界编写测试：配置校验、provider 请求/响应转换、tool 参数校验、runtime loop 停止条件、事件派发。
- 修改 `runtime` 时，应重点覆盖：
  最大轮数停止、tool result 回灌、最终响应输出、错误事件派发。
- 修改 `tools` 时，应重点覆盖：
  schema 校验、超时控制、错误包装、工作目录限制。
- 修改 `provider` 时，应重点覆盖：
  请求组装、tool call 解析、异常响应处理、认证/限流错误映射。

## Commit & Pull Request Guidelines
- 保持提交小而明确，优先使用约定式前缀：`feat:`、`fix:`、`docs:`、`refactor:`、`test:`。
- PR 描述应说明：
  变更目的、涉及模块、是否影响配置或主链路、已运行的测试命令。
- 涉及架构边界调整时，应明确说明是否改变了 `TUI / Runtime / Provider / Tools / Config` 的职责分工。
- 请求评审前执行 `git status`，确认格式化已完成、无无关文件混入、无密钥或本地配置泄露。

## Security & Configuration Tips
- 配置文件路径默认为 `~/.neocode/config.yaml`；配置中只保存环境变量名，不保存明文 API Key。
- `selected_provider`、`current_model`、`workdir`、`shell` 应在启动阶段完成校验，非法配置要尽早失败。
- `filesystem` 工具默认限制在工作目录内，禁止越界访问。
- `bash` 工具必须限制超时、输出长度，并避免交互式阻塞命令。
- `webfetch` 工具必须限制协议范围和响应大小，防止拉取不受控内容。
- 本地运行数据、记忆数据、临时会话和真实密钥不得默认入库；相关目录和配置文件应加入 `.gitignore`。

## MVP Delivery Priorities
- Phase 1：先跑通 `config + 一个 provider + tools registry + filesystem 工具 + runtime loop + 单会话 TUI`。
- Phase 2：补齐会话侧边栏、`bash/webfetch`、流式输出、状态栏和错误展示。
- Phase 3：增强多 provider 切换、session 持久化、更完整的权限控制和工具生态。

## Definition of Done
- 用户可以在 TUI 中输入问题并看到响应。
- Runtime 能驱动至少一个完整的 tool-calling loop。
- Agent 至少可调用一个 provider 和一个工具。
- Tool result 能正确回灌模型并继续推理。
- UI 能展示基本会话内容、运行状态和工具执行反馈。
- 配置可从 `~/.neocode/config.yaml` 正确加载并通过校验。
