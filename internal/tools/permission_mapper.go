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
			ToolName:        toolName,
			Resource:        toolName,
			Operation:       toolName,
			SessionID:       input.SessionID,
			TaskID:          input.TaskID,
			AgentID:         input.AgentID,
			Workdir:         input.Workdir,
			CapabilityToken: input.CapabilityToken,
		},
	}

	switch strings.ToLower(toolName) {
	case ToolNameBash:
		intent := AnalyzeBashCommand(extractStringArgument(input.Arguments, "command"))
		action.Type = security.ActionTypeBash
		action.Payload.Operation = "command"
		action.Payload.TargetType = security.TargetTypeCommand
		action.Payload.Target = extractStringArgument(input.Arguments, "command")
		action.Payload.SemanticType = ""
		action.Payload.SemanticClass = intent.Classification
		action.Payload.NormalizedIntent = intent.NormalizedIntent
		action.Payload.PermissionFingerprint = intent.PermissionFingerprint
		if intent.IsGit {
			action.Payload.SemanticType = "git"
			action.Payload.SemanticClass = NormalizeGitSemanticClass(intent.Classification)
			action.Payload.Operation = "git_" + strings.TrimSpace(intent.Subcommand)
			action.Payload.Resource = BashGitResourceForClass(action.Payload.SemanticClass)
			if strings.TrimSpace(intent.Subcommand) == "" {
				action.Payload.Operation = "git_unknown"
			}
		}
		action.Payload.SandboxTargetType = security.TargetTypeDirectory
		action.Payload.SandboxTarget = extractStringArgument(input.Arguments, "workdir")
		if action.Payload.SandboxTarget == "" {
			action.Payload.SandboxTarget = "."
		}
	case ToolNameFilesystemReadFile:
		action.Type = security.ActionTypeRead
		action.Payload.Operation = "read_file"
		action.Payload.TargetType = security.TargetTypePath
		action.Payload.Target = extractStringArgument(input.Arguments, "path")
		action.Payload.SandboxTargetType = security.TargetTypePath
		action.Payload.SandboxTarget = action.Payload.Target
	case ToolNameFilesystemGrep:
		action.Type = security.ActionTypeRead
		action.Payload.Operation = "grep"
		action.Payload.TargetType = security.TargetTypeDirectory
		action.Payload.Target = extractStringArgument(input.Arguments, "dir")
		action.Payload.SandboxTargetType = security.TargetTypeDirectory
		action.Payload.SandboxTarget = action.Payload.Target
	case ToolNameFilesystemGlob:
		action.Type = security.ActionTypeRead
		action.Payload.Operation = "glob"
		action.Payload.TargetType = security.TargetTypeDirectory
		action.Payload.Target = extractStringArgument(input.Arguments, "dir")
		action.Payload.SandboxTargetType = security.TargetTypeDirectory
		action.Payload.SandboxTarget = action.Payload.Target
	case ToolNameWebFetch:
		action.Type = security.ActionTypeRead
		action.Payload.Operation = "fetch"
		action.Payload.TargetType = security.TargetTypeURL
		action.Payload.Target = extractStringArgument(input.Arguments, "url")
	case ToolNameFilesystemWriteFile:
		action.Type = security.ActionTypeWrite
		action.Payload.Operation = "write_file"
		action.Payload.TargetType = security.TargetTypePath
		action.Payload.Target = extractStringArgument(input.Arguments, "path")
		action.Payload.SandboxTargetType = security.TargetTypePath
		action.Payload.SandboxTarget = action.Payload.Target
	case ToolNameFilesystemEdit:
		action.Type = security.ActionTypeWrite
		action.Payload.Operation = "edit"
		action.Payload.TargetType = security.TargetTypePath
		action.Payload.Target = extractStringArgument(input.Arguments, "path")
		action.Payload.SandboxTargetType = security.TargetTypePath
		action.Payload.SandboxTarget = action.Payload.Target
	case ToolNameFilesystemMoveFile:
		action.Type = security.ActionTypeWrite
		action.Payload.Operation = "move_file"
		action.Payload.TargetType = security.TargetTypePath
		action.Payload.Target = extractStringArgument(input.Arguments, "destination_path")
		action.Payload.SandboxTargetType = security.TargetTypePath
		action.Payload.SandboxTarget = action.Payload.Target
	case ToolNameFilesystemCopyFile:
		action.Type = security.ActionTypeWrite
		action.Payload.Operation = "copy_file"
		action.Payload.TargetType = security.TargetTypePath
		action.Payload.Target = extractStringArgument(input.Arguments, "destination_path")
		action.Payload.SandboxTargetType = security.TargetTypePath
		action.Payload.SandboxTarget = action.Payload.Target
	case ToolNameFilesystemDeleteFile:
		action.Type = security.ActionTypeWrite
		action.Payload.Operation = "delete_file"
		action.Payload.TargetType = security.TargetTypePath
		action.Payload.Target = extractStringArgument(input.Arguments, "path")
		action.Payload.SandboxTargetType = security.TargetTypePath
		action.Payload.SandboxTarget = action.Payload.Target
	case ToolNameFilesystemCreateDir:
		action.Type = security.ActionTypeWrite
		action.Payload.Operation = "create_dir"
		action.Payload.TargetType = security.TargetTypePath
		action.Payload.Target = extractStringArgument(input.Arguments, "path")
		action.Payload.SandboxTargetType = security.TargetTypePath
		action.Payload.SandboxTarget = action.Payload.Target
	case ToolNameFilesystemRemoveDir:
		action.Type = security.ActionTypeWrite
		action.Payload.Operation = "remove_dir"
		action.Payload.TargetType = security.TargetTypePath
		action.Payload.Target = extractStringArgument(input.Arguments, "path")
		action.Payload.SandboxTargetType = security.TargetTypePath
		action.Payload.SandboxTarget = action.Payload.Target
	case ToolNameTodoWrite:
		action.Type = security.ActionTypeWrite
		action.Payload.Operation = "todo_write"
		action.Payload.TargetType = security.TargetTypePath
		action.Payload.Target = extractStringArgument(input.Arguments, "id")
	case ToolNameSpawnSubAgent:
		action.Type = security.ActionTypeWrite
		action.Payload.Operation = ToolNameSpawnSubAgent
		action.Payload.TargetType = security.TargetTypeCommand
		action.Payload.Target = extractSpawnSubAgentPermissionLabel(input.Arguments)
		action.Payload.SandboxTargetType = security.TargetTypePath
		action.Payload.SandboxTarget = extractSpawnSubAgentSandboxPath(input.Arguments, input.Workdir)
	case ToolNameMemoRemember:
		action.Type = security.ActionTypeWrite
		action.Payload.Operation = "memo_remember"
	case ToolNameMemoRecall:
		action.Type = security.ActionTypeRead
		action.Payload.Operation = "memo_recall"
	case ToolNameMemoList:
		action.Type = security.ActionTypeRead
		action.Payload.Operation = "memo_list"
	case ToolNameMemoRemove:
		action.Type = security.ActionTypeWrite
		action.Payload.Operation = "memo_remove"
	case ToolNameDiagnose:
		action.Type = security.ActionTypeRead
		action.Payload.Operation = "diagnose"
		action.Payload.TargetType = security.TargetTypeCommand
		action.Payload.Target = "diagnose"
	default:
		if strings.HasPrefix(strings.ToLower(toolName), "mcp.") {
			toolIdentity := normalizeMCPToolIdentity(toolName)
			action.Type = security.ActionTypeMCP
			action.Payload.Operation = "invoke"
			action.Payload.TargetType = security.TargetTypeMCP
			action.Payload.Target = toolIdentity
			action.Payload.Resource = toolIdentity
			return action, nil
		}
		return security.Action{}, fmt.Errorf("tools: unsupported permission mapping for %q", input.Name)
	}

	return action, nil
}

