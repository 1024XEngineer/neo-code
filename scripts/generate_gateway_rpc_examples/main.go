package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/protocol"
	"neo-code/internal/runtime/controlplane"
	"neo-code/internal/tools"
)

const (
	// targetDocPath 表示需要回填自动生成示例的目标文档路径。
	targetDocPath = "docs/reference/gateway-rpc-api.md"
	// beginMarker 标记自动生成区块起始位置。
	beginMarker = "<!-- AUTO-GENERATED:BEGIN -->"
	// endMarker 标记自动生成区块结束位置。
	endMarker = "<!-- AUTO-GENERATED:END -->"
)

// methodExample 描述单个方法在文档中的自动生成示例片段。
type methodExample struct {
	Method       string
	Request      any
	Success      any
	Failure      any
	Notification any
	Notes        []string
}

// main 生成 Gateway JSON 示例并回填至 API 文档标记区块。
func main() {
	documentPath := filepath.Clean(targetDocPath)
	originalContent, err := os.ReadFile(documentPath)
	if err != nil {
		exitWithError("读取 API 文档失败", err)
	}

	generatedBlock, err := renderGeneratedBlock()
	if err != nil {
		exitWithError("渲染自动生成区块失败", err)
	}

	updatedContent, err := replaceGeneratedBlock(string(originalContent), generatedBlock)
	if err != nil {
		exitWithError("回填自动生成区块失败", err)
	}

	if updatedContent == string(originalContent) {
		fmt.Printf("Gateway JSON 示例已是最新，无需更新：%s\n", documentPath)
		return
	}

	if err := os.WriteFile(documentPath, []byte(updatedContent), 0o644); err != nil {
		exitWithError("写入 API 文档失败", err)
	}

	fmt.Printf("Gateway JSON 示例已更新：%s\n", documentPath)
}

// renderGeneratedBlock 渲染附录中的自动生成内容。
func renderGeneratedBlock() (string, error) {
	examples, err := buildMethodExamples()
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	builder.WriteString("> 以下 JSON 示例由 `go run ./scripts/generate_gateway_rpc_examples` 自动生成。\n")
	builder.WriteString("> 如结构体或字段标签发生变更，请重新执行生成命令。\n\n")

	for _, example := range examples {
		builder.WriteString("### ")
		builder.WriteString(example.Method)
		builder.WriteString("\n\n")

		if example.Request != nil {
			builder.WriteString("Request：\n\n```json\n")
			builder.WriteString(mustPrettyJSON(example.Request))
			builder.WriteString("\n```\n\n")
		}
		if example.Success != nil {
			builder.WriteString("Success Response：\n\n```json\n")
			builder.WriteString(mustPrettyJSON(example.Success))
			builder.WriteString("\n```\n\n")
		}
		if example.Failure != nil {
			builder.WriteString("Failure Response：\n\n```json\n")
			builder.WriteString(mustPrettyJSON(example.Failure))
			builder.WriteString("\n```\n\n")
		}
		if example.Notification != nil {
			builder.WriteString("Notification：\n\n```json\n")
			builder.WriteString(mustPrettyJSON(example.Notification))
			builder.WriteString("\n```\n\n")
		}
		if len(example.Notes) > 0 {
			builder.WriteString("Notes：\n\n")
			for index, note := range example.Notes {
				builder.WriteString(strconv.Itoa(index + 1))
				builder.WriteString(". ")
				builder.WriteString(note)
				builder.WriteString("\n")
			}
			builder.WriteString("\n")
		}
	}

	return strings.TrimRight(builder.String(), "\n"), nil
}

