# Skills 设计与使用说明

## 1. 目标与定位
Skills 是 NeoCode 的“能力提示层”，用于给模型提供任务约束、参考资料和工具偏好，不是新的执行层。

主链路保持不变：

`TUI -> Runtime -> Provider / Tool Manager -> Security -> Executor`

Skills 只影响：
- Context 注入内容
- 工具暴露顺序（提示优先级）

Skills 不影响：
- 工具是否真正可执行
- 权限 ask/deny/allow 决策
- MCP 注册与权限链路

## 2. 发现机制（Discovery）
当前本地发现路径：
- `<workspace>/.neocode/skills/`（项目级，优先）
- `~/.neocode/skills/`（全局）
- `~/.codex/skills/`（当 `~/.neocode/skills/` 不存在时自动回退）

加载规则：
- 扫描 root 下的子目录（忽略隐藏目录）
- 每个 skill 目录要求存在 `SKILL.md`
- 也支持 root 目录直接放置一个 `SKILL.md`
- 缺失文件、无效 metadata、空内容会记录为 `LoadIssue`，不阻塞其它 skill 加载
- 同 ID 冲突按 `project > global` 覆盖

## 3. 加载机制（Loader + Registry）
核心模块：
- `internal/skills/loader.go`：本地扫描与解析
- `internal/skills/registry.go`：内存索引、查询与刷新
- `internal/skills/filter.go`：按 source/scope/workspace 过滤

关键约束：
- `SKILL.md` 单文件读取有大小上限（默认 1 MiB）
- 前置 metadata 和正文解析后统一归一化
- skill id 去重冲突时 fail-closed（冲突项不进入可用列表）

## 4. skill 文件结构（建议）
`SKILL.md` 支持 frontmatter + 正文 section：

```md
---
id: go-review
name: Go Review
description: Go 代码审查助手
version: v1
scope: session
source: local
tool_hints:
  - filesystem_read_file
  - filesystem_grep
---

## Instruction
优先做静态阅读，再给出可执行修改建议。

## References
- [代码规范](./guides/go-style.md)

## Examples
- 先总结问题，再给补丁

## ToolHints
- filesystem_read_file
- filesystem_grep
```

## 5. 激活与会话模型
Runtime 提供会话级接口：
- `ActivateSessionSkill(session_id, skill_id)`
- `DeactivateSessionSkill(session_id, skill_id)`
- `ListSessionSkills(session_id)`
- `ListAvailableSkills(session_id)`

TUI 入口：
- `/skills`
- `/skill use <id>`
- `/skill off <id>`
- `/skill active`

说明：
- `use/off/active` 需要当前有 active session
- session 重载后会恢复 `activated_skills` 状态
- skill 在 registry 中缺失时，会标记为 missing 并发出事件

## 6. 模型如何使用 skill
Runtime 在每轮 context 构建时把激活 skills 注入 `Skills` section，内容包含：
- instruction
- tool_hints（裁剪）
- references（裁剪）
- examples（裁剪）

模型预期行为：
- 把 skill 当成策略与工作流提示
- 只调用当前真实暴露的工具 schema
- 通过正常工具调用链路执行，不跳过权限层

## 7. Tools / Security / MCP 边界
Skills 与安全边界的约束：
- skill 不能注入未注册工具
- skill 不能变成权限 allowlist
- skill 不能绕过 `PermissionEngine` 的 ask/deny/allow
- MCP 工具仍经过统一 registry + exposure filter + permission 检查

当前实现中，`tool_hints` 仅用于对已暴露工具做排序优先级调整，不会新增工具，也不会改变权限决策。

## 8. 可观测事件
Runtime 会发出以下 skills 事件（供 TUI/日志调试）：
- `skill_activated`
- `skill_deactivated`
- `skill_missing`

`/skills` 列表会展示来源层级（`project` / `global`），用于定位覆盖关系。

## 9. 兼容与扩展
当前 focus 是本地 skills；后续如需引入 remote source / marketplace，可在 `Loader` 与 `Registry` 层扩展，不需要改动 runtime 主执行链路。
