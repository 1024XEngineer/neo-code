---
name: "issue-rfc-architecture"
description: "用于创建架构类 Issue（RFC 风格）。当用户需要明确模块边界、核心设计和落地路线时使用。"
---

# Issue RFC Architecture

适用于“架构类”议题，强调边界、职责和关键设计选择。

## 使用步骤

1. 先确认目标问题和影响模块。
2. 运行命令创建 issue：

```bash
./scripts/create_issue.sh --type architecture --title "<架构标题>"
```

3. 如需自定义正文，先准备 markdown 文件，再执行：

```bash
./scripts/create_issue.sh --type architecture --title "<架构标题>" --body-file <path>
```

## 质量要求

- 正文必须包含：目标问题、现状与边界、核心设计、落地清单、验收标准、风险与回滚。
- 设计必须说明“为什么是这个方案”，并给出边界分工。
- 验收项应覆盖正常路径、异常路径、恢复路径。