// normalizeMCPToolIdentity 将 MCP 工具名归一为稳定的全量 identity：mcp.<server>.<tool>。
func normalizeMCPToolIdentity(toolName string) string {
	return strings.ToLower(strings.TrimSpace(toolName))
}

// mcpServerTarget 从 MCP 工具全名中提取 server 级 identity：mcp.<server>。
func mcpServerTarget(toolName string) string {
	return security.CanonicalMCPServerIdentity(normalizeMCPToolIdentity(toolName))
}

func extractStringArgument(raw []byte, key string) string {
	if len(raw) == 0 {
		return ""
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return extractStringArgumentFallback(string(raw), key)
	}

	value, ok := payload[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

// extractStringArgumentFallback 在参数不是严格合法 JSON 时做最小字符串提取，兼容未转义的 Windows 路径。
func extractStringArgumentFallback(raw string, key string) string {
	quotedKey := `"` + strings.TrimSpace(key) + `"`
	start := strings.Index(raw, quotedKey)
	if start < 0 {
		return ""
	}
	rest := raw[start+len(quotedKey):]
	colon := strings.Index(rest, ":")
	if colon < 0 {
		return ""
	}
	rest = strings.TrimSpace(rest[colon+1:])
	if !strings.HasPrefix(rest, `"`) {
		return ""
	}
	rest = rest[1:]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

// extractSpawnSubAgentPermissionLabel 提取 spawn_subagent 的稳定展示目标，仅用于权限展示与审计。
func extractSpawnSubAgentPermissionLabel(raw []byte) string {
	if len(raw) == 0 {
		return ToolNameSpawnSubAgent
	}

	type spawnItem struct {
		ID string `json:"id"`
	}
	type spawnPayload struct {
		ID    string      `json:"id"`
		Items []spawnItem `json:"items"`
	}

	var payload spawnPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ToolNameSpawnSubAgent
	}

	ids := make([]string, 0, len(payload.Items))
	for _, item := range payload.Items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) > 0 {
		return strings.Join(ids, ",")
	}
	if id := strings.TrimSpace(payload.ID); id != "" {
		return id
	}
	return ToolNameSpawnSubAgent
}

// extractSpawnSubAgentSandboxPath 只从 allowed_paths 派生路径校验目标，避免把 prompt/content 当作路径。
func extractSpawnSubAgentSandboxPath(raw []byte, workdir string) string {
	defaultTarget := strings.TrimSpace(workdir)
	if defaultTarget == "" {
		defaultTarget = "."
	}
	if len(raw) == 0 {
		return defaultTarget
	}

	type spawnPayload struct {
		AllowedPaths []string `json:"allowed_paths"`
	}
	var payload spawnPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return defaultTarget
	}
	paths := make([]string, 0, len(payload.AllowedPaths))
	for _, item := range payload.AllowedPaths {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		paths = append(paths, trimmed)
	}
	if len(paths) == 1 {
		return paths[0]
	}
	return defaultTarget
}
