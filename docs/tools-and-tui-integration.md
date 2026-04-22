# Tools 与 TUI 集成设计
## 工具契约
每个工具都应提供：
- 合法且稳定的工具名
- 面向模型的简明描述
- 类 JSON Schema 的参数定义
- 接收 `context.Context` 和结构化输入的 `Execute` 方法

## Registry 职责
- 以名字注册工具
- 向 Provider 返回可消费的工具 schema 列表
- 根据工具名分发执行，并把失败规范化为可回灌给模型的 ToolResult

## 当前工具集
- `filesystem_read_file`
- `filesystem_write_file`
- `filesystem_grep`
- `filesystem_glob`
- `filesystem_edit`
- `bash`
- `webfetch`
- `memo_remember`
- `memo_recall`
- `memo_list`
- `memo_remove`

## Bash Git 语义治理
- 不新增 `git_*` 工具，Git 能力统一走 `bash`，并在工具层执行前做语义解析。
- `bash` 调用会被归类为：`read_only`、`local_mutation`、`remote_op`、`destructive`、`unknown`。
- 推荐策略默认：
  - `bash_git_read_only`、`bash_git_remote_op`、`bash_git_destructive`、`bash_git_local_mutation`、`bash_git_unknown` 统一走审批。
  - 非 Git 或无法判定语义的 `bash` 命令走兜底审批。
- Session 级权限记忆基于 `permission_fingerprint`，同义写法可复用授权；`remote_op` 不复用 `allow_session`，每次都要求审批。
- `bash` ToolResult 会附带结构化字段：`ok`、`classification`、`normalized_intent`、`permission_fingerprint`、`exit_code`，并通过 `status/ok/meta.*` 回灌模型。

## Memo 能力集成
- `memo_remember`、`memo_recall`、`memo_list`、`memo_remove` 作为标准工具暴露给模型，沿 `Runtime -> Tool Manager -> internal/tools/memo` 链路执行。
- 自动记忆提取不作为单独工具暴露给模型，也不由 TUI 直接触发；它在 runtime 完成最终回复后由 memo 子系统后台调度。
- TUI 的 `/memo`、`/remember`、`/forget` 等 Slash Command 不再直接依赖 memo service，而是通过 `Runtime.ExecuteSystemTool` 统一入口触发系统工具执行，保证 UI 与 memo 逻辑解耦。
- TUI 不会展示后台自动提取的中间状态。

## Skills 能力集成
- Skills 由 `internal/skills` 统一发现、加载和注册；TUI 不直接读取 `SKILL.md` 文件。
- TUI 通过 runtime 接口管理会话激活状态：`/skills`、`/skill use <id>`、`/skill off <id>`、`/skill active`。
- Skills 只影响提示注入与工具排序优先级，不改变工具执行入口；真实调用仍走 `Runtime -> Tool Manager -> Security -> Executor`。
- Skills 不提供权限豁免；命中 ask/deny 规则时行为与未启用 skill 保持一致。

## TUI 集成方式
- 本地配置操作统一通过 Slash Command 完成，例如 Base URL、API Key 和模型选择
- runtime 事件以内联形式渲染到 transcript 中，而不是单独拆出控制台面板
- 工具开始和结束事件会以轻量提示插入聊天流，使交互更沉浸

## 交互原则
Composer 是唯一的控制入口。只要某个功能本质上是在修改本地 Agent 状态，优先通过 Slash Command 发现和触发，而不是继续叠加额外快捷键。
