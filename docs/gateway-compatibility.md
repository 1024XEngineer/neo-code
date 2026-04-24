# Gateway 兼容性与弃用策略

本文定义 Gateway 对外契约的版本兼容规则，适用于方法、字段、错误码与发布资产。

## 1. 兼容性分层

1. Stable（稳定层）：默认向后兼容，不做破坏性改动。
2. Experimental（实验层）：允许演进，但必须有显式标注与迁移说明。

当前分层：

1. Stable Core：`gateway.authenticate`、`gateway.ping`、`gateway.bindStream`、`gateway.run`、`gateway.compact`、`gateway.cancel`、`gateway.listSessions`、`gateway.loadSession`、`gateway.resolvePermission`、`gateway.event`
2. Experimental：`wake.openUrl`

## 2. 字段弃用生命周期（必须遵守）

### 2.1 标准流程

1. **v1.2 标记 Deprecated**  
   字段继续可用；文档、日志、响应元信息中标记 `deprecated: true`（或等效说明）。
2. **v1.3 兼容保留期**  
   新客户端 SHOULD 停止依赖该字段；服务端保持兼容读取/写出策略。
3. **v1.4 正式移除**  
   字段从请求/响应契约中删除；若客户端仍发送，返回可诊断错误（通常 `invalid_frame` 或 `unsupported_action`，视场景而定）。

### 2.2 示例

若字段 `params.legacy_x` 计划移除：

1. v1.2：文档标记 Deprecated，并在 release notes 给迁移路径。
2. v1.3：继续接受 `legacy_x`，但服务端优先使用新字段。
3. v1.4：拒绝 `legacy_x`，返回明确错误与替代字段提示。

## 3. 破坏性变更门禁

以下变更 MUST 走 RFC 流程并通过灰度窗口：

1. 删除 Stable 方法。
2. 修改 Stable 方法必填字段语义。
3. 修改稳定 `gateway_code` 含义。
4. 改变资产命名规则（下载 URL / checksum 路径）。

## 4. 双产物发布兼容承诺

1. `neocode`：保留现有主入口行为。
2. `neocode-gateway`：仅承载网关服务语义。
3. 同参条件下，`neocode gateway` 与 `neocode-gateway` MUST 行为等价（参数归一化、错误语义、关键日志字段）。

## 5. 回滚原则

1. 升级失败时 SHOULD 先回滚二进制版本，再恢复配置。
2. 回滚版本 MUST 与当前稳定协议兼容（至少同主版本）。
3. 回滚步骤必须在发布说明中提供可执行命令与验证点（`/healthz`、`/rpc` 最小请求）。
