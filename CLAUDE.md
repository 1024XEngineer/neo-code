# CLAUDE.md — Claude Code 项目快速入口

本文件是 Claude Code 在本仓库中工作的快速入口。**完整且最高优先级的协作规则以 `AGENTS.md` 为准**；如果两份文档出现冲突，先遵守 `AGENTS.md`，再同步修正本文。

## 项目定位

NeoCode 是一个 Go 实现的编码 Agent。主链路必须保持为：

```text
用户输入(TUI) -> 网关中继(Gateway) -> Agent 推理(Runtime) -> 工具调用(Tools) -> 结果回传 -> UI 展示
```

## 模块索引

| 目录 | 定位 |
|------|------|
| `cmd/neocode` | CLI 入口 |
| `internal/app` | 应用装配与 bootstrap |
| `internal/config` | 配置加载与校验 |
| `internal/tui` | 事件消费与 Bubble Tea 渲染 |
| `internal/gateway` | 协议路由与边界隔离 |
| `internal/runtime` | ReAct 循环与会话编排 |
| `internal/provider` | 模型厂商协议适配 |
| `internal/context` | Prompt 构建与上下文裁剪 |
| `internal/session` | 会话领域模型与持久化 |
| `internal/tools` | 可被模型调用的工具契约与执行 |

各模块的**边界原则**见 `AGENTS.md` 第 4 节；**当前具体实现细节**参考下方对应文档。

## 当前关键设计参考

| 主题 | 文档 |
|------|------|
| Budget 闭环与 Compact 策略 | `docs/context-compact.md` |
| Runtime 事件流与 Usage 对账 | `docs/runtime-provider-event-flow.md` |
| 停止原因与决策优先级 | `docs/stop-reason-and-decision-priority.md` |
| Gateway RPC API 与错误码 | `docs/reference/gateway-rpc-api.md`、`docs/reference/gateway-error-catalog.md` |
| Provider 接入指南 | `docs/guides/adding-providers.md` |
| Skills 系统设计 | `docs/skills-system-design.md` |

## 常用命令

```bash
go run ./cmd/neocode
go build ./...
go test ./...
gofmt -w ./cmd ./internal
```

## 提交前检查

详见 `AGENTS.md` 第 10 节。核心确认：主链路完整、无跨层接线、测试补齐、`git status` 干净。
