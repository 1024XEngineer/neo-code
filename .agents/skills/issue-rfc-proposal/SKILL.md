---
name: "issue-rfc-proposal"
description: "用于创建提案类 Issue（RFC 风格）。当用户希望在本仓库发起‘目标问题 -> 设计 -> 落地清单’的提案讨论时使用。"
---

# Issue RFC Proposal

适用于“提案类”议题，要求输出遵循：目标问题（Why）-> 设计方案（How）-> 落地清单（What）。

## 使用步骤

1. 先让用户明确提案标题与核心痛点。
2. 运行命令创建 issue：

```bash
./scripts/create_issue.sh --type proposal --title "<提案标题>"
```

3. 如需自定义正文，先准备 markdown 文件，再执行：

```bash
./scripts/create_issue.sh --type proposal --title "<提案标题>" --body-file <path>
```

## 质量要求

- 正文必须包含：目标问题、设计方案、落地清单、验收标准、风险与回滚。
- 非目标必须明确，避免提案发散。
- 验收标准必须可验证，避免空泛表述。
