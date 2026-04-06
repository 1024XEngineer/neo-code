# CLI 模块

## 角色定位

CLI 负责程序启动与命令入口，当前主要职责是装配应用并启动 TUI。

## [CURRENT] 实现基线

- 入口文件：`cmd/neocode/main.go`。
- 当前行为：调用 `app.NewProgram` 并运行 Bubble Tea 程序。
- 尚未形成完整的子命令体系（如 `mcp`、`exec` 等）。

## [PROPOSED] 目标态

- 引入结构化命令路由，覆盖 `chat`、`exec`、`config`、`mcp` 等子命令。
- 在 CLI 层支持网关模式与本地 runtime 模式切换。

## 上下游边界

- 上游：终端用户。
- 下游：`internal/app` 装配层、tui/runtime。
- 约束：CLI 不承载核心业务编排逻辑。

## 联调注意事项

- 当前 CLI 主要是启动器，不要按完整命令平台能力依赖。
