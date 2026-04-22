---
title: 记忆与 Skills
description: 说明 /memo、/remember、/forget 与 Skills 的发现、激活和会话范围。
---

# 记忆与 Skills

## 记忆相关命令

当前会话里可以直接使用：

```text
/memo
/remember 这个仓库默认使用 powershell
/forget powershell
```

对应含义：

- `/memo`：查看持久记忆索引
- `/remember <text>`：写入一条显式记忆
- `/forget <keyword>`：按关键词删除匹配记忆

这适合保留跨会话的偏好、项目事实或反复重复的上下文前提。

## Skills 是什么

Skills 是 NeoCode 的“能力提示层”，不是新的执行层。它们影响：

- Context 注入内容
- 工具暴露顺序上的提示优先级

Skills 不会改变：

- 工具是否真的可执行
- 权限 ask / deny / allow 决策
- MCP 注册与权限链路

## Skills 的发现与路径

当前本地发现路径为：

```text
~/.neocode/skills/
```

加载规则包括：

- 扫描 root 下的子目录
- 每个 Skill 目录要求存在 `SKILL.md`
- 也支持 root 目录直接放一个 `SKILL.md`

## 会话内 Skills 命令

```text
/skills
/skill use <id>
/skill off <id>
/skill active
```

含义如下：

- `/skills`：查看当前工作区 / 会话可用的 Skills
- `/skill use <id>`：激活某个 Skill
- `/skill off <id>`：停用某个 Skill
- `/skill active`：查看当前会话已激活的 Skills

Skill 激活状态会跟随会话恢复；如果 registry 中缺失，对应 Skill 会被标记为 missing 并发出事件。

## Skill 文件结构

一个典型的 `SKILL.md` 可以长这样：

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
```

## 使用建议

- 记忆适合沉淀“事实”和“偏好”
- Skill 适合沉淀“工作流提示”和“工具偏好”
- 两者都不应该替代真实工具调用和权限判断

如果你想了解它们在实现中的边界，可以继续看 [深入阅读](/reference/)。
