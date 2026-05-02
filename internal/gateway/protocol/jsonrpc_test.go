package protocol

import (
	"encoding/json"
	"testing"
)

func TestNormalizeJSONRPCRequestPing(t *testing.T) {
	normalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"ping-1"`),
		Method:  MethodGatewayPing,
		Params:  json.RawMessage(`{}`),
	})
	if rpcErr != nil {
		t.Fatalf("normalize ping request: %v", rpcErr)
	}
	if normalized.RequestID != "ping-1" {
		t.Fatalf("request_id = %q, want %q", normalized.RequestID, "ping-1")
	}
	if normalized.Action != "ping" {
		t.Fatalf("action = %q, want %q", normalized.Action, "ping")
	}
}

func TestNormalizeJSONRPCRequestAuthenticate(t *testing.T) {
	normalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"auth-1"`),
		Method:  MethodGatewayAuthenticate,
		Params:  json.RawMessage(`{"token":"abc"}`),
	})
	if rpcErr != nil {
		t.Fatalf("normalize authenticate request: %v", rpcErr)
	}
	if normalized.Action != "authenticate" {
		t.Fatalf("action = %q, want %q", normalized.Action, "authenticate")
	}
	params, ok := normalized.Payload.(AuthenticateParams)
	if !ok {
		t.Fatalf("payload type = %T, want AuthenticateParams", normalized.Payload)
	}
	if params.Token != "abc" {
		t.Fatalf("token = %q, want %q", params.Token, "abc")
	}
}

func TestNormalizeJSONRPCRequestPingWithNumericID(t *testing.T) {
	normalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`123`),
		Method:  MethodGatewayPing,
		Params:  json.RawMessage(`{}`),
	})
	if rpcErr != nil {
		t.Fatalf("normalize ping request with numeric id: %v", rpcErr)
	}
	if normalized.RequestID != "123" {
		t.Fatalf("request_id = %q, want %q", normalized.RequestID, "123")
	}
}

func TestNormalizeJSONRPCRequestWakeOpenURL(t *testing.T) {
	normalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"wake-1"`),
		Method:  MethodWakeOpenURL,
		Params: json.RawMessage(`{
			"action":"review",
			"session_id":"session-1",
			"workdir":"/tmp/repo",
			"params":{"path":"README.md"}
		}`),
	})
	if rpcErr != nil {
		t.Fatalf("normalize wake request: %v", rpcErr)
	}
	if normalized.Action != MethodWakeOpenURL {
		t.Fatalf("action = %q, want %q", normalized.Action, MethodWakeOpenURL)
	}
	if normalized.SessionID != "session-1" {
		t.Fatalf("session_id = %q, want %q", normalized.SessionID, "session-1")
	}
	if normalized.Workdir != "/tmp/repo" {
		t.Fatalf("workdir = %q, want %q", normalized.Workdir, "/tmp/repo")
	}
	intent, ok := normalized.Payload.(WakeIntent)
	if !ok {
		t.Fatalf("payload type = %T, want WakeIntent", normalized.Payload)
	}
	if intent.Params["path"] != "README.md" {
		t.Fatalf("intent.params[path] = %q, want %q", intent.Params["path"], "README.md")
	}
}

func TestNormalizeJSONRPCRequestBindStream(t *testing.T) {
	normalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"bind-1"`),
		Method:  MethodGatewayBindStream,
		Params: json.RawMessage(`{
			"session_id":"session-1",
			"run_id":"run-1",
			"channel":"ws"
		}`),
	})
	if rpcErr != nil {
		t.Fatalf("normalize bindStream request: %v", rpcErr)
	}
	if normalized.Action != "bind_stream" {
		t.Fatalf("action = %q, want %q", normalized.Action, "bind_stream")
	}
	if normalized.SessionID != "session-1" {
		t.Fatalf("session_id = %q, want %q", normalized.SessionID, "session-1")
	}
	if normalized.RunID != "run-1" {
		t.Fatalf("run_id = %q, want %q", normalized.RunID, "run-1")
	}
	params, ok := normalized.Payload.(BindStreamParams)
	if !ok {
		t.Fatalf("payload type = %T, want BindStreamParams", normalized.Payload)
	}
	if params.Channel != "ws" {
		t.Fatalf("channel = %q, want %q", params.Channel, "ws")
	}
}

