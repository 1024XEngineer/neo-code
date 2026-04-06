# NeoCode 架构文档

> 状态：v2.0.0-draft.1  
> 目录：`docs/architecture`  
> 目标：将架构说明按模块拆分为 `README.md + interface.go`，并逐步替代 `docs/interfaces`。

## 标签约定

- `[CURRENT]`：当前仓库已实现且可联调。
- `[PROPOSED]`：目标态设计。
- `[NOT IMPLEMENTED YET]`：明确未落地，不可按当前行为依赖。

## 总体架构

### [CURRENT] 当前主链路

```mermaid
flowchart LR
    CLI["CLI"] --> TUI["TUI"]
    TUI --> Runtime["Runtime"]
    Runtime --> Context["Context"]
    Runtime --> Provider["Provider"]
    Runtime --> Tools["Tools"]
    Runtime --> Config["Config"]
    Runtime --> Session["Session Store"]
```

### [PROPOSED][NOT IMPLEMENTED YET] 目标入口

```mermaid
flowchart LR
    Client["TUI / CLI / Web"] --> Gateway["Gateway (REST/WS)"]
    Gateway --> Runtime["Runtime"]
    Runtime --> Context["Context"]
    Runtime --> Provider["Provider"]
    Runtime --> Tools["Tools"]
    Runtime --> Config["Config"]
    Runtime --> Session["Session"]
```

## 模块清单

| 模块 | 文档 | 契约 | 当前状态 |
|---|---|---|---|
| Runtime | [runtime/README.md](./runtime/README.md) | [runtime/interface.go](./runtime/interface.go) | `[CURRENT]` |
| Context | [context/README.md](./context/README.md) | [context/interface.go](./context/interface.go) | `[CURRENT]` |
| Provider | [provider/README.md](./provider/README.md) | [provider/interface.go](./provider/interface.go) | `[CURRENT]` |
| Tools | [tools/README.md](./tools/README.md) | [tools/interface.go](./tools/interface.go) | `[CURRENT]` |
| Config | [config/README.md](./config/README.md) | [config/interface.go](./config/interface.go) | `[CURRENT]` |
| TUI | [tui/README.md](./tui/README.md) | [tui/interface.go](./tui/interface.go) | `[CURRENT]` |
| CLI | [cli/README.md](./cli/README.md) | [cli/interface.go](./cli/interface.go) | `[CURRENT]` |
| Gateway | [gateway/README.md](./gateway/README.md) | [gateway/interface.go](./gateway/interface.go) | `[PROPOSED]` |
| Session | [session/README.md](./session/README.md) | [session/interface.go](./session/interface.go) | `[CURRENT]` |

## 阅读顺序

1. 先读 `runtime`、`session`，建立主编排与持久化基线。
2. 再读 `context`、`provider`、`tools`、`config`，理解编排依赖边界。
3. 最后读 `tui`、`cli`、`gateway`，理解入口层与协议层演进。

## 与旧文档关系

- 本目录是新的模块化架构事实源。
- `docs/interfaces` 进入“逐步迁移替代”阶段，迁移中保持语义一致。
- `docs/session-persistence-design.md` 的关键内容会逐步沉淀到 `docs/architecture/session/*`。

## 约束

- 本目录中的 `interface.go` 用于架构契约表达，不参与生产代码实现。
- 未落地能力必须显式标注 `[PROPOSED]` 或 `[NOT IMPLEMENTED YET]`。
