# Architecture Overview

NeoCode is a local AI coding agent written in Go. The main execution path is:

**User Input → Agent Reasoning → Tool Call → Result → Continue Reasoning → UI Output**

## Core layers

| Layer | Responsibility |
|-------|---------------|
| TUI (`internal/tui`) | Terminal interface built with Bubble Tea. Handles display and input |
| Runtime (`internal/runtime`) | ReAct main loop. Orchestrates reasoning, tool calls, and state management |
| Provider (`internal/provider`) | Model service adapters. Vendor differences are contained here |
| Tools (`internal/tools`) | Tool implementations: file operations, bash execution, WebFetch, etc. |
| Session (`internal/session`) | Session persistence via JSON |
| Config (`internal/config`) | Configuration loading and validation |

## Design principles

- **One-way layer dependencies**: TUI calls Runtime; Runtime calls Provider and Tool Manager only
- **Vendor isolation**: Protocol differences (OpenAI / Gemini / Anthropic) stay inside `internal/provider`
- **Centralized tool capabilities**: All agent-callable capabilities live in `internal/tools`
- **Unified state management**: Session state, message history, and tool call records are managed by `runtime`

## Related

- [Configuration guide](../guides/configuration)
- [Switching models](../guides/providers)
