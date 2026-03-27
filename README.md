# NeoCode Coding Agent MVP

一个基于 Go + Bubble Tea 的本地 Coding Agent MVP，当前已经打通完整主链路：

`用户输入 -> Runtime 推理编排 -> Provider 调用 -> Tool 调用 -> Tool Result 回灌 -> TUI 展示`

## 现在能做什么

- 自动生成默认配置文件
  - 首次启动如果没有 `~/.neocode/config.yaml`，程序会自动创建
- 配置管理
  - 加载并校验 `provider / model / workdir / shell / sessions_path`
- Provider
  - 内置 OpenAI 兼容 Chat Completions
  - 支持普通响应与流式响应
  - 支持 tool schema 下发和 tool call 解析
- Tools
  - `filesystem`
  - `bash`
  - `webfetch`
- Runtime
  - 单一编排中心
  - Tool-calling loop
  - 多 provider 切换
  - 会话持久化
  - 运行事件派发
- TUI
  - 多会话侧栏
  - Provider 状态栏
  - 消息视图
  - 输入框
  - 流式输出展示

## 快速开始

1. 设置 API Key 环境变量，例如：

```powershell
$env:OPENAI_API_KEY="your-api-key"
```

2. 直接启动：

```bash
go run ./cmd/neocode
```

说明：

- 如果本机还没有 `~/.neocode/config.yaml`，程序会自动生成一份默认配置
- 默认配置会使用 `openai` 这个 provider
- 会话默认保存在 `~/.neocode/sessions.json`

也可以显式指定配置文件：

```bash
go run ./cmd/neocode -config ./config.example.yaml
```

## 默认配置说明

首次自动生成的配置大致如下：

```yaml
providers:
  - name: openai
    type: openai
    base_url: https://api.openai.com/v1
    model: gpt-4.1-mini
    api_key_env: OPENAI_API_KEY

selected_provider: openai
current_model: gpt-4.1-mini
workdir: .
shell: powershell
sessions_path: ~/.neocode/sessions.json
```

你通常只需要关心这几项：

- `providers`：模型服务配置，当前支持 `openai` / `openai-compatible`
- `selected_provider`：默认启用的 provider
- `current_model`：当前模型名
- `workdir`：工具访问和命令执行的工作目录
- `shell`：`bash_exec` 使用的 shell
- `sessions_path`：会话持久化文件位置

## 交互快捷键

- `Enter`：提交问题
- `Ctrl+N`：新建会话
- `Tab / Shift+Tab`：切换会话
- `Ctrl+P`：切换 provider
- `Ctrl+C`：退出

## 开发命令

```bash
go fmt ./...
go test ./...
go build ./...
```

## 内置工具

- `fs_read_file`：读取工作目录内文本文件
- `fs_write_file`：新建、覆盖或追加工作目录内文本文件
- `fs_edit_file`：对已有文本文件执行精确片段替换，适合局部修改
- `fs_list_dir`：列出工作目录内目录内容
- `fs_search`：在工作目录内搜索文本
- `bash_exec`：在当前工作目录执行非交互 shell 命令
- `web_fetch`：抓取 HTTP/HTTPS 网页文本内容

当前 `tools` 层的职责边界如下：

- `Registry` 只负责工具注册、schema 暴露和按名称查找
- `Executor` 负责实际执行工具，并统一补齐 `ToolCallID`、`Name`、错误结果和错误内容
- `Runtime` 通过注入的 tool catalog 与 tool executor 编排工具调用，不直接耦合具体工具实现

## 项目结构

```text
cmd/neocode/main.go
internal/app/
internal/config/
internal/provider/
internal/runtime/
internal/tools/
internal/tui/
docs/
```

架构说明见 [docs/mvp-architecture.md]

## TUI 交互升级

- TUI 现在默认采用“内容优先”的工作台布局：
  - 左侧会话栏默认收起，需要时以抽屉方式展开
  - 主区域优先展示会话内容
  - 右侧保留 runtime 面板，用于查看工具和活动
  - 底部保留多行 composer
- 会话内容不再只是纯文本，而是按消息角色渲染为结构化卡片：
  - user
  - assistant
  - tool
  - streaming
- fenced code block 会被识别为独立代码块，支持块级选中、导航和复制。
- 主会话区与 runtime 面板都支持鼠标滚轮和键盘滚动；当你离开底部阅读历史内容时，新的流式输出不会强制把视图拉回到底部。
- 复制操作以内建动作为主，不再依赖终端原生选中文本：
  - `y`：复制当前代码块
  - `Y`：复制当前消息
- 渲染代码已按组件职责拆分为 `theme`、共享 panel helper、root 布局、conversation 视图和 runtime 视图，便于后续继续迭代而不把 `view.go` 堆成单文件。

## TUI 快捷键

- `Enter`：发送输入
- `Ctrl+J`：插入换行
- `Ctrl+L`：清空当前输入
- `Ctrl+B`：展开或收起会话抽屉
- `/`：打开会话抽屉并聚焦筛选
- `Ctrl+N`：新建会话
- `Ctrl+P`：切换 provider
- `PgUp / PgDn`：按页滚动主会话区
- `Home / End` 或 `g / G`：跳到顶部或底部
- `[` / `]`：切换上一段或下一段代码块
- `y` / `Y`：复制当前代码块或当前消息
- `L`：跳到最新输出
- `?`：打开帮助面板
