# Todo Schema Migration

## 新增字段
- `required`：是否参与 final 收口拦截，默认 `true`。
- `blocked_reason`：`internal_dependency / permission_wait / user_input_wait / external_resource_wait / unknown`。

## 兼容策略
- 旧数据缺失 `required` 时，按 `true` 处理。
- 旧数据缺失 `blocked_reason` 时，按 `unknown` 处理。
- 旧 blocked todo 不会因新字段缺失导致反序列化失败。

## 默认值语义
- `required=nil` 视为 required=true（兼容旧 session）。
- `blocked_reason=""` 规整为 `unknown`。

## 持久化迁移注意事项
- `CurrentTodoVersion` 升级到 `5`。
- 归一化流程在加载与写入两侧都应用缺省值规则。
- 工具层 schema 与 patch 同步支持新字段，避免 runtime/工具协议漂移。

