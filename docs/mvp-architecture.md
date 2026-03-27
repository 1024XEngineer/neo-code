# NeoCode Coding Agent MVP 架构说明

## 目标

项目围绕最小可运行闭环设计：

`用户输入 -> Runtime 推理 -> Tool 调用 -> Tool Result 回灌 -> UI 展示`

## 模块边界

- `Runtime`
  - 唯一编排中心
  - 负责会话上下文、Prompt 组装、Provider 调用、Tool 执行和事件派发
- `TUI`
  - 只负责交互和渲染
  - 不直接调用 Provider
  - 不直接执行 Tool
- `Provider`
  - 只负责模型协议适配、请求组装、响应解析和错误映射
- `Tools`
  - `Registry` 负责注册、schema 暴露和工具查找
  - `Executor` 负责执行工具、补齐标准结果并统一错误包装
- `Config`
  - 负责配置加载、默认值补全和启动校验

## 已落地实现

- `config`
  - 首次启动自动生成 `~/.neocode/config.yaml`
  - 默认补全 `selected_provider / current_model / workdir / shell / sessions_path`
- `provider`
  - OpenAI 兼容 Chat Completions 实现
  - 支持 tool schema 下发
  - 支持 tool call 解析
  - 支持流式增量输出
- `tools`
  - `fs_read_file`
  - `fs_write_file`
  - `fs_list_dir`
  - `fs_search`
  - `bash_exec`
  - `web_fetch`
- `runtime`
  - `SessionStore`
  - `PromptBuilder`
  - `EventBus`
  - Tool-calling loop
  - 多 provider 目录与切换
  - 会话持久化
- `tui`
  - 会话侧边栏
  - Provider 切换
  - 状态栏
  - 消息区
  - 输入区
  - 流式响应展示

## Runtime Loop

1. TUI 提交用户输入
2. Runtime 写入会话消息
3. Runtime 通过 tool catalog 组装 system prompt、历史消息和 tool schema
4. Runtime 调用当前 provider
5. 如果返回普通文本，则流式派发 chunk，并最终写入 assistant message
6. 如果返回 tool calls，则通过 tool executor 执行工具
7. Tool Result 回写到会话
8. Runtime 再次调用 provider，直到结束或达到最大轮数

## 配置与首次启动

- 默认配置文件路径：`~/.neocode/config.yaml`
- 默认会话存储路径：`~/.neocode/sessions.json`
- 如果配置文件不存在，启动时会自动生成默认配置
- 如果当前选中的 provider 缺少 API Key 环境变量，启动会直接给出中文提示

## Session 持久化

- 会话默认落盘到 `~/.neocode/sessions.json`
- 启动时自动恢复历史会话
- 创建会话、改标题、追加消息时都会自动保存

## Provider 切换

- 配置层支持多个 provider 条目
- Runtime 维护可用 provider 列表和当前激活 provider
- TUI 通过 `Ctrl+P` 循环切换 provider
- 当前已实现 `openai` / `openai-compatible` 类型

## 安全约束

- `filesystem` 工具限制在 `workdir` 内
- `bash_exec` 非交互执行，并带超时与输出截断
- `web_fetch` 仅允许 `http/https` 且限制响应体大小
- 配置文件只保存环境变量名，不保存明文 API Key

## TUI 交互升级

- TUI 仍然严格遵守边界：
  - 只消费 runtime 事件
  - 不直接调用 provider
  - 不直接执行 tools
- 界面从基础消息区升级为“阅读与操作优先”的终端工作台：
  - 左侧会话栏默认收起，以抽屉方式展开
  - 主区域使用 conversation viewport 展示结构化消息卡片
  - 右侧 runtime 面板展示活跃工具和最近活动
  - 底部使用多行 composer 承载输入
- assistant 消息中的 fenced code block 会被识别为独立代码块，支持块级导航与复制。
- 主会话区支持鼠标滚轮、方向键、`PgUp / PgDn`、`Home / End`、`g / G` 等阅读操作；当用户离开底部时，新的流式输出不会强制打断当前阅读位置。
- 这一轮升级没有改变 `TUI / Runtime / Provider / Tools / Config` 的职责边界，只增强了 TUI 的状态组织、渲染能力与交互闭环。
- 当前 `internal/tui` 的渲染层已经按职责拆分为 root 布局、conversation 视图、runtime 视图、主题变量和共享 panel helper，后续新增组件时应优先沿这个方向扩展，而不是重新堆回单个大文件。
