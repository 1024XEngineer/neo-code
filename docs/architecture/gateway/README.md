# Gateway 模块设计与接口文档

> 文档版本：v3.1
> 文档定位：详细设计文档（LLD）+ 接口文档（API/Contract）

## 规范词约定

- `MUST`：必须满足的架构契约，违反会破坏入口一致性与联调稳定性。
- `SHOULD`：强烈建议遵循，若例外必须记录原因。
- `MAY`：可选增强能力。

## 1. 详细设计（LLD）

### 1.1 目的与范围

Gateway 是系统唯一协议入口，负责把 `Client -> Gateway -> Runtime` 的链路固化为统一通信模型。

Gateway 模块 MUST 覆盖：

- HTTP/WS 协议接入、连接生命周期管理。
- 请求帧校验与动作分发（run/compact/cancel/session 操作）。
- 与 Runtime 的类型映射和错误归一。
- Runtime 事件到客户端帧的回推。

Gateway 模块 MUST NOT 覆盖：

- 业务编排与终态门禁（由 Runtime 负责）。
- 模型调用与工具执行（由 Provider/Tools 负责）。
- 会话存储实现（由 Session 负责）。

### 1.2 架构模式

- 模式：协议适配器 + 连接管理器 + 运行时桥接口。
- 主契约：`gateway.Gateway`。
- 下游端口：`gateway.RuntimePort`。
- 协议帧：`gateway.MessageFrame`（`payload_kind + payload`）。

### 1.3 核心流程

#### 1.3.1 客户端请求到 Runtime 的映射流程

```mermaid
sequenceDiagram
    participant CL as Client
    participant GW as Gateway
    participant RT as RuntimePort

    CL->>GW: MessageFrame(type=request, action=run)
    GW->>GW: 校验 FrameType/Action/PayloadKind/Payload
    GW->>RT: Run(runtime.UserInput)
    GW-->>CL: MessageFrame(type=ack)
```

#### 1.3.2 Runtime 事件回推流程

```mermaid
sequenceDiagram
    participant RT as RuntimePort
    participant GW as Gateway
    participant CL as Client

    RT-->>GW: runtime.RuntimeEvent
    GW->>GW: 事件映射为 RuntimeEventPayload
    GW-->>CL: MessageFrame(type=event, payload_kind=runtime_event)
```

### 1.4 多模态输入约束

- run 请求 MUST 通过 `RunRequestPayload` 承载输入。
- `RunRequestPayload.input_parts` MUST 采用 `provider.MessagePart` 语义，支持文本与图片等非文本输入。
- 非文本输入 SHOULD 使用 `provider.AssetRef` 描述来源与元数据。
- 当请求模态不合法时，Gateway MUST 返回 `type=error` 帧并给出稳定错误码。

### 1.5 边界与职责约束

- 上游：CLI、TUI、Web/Desktop。
- 下游：RuntimePort。
- 边界约束：Gateway 只做协议适配和连接管理，不承载业务状态机。

## 2. 接口文档（API/Contract）

### 2.1 公共规范

- 网关主入口 MUST 通过 `Gateway.Serve(ctx, runtimePort)` 启动。
- 所有客户端消息 MUST 映射为 `MessageFrame`。
- `MessageFrame.Payload` MUST 是 `FramePayload` 的已知实现，且与 `payload_kind` 一一对应。

### 2.2 接口目录

| 接口 | 职责 |
|---|---|
| `Gateway` | 网关主契约（服务生命周期 + 运行时桥接） |
| `RuntimePort` | 网关下游端口契约（run/compact/cancel/events/session） |

### 2.3 关键类型目录

| 类型 | 说明 |
|---|---|
| `MessageFrame` | 网关统一请求/事件/错误帧 |
| `FrameType` | 帧类型枚举（request/event/error/ack） |
| `FrameAction` | 动作枚举（run/compact/cancel/list/load/set_workdir） |
| `PayloadKind` | payload 类型标签 |
| `FramePayload` | 密封 payload 接口 |
| `FrameError` | 错误帧负载 |

### 2.4 FrameType + FrameAction -> Payload 映射矩阵

| FrameType | FrameAction | PayloadKind | Payload 类型 | 必填字段 |
|---|---|---|---|---|
| request | run | run_request | `RunRequestPayload` | `payload` |
| request | compact | compact_request | `CompactRequestPayload` | 无 |
| request | cancel | cancel_request | `CancelRequestPayload` | 无 |
| request | list_sessions | list_sessions_request | `ListSessionsRequestPayload` | 无 |
| request | load_session | load_session_request | `LoadSessionRequestPayload` | `session_id` |
| request | set_session_workdir | set_session_workdir_request | `SetSessionWorkdirRequestPayload` | `session_id`、`payload.workdir` |
| event | * | runtime_event | `RuntimeEventPayload` | `run_id`、`payload.event` |
| ack | * | ack | `AckPayload` | `request_id`、`payload.accepted` |
| error | * | 无 | 无 | `error.code`、`error.message` |

### 2.5 JSON 示例

#### 2.5.1 多模态 run 请求帧

```json
{
  "type": "request",
  "action": "run",
  "payload_kind": "run_request",
  "request_id": "req_001",
  "session_id": "sess_abc",
  "payload": {
    "input_text": "请分析这张图",
    "input_parts": [
      {"type": "text", "text": "请先读取图片中的文字"},
      {
        "type": "image",
        "asset": {
          "source": "local_uri",
          "uri": "file:///workspace/assets/screen.png",
          "mime_type": "image/png",
          "file_name": "screen.png"
        }
      }
    ],
    "workdir": "/workspace/project"
  }
}
```

#### 2.5.2 运行事件帧

```json
{
  "type": "event",
  "payload_kind": "runtime_event",
  "run_id": "run_123",
  "session_id": "sess_abc",
  "payload": {
    "event": {
      "type": "run_progress",
      "run_id": "run_123",
      "session_id": "sess_abc",
      "payload": {
        "message": "receiving text delta"
      }
    }
  }
}
```

#### 2.5.3 校验失败错误帧

```json
{
  "type": "error",
  "action": "run",
  "request_id": "req_001",
  "error": {
    "code": "invalid_payload_kind",
    "message": "payload_kind run_request does not match frame action"
  }
}
```

### 2.6 变更规则

- 新增帧字段 MUST 向后兼容。
- 既有 `FrameType`/`FrameAction` 语义 MUST 保持稳定。
- 新增 payload 类型 MUST 同步更新映射矩阵与示例。

## 3. 评审检查清单

- 是否明确 `Gateway` 为唯一主契约锚点。
- 是否给出 `FrameType + FrameAction -> Payload` 映射矩阵。
- `MessageFrame.Payload` 是否已由 `any` 收敛为 `FramePayload`。
- 是否包含多模态 run 请求示例（文本 + 图片附件）。
- README 类型名是否与 `gateway/interface.go` 一致。