// buildMethodExamples 基于 Go 结构体构造 RPC 方法示例集合。
func buildMethodExamples() ([]methodExample, error) {
	runRequestID := "req-run-1"
	runID := "run-demo-1"
	sessionID := "session-demo-1"

	authenticateRequest := buildRequest(
		"req-auth-1",
		protocol.MethodGatewayAuthenticate,
		protocol.AuthenticateParams{Token: "<TOKEN>"},
	)
	authenticateSuccess := buildSuccessResponse("req-auth-1", gateway.MessageFrame{
		Type:      gateway.FrameTypeAck,
		Action:    gateway.FrameActionAuthenticate,
		RequestID: "req-auth-1",
		Payload: map[string]string{
			"message":    "authenticated",
			"subject_id": "local_admin",
		},
	})
	authenticateFailure := buildFailureResponse(
		"req-auth-1",
		protocol.MapGatewayCodeToJSONRPCCode(gateway.ErrorCodeUnauthorized.String()),
		"invalid auth token",
		gateway.ErrorCodeUnauthorized.String(),
	)

	pingRequest := buildRequest("req-ping-1", protocol.MethodGatewayPing, map[string]any{})
	pingSuccess := buildSuccessResponse("req-ping-1", gateway.MessageFrame{
		Type:      gateway.FrameTypeAck,
		Action:    gateway.FrameActionPing,
		RequestID: "req-ping-1",
		Payload: map[string]string{
			"message": "pong",
			"version": "dev",
		},
	})
	pingFailure := buildFailureResponse(
		"req-ping-1",
		protocol.MapGatewayCodeToJSONRPCCode(gateway.ErrorCodeUnauthorized.String()),
		"unauthorized",
		gateway.ErrorCodeUnauthorized.String(),
	)

	bindRequest := buildRequest("req-bind-1", protocol.MethodGatewayBindStream, protocol.BindStreamParams{
		SessionID: sessionID,
		RunID:     runID,
		Channel:   string(gateway.StreamChannelAll),
	})
	bindSuccess := buildSuccessResponse("req-bind-1", gateway.MessageFrame{
		Type:      gateway.FrameTypeAck,
		Action:    gateway.FrameActionBindStream,
		RequestID: "req-bind-1",
		SessionID: sessionID,
		RunID:     runID,
		Payload: map[string]any{
			"message": "stream binding updated",
			"channel": string(gateway.StreamChannelAll),
		},
	})
	bindFailure := buildFailureResponse(
		"req-bind-1",
		protocol.MapGatewayCodeToJSONRPCCode(gateway.ErrorCodeInvalidAction.String()),
		"invalid bind_stream channel",
		gateway.ErrorCodeInvalidAction.String(),
	)

	runRequest := buildRequest(runRequestID, protocol.MethodGatewayRun, protocol.RunParams{
		SessionID: sessionID,
		RunID:     runID,
		InputText: "请分析这段代码并给出改进建议",
		InputParts: []protocol.RunInputPart{
			{
				Type: "text",
				Text: "补充一段上下文描述",
			},
			{
				Type: "image",
				Media: &protocol.RunInputMedia{
					URI:      "file:///tmp/screenshot.png",
					MimeType: "image/png",
					FileName: "screenshot.png",
				},
			},
		},
		Workdir: "C:/workspace/demo",
	})
	runSuccess := buildSuccessResponse(runRequestID, gateway.MessageFrame{
		Type:      gateway.FrameTypeAck,
		Action:    gateway.FrameActionRun,
		RequestID: runRequestID,
		SessionID: sessionID,
		RunID:     runID,
		Payload: map[string]string{
			"message": "run accepted",
		},
	})
	runFailure := buildFailureResponse(
		runRequestID,
		protocol.MapGatewayCodeToJSONRPCCode(gateway.ErrorCodeInvalidMultimodalPayload.String()),
		"input_parts[image] requires media.uri",
		gateway.ErrorCodeInvalidMultimodalPayload.String(),
	)

	compactRequest := buildRequest("req-compact-1", protocol.MethodGatewayCompact, protocol.CompactParams{
		SessionID: sessionID,
		RunID:     runID,
	})
	compactSuccess := buildSuccessResponse("req-compact-1", gateway.MessageFrame{
		Type:      gateway.FrameTypeAck,
		Action:    gateway.FrameActionCompact,
		RequestID: "req-compact-1",
		SessionID: sessionID,
		RunID:     runID,
		Payload: gateway.CompactResult{
			Applied:        true,
			BeforeChars:    12345,
			AfterChars:     4567,
			SavedRatio:     0.63,
			TriggerMode:    "manual",
			TranscriptID:   "compact-demo-1",
			TranscriptPath: ".neocode/transcripts/compact-demo-1.md",
		},
	})
	compactFailure := buildFailureResponse(
		"req-compact-1",
		protocol.MapGatewayCodeToJSONRPCCode(gateway.ErrorCodeTimeout.String()),
		"compact timed out",
		gateway.ErrorCodeTimeout.String(),
	)

	executeSystemToolRequest := buildRequest("req-exec-tool-1", protocol.MethodGatewayExecuteSystemTool, protocol.ExecuteSystemToolParams{
		SessionID: sessionID,
		RunID:     runID,
		Workdir:   "C:/workspace/demo",
		ToolName:  tools.ToolNameMemoList,
		Arguments: json.RawMessage("{}"),
	})
	executeSystemToolSuccess := buildSuccessResponse("req-exec-tool-1", gateway.MessageFrame{
		Type:      gateway.FrameTypeAck,
		Action:    gateway.FrameActionExecuteSystemTool,
		RequestID: "req-exec-tool-1",
		SessionID: sessionID,
		RunID:     runID,
		Payload: tools.ToolResult{
			Name:    tools.ToolNameMemoList,
			Content: "[memo] listed successfully",
		},
	})
	executeSystemToolFailure := buildFailureResponse(
		"req-exec-tool-1",
		protocol.MapGatewayCodeToJSONRPCCode(gateway.ErrorCodeInvalidAction.String()),
		"invalid execute_system_tool tool_name",
		gateway.ErrorCodeInvalidAction.String(),
	)

	activateSkillRequest := buildRequest("req-skill-on-1", protocol.MethodGatewayActivateSessionSkill, protocol.ActivateSessionSkillParams{
		SessionID: sessionID,
		SkillID:   "go-review",
	})
	activateSkillSuccess := buildSuccessResponse("req-skill-on-1", gateway.MessageFrame{
		Type:      gateway.FrameTypeAck,
		Action:    gateway.FrameActionActivateSessionSkill,
		RequestID: "req-skill-on-1",
		SessionID: sessionID,
		Payload: map[string]any{
			"session_id": sessionID,
			"skill_id":   "go-review",
			"message":    "skill activated",
		},
	})
	activateSkillFailure := buildFailureResponse(
		"req-skill-on-1",
		protocol.MapGatewayCodeToJSONRPCCode(gateway.ErrorCodeMissingRequiredField.String()),
		"missing required field: params.skill_id",
		gateway.ErrorCodeMissingRequiredField.String(),
	)

	deactivateSkillRequest := buildRequest("req-skill-off-1", protocol.MethodGatewayDeactivateSessionSkill, protocol.DeactivateSessionSkillParams{
		SessionID: sessionID,
		SkillID:   "go-review",
	})
	deactivateSkillSuccess := buildSuccessResponse("req-skill-off-1", gateway.MessageFrame{
		Type:      gateway.FrameTypeAck,
		Action:    gateway.FrameActionDeactivateSessionSkill,
		RequestID: "req-skill-off-1",
		SessionID: sessionID,
		Payload: map[string]any{
			"session_id": sessionID,
			"skill_id":   "go-review",
			"message":    "skill deactivated",
		},
	})
	deactivateSkillFailure := buildFailureResponse(
		"req-skill-off-1",
		protocol.MapGatewayCodeToJSONRPCCode(gateway.ErrorCodeMissingRequiredField.String()),
		"missing required field: params.skill_id",
		gateway.ErrorCodeMissingRequiredField.String(),
	)

	listSessionSkillsRequest := buildRequest("req-skill-active-1", protocol.MethodGatewayListSessionSkills, protocol.ListSessionSkillsParams{
		SessionID: sessionID,
	})
	listSessionSkillsSuccess := buildSuccessResponse("req-skill-active-1", gateway.MessageFrame{
		Type:      gateway.FrameTypeAck,
		Action:    gateway.FrameActionListSessionSkills,
		RequestID: "req-skill-active-1",
		SessionID: sessionID,
		Payload: map[string]any{
			"skills": []gateway.SessionSkillState{
				{
					SkillID: "go-review",
					Descriptor: &gateway.SkillDescriptor{
						ID:          "go-review",
						Name:        "Go Review",
						Description: "Review Go code with actionable findings.",
						Version:     "1.0.0",
						Source: gateway.SkillSource{
							Kind: "local",
						},
						Scope: "session",
					},
				},
			},
		},
	})
	listSessionSkillsFailure := buildFailureResponse(
		"req-skill-active-1",
		protocol.MapGatewayCodeToJSONRPCCode(gateway.ErrorCodeMissingRequiredField.String()),
		"missing required field: params.session_id",
		gateway.ErrorCodeMissingRequiredField.String(),
	)

	listAvailableSkillsRequest := buildRequest("req-skill-list-1", protocol.MethodGatewayListAvailableSkills, protocol.ListAvailableSkillsParams{
		SessionID: sessionID,
	})
	listAvailableSkillsSuccess := buildSuccessResponse("req-skill-list-1", gateway.MessageFrame{
		Type:      gateway.FrameTypeAck,
		Action:    gateway.FrameActionListAvailableSkills,
		RequestID: "req-skill-list-1",
		SessionID: sessionID,
		Payload: map[string]any{
			"skills": []gateway.AvailableSkillState{
				{
					Descriptor: gateway.SkillDescriptor{
						ID:          "go-review",
						Name:        "Go Review",
						Description: "Review Go code with actionable findings.",
						Version:     "1.0.0",
						Source: gateway.SkillSource{
							Kind: "local",
						},
						Scope: "session",
					},
					Active: true,
				},
			},
		},
	})
	listAvailableSkillsFailure := buildFailureResponse(
		"req-skill-list-1",
		protocol.MapGatewayCodeToJSONRPCCode(gateway.ErrorCodeAccessDenied.String()),
		"access denied",
		gateway.ErrorCodeAccessDenied.String(),
	)

	cancelRequest := buildRequest("req-cancel-1", protocol.MethodGatewayCancel, protocol.CancelParams{
		SessionID: sessionID,
		RunID:     runID,
	})
	cancelSuccess := buildSuccessResponse("req-cancel-1", gateway.MessageFrame{
		Type:      gateway.FrameTypeAck,
		Action:    gateway.FrameActionCancel,
		RequestID: "req-cancel-1",
		Payload: map[string]any{
			"canceled": true,
			"run_id":   runID,
		},
	})
	cancelFailure := buildFailureResponse(
		"req-cancel-1",
		protocol.MapGatewayCodeToJSONRPCCode(gateway.ErrorCodeResourceNotFound.String()),
		"cancel target not found",
		gateway.ErrorCodeResourceNotFound.String(),
	)

	listSessionsRequest := buildRequest("req-list-1", protocol.MethodGatewayListSessions, nil)
	listSessionsSuccess := buildSuccessResponse("req-list-1", gateway.MessageFrame{
		Type:      gateway.FrameTypeAck,
		Action:    gateway.FrameActionListSessions,
		RequestID: "req-list-1",
		Payload: map[string]any{
			"sessions": []gateway.SessionSummary{
				{
					ID:        sessionID,
					Title:     "gateway 文档联调",
					CreatedAt: mustParseTime("2026-04-22T09:00:00Z"),
					UpdatedAt: mustParseTime("2026-04-22T09:10:00Z"),
				},
			},
		},
	})
	listSessionsFailure := buildFailureResponse(
		"req-list-1",
		protocol.MapGatewayCodeToJSONRPCCode(gateway.ErrorCodeUnauthorized.String()),
		"unauthorized",
		gateway.ErrorCodeUnauthorized.String(),
	)

	loadSessionRequest := buildRequest("req-load-1", protocol.MethodGatewayLoadSession, protocol.LoadSessionParams{
		SessionID: sessionID,
	})
	loadSessionSuccess := buildSuccessResponse("req-load-1", gateway.MessageFrame{
		Type:      gateway.FrameTypeAck,
		Action:    gateway.FrameActionLoadSession,
		RequestID: "req-load-1",
		SessionID: sessionID,
		Payload: gateway.Session{
			ID:        sessionID,
			Title:     "gateway 文档联调",
			CreatedAt: mustParseTime("2026-04-22T09:00:00Z"),
			UpdatedAt: mustParseTime("2026-04-22T09:10:00Z"),
			Workdir:   "C:/workspace/demo",
			Messages:  []gateway.SessionMessage{},
		},
	})
	loadSessionFailure := buildFailureResponse(
		"req-load-1",
		protocol.MapGatewayCodeToJSONRPCCode(gateway.ErrorCodeAccessDenied.String()),
		"load_session access denied",
		gateway.ErrorCodeAccessDenied.String(),
	)

	resolvePermissionRequest := buildRequest(
		"req-permission-1",
		protocol.MethodGatewayResolvePermission,
		protocol.ResolvePermissionParams{
			RequestID: "perm-request-1",
			Decision:  string(gateway.PermissionResolutionAllowOnce),
		},
	)
	resolvePermissionSuccess := buildSuccessResponse("req-permission-1", gateway.MessageFrame{
		Type:      gateway.FrameTypeAck,
		Action:    gateway.FrameActionResolvePermission,
		RequestID: "req-permission-1",
		Payload: map[string]any{
			"request_id": "perm-request-1",
			"decision":   string(gateway.PermissionResolutionAllowOnce),
			"message":    "permission resolved",
		},
	})
	resolvePermissionFailure := buildFailureResponse(
		"req-permission-1",
		protocol.MapGatewayCodeToJSONRPCCode(gateway.ErrorCodeInvalidAction.String()),
		"invalid resolve_permission decision",
		gateway.ErrorCodeInvalidAction.String(),
	)

	wakeRequest := buildRequest("req-wake-1", protocol.MethodWakeOpenURL, protocol.WakeIntent{
		Action:    protocol.WakeActionReview,
		SessionID: sessionID,
		Workdir:   "C:/workspace/demo",
		Params: map[string]string{
			"path": "README.md",
		},
		RawURL: "http://neocode:18921/review?path=README.md",
	})
	wakeSuccess := buildSuccessResponse("req-wake-1", gateway.MessageFrame{
		Type:      gateway.FrameTypeAck,
		Action:    gateway.FrameActionWakeOpenURL,
		RequestID: "req-wake-1",
		SessionID: sessionID,
		Payload: map[string]any{
			"message": "wake intent accepted",
			"action":  protocol.WakeActionReview,
			"params": map[string]string{
				"path": "README.md",
			},
		},
	})
	wakeFailure := buildFailureResponse(
		"req-wake-1",
		protocol.MapGatewayCodeToJSONRPCCode(gateway.ErrorCodeMissingRequiredField.String()),
		"missing required field: params.path",
		gateway.ErrorCodeMissingRequiredField.String(),
	)

	eventNotification := protocol.NewJSONRPCNotification(protocol.MethodGatewayEvent, gateway.MessageFrame{
		Type:      gateway.FrameTypeEvent,
		Action:    gateway.FrameActionRun,
		SessionID: sessionID,
		RunID:     runID,
		Payload: map[string]any{
			"event_type": string(gateway.RuntimeEventTypeRunProgress),
			"payload": map[string]any{
				"runtime_event_type": "agent_chunk",
				"turn":               3,
				"phase":              "reasoning",
				"timestamp":          "2026-04-22T09:01:02.123456789Z",
				"payload_version":    controlplane.PayloadVersion,
				"payload": map[string]any{
					"delta": "正在分析请求...",
				},
			},
		},
	})

	examples := []methodExample{
		{
			Method:  protocol.MethodGatewayAuthenticate,
			Request: authenticateRequest,
			Success: authenticateSuccess,
			Failure: authenticateFailure,
		},
		{
			Method:  protocol.MethodGatewayPing,
			Request: pingRequest,
			Success: pingSuccess,
			Failure: pingFailure,
		},
		{
			Method:  protocol.MethodGatewayBindStream,
			Request: bindRequest,
			Success: bindSuccess,
			Failure: bindFailure,
			Notes: []string{
				"`bindStream` 仅建立订阅绑定，后续运行事件通过 `gateway.event` 推送。",
			},
		},
		{
			Method:  protocol.MethodGatewayRun,
			Request: runRequest,
			Success: runSuccess,
			Failure: runFailure,
			Notes: []string{
				"`Success Response` 只代表受理成功，不代表运行完成。",
				"运行完成或失败需要通过 `gateway.event` 的 `run_done/run_error` 判断。",
			},
		},
		{
			Method:  protocol.MethodGatewayCompact,
			Request: compactRequest,
			Success: compactSuccess,
			Failure: compactFailure,
		},
		{
			Method:  protocol.MethodGatewayExecuteSystemTool,
			Request: executeSystemToolRequest,
			Success: executeSystemToolSuccess,
			Failure: executeSystemToolFailure,
			Notes: []string{
				"`tool_name` 在网关层按白名单校验，当前允许 memo 系列工具与 diagnose。",
			},
		},
		{
			Method:  protocol.MethodGatewayActivateSessionSkill,
			Request: activateSkillRequest,
			Success: activateSkillSuccess,
			Failure: activateSkillFailure,
		},
		{
			Method:  protocol.MethodGatewayDeactivateSessionSkill,
			Request: deactivateSkillRequest,
			Success: deactivateSkillSuccess,
			Failure: deactivateSkillFailure,
		},
		{
			Method:  protocol.MethodGatewayListSessionSkills,
			Request: listSessionSkillsRequest,
			Success: listSessionSkillsSuccess,
			Failure: listSessionSkillsFailure,
		},
		{
			Method:  protocol.MethodGatewayListAvailableSkills,
			Request: listAvailableSkillsRequest,
			Success: listAvailableSkillsSuccess,
			Failure: listAvailableSkillsFailure,
		},
		{
			Method:  protocol.MethodGatewayCancel,
			Request: cancelRequest,
			Success: cancelSuccess,
			Failure: cancelFailure,
		},
		{
			Method:  protocol.MethodGatewayListSessions,
			Request: listSessionsRequest,
			Success: listSessionsSuccess,
			Failure: listSessionsFailure,
		},
		{
			Method:  protocol.MethodGatewayLoadSession,
			Request: loadSessionRequest,
			Success: loadSessionSuccess,
			Failure: loadSessionFailure,
		},
		{
			Method:  protocol.MethodGatewayResolvePermission,
			Request: resolvePermissionRequest,
			Success: resolvePermissionSuccess,
			Failure: resolvePermissionFailure,
		},
		{
			Method:  protocol.MethodWakeOpenURL,
			Request: wakeRequest,
			Success: wakeSuccess,
			Failure: wakeFailure,
		},
		{
			Method:       protocol.MethodGatewayEvent,
			Notification: eventNotification,
		},
	}

	return examples, nil
}