func TestNormalizeJSONRPCRequestListSessionTodosAndRuntimeSnapshot(t *testing.T) {
	t.Run("list session todos success", func(t *testing.T) {
		normalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`"todo-1"`),
			Method:  MethodGatewayListSessionTodos,
			Params:  json.RawMessage(`{"session_id":" s-1 "}`),
		})
		if rpcErr != nil {
			t.Fatalf("normalize list session todos request: %v", rpcErr)
		}
		if normalized.Action != "session_todos_list" {
			t.Fatalf("action = %q, want %q", normalized.Action, "session_todos_list")
		}
		if normalized.SessionID != "s-1" {
			t.Fatalf("session_id = %q, want %q", normalized.SessionID, "s-1")
		}
		params, ok := normalized.Payload.(ListSessionTodosParams)
		if !ok {
			t.Fatalf("payload type = %T, want ListSessionTodosParams", normalized.Payload)
		}
		if params.SessionID != "s-1" {
			t.Fatalf("params.session_id = %q, want %q", params.SessionID, "s-1")
		}
	})

	t.Run("runtime snapshot success", func(t *testing.T) {
		normalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`"snapshot-1"`),
			Method:  MethodGatewayGetRuntimeSnapshot,
			Params:  json.RawMessage(`{"session_id":" s-2 "}`),
		})
		if rpcErr != nil {
			t.Fatalf("normalize runtime snapshot request: %v", rpcErr)
		}
		if normalized.Action != "runtime_snapshot_get" {
			t.Fatalf("action = %q, want %q", normalized.Action, "runtime_snapshot_get")
		}
		if normalized.SessionID != "s-2" {
			t.Fatalf("session_id = %q, want %q", normalized.SessionID, "s-2")
		}
		params, ok := normalized.Payload.(GetRuntimeSnapshotParams)
		if !ok {
			t.Fatalf("payload type = %T, want GetRuntimeSnapshotParams", normalized.Payload)
		}
		if params.SessionID != "s-2" {
			t.Fatalf("params.session_id = %q, want %q", params.SessionID, "s-2")
		}
	})

	t.Run("list session todos missing params", func(t *testing.T) {
		_, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`"todo-missing-1"`),
			Method:  MethodGatewayListSessionTodos,
			Params:  json.RawMessage(`null`),
		})
		if rpcErr == nil || rpcErr.Code != JSONRPCCodeInvalidParams {
			t.Fatalf("expected invalid params error, got %#v", rpcErr)
		}
	})

	t.Run("runtime snapshot missing session id", func(t *testing.T) {
		_, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`"snapshot-missing-1"`),
			Method:  MethodGatewayGetRuntimeSnapshot,
			Params:  json.RawMessage(`{"session_id":"   "}`),
		})
		if rpcErr == nil || rpcErr.Code != JSONRPCCodeInvalidParams {
			t.Fatalf("expected invalid params error, got %#v", rpcErr)
		}
	})
}

