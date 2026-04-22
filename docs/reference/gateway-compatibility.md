# Gateway Compatibility Spec（兼容性规范）

本文档定义 Gateway 对外协议的兼容性策略，目标是让第三方在版本演进中可预测、可迁移、可回滚。

## 1. 兼容性目标

1. 保证稳定核心接口在小版本演进中默认向后兼容。  
2. 对破坏性变更提供可观测的迁移窗口，而不是“静默断裂”。  
3. 用统一规则处理字段新增、字段废弃、错误码演进与实验能力升级。  

## 2. 适用范围

本规范适用于：

1. JSON-RPC 方法名、参数字段、返回字段。  
2. 稳定错误码（`gateway_code`）集合。  
3. `/rpc` 的 HTTP 与 JSON-RPC 状态映射约束。  
4. `gateway.event` 事件 envelope 的结构约束。  

## 3. 稳定性分层

| 层级 | 范围 | 兼容承诺 |
| --- | --- | --- |
| Stable Core | `gateway.authenticate`、`gateway.ping`、`gateway.bindStream`、`gateway.run`、`gateway.compact`、`gateway.cancel`、`gateway.listSessions`、`gateway.loadSession`、`gateway.resolvePermission`、`gateway.event` | 小版本（`v1.x`）内默认向后兼容；破坏性变更必须走废弃流程。 |
| Experimental | `wake.openUrl` 等实验能力 | 允许在小版本内调整；调用方必须具备能力探测与降级路径。 |

## 4. 版本语义

1. `MAJOR`：允许破坏性变更。  
2. `MINOR`：默认非破坏性，允许新增字段/新增可选能力。  
3. `PATCH`：仅修复，不应改变对外契约。  

## 5. 什么是破坏性变更

以下变更视为破坏性：

1. 删除 Stable 方法。  
2. 将原可选字段改为必填字段。  
3. 修改字段类型（如 `string -> object`）。  
4. 改变稳定错误码语义或删除稳定错误码。  
5. 改变已承诺的状态码映射（如 `unauthorized` 不再返回 `401`）。  

## 6. 字段优雅废弃流程（重点）

### 6.1 标准流程

1. `Deprecate` 阶段：文档标注 `Deprecated`，字段仍可读可写。  
2. `Transition` 阶段：继续兼容旧字段，同时推广新字段；发布迁移指南与双写窗口。  
3. `Remove` 阶段：删除旧字段支持；若客户端仍发送旧字段，返回可诊断错误。  

### 6.2 规范要求

1. Stable 字段从 `Deprecated` 到 `Remove` 的窗口 `MUST` 至少跨 2 个 `MINOR` 版本。  
2. 在 `Deprecated` 与 `Transition` 阶段，服务端 `MUST` 保持旧字段语义不变。  
3. 在 `Remove` 阶段，服务端 `MUST` 返回稳定可判定错误（优先 `invalid_frame` 或 `invalid_action`）。  
4. 发布方 `MUST` 在发布说明中给出迁移起止版本、替代字段与示例。  

### 6.3 你要求的版本节奏示例

> 示例仅用于说明流程，不代表当前已存在该字段。

| 版本 | 动作 | 对客户端影响 | 服务端行为 |
| --- | --- | --- | --- |
| `v1.2` | 标记 `old_field` 为 Deprecated | 客户端应开始迁移到 `new_field` | 同时接受 `old_field/new_field`。 |
| `v1.3` | 迁移过渡期 | 客户端应完成切换并压测回归 | 继续兼容旧字段，文档明确“下一小版本移除”。 |
| `v1.4` | 正式移除 `old_field` | 未迁移客户端会失败 | 不再接受旧字段，返回稳定错误并附明确 message。 |

## 7. 当前实现状态与工程建议

1. 当前实现已具备稳定 `gateway_code` 与方法分层。  
2. 当前实现对“废弃字段”的机器可读告警尚无统一协议字段。  
3. 在机器告警机制落地前，应以文档、发布说明、迁移指南作为主通知渠道。  

## 8. 客户端兼容性实现建议

1. 客户端 `MUST` 忽略未知返回字段，避免因新增字段崩溃。  
2. 客户端 `SHOULD` 对 Experimental 方法做能力探测，不应硬编码强依赖。  
3. 客户端 `SHOULD` 以 `gateway_code` 作为异常分支主键，而非 `message` 文本。  
4. 客户端 `SHOULD` 在升级前执行双版本回归（当前版本与目标版本）。  
