---
title: NeoCode x Clawd Integration Example
description: Minimal setup using HTTP observe hooks to drive Clawd desktop-pet state.
---

# NeoCode x Clawd Integration Example

This is the minimal production-friendly approach:

- keep NeoCode core flow unchanged
- avoid opening dangerous execution hooks
- use `http + observe` only for lifecycle event delivery

## Architecture

```text
NeoCode runtime hooks
  -> http observe (POST JSON)
  -> local bridge service
  -> Clawd hook/event endpoint
```

Why use a bridge service:

1. Clawd protocol changes do not require NeoCode config rewrites
2. You can map, throttle, or dedupe events safely
3. Bridge failures won't block NeoCode runs

## Step 1: Configure NeoCode hooks

Add this to `~/.neocode/config.yaml`:

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

## Step 2: Start a local bridge

Example Node.js bridge:

```js
// bridge.js
import express from "express";
import fetch from "node-fetch";

const app = express();
app.use(express.json({ limit: "256kb" }));

const CLAWD_ENDPOINT = process.env.CLAWD_ENDPOINT || "http://127.0.0.1:3111/hook";

function mapEvent(payload) {
  const point = payload?.point || "unknown";
  const toolName = payload?.metadata?.tool_name || "";

  if (point === "session_start") return { state: "working", detail: "task started" };
  if (point === "before_tool_call") return { state: "tool_running", detail: toolName || "tool call" };
  if (point === "after_tool_result") return { state: "tool_done", detail: toolName || "tool result" };
  if (point === "session_end") return { state: "idle", detail: "task finished" };
  return { state: "working", detail: point };
}

app.post("/neocode-hook", async (req, res) => {
  try {
    const payload = req.body || {};
    const mapped = mapEvent(payload);

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
  console.log("NeoCode -> Clawd bridge on http://127.0.0.1:3101");
});
```

## Step 3: Validate end-to-end

1. Start bridge service (`127.0.0.1:3101`)
2. Run a NeoCode task that calls at least one tool
3. Verify bridge receives `POST /neocode-hook`
4. Verify Clawd state transitions (`working -> tool_running -> tool_done -> idle`)

## Troubleshooting

If no callback arrives:

1. confirm `runtime.hooks.enabled=true`
2. confirm `kind=http` and `mode=observe`
3. confirm `params.url` is absolute `http/https`

Will callback failures stop my run?

- No. `http observe` is designed as non-blocking with `warn_only`.

## Security baseline

1. Bind bridge to `127.0.0.1` only
2. Set request body size limits
3. Dedupe repeated events (`run_id + point + ts`)
4. Forward only fields needed by Clawd

