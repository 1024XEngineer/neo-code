# Gateway 第三方接入协作指南

本文面向第三方客户端开发者，目标是让接入方在不阅读源码的前提下完成最小接入与常见故障定位。

## 1. Getting Started

### 1.1 启动方式

推荐优先使用 Gateway-Only 发布物：

```bash
neocode-gateway --http-listen 127.0.0.1:8080
```

兼容方式：

```bash
neocode gateway --http-listen 127.0.0.1:8080
```

### 1.2 最小握手

1. 连接 `/rpc`、`/ws` 或 `IPC`。
2. 发送 `gateway.authenticate`（或在 HTTP 头使用 Bearer Token）。
3. 可选发送 `gateway.bindStream` 绑定会话流。
4. 发送 `gateway.run`。

## 2. Message Protocol

网关控制面统一 JSON-RPC 2.0。

### 2.1 请求结构

```json
{
  "jsonrpc": "2.0",
  "id": "req-1",
  "method": "gateway.run",
  "params": {
    "session_id": "sess-1",
    "input_text": "请审查 README"
  }
}
```

### 2.2 响应结构

```json
{
  "jsonrpc": "2.0",
  "id": "req-1",
  "result": {
    "type": "ack",
    "action": "gateway.run",
    "request_id": "req-1"
  }
}
```

### 2.3 通知结构

网关事件使用 `gateway.event` 通知，典型包含 run 进度、完成、错误。

## 3. Status Codes

接入方应同时处理三层状态：

1. HTTP 状态（如 401）。
2. JSON-RPC `error.code`（如 `-32602`）。
3. 网关稳定码 `error.data.gateway_code`（如 `unauthorized`）。

建议以 `gateway_code` 作为应用层分支主键。

## 4. Client Best Practices

1. MUST 实现断线重连，并在重连后重新认证。
2. SHOULD 对幂等请求使用客户端 request id，便于重试去重。
3. MUST 对 `gateway.run` 与流事件建立会话/运行关联键（`session_id` + `run_id`）。
4. SHOULD 对瞬时错误做指数退避重试；对鉴权/参数错误直接失败。
5. SHOULD 维护心跳超时策略，及时回收失活连接。

## 5. Failure Playbook

### 5.1 连接失败

现象：dial 失败或 `gateway_unreachable`。  
处理：优先检查网关进程、监听地址、权限与本机防火墙。

### 5.2 认证失败

现象：HTTP `401` 或 `gateway_code=unauthorized`。  
处理：检查 token 文件、Bearer 头格式、token 是否过期/错配。

### 5.3 参数错误

现象：`gateway_code=missing_required_field` 或 `invalid_action`。  
处理：按 API 文档逐项校验 `params` 必填字段与枚举值。

### 5.4 超时与取消

现象：`gateway_code=timeout` 或运行长时间无事件。  
处理：客户端触发 `gateway.cancel`，并按 run_id 做状态回收。

## 6. 部署拓扑建议

1. 本地内嵌网关：`neocode gateway`，适合单机开发工作流。
2. 独立网关服务：`neocode-gateway`，适合第三方客户端统一接入与独立运维。

建议：

1. 默认仅监听回环地址。
2. 对外暴露时显式配置监听地址与访问边界，不应直接公网裸露。

## 7. 最小鉴权 / ACL 模板

最小安全基线（示意）：

```yaml
gateway:
  security:
    acl_mode: strict
    # token_file 默认为 ~/.neocode/auth.json
  network:
    listen: 127.0.0.1:8080
```

接入方 MUST 满足：

1. 通过 `gateway.authenticate` 或 Bearer Token 建立认证态。
2. 对 `unauthorized` 与 `access_denied` 做明确分支处理。

## 8. 升级与回滚步骤

升级后验收：

1. `GET /healthz` 返回成功。
2. `/rpc` 未鉴权请求返回 `unauthorized`（用于验证鉴权链路）。
3. `gateway.run` 最小链路可达。

回滚步骤：

1. 停止当前网关进程。
2. 切回上一版已验证二进制。
3. 重复上述验收步骤。
