package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"neo-code/internal/security"
)

func buildPermissionAction(input ToolCallInput) (security.Action, error) {
	toolName := strings.TrimSpace(input.Name)
	if toolName == "" {
		return security.Action{}, fmt.Errorf("tools: tool name is empty")
	}

	action := security.Action{
		Payload: security.ActionPayload{
			ToolName:  toolName,
			Resource:  toolName,
			Operation: toolName,
			SessionID: input.SessionID,
			Workdir:   input.Workdir,
		},
	}

	switch strings.ToLower(toolName) {
	case "bash":
		action.Type = security.ActionTypeBash
		action.Payload.Operation = "command"
		action.Payload.TargetType = security.TargetTypeCommand
		action.Payload.Target = extractStringArgument(input.Arguments, "command")
		action.Payload.SandboxTargetType = security.TargetTypeDirectory
		action.Payload.SandboxTarget = extractStringArgument(input.Arguments, "workdir")
	case "filesystem_read_file":
		action.Type = security.ActionTypeRead
		action.Payload.Operation = "read_file"
		action.Payload.TargetType = security.TargetTypePath
		action.Payload.Target = extractStringArgument(input.Arguments, "path")
		action.Payload.SandboxTargetType = security.TargetTypePath
		action.Payload.SandboxTarget = action.Payload.Target
	case "filesystem_grep":
		action.Type = security.ActionTypeRead
		action.Payload.Operation = "grep"
		action.Payload.TargetType = security.TargetTypeDirectory
		action.Payload.Target = extractStringArgument(input.Arguments, "dir")
		action.Payload.SandboxTargetType = security.TargetTypeDirectory
		action.Payload.SandboxTarget = action.Payload.Target
	case "filesystem_glob":
		action.Type = security.ActionTypeRead
		action.Payload.Operation = "glob"
		action.Payload.TargetType = security.TargetTypeDirectory
		action.Payload.Target = extractStringArgument(input.Arguments, "dir")
		action.Payload.SandboxTargetType = security.TargetTypeDirectory
		action.Payload.SandboxTarget = action.Payload.Target
	case "webfetch":
		action.Type = security.ActionTypeRead
		action.Payload.Operation = "fetch"
		action.Payload.TargetType = security.TargetTypeURL
		action.Payload.Target = extractStringArgument(input.Arguments, "url")
	case "filesystem_write_file":
		action.Type = security.ActionTypeWrite
		action.Payload.Operation = "write_file"
		action.Payload.TargetType = security.TargetTypePath
		action.Payload.Target = extractStringArgument(input.Arguments, "path")
		action.Payload.SandboxTargetType = security.TargetTypePath
		action.Payload.SandboxTarget = action.Payload.Target
	case "filesystem_edit":
		action.Type = security.ActionTypeWrite
		action.Payload.Operation = "edit"
		action.Payload.TargetType = security.TargetTypePath
		action.Payload.Target = extractStringArgument(input.Arguments, "path")
		action.Payload.SandboxTargetType = security.TargetTypePath
		action.Payload.SandboxTarget = action.Payload.Target
	default:
		if strings.HasPrefix(strings.ToLower(toolName), "mcp.") {
			action.Type = security.ActionTypeMCP
			action.Payload.Operation = "invoke"
			action.Payload.TargetType = security.TargetTypeMCP
			action.Payload.Target = mcpServerTarget(toolName)
			action.Payload.Resource = toolName
			return action, nil
		}
		return security.Action{}, fmt.Errorf("tools: unsupported permission mapping for %q", input.Name)
	}

	return action, nil
}

func mcpServerTarget(toolName string) string {
	parts := strings.Split(strings.TrimSpace(toolName), ".")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func extractStringArgument(raw []byte, key string) string {
	if len(raw) == 0 {
		return ""
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}

	value, ok := payload[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}
