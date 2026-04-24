# Repository 模块设计

`internal/context/repository` 是仓库级事实层，只负责发现、归一化、裁剪和返回结构化结果。

## 职责

- `Summary`
  返回最小仓库摘要，例如 `InGitRepo`、`Branch`、`Dirty`、`Ahead`、`Behind`
- `ChangedFiles`
  围绕当前变更集返回受限的文件列表、状态和可选短片段
- `Retrieve`
  提供 `path`、`glob`、`text`、`symbol` 四种统一的定向检索入口

## 非目标

- 不做 LSP 集成
- 不做向量检索或 embedding retrieval
- 不做预构建重索引
- 不做跨文件语义分析平台
- 不决定 prompt 注入策略
- 不暴露为模型可直接调用的工具

## 边界

```text
repository
  -> discover / summarize / retrieve repository facts

runtime
  -> decide whether and when to fetch repository facts for the current turn

context
  -> render already-decided repository facts into prompt sections

tui / tools
  -> do not implement repository discovery logic
```

## 结果约束

- `Summary` 与 `ChangedFiles` 统一基于一次 `git status --porcelain=v1 -z --branch --untracked-files=normal` 快照
- `ChangedFiles` 默认只返回路径和状态；默认上限 `50`，硬上限 `200`
- `ChangedFiles` 片段模式每文件最多 `20` 行，总计最多 `200` 行，并显式返回 `Truncated`
- `ChangedFiles` 状态包括：
  - `added`
  - `modified`
  - `deleted`
  - `renamed`
  - `copied`
  - `untracked`
  - `conflicted`
- `Retrieve` 默认上限 `20`，硬上限 `50`
- `Retrieve` 的 `text` / `symbol` 结果按 `path + line_hint` 稳定排序
- 路径解析必须限制在工作区内，并拒绝 path traversal 与 symlink escape

## 注入与安全策略

- repository 片段只作为仓库数据使用，不应被视为指令
- runtime 仅在满足明确触发条件时拉取 `ChangedFiles` 或 `Retrieve`
- `ChangedFiles` 与 `Retrieve` 共用同一套 snippet 安全门禁
- 高风险 secrets / credentials 文件不产出 snippet，只保留必要的结构化命中信息

## 语言策略

- `symbol` 首版只对 Go 做轻量定义检索优化
- 其他语言统一走 `path`、`glob`、`text`