func TestNormalizeJSONRPCRequestRuntimeMethods(t *testing.T) {
	runRequest := JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"run-1"`),
		Method:  MethodGatewayRun,
		Params: json.RawMessage(`{
			"session_id":" session-1 ",
			"run_id":" run-1 ",
			"input_text":" hello ",
			"workdir":" /tmp/work ",
			"input_parts":[
				{"type":" TEXT ","text":" world "},
				{"type":" image ","media":{"uri":" /tmp/a.png ","mime_type":" image/png ","file_name":" a.png "}}
			]
		}`),
	}
	normalized, rpcErr := NormalizeJSONRPCRequest(runRequest)
	if rpcErr != nil {
		t.Fatalf("normalize run request: %v", rpcErr)
	}
	if normalized.Action != "run" {
		t.Fatalf("run action = %q, want %q", normalized.Action, "run")
	}
	if normalized.SessionID != "session-1" || normalized.RunID != "run-1" || normalized.Workdir != "/tmp/work" {
		t.Fatalf("normalized run identifiers = %#v", normalized)
	}
	runParams, ok := normalized.Payload.(RunParams)
	if !ok {
		t.Fatalf("run payload type = %T, want RunParams", normalized.Payload)
	}
	if runParams.InputText != "hello" {
		t.Fatalf("run input_text = %q, want %q", runParams.InputText, "hello")
	}
	if len(runParams.InputParts) != 2 {
		t.Fatalf("run input_parts len = %d, want 2", len(runParams.InputParts))
	}
	if runParams.InputParts[0].Type != "text" || runParams.InputParts[0].Text != "world" {
		t.Fatalf("run text part = %#v, want normalized text part", runParams.InputParts[0])
	}
	if runParams.InputParts[1].Type != "image" || runParams.InputParts[1].Media == nil || runParams.InputParts[1].Media.URI != "/tmp/a.png" {
		t.Fatalf("run image part = %#v, want normalized image part", runParams.InputParts[1])
	}
	if runParams.InputParts[1].Media.MimeType != "image/png" || runParams.InputParts[1].Media.FileName != "a.png" {
		t.Fatalf("run image media = %#v, want trimmed mime/file_name", runParams.InputParts[1].Media)
	}

	compactNormalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"compact-1"`),
		Method:  MethodGatewayCompact,
		Params:  json.RawMessage(`{"session_id":" s-1 ","run_id":" r-1 "}`),
	})
	if rpcErr != nil {
		t.Fatalf("normalize compact request: %v", rpcErr)
	}
	if compactNormalized.Action != "compact" || compactNormalized.SessionID != "s-1" || compactNormalized.RunID != "r-1" {
		t.Fatalf("compact normalized = %#v", compactNormalized)
	}

	execNormalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"exec-1"`),
		Method:  MethodGatewayExecuteSystemTool,
		Params: json.RawMessage(`{
			"session_id":" s-1 ",
			"run_id":" r-1 ",
			"workdir":" /repo ",
			"tool_name":" memo_list ",
			"arguments":{"scope":"all"}
		}`),
	})
	if rpcErr != nil {
		t.Fatalf("normalize executeSystemTool request: %v", rpcErr)
	}
	if execNormalized.Action != "execute_system_tool" {
		t.Fatalf("executeSystemTool action = %q, want %q", execNormalized.Action, "execute_system_tool")
	}
	if execNormalized.SessionID != "s-1" || execNormalized.RunID != "r-1" || execNormalized.Workdir != "/repo" {
		t.Fatalf("executeSystemTool normalized ids/workdir = %#v", execNormalized)
	}
	execParams, ok := execNormalized.Payload.(ExecuteSystemToolParams)
	if !ok {
		t.Fatalf("executeSystemTool payload type = %T, want ExecuteSystemToolParams", execNormalized.Payload)
	}
	if execParams.ToolName != "memo_list" {
		t.Fatalf("executeSystemTool tool_name = %q, want %q", execParams.ToolName, "memo_list")
	}
	if string(execParams.Arguments) != `{"scope":"all"}` {
		t.Fatalf("executeSystemTool arguments = %s, want %s", string(execParams.Arguments), `{"scope":"all"}`)
	}

	activateSkillNormalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"activate-skill-1"`),
		Method:  MethodGatewayActivateSessionSkill,
		Params:  json.RawMessage(`{"session_id":" s-1 ","skill_id":" go-review "}`),
	})
	if rpcErr != nil {
		t.Fatalf("normalize activateSessionSkill request: %v", rpcErr)
	}
	if activateSkillNormalized.Action != "activate_session_skill" || activateSkillNormalized.SessionID != "s-1" {
		t.Fatalf("activateSessionSkill normalized = %#v", activateSkillNormalized)
	}
	activateSkillParams, ok := activateSkillNormalized.Payload.(ActivateSessionSkillParams)
	if !ok {
		t.Fatalf("activateSessionSkill payload type = %T, want ActivateSessionSkillParams", activateSkillNormalized.Payload)
	}
	if activateSkillParams.SessionID != "s-1" || activateSkillParams.SkillID != "go-review" {
		t.Fatalf("activateSessionSkill payload = %#v, want trimmed session_id/skill_id", activateSkillParams)
	}

	deactivateSkillNormalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"deactivate-skill-1"`),
		Method:  MethodGatewayDeactivateSessionSkill,
		Params:  json.RawMessage(`{"session_id":" s-1 ","skill_id":" go-review "}`),
	})
	if rpcErr != nil {
		t.Fatalf("normalize deactivateSessionSkill request: %v", rpcErr)
	}
	if deactivateSkillNormalized.Action != "deactivate_session_skill" || deactivateSkillNormalized.SessionID != "s-1" {
		t.Fatalf("deactivateSessionSkill normalized = %#v", deactivateSkillNormalized)
	}
	deactivateSkillParams, ok := deactivateSkillNormalized.Payload.(DeactivateSessionSkillParams)
	if !ok {
		t.Fatalf("deactivateSessionSkill payload type = %T, want DeactivateSessionSkillParams", deactivateSkillNormalized.Payload)
	}
	if deactivateSkillParams.SessionID != "s-1" || deactivateSkillParams.SkillID != "go-review" {
		t.Fatalf("deactivateSessionSkill payload = %#v, want trimmed session_id/skill_id", deactivateSkillParams)
	}

	listSessionSkillsNormalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"list-session-skills-1"`),
		Method:  MethodGatewayListSessionSkills,
		Params:  json.RawMessage(`{"session_id":" s-1 "}`),
	})
	if rpcErr != nil {
		t.Fatalf("normalize listSessionSkills request: %v", rpcErr)
	}
	if listSessionSkillsNormalized.Action != "list_session_skills" || listSessionSkillsNormalized.SessionID != "s-1" {
		t.Fatalf("listSessionSkills normalized = %#v", listSessionSkillsNormalized)
	}
	if _, ok := listSessionSkillsNormalized.Payload.(ListSessionSkillsParams); !ok {
		t.Fatalf("listSessionSkills payload type = %T, want ListSessionSkillsParams", listSessionSkillsNormalized.Payload)
	}

	listAvailableSkillsNormalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"list-available-skills-1"`),
		Method:  MethodGatewayListAvailableSkills,
		Params:  json.RawMessage(`{"session_id":" s-1 "}`),
	})
	if rpcErr != nil {
		t.Fatalf("normalize listAvailableSkills request: %v", rpcErr)
	}
	if listAvailableSkillsNormalized.Action != "list_available_skills" || listAvailableSkillsNormalized.SessionID != "s-1" {
		t.Fatalf("listAvailableSkills normalized = %#v", listAvailableSkillsNormalized)
	}
	if _, ok := listAvailableSkillsNormalized.Payload.(ListAvailableSkillsParams); !ok {
		t.Fatalf("listAvailableSkills payload type = %T, want ListAvailableSkillsParams", listAvailableSkillsNormalized.Payload)
	}

	listAvailableSkillsWithoutParams, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"list-available-skills-2"`),
		Method:  MethodGatewayListAvailableSkills,
	})
	if rpcErr != nil {
		t.Fatalf("normalize listAvailableSkills without params request: %v", rpcErr)
	}
	listAvailableSkillsWithoutParamsPayload, ok := listAvailableSkillsWithoutParams.Payload.(ListAvailableSkillsParams)
	if !ok {
		t.Fatalf(
			"listAvailableSkills without params payload type = %T, want ListAvailableSkillsParams",
			listAvailableSkillsWithoutParams.Payload,
		)
	}
	if listAvailableSkillsWithoutParamsPayload.SessionID != "" {
		t.Fatalf("listAvailableSkills without params payload = %#v, want empty session_id", listAvailableSkillsWithoutParamsPayload)
	}

	cancelNormalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"cancel-1"`),
		Method:  MethodGatewayCancel,
		Params:  json.RawMessage(`{"run_id":" r-0 "}`),
	})
	if rpcErr != nil {
		t.Fatalf("normalize cancel request: %v", rpcErr)
	}
	if cancelNormalized.Action != "cancel" {
		t.Fatalf("cancel action = %q, want %q", cancelNormalized.Action, "cancel")
	}
	cancelParams, ok := cancelNormalized.Payload.(CancelParams)
	if !ok {
		t.Fatalf("cancel payload type = %T, want CancelParams", cancelNormalized.Payload)
	}
	if cancelParams.SessionID != "" || cancelParams.RunID != "r-0" {
		t.Fatalf("cancel payload = %#v, want trimmed run_id", cancelParams)
	}

	cancelWithParams, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"cancel-2"`),
		Method:  MethodGatewayCancel,
		Params:  json.RawMessage(`{"session_id":" s-1 ","run_id":" r-1 "}`),
	})
	if rpcErr != nil {
		t.Fatalf("normalize cancel request with params: %v", rpcErr)
	}
	cancelWithParamsPayload, ok := cancelWithParams.Payload.(CancelParams)
	if !ok {
		t.Fatalf("cancel payload type = %T, want CancelParams", cancelWithParams.Payload)
	}
	if cancelWithParamsPayload.SessionID != "s-1" || cancelWithParamsPayload.RunID != "r-1" {
		t.Fatalf("cancel payload = %#v, want trimmed session_id/run_id", cancelWithParamsPayload)
	}

	listNormalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"list-1"`),
		Method:  MethodGatewayListSessions,
	})
	if rpcErr != nil {
		t.Fatalf("normalize list request: %v", rpcErr)
	}
	if listNormalized.Action != "list_sessions" {
		t.Fatalf("list action = %q, want %q", listNormalized.Action, "list_sessions")
	}

	loadNormalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"load-1"`),
		Method:  MethodGatewayLoadSession,
		Params:  json.RawMessage(`{"session_id":" s-1 "}`),
	})
	if rpcErr != nil {
		t.Fatalf("normalize load request: %v", rpcErr)
	}
	if loadNormalized.Action != "load_session" || loadNormalized.SessionID != "s-1" {
		t.Fatalf("load normalized = %#v", loadNormalized)
	}
	if _, ok := loadNormalized.Payload.(LoadSessionParams); !ok {
		t.Fatalf("load payload type = %T, want LoadSessionParams", loadNormalized.Payload)
	}

	resolveNormalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`"resolve-1"`),
		Method:  MethodGatewayResolvePermission,
		Params:  json.RawMessage(`{"request_id":" req-1 ","decision":" ALLOW_SESSION "}`),
	})
	if rpcErr != nil {
		t.Fatalf("normalize resolve_permission request: %v", rpcErr)
	}
	if resolveNormalized.Action != "resolve_permission" {
		t.Fatalf("resolve action = %q, want %q", resolveNormalized.Action, "resolve_permission")
	}
	resolveParams, ok := resolveNormalized.Payload.(ResolvePermissionParams)
	if !ok {
		t.Fatalf("resolve payload type = %T, want ResolvePermissionParams", resolveNormalized.Payload)
	}
	if resolveParams.RequestID != "req-1" || resolveParams.Decision != "allow_session" {
		t.Fatalf("resolve payload = %#v, want normalized request_id/decision", resolveParams)
	}
}

