# Todo Schema Migration

## 字段语义

- `required`:是否参与 final 收口拦截,默认 `true`。
- `blocked_reason`:阻塞原因 enum,合法值 `internal_dependency / permission_wait / user_input_wait / external_resource_wait / unknown`,以及空字符串 `""`(代表"未阻塞")。

## 不变量(v7 起)

**`blocked_reason != "" 当且仅当 status == "blocked"`**

- 当 todo 处于 `blocked` 状态时,`blocked_reason` 必须给出 5 个 enum 值之一(`unknown` 表示"已阻塞但无法说出具体原因",仍是合法值,只是含义改为业务级"原因不详",而非 schema 级 sentinel)。
- 当 todo 处于其他任何状态(`pending` / `in_progress` / `completed` / `failed` / `canceled`)时,`blocked_reason` 必须为空字符串。
- 状态从 `blocked` 转出(via `ClaimTodo` / `CompleteTodo` / `FailTodo` 或 `UpdateTodo` 的 patch)时,`blocked_reason` 由实现自动清空,不依赖调用方记得清。

## 兼容与迁移

- `required=nil` 视为 `required=true`(兼容旧 session)。
- 旧数据(v6 及更早)中状态非 blocked 但 `blocked_reason="unknown"` 的 todo,在第一次通过 `normalizeAndValidateTodos` 加载时由 `normalizeTodoItem` 自动清空,无业务侧动作。
- 旧 blocked todo 缺失 `blocked_reason` 时,按空字符串保留(即"该 blocked todo 暂未声明原因");LLM/工具后续可通过 `UpdateTodo` 写入真实原因或显式 `unknown`。

## LLM 工具协议

`todo_write` schema 中 `blocked_reason` 字段保留 5 个合法 enum 值,描述明确:**仅当 `status == "blocked"` 时填写;其他状态请省略本字段**。即使模型违反规则把字段塞到非 blocked todo 上,服务端在 `normalizeTodoItem` 中也会强制清空。

## 持久化版本

- `CurrentTodoVersion = 7`(从 6 升级)。
- 归一化流程(`normalizeTodoItem` + `normalizeAndValidateTodos`)在加载与写入两侧都应用上述不变量,作为旧数据的迁移钩子;无需额外的版本对比 / 一次性迁移脚本。
- wire 出口(gateway / runtime events / tool result / verifier snapshot)直接序列化 raw `BlockedReason`,`omitempty` 自动隐藏空值;前端/TUI 拿到的 `blocked_reason` 字段只在 status=blocked 时存在。

## ReplaceTodos / `todo_write action:"plan"` 不变量

- `items` 必须是非空集合。空集合(`[]`)在工具层(`internal/tools/todo/write.go` actionPlan 守卫)与 session 层(`Session.ReplaceTodos`)都会被拒绝(`errTodoInvalidArguments`)。
- 清空意图请走以下任一路径:
  - 单条删除:`todo_write { action: "remove", id: "..." }` 逐条删;
  - 状态切换:`todo_write { action: "set_status", id: "...", status: "completed" / "canceled" }`,保留 todo 历史记录。
- 这条契约从 `CurrentTodoVersion = 7` 起生效;前端 `useRuntimeInsightStore.setTodoSnapshot` 也会忽略空 items 更新作为兜底,防止任何回归再次把活的 todo 列表抹成 stale。
