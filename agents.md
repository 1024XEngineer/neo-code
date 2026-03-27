# NeoCode
## Name
NeoCode
## Description
NeoCode 是一个基于 Go 和 Bubble Tea 构建的本地编码 Agent。它在终端中运行 ReAct 闭环，可以对话、调用工具、持久化会话，并以流式方式展示推理结果。
## Capabilities
- 通过 provider 无关的 runtime 读取、理解并操作当前工作区代码。
- 调用文件系统、Shell 和 Web 工具完成读取、写入、搜索、精准修改等任务。
- 将会话历史持久化到本地，并在 TUI 侧边栏中恢复旧会话。
- 在瀑布流终端界面中实时展示模型输出和工具执行过程。
- 通过 Slash Command 在运行时更新 Provider URL、API Key 和当前模型。
## Tools
- `filesystem_read_file`：读取工作区内的文件内容。
- `filesystem_write_file`：创建或覆盖工作区内的文件。
- `filesystem_grep`：在工作区内按关键字或正则搜索代码片段。
- `filesystem_glob`：按通配模式列出文件路径，帮助 Agent 探测代码树结构。
- `filesystem_edit`：基于唯一匹配块做精准替换，避免整文件重写。
- `bash`：在限定工作区内执行 Shell 命令，并支持超时控制。
- `webfetch`：按上下文超时规则抓取远程 HTTP 内容。
## Project Structure
- `cmd/neocode`：CLI 入口。
- `internal/config`：配置模型、YAML 加载、环境变量管理和并发安全访问。
- `internal/provider`：Provider 接口、领域模型以及各模型厂商适配器。
- `internal/runtime`：事件流、Session 持久化、Prompt 编排与 ReAct 主循环。
- `internal/tools`：工具契约、注册表和具体工具实现。
- `internal/tui`：Bubble Tea 状态机、渲染层、Slash Command 和事件桥接。
## How To Work With This Project
- 从 runtime 边界开始理解系统。TUI 不能直接调用 provider 或 tools。
- 将 `internal/provider` 视为防腐层。runtime 只能操作 provider 标准消息和工具调用结构。
- 一切配置修改都必须经由 `ConfigManager`，以保证并发安全。
- API Key 不得写入 YAML，只允许在配置中保存环境变量名，并在运行时解析真实值。
- 对 config、provider streaming、runtime orchestration 或 tools 的改动，优先补充或更新测试。
## Setup Commands
- 启动应用：`go run ./cmd/neocode`
- 运行测试：`go test ./...`
- 格式化代码：`gofmt -w ./cmd ./internal`
## Interaction Tips
- 普通编码任务和仓库问题可以直接用自然语言输入。
- 本地控制命令统一通过 Composer 中的 Slash Command 触发：
  - `/set url <url>`
  - `/set key <key>`
  - `/model`
- 第一次启动应用后，NeoCode 会自动创建自己的托管目录、配置文件和会话存储目录。
