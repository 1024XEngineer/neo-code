# TUI-Gateway Contract Matrix (Single-Version Baseline)

This document freezes the contract that TUI consumes from gateway.
It is intentionally single-version and fail-fast by design.

## Scope

- Transport contract: JSON-RPC 2.0 (`internal/gateway/protocol`)
- Runtime contract: gateway DTOs (`internal/gateway/contracts.go`)
- Event payload version source of truth: `internal/runtime/controlplane/envelope.go`

## RPC Methods Used By TUI

| Method | Params Type | Result Payload | Notes |
| --- | --- | --- | --- |
| `gateway.authenticate` | `protocol.AuthenticateParams` | frame ack | Must succeed before runtime actions |
| `gateway.bindStream` | `protocol.BindStreamParams` | frame ack | Binds session/run event stream |
| `gateway.run` | `protocol.RunParams` | frame ack with `session_id`/`run_id` | Async acceptance only |
| `gateway.compact` | `protocol.CompactParams` | `gateway.CompactResult` | Manual compact |
| `gateway.executeSystemTool` | `protocol.ExecuteSystemToolParams` | `tools.ToolResult` | Tool execution passthrough |
| `gateway.resolvePermission` | `protocol.ResolvePermissionParams` | frame ack | Permission decision submit |
| `gateway.cancel` | `protocol.CancelParams` | frame ack | Cancels run by run/session binding |
| `gateway.listSessions` | none | `[]gateway.SessionSummary` | Session list |
| `gateway.loadSession` | `protocol.LoadSessionParams` | `gateway.Session` | Full session snapshot |
| `gateway.activateSessionSkill` | `protocol.ActivateSessionSkillParams` | frame ack | Activate skill in session |
| `gateway.deactivateSessionSkill` | `protocol.DeactivateSessionSkillParams` | frame ack | Deactivate skill in session |
| `gateway.listSessionSkills` | `protocol.ListSessionSkillsParams` | `[]gateway.SessionSkillState` | Active skill states |
| `gateway.listAvailableSkills` | `protocol.ListAvailableSkillsParams` | `[]gateway.AvailableSkillState` | Available skill catalog |

## Runtime Event Contract

- Notification method: `gateway.event`
- TUI only accepts a runtime envelope payload with these required keys:
  - `runtime_event_type` (string)
  - `turn` (number)
  - `phase` (string)
  - `timestamp` (RFC3339 or RFC3339Nano)
  - `payload_version` (number)
  - `payload` (event-specific object/string)
- `payload_version` must equal `controlplane.PayloadVersion`.
- Version mismatch is treated as a hard incompatibility and must fail fast.

## Error Contract

TUI consumes standard JSON-RPC errors and gateway extended error codes from
`protocol.JSONRPCError.Data.GatewayCode`.

Primary gateway codes used for UI mapping:

- `invalid_frame`
- `invalid_action`
- `invalid_multimodal_payload`
- `missing_required_field`
- `unsupported_action`
- `internal_error`
- `timeout`
- `unsafe_path`
- `unauthorized`
- `access_denied`
- `resource_not_found`

## Non-Goals

- No multi-version payload decoding.
- No alias method fallback.
- No legacy field fallback in event payload.
