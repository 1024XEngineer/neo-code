---
title: Gateway 与 URL Dispatch
description: 介绍 neocode gateway、url-dispatch、网络访问面、鉴权和当前 JSON-RPC 方法。
---

# Gateway 与 URL Dispatch

## 启动 Gateway

最简单的方式：

```bash
go run ./cmd/neocode gateway
```

指定网络访问面监听地址：

```bash
go run ./cmd/neocode gateway --http-listen 127.0.0.1:8080
```

## 当前网络访问面

当前实现里，Gateway 网络访问面提供这些端点：

- `POST /rpc`：单次 JSON-RPC 请求入口
- `GET /ws`：WebSocket 流式入口，包含心跳
- `GET /sse`：SSE 流式入口，MVP 默认触发 `gateway.ping`，包含心跳

## 安全限制

为了防止跨站调用，网络访问面会校验来源。当前仅允许：

- `http://localhost`
- `http://127.0.0.1`
- `http://[::1]`
- `app://` 前缀来源

不在允许列表中的浏览器跨域请求会被拦截并返回 `403`。

如果请求没有携带 `Origin` 头，例如 `curl`、Postman 或本地脚本直连，网关默认放行。

## URL Dispatch

当前支持通过 URL Scheme 把请求派发到本地 Gateway：

```bash
go run ./cmd/neocode url-dispatch --url "neocode://review?path=README.md"
```

目前的 MVP 限制：

- 仅支持 `review` 动作
- 必须提供 `path` 参数
- 其他动作会在网关侧被拒绝

## 鉴权与 Silent Auth

启动 `neocode gateway` 时，会自动读取：

```text
~/.neocode/auth.json
```

如果凭证不存在或损坏，会自动生成新的高强度 token 并写回文件。`url-dispatch` 会读取同一 token，先发 `gateway.authenticate`，再发业务请求。

认证与授权顺序为：

```text
Auth -> ACL -> Dispatch
```

## 运维端点

- 无需鉴权：`GET /healthz`、`GET /version`
- 需要鉴权：`GET /metrics`、`GET /metrics.json`

访问 metrics 端点时，需要携带：

```text
Authorization: Bearer <token>
```

## 当前 JSON-RPC 方法

下面这些方法来自 README 中列出的当前实现：

- `gateway.authenticate`
- `gateway.ping`
- `gateway.bindStream`
- `gateway.run`
- `gateway.compact`
- `gateway.cancel`
- `gateway.listSessions`
- `gateway.loadSession`
- `gateway.resolvePermission`
- `wake.openUrl`
- `gateway.event`

## 继续阅读

- 想看完整网关设计：见 [Gateway 详细设计](https://github.com/1024XEngineer/neo-code/blob/main/docs/gateway-detailed-design.md)
- 想看普通 CLI 使用路径：回到 [首次上手](./quick-start)
