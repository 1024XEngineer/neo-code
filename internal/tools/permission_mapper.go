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
	case ToolNameTodoWrite:
		action.Type = security.ActionTypeWrite
		action.Payload.Operation = "todo_write"
		action.Payload.TargetType = security.TargetTypePath
		action.Payload.Target = extractStringArgument(input.Arguments, "id")
	case ToolNameSpawnSubAgent:
		action.Type = security.ActionTypeWrite
		action.Payload.Operation = ToolNameSpawnSubAgent
		action.Payload.TargetType = security.TargetTypePath
		action.Payload.Target = extractSpawnSubAgentTarget(input.Arguments)
		if action.Payload.Target == "" {
			return security.Action{}, fmt.Errorf("tools: spawn_subagent permission target is empty")
		}
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

// extractSpawnSubAgentTarget 提取 spawn_subagent 的稳定权限目标，优先 items[].id，再回退 id/prompt。
func extractSpawnSubAgentTarget(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}

	type spawnItem struct {
		ID string `json:"id"`
	}
	type spawnPayload struct {
		ID      string      `json:"id"`
		Prompt  string      `json:"prompt"`
		Content string      `json:"content"`
		Items   []spawnItem `json:"items"`
	}

	var payload spawnPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
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
	prompt := strings.TrimSpace(payload.Prompt)
	if prompt == "" {
		prompt = strings.TrimSpace(payload.Content)
	}
	if prompt == "" {
		return ""
	}
	const maxTargetChars = 80
	runes := []rune(prompt)
	if len(runes) <= maxTargetChars {
		return prompt
	}
	return string(runes[:maxTargetChars]) + "..."
}
