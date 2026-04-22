---
name: "issue-rfc-implementation"
description: "用于创建实现类 Issue（RFC 执行单风格）。当用户要把已确认提案/架构落地成可执行任务时使用。"
---

# Issue RFC Implementation

适用于“实现类”议题，强调关联上游 RFC、改动范围和验证闭环。

## 使用步骤

1. 先确认已关联的提案/架构 issue。
2. 运行命令创建 issue：

```bash
./scripts/create_issue.sh --type implementation --title "<实现标题>"
```

3. 如需自定义正文，先准备 markdown 文件，再执行：

```bash
./scripts/create_issue.sh --type implementation --title "<实现标题>" --body-file <path>
```

## 质量要求

- 正文必须包含：关联 RFC、目标问题、实现设计、任务清单、测试验证、风险与回滚。
- 任务清单要可执行且可追踪，不接受抽象口号。
- 测试清单至少覆盖正常路径、边界条件、异常分支。
