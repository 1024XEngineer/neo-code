# Repository Guidelines

## Project Structure & Module Organization
- `cmd/tui/main.go` 是TUI模式的主要入口，负责初始化配置和启动终端用户界面。
- `cmd/server/main.go` 是服务器模式的入口，用于验证服务组装，目前仅作为占位符存在。
- `internal/tui/core/` 包含TUI的核心逻辑，实现了Bubble Tea ELM架构（Model-Update-View）。
- `internal/tui/infra/` 包含TUI的基础设施层，负责与后端服务交互。
- `internal/server/domain/` 定义了核心领域模型和接口，遵循依赖倒置原则。
- `internal/server/service/` 实现了应用服务层，负责编排业务流程。
- `internal/server/infra/provider/` 包含具体的外部服务提供方实现，如ModelScopeProvider。
- `internal/server/infra/repository/` 包含数据访问层的实现，包括文件存储和内存存储。
- `configs/` 目录包含应用配置相关的代码和默认人设文件。
- `docs/` 目录包含项目文档，如架构设计说明和安全指南。
- `data/` 目录用于存储运行时数据，如长期记忆文件。

## Build, Test, and Development Commands
- `go build ./...` 编译所有Go包；在推送前修复语法或依赖问题。
- `go test ./...` 执行所有Go测试并报告覆盖率；在修改`internal/`目录下的逻辑后重新运行。
- `go run ./cmd/tui` 从仓库根目录运行TUI模式进行手动实验。
- `go run ./cmd/server` 运行服务器模式以验证服务组装。
- `gofmt -w <file>` (或 `go fmt ./...`) 格式化Go文件以保持缩进/空格一致，在暂存前执行。

## Coding Style & Naming Conventions
- 遵循惯用的Go语言风格：使用短小的全小写包名 (`tui`, `server`, `domain`, `service`), 导出标识符使用PascalCase，未导出标识符使用camelCase。
- 使用制表符进行缩进（Go默认），尽可能将行长度控制在~120字符以内。
- 在提交前应用`gofmt`/`goimports`；避免手动格式化。
- 为导出符号添加完整的句子注释，以满足Go工具链和未来读者的需求。

## Testing Guidelines
- 将单元测试命名为 `*_test.go` 并且函数名为 `TestXxx`；当添加新的测试套件时模仿现有的包结构。
- 当前行内没有使用标准库以外的测试框架。
- 在任何触及接口边界的更改后运行 `go test ./...`，特别是在 `provider` 提供方或 `service` 层内。

## Commit & Pull Request Guidelines
- 保持提交小而具有描述性；优先使用约定的前缀，例如 `feat:`、`fix:`、`docs:` 或 `refactor:`，后跟简要摘要（例如 `feat: add model switch command`）。
- 包含简洁的PR描述，列出关联的问题（如果有），并注明已运行的测试命令。
- 在请求审查之前，运行 `git status`，确保已应用gofmt，并仔细检查是否有秘密值泄露到被跟踪的文件中（例如 `.env`）。

## Security & Configuration Tips
- 将 `.env` 或类似文件视为本地配置；不要提交密钥。如果需要共享值，请在README或本指南中记录它们。
- 将API密钥保留在源文件之外；优先在运行时通过环境变量传递。
- API Key必须放在系统环境变量 `AI_API_KEY` 中，不再写入 `config.yaml`。
- `config.yaml` 已在 `.gitignore` 中忽略，不应提交真实密钥。
- `data/` 已在 `.gitignore` 中忽略，本地记忆不会默认入库。
