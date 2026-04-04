# 工具执行期 TOCTOU 防护设计

## 背景
`WorkspaceSandbox` 在此前版本主要完成“路径边界校验”，执行链为：

`Permission + Sandbox Check -> Tool.Execute(path string)`

这会留下检查与执行之间的 TOCTOU（Time-of-Check to Time-of-Use）窗口：路径在校验后被替换，工具仍可能访问到非预期对象。

## 本次实现
本次将链路升级为：

`Permission + Sandbox Check -> WorkspaceExecutionPlan -> Tool.Execute(plan + args)`

核心变化如下：

1. `WorkspaceSandbox.Check` 不再只返回 `error`，而是返回 `*WorkspaceExecutionPlan`。
2. `ToolManager` 将 plan 透传到 `ToolCallInput.WorkspacePlan`。
3. `filesystem_read_file` / `filesystem_write_file` / `filesystem_edit` / `bash` 在真实执行前调用 `plan.ValidateForExecution()` 复验锚点状态。
4. 工具使用 `tools.ResolveWorkspaceTarget` 统一消费 plan，避免再次仅依赖字符串路径解析。

## 执行期绑定机制
`WorkspaceExecutionPlan` 在 sandbox 阶段记录：

- 规范化后的 workspace root
- 规范化后的最终 target
- 最近存在路径锚点（anchor）
- 锚点快照（模式、大小、修改时间、符号链接目标）

工具执行前会复验：

1. 当前锚点是否仍为同一路径；
2. 锚点快照是否与校验阶段一致；
3. 锚点解析后的真实路径是否仍在 workspace 内。

若任一步失败，返回稳定错误：`workspace target changed before execution` 或 `escapes workspace root via symlink`。

## 当前防护边界
已覆盖：

- 校验后 symlink 被替换导致 read 越界
- 校验后父目录被替换导致 write 越界
- 校验后 bash workdir 被替换导致 cwd 漂移

仍存在限制（已显式记录）：

- 未实现跨平台 `openat/no-follow/dirfd` 级别的系统调用原子封装
- 仍非容器级隔离，不替代系统沙箱

本次目标是把“校验结果”显式带入执行期，显著缩小 TOCTOU 窗口，并为后续更强执行器（含 MCP）预留统一接口。
