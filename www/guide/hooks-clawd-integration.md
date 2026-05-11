---
title: NeoCode x Clawd 桌宠接入示例
description: 用 http observe hooks 把 NeoCode 生命周期事件推送给 Clawd 桌宠组件的最小接入方案。
---

# NeoCode x Clawd 桌宠接入示例

这篇文档讲的是“最小可用接入”：

- 不改 NeoCode 主链路
- 不开放危险执行能力
- 只通过 `http + observe` 把运行状态推送给桌宠

如果你只想“让桌宠看到 NeoCode 在忙什么”，按本文即可。

## 先理解整体链路

```text
NeoCode runtime hooks
  -> http observe (POST JSON)
  -> 本地桥接服务（你自己起一个小服务）
  -> Clawd 的 hook/event 接口
```

为什么推荐“桥接服务”而不是直接写死到桌宠？

1. 桌宠接口变更时，你只改桥接，不动 NeoCode 配置
2. 可以做字段映射与节流，避免事件风暴
3. 出错时不会影响 NeoCode 主任务

## 第一步：配置 NeoCode hooks

在 `~/.neocode/config.yaml` 里增加：

```yaml
runtime:
  hooks:
    enabled: true
    user_hooks_enabled: true
    default_timeout_sec: 2
    default_failure_policy: warn_only
    items:
      - id: clawd-session-start
        enabled: true
        point: session_start
        scope: user
        kind: http
        mode: observe
        params:
          url: "http://127.0.0.1:3101/neocode-hook"
          method: POST
          headers:
            X-Event-Source: neocode
          include_metadata: true

      - id: clawd-before-tool
        enabled: true
        point: before_tool_call
        scope: user
        kind: http
        mode: observe
        params:
          url: "http://127.0.0.1:3101/neocode-hook"
          method: POST
          include_metadata: true

      - id: clawd-after-tool
        enabled: true
        point: after_tool_result
        scope: user
        kind: http
        mode: observe
        params:
          url: "http://127.0.0.1:3101/neocode-hook"
          method: POST
          include_metadata: true

      - id: clawd-session-end
        enabled: true
        point: session_end
        scope: user
        kind: http
        mode: observe
        params:
          url: "http://127.0.0.1:3101/neocode-hook"
          method: POST
          include_metadata: true
```

这四个点位基本覆盖了“开始任务 -> 调工具 -> 完成任务”的常见展示需求。

## 第二步：起一个本地桥接服务

下面给一个最小 Node.js 示例（你也可以用 Go/Python）：

```js
// bridge.js
import express from "express";
import fetch from "node-fetch";

const app = express();
app.use(express.json({ limit: "256kb" }));

// 你需要替换成 Clawd 实际接收地址
const CLAWD_ENDPOINT = process.env.CLAWD_ENDPOINT || "http://127.0.0.1:3111/hook";

function mapNeoCodeEvent(payload) {
  const point = payload?.point || "unknown";
  const toolName = payload?.metadata?.tool_name || "";

  // 统一映射成桌宠更容易消费的状态
  if (point === "session_start") return { state: "working", detail: "task started" };
  if (point === "before_tool_call") return { state: "tool_running", detail: toolName || "tool call" };
  if (point === "after_tool_result") return { state: "tool_done", detail: toolName || "tool result" };
  if (point === "session_end") return { state: "idle", detail: "task finished" };
  return { state: "working", detail: point };
}

app.post("/neocode-hook", async (req, res) => {
  try {
    const payload = req.body || {};
    const mapped = mapNeoCodeEvent(payload);

    await fetch(CLAWD_ENDPOINT, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        source: "neocode",
        run_id: payload.run_id || "",
        session_id: payload.session_id || "",
        point: payload.point || "",
        state: mapped.state,
        detail: mapped.detail,
        ts: payload.triggered_at || new Date().toISOString(),
      }),
    });

    res.status(204).end();
  } catch (err) {
    console.error("bridge error:", err);
    res.status(500).json({ error: "bridge failed" });
  }
});

app.listen(3101, "127.0.0.1", () => {
  console.log("NeoCode -> Clawd bridge listening on http://127.0.0.1:3101");
});
```

## 第三步：联调检查

按这个顺序做：

1. 启动桥接服务（确保 `127.0.0.1:3101` 可访问）
2. 启动 NeoCode，执行一次会触发工具调用的任务
3. 检查桥接日志是否有 `POST /neocode-hook`
4. 检查 Clawd 是否收到状态变化（working -> tool_running -> tool_done -> idle）

## 常见问题

### 1) 我看不到任何回调

先查三件事：

1. `runtime.hooks.enabled` 是否为 `true`
2. `kind=http` + `mode=observe` 是否写对
3. `params.url` 是否是绝对 `http/https` 地址

### 2) 回调失败会不会影响主任务

不会。`http observe` 设计为观测通道，建议搭配 `warn_only`，即使桥接服务挂了，NeoCode 主链路也继续执行。

### 3) 会不会把敏感内容都发给桌宠

建议只保留必要字段，特别是桥接层不要把完整文本、密钥相关内容透传到外部服务。  
如果需要，可以在桥接服务里做二次过滤后再转发。

## 建议的生产实践

1. 桥接只监听 `127.0.0.1`
2. 给桥接服务加请求大小限制（如 256KB）
3. 对重复事件做去重（`run_id + point + ts`）
4. 桌宠端只消费状态，不依赖完整业务语义
5. 桥接崩溃时自动拉起（pm2 / systemd / NSSM）

这样做可以把“可观测性”开放给用户，同时保持 NeoCode 主链路稳定、安全、可控。