// buildRequest 构建标准 JSON-RPC 请求对象。
func buildRequest(requestID, method string, params any) protocol.JSONRPCRequest {
	request := protocol.JSONRPCRequest{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      quotedID(requestID),
		Method:  strings.TrimSpace(method),
	}
	if params != nil {
		request.Params = marshalRawJSON(params)
	}
	return request
}

// buildSuccessResponse 基于 MessageFrame 生成 JSON-RPC 成功响应。
func buildSuccessResponse(requestID string, frame gateway.MessageFrame) protocol.JSONRPCResponse {
	response, err := protocol.NewJSONRPCResultResponse(quotedID(requestID), frame)
	if err != nil {
		panic(fmt.Errorf("构造成功响应失败: %v", err))
	}
	return response
}

// buildFailureResponse 构建 JSON-RPC 失败响应。
func buildFailureResponse(requestID string, code int, message, gatewayCode string) protocol.JSONRPCResponse {
	return protocol.NewJSONRPCErrorResponse(
		quotedID(requestID),
		protocol.NewJSONRPCError(code, strings.TrimSpace(message), strings.TrimSpace(gatewayCode)),
	)
}

// replaceGeneratedBlock 将文档中标记区块替换为新生成的文本内容。
func replaceGeneratedBlock(documentContent, generatedContent string) (string, error) {
	startIndex := strings.Index(documentContent, beginMarker)
	if startIndex < 0 {
		return "", fmt.Errorf("未找到自动生成起始标记 %q", beginMarker)
	}
	contentStart := startIndex + len(beginMarker)
	endOffset := strings.Index(documentContent[contentStart:], endMarker)
	if endOffset < 0 {
		return "", fmt.Errorf("未找到自动生成结束标记 %q", endMarker)
	}
	endIndex := contentStart + endOffset
	if endIndex < startIndex {
		return "", fmt.Errorf("自动生成区块标记顺序非法")
	}

	replaced := documentContent[:contentStart] + "\n" + generatedContent + "\n" + documentContent[endIndex:]
	return replaced, nil
}

// quotedID 将请求 ID 转换为 JSON-RPC 合法的 RawMessage 字面量。
func quotedID(requestID string) json.RawMessage {
	return json.RawMessage(strconv.Quote(strings.TrimSpace(requestID)))
}

// marshalRawJSON 将任意结构体编码为 RawMessage。
func marshalRawJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Errorf("编码 RawMessage 失败: %w", err))
	}
	return json.RawMessage(raw)
}

// mustPrettyJSON 将对象编码为缩进 JSON 字符串。
func mustPrettyJSON(value any) string {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		panic(fmt.Errorf("编码 JSON 示例失败: %w", err))
	}
	return string(raw)
}

// mustParseTime 将 RFC3339 时间文本解析为 time.Time，失败时直接 panic。
func mustParseTime(raw string) time.Time {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		panic("时间文本不能为空")
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		panic(fmt.Errorf("解析时间失败: %w", err))
	}
	return parsed
}

// exitWithError 统一输出错误并结束进程。
func exitWithError(message string, err error) {
	_, _ = fmt.Fprintf(os.Stderr, "%s: %v\n", message, err)
	os.Exit(1)
}
