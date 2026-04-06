# Interfaces 文档导航

> 文档域：`docs/interfaces`  
> 当前版本：v2.0.0-draft.3  
> 最后更新：2026-04-06

本目录维护 NeoCode Runtime V2 接口规范。  
当前处于“逐步迁移替代”阶段：模块化架构文档已迁移到 [docs/architecture](../architecture/README.md)。

## 术语与标签约定

- `[CURRENT]`：当前仓库可联调事实。
- `[PROPOSED]`：V2 目标态设计。
- `[FUTURE]`：后续阶段能力。
- `[NOT IMPLEMENTED YET]`：明确未落地，不可按现状依赖。

## 文档定位

- `docs/interfaces`：接口语义收敛与迁移映射。
- `docs/architecture`：模块化架构事实源（`README.md + interface.go`）。

## 文档清单

- [runtime-interface-spec.md](./runtime-interface-spec.md)  
  运行时主规范，覆盖 Runtime/Context/Provider/Tools/Config 及事件语义。
- [context-compact.md](./context-compact.md)  
  compact 专题规范，覆盖 manual/reactive 当前行为与 proactive 目标态。
- [interface-migration-map.md](./interface-migration-map.md)  
  旧协商名、当前命名与 V2 定名的状态化映射（含 session 维度）。

## 阅读顺序（推荐）

1. 先读 [docs/architecture/README.md](../architecture/README.md)。  
2. 再读 `runtime-interface-spec.md` 的 `Current Baseline`。  
3. 然后读 `context-compact.md` 与 `interface-migration-map.md`。  

## 当前与目标入口模型

- `[CURRENT]` 当前稳定入口：`TUI/CLI -> Runtime`。
- `[PROPOSED][NOT IMPLEMENTED YET]` 目标入口：`TUI/CLI/Web -> Gateway(REST/WS) -> Runtime`。

## 变更规则

- 规范变更必须先改文档，再改实现。
- 未落地能力必须标注 `[PROPOSED]` 或 `[NOT IMPLEMENTED YET]`。
- 若实现与文档冲突，需在同一迭代修正文档或实现至少一侧，不允许长期失真。
- 所有接口片段统一使用 Go `interface/struct` 形式，导出注释使用中文。

## 当前里程碑约束

- 本轮只修正文档，不改 runtime/context/provider/tools 业务代码。
- reactive compact 维持“目标态必选项”定位，但当前主链未自动接入。
- proactive compact 本轮仅作为 Proposed 能力保留。