func TestNormalizeJSONRPCRequestErrors(t *testing.T) {
	testCases := []struct {
		name            string
		request         JSONRPCRequest
		wantCode        int
		wantGatewayCode string
	}{
		{
			name: "missing id",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				Method:  MethodGatewayPing,
			},
			wantCode:        JSONRPCCodeInvalidRequest,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "invalid version",
			request: JSONRPCRequest{
				JSONRPC: "1.0",
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayPing,
			},
			wantCode:        JSONRPCCodeInvalidRequest,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "invalid id object",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`{}`),
				Method:  MethodGatewayPing,
			},
			wantCode:        JSONRPCCodeInvalidRequest,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "invalid id array",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`[]`),
				Method:  MethodGatewayPing,
			},
			wantCode:        JSONRPCCodeInvalidRequest,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "invalid id boolean",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`true`),
				Method:  MethodGatewayPing,
			},
			wantCode:        JSONRPCCodeInvalidRequest,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "authenticate missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayAuthenticate,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "missing method",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
			},
			wantCode:        JSONRPCCodeInvalidRequest,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "method not found",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  "gateway.unknown",
			},
			wantCode:        JSONRPCCodeMethodNotFound,
			wantGatewayCode: GatewayCodeUnsupportedAction,
		},
		{
			name: "wake missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodWakeOpenURL,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "wake invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodWakeOpenURL,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "bindStream missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayBindStream,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "bindStream missing session",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayBindStream,
				Params:  json.RawMessage(`{"channel":"all"}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "bindStream invalid channel",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayBindStream,
				Params:  json.RawMessage(`{"session_id":"s-1","channel":"tcp"}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidAction,
		},
		{
			name: "run missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayRun,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "run invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayRun,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "cancel invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayCancel,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "executeSystemTool missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayExecuteSystemTool,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "executeSystemTool invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayExecuteSystemTool,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "executeSystemTool missing tool_name",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayExecuteSystemTool,
				Params:  json.RawMessage(`{"tool_name":" "}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "activateSessionSkill missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayActivateSessionSkill,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "activateSessionSkill invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayActivateSessionSkill,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "activateSessionSkill missing session_id",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayActivateSessionSkill,
				Params:  json.RawMessage(`{"skill_id":"go-review"}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "activateSessionSkill missing skill_id",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayActivateSessionSkill,
				Params:  json.RawMessage(`{"session_id":"s-1"}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "deactivateSessionSkill missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayDeactivateSessionSkill,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "deactivateSessionSkill invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayDeactivateSessionSkill,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "deactivateSessionSkill missing session_id",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayDeactivateSessionSkill,
				Params:  json.RawMessage(`{"skill_id":"go-review"}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "deactivateSessionSkill missing skill_id",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayDeactivateSessionSkill,
				Params:  json.RawMessage(`{"session_id":"s-1"}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "listSessionSkills missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayListSessionSkills,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "listSessionSkills invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayListSessionSkills,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "listSessionSkills missing session_id",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayListSessionSkills,
				Params:  json.RawMessage(`{"session_id":" "}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "listAvailableSkills invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayListAvailableSkills,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "compact missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayCompact,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "compact invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayCompact,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "compact missing session_id",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayCompact,
				Params:  json.RawMessage(`{"run_id":"r-1"}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "loadSession missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayLoadSession,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "loadSession invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayLoadSession,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "loadSession missing session_id",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayLoadSession,
				Params:  json.RawMessage(`{"session_id":"  "}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "resolvePermission missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayResolvePermission,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "resolvePermission invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayResolvePermission,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "resolvePermission missing request_id",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayResolvePermission,
				Params:  json.RawMessage(`{"decision":"allow_once"}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "resolvePermission invalid decision",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayResolvePermission,
				Params:  json.RawMessage(`{"request_id":"req-1","decision":"invalid"}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidAction,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, rpcErr := NormalizeJSONRPCRequest(tc.request)
			if rpcErr == nil {
				t.Fatal("expected rpc error")
			}
			if rpcErr.Code != tc.wantCode {
				t.Fatalf("rpc code = %d, want %d", rpcErr.Code, tc.wantCode)
			}
			if gatewayCode := GatewayCodeFromJSONRPCError(rpcErr); gatewayCode != tc.wantGatewayCode {
				t.Fatalf("gateway_code = %q, want %q", gatewayCode, tc.wantGatewayCode)
			}
		})
	}
}

func TestJSONRPCDecode_RejectUnknownFields(t *testing.T) {
	testCases := []struct {
		name   string
		method string
		params string
	}{
		{
			name:   "run params contain unknown field",
			method: MethodGatewayRun,
			params: `{"session_id":"s-1","input_text":"hello","unknown":"x"}`,
		},
		{
			name:   "cancel params contain unknown field",
			method: MethodGatewayCancel,
			params: `{"run_id":"r-1","typo_field":"x"}`,
		},
		{
			name:   "loadSession params contain unknown field",
			method: MethodGatewayLoadSession,
			params: `{"session_id":"s-1","extra":1}`,
		},
		{
			name:   "executeSystemTool params contain unknown field",
			method: MethodGatewayExecuteSystemTool,
			params: `{"tool_name":"memo_list","unknown":"x"}`,
		},
		{
			name:   "activateSessionSkill params contain unknown field",
			method: MethodGatewayActivateSessionSkill,
			params: `{"session_id":"s-1","skill_id":"go-review","unknown":"x"}`,
		},
		{
			name:   "deactivateSessionSkill params contain unknown field",
			method: MethodGatewayDeactivateSessionSkill,
			params: `{"session_id":"s-1","skill_id":"go-review","unknown":"x"}`,
		},
		{
			name:   "listSessionSkills params contain unknown field",
			method: MethodGatewayListSessionSkills,
			params: `{"session_id":"s-1","unknown":"x"}`,
		},
		{
			name:   "listAvailableSkills params contain unknown field",
			method: MethodGatewayListAvailableSkills,
			params: `{"session_id":"s-1","unknown":"x"}`,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"strict-unknown"`),
				Method:  tc.method,
				Params:  json.RawMessage(tc.params),
			})
			if rpcErr == nil {
				t.Fatal("expected rpc error for unknown field")
			}
			if rpcErr.Code != JSONRPCCodeInvalidParams {
				t.Fatalf("rpc code = %d, want %d", rpcErr.Code, JSONRPCCodeInvalidParams)
			}
			if gatewayCode := GatewayCodeFromJSONRPCError(rpcErr); gatewayCode != GatewayCodeInvalidFrame {
				t.Fatalf("gateway_code = %q, want %q", gatewayCode, GatewayCodeInvalidFrame)
			}
		})
	}
}

func TestNormalizeJSONRPCRequestInvalidIDReturnsNullResponseID(t *testing.T) {
	normalized, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      json.RawMessage(`{}`),
		Method:  MethodGatewayPing,
	})
	if rpcErr == nil {
		t.Fatal("expected rpc error")
	}
	if rpcErr.Code != JSONRPCCodeInvalidRequest {
		t.Fatalf("rpc code = %d, want %d", rpcErr.Code, JSONRPCCodeInvalidRequest)
	}
	if normalized.ID != nil {
		t.Fatalf("normalized id = %s, want nil", string(normalized.ID))
	}
}

func TestJSONRPCHelpers(t *testing.T) {
	response, rpcErr := NewJSONRPCResultResponse(json.RawMessage(`"req-1"`), map[string]string{"message": "ok"})
	if rpcErr != nil {
		t.Fatalf("new jsonrpc result response: %v", rpcErr)
	}
	if response.JSONRPC != JSONRPCVersion {
		t.Fatalf("jsonrpc = %q, want %q", response.JSONRPC, JSONRPCVersion)
	}
	if string(response.ID) != `"req-1"` {
		t.Fatalf("id = %s, want %s", response.ID, `"req-1"`)
	}
	var result map[string]string
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatalf("decode result raw message: %v", err)
	}
	if result["message"] != "ok" {
		t.Fatalf(`result["message"] = %q, want %q`, result["message"], "ok")
	}

	_, rpcErr = NewJSONRPCResultResponse(json.RawMessage(`"req-chan"`), map[string]any{"bad": make(chan int)})
	if rpcErr == nil {
		t.Fatal("expected result encode error")
	}
	if rpcErr.Code != JSONRPCCodeInternalError {
		t.Fatalf("rpc code = %d, want %d", rpcErr.Code, JSONRPCCodeInternalError)
	}
	if gatewayCode := GatewayCodeFromJSONRPCError(rpcErr); gatewayCode != GatewayCodeInternalError {
		t.Fatalf("gateway_code = %q, want %q", gatewayCode, GatewayCodeInternalError)
	}

	rpcErr = NewJSONRPCError(JSONRPCCodeInternalError, "boom", GatewayCodeInternalError)
	errorResponse := NewJSONRPCErrorResponse(json.RawMessage(`"req-2"`), rpcErr)
	if errorResponse.Error == nil {
		t.Fatal("error response should include rpc error payload")
	}
	if GatewayCodeFromJSONRPCError(errorResponse.Error) != GatewayCodeInternalError {
		t.Fatalf("gateway_code = %q, want %q", GatewayCodeFromJSONRPCError(errorResponse.Error), GatewayCodeInternalError)
	}
	if GatewayCodeFromJSONRPCError(nil) != "" {
		t.Fatal("gateway_code for nil rpc error should be empty")
	}

	if MapGatewayCodeToJSONRPCCode(GatewayCodeUnsupportedAction) != JSONRPCCodeMethodNotFound {
		t.Fatal("unsupported_action should map to method_not_found")
	}
	if MapGatewayCodeToJSONRPCCode(GatewayCodeInvalidAction) != JSONRPCCodeInvalidParams {
		t.Fatal("invalid_action should map to invalid_params")
	}
	if MapGatewayCodeToJSONRPCCode(GatewayCodeUnauthorized) != JSONRPCCodeInvalidParams {
		t.Fatal("unauthorized should map to invalid_params")
	}
	if MapGatewayCodeToJSONRPCCode(GatewayCodeAccessDenied) != JSONRPCCodeInvalidParams {
		t.Fatal("access_denied should map to invalid_params")
	}
	if MapGatewayCodeToJSONRPCCode(GatewayCodeTimeout) != JSONRPCCodeInternalError {
		t.Fatal("timeout should map to internal_error")
	}
	if MapGatewayCodeToJSONRPCCode("unknown") != JSONRPCCodeInternalError {
		t.Fatal("unknown code should map to internal_error")
	}

	notification := NewJSONRPCNotification(MethodGatewayEvent, map[string]any{"message": "ok"})
	if notification.JSONRPC != JSONRPCVersion {
		t.Fatalf("notification jsonrpc = %q, want %q", notification.JSONRPC, JSONRPCVersion)
	}
	if notification.Method != MethodGatewayEvent {
		t.Fatalf("notification method = %q, want %q", notification.Method, MethodGatewayEvent)
	}
}

func TestNewJSONRPCErrorResponseWithNilIDEncodesNull(t *testing.T) {
	response := NewJSONRPCErrorResponse(nil, NewJSONRPCError(JSONRPCCodeParseError, "parse error", GatewayCodeInvalidFrame))
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal error response: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("unmarshal encoded response: %v", err)
	}
	if _, ok := payload["id"]; !ok {
		t.Fatal("encoded response should contain id field")
	}
	if payload["id"] != nil {
		t.Fatalf("encoded response id = %#v, want nil", payload["id"])
	}
}

func TestNormalizeJSONRPCRequestNewMethodErrors(t *testing.T) {
	testCases := []struct {
		name            string
		request         JSONRPCRequest
		wantCode        int
		wantGatewayCode string
	}{
		{
			name: "deleteSession missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayDeleteSession,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "deleteSession invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayDeleteSession,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "deleteSession missing session_id",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayDeleteSession,
				Params:  json.RawMessage(`{"session_id":"  "}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "renameSession missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayRenameSession,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "renameSession invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayRenameSession,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "renameSession missing session_id",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayRenameSession,
				Params:  json.RawMessage(`{"title":"New Title"}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "renameSession missing title",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayRenameSession,
				Params:  json.RawMessage(`{"session_id":"s-1"}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "listFiles invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayListFiles,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "listModels invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayListModels,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "setSessionModel missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewaySetSessionModel,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "setSessionModel invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewaySetSessionModel,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "setSessionModel missing session_id",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewaySetSessionModel,
				Params:  json.RawMessage(`{"model_id":"gpt-4"}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "setSessionModel missing model_id",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewaySetSessionModel,
				Params:  json.RawMessage(`{"session_id":"s-1"}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "getSessionModel missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayGetSessionModel,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "getSessionModel invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayGetSessionModel,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "getSessionModel missing session_id",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayGetSessionModel,
				Params:  json.RawMessage(`{"session_id":"  "}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "createCustomProvider missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayCreateCustomProvider,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "createCustomProvider invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayCreateCustomProvider,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "createCustomProvider missing name",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayCreateCustomProvider,
				Params:  json.RawMessage(`{"driver":"openai","api_key_env":"KEY"}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "createCustomProvider missing driver",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayCreateCustomProvider,
				Params:  json.RawMessage(`{"name":"custom","api_key_env":"KEY"}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "createCustomProvider missing api_key_env",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayCreateCustomProvider,
				Params:  json.RawMessage(`{"name":"custom","driver":"openai"}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "deleteCustomProvider missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayDeleteCustomProvider,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "deleteCustomProvider invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayDeleteCustomProvider,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "deleteCustomProvider missing provider_id",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayDeleteCustomProvider,
				Params:  json.RawMessage(`{"provider_id":"  "}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "selectProviderModel missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewaySelectProviderModel,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "selectProviderModel invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewaySelectProviderModel,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "selectProviderModel missing provider_id",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewaySelectProviderModel,
				Params:  json.RawMessage(`{"provider_id":"  "}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "upsertMCPServer missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayUpsertMCPServer,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "upsertMCPServer invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayUpsertMCPServer,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "upsertMCPServer missing server.id",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayUpsertMCPServer,
				Params:  json.RawMessage(`{"server":{"id":"  "}}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "setMCPServerEnabled missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewaySetMCPServerEnabled,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "setMCPServerEnabled invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewaySetMCPServerEnabled,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "setMCPServerEnabled missing id",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewaySetMCPServerEnabled,
				Params:  json.RawMessage(`{"enabled":true}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "deleteMCPServer missing params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayDeleteMCPServer,
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
		{
			name: "deleteMCPServer invalid params",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayDeleteMCPServer,
				Params:  json.RawMessage(`{invalid}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeInvalidFrame,
		},
		{
			name: "deleteMCPServer missing id",
			request: JSONRPCRequest{
				JSONRPC: JSONRPCVersion,
				ID:      json.RawMessage(`"x"`),
				Method:  MethodGatewayDeleteMCPServer,
				Params:  json.RawMessage(`{"id":"  "}`),
			},
			wantCode:        JSONRPCCodeInvalidParams,
			wantGatewayCode: GatewayCodeMissingRequiredField,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, rpcErr := NormalizeJSONRPCRequest(tc.request)
			if rpcErr == nil {
				t.Fatal("expected rpc error")
			}
			if rpcErr.Code != tc.wantCode {
				t.Fatalf("rpc code = %d, want %d", rpcErr.Code, tc.wantCode)
			}
			if gatewayCode := GatewayCodeFromJSONRPCError(rpcErr); gatewayCode != tc.wantGatewayCode {
				t.Fatalf("gateway_code = %q, want %q", gatewayCode, tc.wantGatewayCode)
			}
		})
	}
}

func TestDecodeParamsOptionalMethods(t *testing.T) {
	t.Run("listFiles no params", func(t *testing.T) {
		frame, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`"1"`),
			Method:  MethodGatewayListFiles,
		})
		if rpcErr != nil {
			t.Fatalf("unexpected error: %+v", rpcErr)
		}
		if frame.Action != "list_files" {
			t.Fatalf("action = %q, want %q", frame.Action, "list_files")
		}
	})

	t.Run("listFiles empty object params", func(t *testing.T) {
		frame, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`"2"`),
			Method:  MethodGatewayListFiles,
			Params:  json.RawMessage(`{}`),
		})
		if rpcErr != nil {
			t.Fatalf("unexpected error: %+v", rpcErr)
		}
		if frame.Action != "list_files" {
			t.Fatalf("action = %q, want %q", frame.Action, "list_files")
		}
	})

	t.Run("listModels no params", func(t *testing.T) {
		frame, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`"3"`),
			Method:  MethodGatewayListModels,
		})
		if rpcErr != nil {
			t.Fatalf("unexpected error: %+v", rpcErr)
		}
		if frame.Action != "list_models" {
			t.Fatalf("action = %q, want %q", frame.Action, "list_models")
		}
	})

	t.Run("listModels empty object params", func(t *testing.T) {
		frame, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`"4"`),
			Method:  MethodGatewayListModels,
			Params:  json.RawMessage(`{}`),
		})
		if rpcErr != nil {
			t.Fatalf("unexpected error: %+v", rpcErr)
		}
		if frame.Action != "list_models" {
			t.Fatalf("action = %q, want %q", frame.Action, "list_models")
		}
	})
}

func TestNormalizeJSONRPCRequestNewRPCMethods(t *testing.T) {
	t.Run("deleteSession valid params", func(t *testing.T) {
		frame, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`"1"`),
			Method:  MethodGatewayDeleteSession,
			Params:  json.RawMessage(`{"session_id":"s-1"}`),
		})
		if rpcErr != nil {
			t.Fatalf("unexpected error: %+v", rpcErr)
		}
		if frame.Action != "delete_session" {
			t.Fatalf("action = %q, want %q", frame.Action, "delete_session")
		}
	})

	t.Run("renameSession valid params", func(t *testing.T) {
		frame, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`"2"`),
			Method:  MethodGatewayRenameSession,
			Params:  json.RawMessage(`{"session_id":"s-1","title":"New Title"}`),
		})
		if rpcErr != nil {
			t.Fatalf("unexpected error: %+v", rpcErr)
		}
		if frame.Action != "rename_session" {
			t.Fatalf("action = %q, want %q", frame.Action, "rename_session")
		}
	})

	t.Run("renameSession missing params", func(t *testing.T) {
		_, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`"3"`),
			Method:  MethodGatewayRenameSession,
		})
		if rpcErr == nil {
			t.Fatal("expected error for missing params")
		}
		if rpcErr.Code != JSONRPCCodeInvalidParams {
			t.Fatalf("code = %d, want %d", rpcErr.Code, JSONRPCCodeInvalidParams)
		}
	})

	t.Run("listFiles valid params", func(t *testing.T) {
		frame, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`"4"`),
			Method:  MethodGatewayListFiles,
			Params:  json.RawMessage(`{"workdir":"/tmp","path":"."}`),
		})
		if rpcErr != nil {
			t.Fatalf("unexpected error: %+v", rpcErr)
		}
		if frame.Action != "list_files" {
			t.Fatalf("action = %q, want %q", frame.Action, "list_files")
		}
	})

	t.Run("listModels no params required", func(t *testing.T) {
		frame, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`"5"`),
			Method:  MethodGatewayListModels,
			Params:  json.RawMessage(`{}`),
		})
		if rpcErr != nil {
			t.Fatalf("unexpected error: %+v", rpcErr)
		}
		if frame.Action != "list_models" {
			t.Fatalf("action = %q, want %q", frame.Action, "list_models")
		}
	})

	t.Run("setSessionModel valid params", func(t *testing.T) {
		frame, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`"6"`),
			Method:  MethodGatewaySetSessionModel,
			Params:  json.RawMessage(`{"session_id":"s-1","model_id":"gpt-4"}`),
		})
		if rpcErr != nil {
			t.Fatalf("unexpected error: %+v", rpcErr)
		}
		if frame.Action != "set_session_model" {
			t.Fatalf("action = %q, want %q", frame.Action, "set_session_model")
		}
	})

	t.Run("setSessionModel missing params", func(t *testing.T) {
		_, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`"7"`),
			Method:  MethodGatewaySetSessionModel,
		})
		if rpcErr == nil {
			t.Fatal("expected error for missing params")
		}
		if rpcErr.Code != JSONRPCCodeInvalidParams {
			t.Fatalf("code = %d, want %d", rpcErr.Code, JSONRPCCodeInvalidParams)
		}
	})

	t.Run("getSessionModel valid params", func(t *testing.T) {
		frame, rpcErr := NormalizeJSONRPCRequest(JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      json.RawMessage(`"8"`),
			Method:  MethodGatewayGetSessionModel,
			Params:  json.RawMessage(`{"session_id":"s-1"}`),
		})
		if rpcErr != nil {
			t.Fatalf("unexpected error: %+v", rpcErr)
		}
		if frame.Action != "get_session_model" {
			t.Fatalf("action = %q, want %q", frame.Action, "get_session_model")
		}
	})
}
