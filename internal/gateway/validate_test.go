package gateway

import (
	"strings"
	"testing"

	"neo-code/internal/gateway/protocol"
)

func TestValidateFrame_BasicRules(t *testing.T) {
	tests := []struct {
		name      string
		frame     MessageFrame
		wantNil   bool
		wantCode  string
		wantField string
	}{
		{
			name: "valid ping request",
			frame: MessageFrame{
				Type:      FrameTypeRequest,
				Action:    FrameActionPing,
				RequestID: "req-ping",
			},
			wantNil: true,
		},
		{
			name: "authenticate missing payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionAuthenticate,
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "payload",
		},
		{
			name: "execute_system_tool missing payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionExecuteSystemTool,
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "payload",
		},
		{
			name: "activate_session_skill missing payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionActivateSessionSkill,
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "payload",
		},
		{
			name: "list_session_skills missing session_id",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionListSessionSkills,
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "session_id",
		},
		{
			name: "valid wake open url request",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionWakeOpenURL,
				Payload: map[string]any{
					"action": "review",
				},
			},
			wantNil: true,
		},
		{
			name: "valid run with input_text",
			frame: MessageFrame{
				Type:      FrameTypeRequest,
				Action:    FrameActionRun,
				InputText: "hello",
			},
			wantNil: true,
		},
		{
			name: "invalid frame type",
			frame: MessageFrame{
				Type: FrameType("unknown"),
			},
			wantCode: ErrorCodeInvalidFrame.String(),
		},
		{
			name: "request missing action",
			frame: MessageFrame{
				Type: FrameTypeRequest,
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "action",
		},
		{
			name: "request invalid action",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameAction("foo"),
			},
			wantCode: ErrorCodeInvalidAction.String(),
		},
		{
			name: "run missing both input_text and input_parts",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionRun,
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "input_text_or_input_parts",
		},
		{
			name: "compact missing session_id",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionCompact,
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "session_id",
		},
		{
			name: "load_session missing session_id",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionLoadSession,
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "session_id",
		},
		{
			name: "resolve_permission valid struct payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionResolvePermission,
				Payload: PermissionResolutionInput{
					RequestID: "perm-1",
					Decision:  PermissionResolutionAllowOnce,
				},
			},
			wantNil: true,
		},
		{
			name: "resolve_permission missing payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionResolvePermission,
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "payload",
		},
		{
			name: "wake open url missing payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionWakeOpenURL,
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "payload",
		},
		{
			name: "resolve_permission missing request_id",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionResolvePermission,
				Payload: map[string]any{
					"decision": "allow_session",
				},
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "payload.request_id",
		},
		{
			name: "resolve_permission invalid decision",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionResolvePermission,
				Payload: map[string]any{
					"request_id": "perm-1",
					"decision":   "allow_forever",
				},
			},
			wantCode: ErrorCodeInvalidAction.String(),
		},
		{
			name: "event frame allows empty action",
			frame: MessageFrame{
				Type: FrameTypeEvent,
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFrame(tt.frame)
			if tt.wantNil {
				if err != nil {
					t.Fatalf("expected nil error, got: %#v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			if err.Code != tt.wantCode {
				t.Fatalf("error code mismatch: got %q want %q", err.Code, tt.wantCode)
			}
			if tt.wantField != "" && !strings.Contains(err.Message, tt.wantField) {
				t.Fatalf("expected message to contain %q, got %q", tt.wantField, err.Message)
			}
		})
	}
}

func TestValidateFrameNewActions(t *testing.T) {
	tests := []struct {
		name      string
		frame     MessageFrame
		wantNil   bool
		wantCode  string
		wantField string
	}{
		{
			name: "rename_session valid with payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionRenameSession,
				Payload: map[string]any{
					"session_id": "s-1",
					"title":      "New Title",
				},
			},
			wantNil: true,
		},
		{
			name: "rename_session missing payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionRenameSession,
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "payload",
		},
		{
			name: "set_session_model valid with payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionSetSessionModel,
				Payload: map[string]any{
					"session_id": "s-1",
					"model_id":   "m-1",
				},
			},
			wantNil: true,
		},
		{
			name: "set_session_model missing payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionSetSessionModel,
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "payload",
		},
		{
			name: "create_custom_provider valid with payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionCreateCustomProvider,
				Payload: map[string]any{
					"name":       "p",
					"driver":     "d",
					"api_key_env": "e",
				},
			},
			wantNil: true,
		},
		{
			name: "create_custom_provider missing payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionCreateCustomProvider,
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "payload",
		},
		{
			name: "delete_custom_provider valid with payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionDeleteCustomProvider,
				Payload: map[string]any{
					"provider_id": "p-1",
				},
			},
			wantNil: true,
		},
		{
			name: "delete_custom_provider missing payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionDeleteCustomProvider,
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "payload",
		},
		{
			name: "select_provider_model valid with payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionSelectProviderModel,
				Payload: map[string]any{
					"provider_id": "p-1",
				},
			},
			wantNil: true,
		},
		{
			name: "select_provider_model missing payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionSelectProviderModel,
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "payload",
		},
		{
			name: "upsert_mcp_server valid with payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionUpsertMCPServer,
				Payload: map[string]any{
					"server": map[string]any{"id": "mcp-1"},
				},
			},
			wantNil: true,
		},
		{
			name: "upsert_mcp_server missing payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionUpsertMCPServer,
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "payload",
		},
		{
			name: "set_mcp_server_enabled valid with payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionSetMCPServerEnabled,
				Payload: map[string]any{
					"id":      "mcp-1",
					"enabled": true,
				},
			},
			wantNil: true,
		},
		{
			name: "set_mcp_server_enabled missing payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionSetMCPServerEnabled,
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "payload",
		},
		{
			name: "delete_mcp_server valid with payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionDeleteMCPServer,
				Payload: map[string]any{
					"id": "mcp-1",
				},
			},
			wantNil: true,
		},
		{
			name: "delete_mcp_server missing payload",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionDeleteMCPServer,
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "payload",
		},
		{
			name: "list_providers no payload required",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionListProviders,
			},
			wantNil: true,
		},
		{
			name: "list_mcp_servers no payload required",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionListMCPServers,
			},
			wantNil: true,
		},
		{
			name: "list_models no payload required",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionListModels,
			},
			wantNil: true,
		},
		{
			name: "get_session_model missing session_id",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionGetSessionModel,
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "session_id",
		},
		{
			name: "delete_session missing session_id",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionDeleteSession,
			},
			wantCode:  ErrorCodeMissingRequiredField.String(),
			wantField: "session_id",
		},
		{
			name: "list_files no session_id required",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionListFiles,
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFrame(tt.frame)
			if tt.wantNil {
				if err != nil {
					t.Fatalf("expected nil error, got: %#v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			if err.Code != tt.wantCode {
				t.Fatalf("error code mismatch: got %q want %q", err.Code, tt.wantCode)
			}
			if tt.wantField != "" && !strings.Contains(err.Message, tt.wantField) {
				t.Fatalf("expected message to contain %q, got %q", tt.wantField, err.Message)
			}
		})
	}
}

func TestValidateFrameInputParts(t *testing.T) {
	tests := []struct {
		name     string
		frame    MessageFrame
		wantNil  bool
		wantCode string
	}{
		{
			name: "run with valid input_parts",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionRun,
				InputParts: []InputPart{
					{Type: InputPartTypeText, Text: "hello"},
				},
			},
			wantNil: true,
		},
		{
			name: "compact with input_parts invalid",
			frame: MessageFrame{
				Type:       FrameTypeRequest,
				Action:     FrameActionCompact,
				SessionID:  "session-1",
				InputParts: []InputPart{{Type: InputPartTypeText, Text: "hello"}},
			},
			wantNil: true,
		},
		{
			name: "list_files with input_parts invalid type",
			frame: MessageFrame{
				Type:       FrameTypeRequest,
				Action:     FrameActionListFiles,
				InputParts: []InputPart{{Type: InputPartTypeText, Text: "hello"}},
			},
			wantNil: true,
		},
		{
			name: "rename_session with input_parts invalid",
			frame: MessageFrame{
				Type:       FrameTypeRequest,
				Action:     FrameActionRenameSession,
				Payload:    map[string]any{"session_id": "s-1", "title": "T"},
				InputParts: []InputPart{{Type: InputPartTypeText, Text: "hello"}},
			},
			wantNil: true,
		},
		{
			name: "execute_system_tool with input_parts invalid",
			frame: MessageFrame{
				Type:       FrameTypeRequest,
				Action:     FrameActionExecuteSystemTool,
				Payload:    map[string]any{"tool_name": "memo_list"},
				InputParts: []InputPart{{Type: InputPartTypeText, Text: "hello"}},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFrame(tt.frame)
			if tt.wantNil {
				if err != nil {
					t.Fatalf("expected nil error, got: %#v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			if err.Code != tt.wantCode {
				t.Fatalf("error code mismatch: got %q want %q", err.Code, tt.wantCode)
			}
		})
	}
}

func TestDecodePayloadErrors(t *testing.T) {
	t.Run("marshal error", func(t *testing.T) {
		err := decodePayload(struct{ Bad chan int `json:"bad"` }{Bad: make(chan int)}, &protocol.RenameSessionParams{})
		if err == nil {
			t.Fatal("expected decodePayload error")
		}
	})

	t.Run("unmarshal error", func(t *testing.T) {
		err := decodePayload(`{"session_id": 123}`, &protocol.RenameSessionParams{})
		if err == nil {
			t.Fatal("expected decodePayload error")
		}
	})
}

func TestConvertProtocolModelDescriptors(t *testing.T) {
	t.Run("empty list returns nil", func(t *testing.T) {
		result := convertProtocolModelDescriptors(nil)
		if result != nil {
			t.Fatalf("expected nil, got %#v", result)
		}
		result = convertProtocolModelDescriptors([]protocol.ProviderModelDescriptor{})
		if result != nil {
			t.Fatalf("expected nil for empty slice, got %#v", result)
		}
	})

	t.Run("full conversion", func(t *testing.T) {
		models := []protocol.ProviderModelDescriptor{
			{
				ID:              "gpt-4",
				Name:            " GPT-4 ",
				Description:     " desc ",
				ContextWindow:   8192,
				MaxOutputTokens: 4096,
				CapabilityHints: protocol.ProviderModelCapabilityHints{
					ToolCalling: "supported",
					ImageInput:  "unsupported",
				},
			},
		}
		result := convertProtocolModelDescriptors(models)
		if len(result) != 1 {
			t.Fatalf("expected 1 result, got %d", len(result))
		}
		if result[0].ID != "gpt-4" {
			t.Fatalf("id = %q, want %q", result[0].ID, "gpt-4")
		}
		if result[0].Name != "GPT-4" {
			t.Fatalf("name = %q, want %q", result[0].Name, "GPT-4")
		}
		if result[0].Description != "desc" {
			t.Fatalf("description = %q, want %q", result[0].Description, "desc")
		}
		if result[0].ContextWindow != 8192 {
			t.Fatalf("context_window = %d, want %d", result[0].ContextWindow, 8192)
		}
		if result[0].MaxOutputTokens != 4096 {
			t.Fatalf("max_output_tokens = %d, want %d", result[0].MaxOutputTokens, 4096)
		}
		if result[0].CapabilityHints.ToolCalling != "supported" {
			t.Fatalf("tool_calling = %q, want %q", result[0].CapabilityHints.ToolCalling, "supported")
		}
		if result[0].CapabilityHints.ImageInput != "unsupported" {
			t.Fatalf("image_input = %q, want %q", result[0].CapabilityHints.ImageInput, "unsupported")
		}
	})
}

func TestConvertProtocolMCPServer(t *testing.T) {
	t.Run("empty env", func(t *testing.T) {
		server := protocol.MCPServerParams{
			ID:      " mcp-1 ",
			Enabled: true,
			Source:  "stdio",
			Version: " 1.0 ",
			Stdio: protocol.MCPStdioParams{
				Command:           " node ",
				Args:              []string{"index.js"},
				Workdir:           " /work ",
				StartTimeoutSec:   10,
				CallTimeoutSec:    30,
				RestartBackoffSec: 5,
			},
			Env: nil,
		}
		result := convertProtocolMCPServer(server)
		if result.ID != "mcp-1" {
			t.Fatalf("id = %q, want %q", result.ID, "mcp-1")
		}
		if result.Source != "stdio" {
			t.Fatalf("source = %q, want %q", result.Source, "stdio")
		}
		if result.Version != "1.0" {
			t.Fatalf("version = %q, want %q", result.Version, "1.0")
		}
		if result.Stdio.Command != "node" {
			t.Fatalf("command = %q, want %q", result.Stdio.Command, "node")
		}
		if len(result.Stdio.Args) != 1 || result.Stdio.Args[0] != "index.js" {
			t.Fatalf("args = %v, want [index.js]", result.Stdio.Args)
		}
		if result.Stdio.Workdir != "/work" {
			t.Fatalf("workdir = %q, want %q", result.Stdio.Workdir, "/work")
		}
		if result.Stdio.StartTimeoutSec != 10 {
			t.Fatalf("start_timeout_sec = %d, want %d", result.Stdio.StartTimeoutSec, 10)
		}
		if result.Stdio.CallTimeoutSec != 30 {
			t.Fatalf("call_timeout_sec = %d, want %d", result.Stdio.CallTimeoutSec, 30)
		}
		if result.Stdio.RestartBackoffSec != 5 {
			t.Fatalf("restart_backoff_sec = %d, want %d", result.Stdio.RestartBackoffSec, 5)
		}
		if len(result.Env) != 0 {
			t.Fatalf("expected empty env, got %v", result.Env)
		}
	})

	t.Run("full fields with env", func(t *testing.T) {
		server := protocol.MCPServerParams{
			ID:      "mcp-2",
			Enabled: false,
			Source:  "stdio",
			Stdio: protocol.MCPStdioParams{
				Command: "python",
				Args:    []string{"server.py"},
			},
			Env: []protocol.MCPEnvVarParams{
				{Name: " KEY ", Value: " VAL ", ValueEnv: " ENV "},
			},
		}
		result := convertProtocolMCPServer(server)
		if len(result.Env) != 1 {
			t.Fatalf("expected 1 env var, got %d", len(result.Env))
		}
		if result.Env[0].Name != "KEY" {
			t.Fatalf("env name = %q, want %q", result.Env[0].Name, "KEY")
		}
		if result.Env[0].Value != "VAL" {
			t.Fatalf("env value = %q, want %q", result.Env[0].Value, "VAL")
		}
		if result.Env[0].ValueEnv != "ENV" {
			t.Fatalf("env value_env = %q, want %q", result.Env[0].ValueEnv, "ENV")
		}
	})
}

func TestIsValidFrameActionNewActions(t *testing.T) {
	newActions := []FrameAction{
		FrameActionRenameSession,
		FrameActionListFiles,
		FrameActionListModels,
		FrameActionSetSessionModel,
		FrameActionGetSessionModel,
		FrameActionListProviders,
		FrameActionCreateCustomProvider,
		FrameActionDeleteCustomProvider,
		FrameActionSelectProviderModel,
		FrameActionListMCPServers,
		FrameActionUpsertMCPServer,
		FrameActionSetMCPServerEnabled,
		FrameActionDeleteMCPServer,
	}
	for _, action := range newActions {
		if !isValidFrameAction(action) {
			t.Fatalf("action %q should be valid", action)
		}
	}
}

func TestValidateFrame_MultimodalPayloadRules(t *testing.T) {
	tests := []struct {
		name     string
		frame    MessageFrame
		wantNil  bool
		wantCode string
	}{
		{
			name: "valid text part",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionRun,
				InputParts: []InputPart{
					{Type: InputPartTypeText, Text: "hello"},
				},
			},
			wantNil: true,
		},
		{
			name: "valid image part",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionRun,
				InputParts: []InputPart{
					{
						Type: InputPartTypeImage,
						Media: &Media{
							URI:      "file:///a.png",
							MimeType: "image/png",
						},
					},
				},
			},
			wantNil: true,
		},
		{
			name: "text part with empty text",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionRun,
				InputParts: []InputPart{
					{Type: InputPartTypeText, Text: "   "},
				},
			},
			wantCode: ErrorCodeInvalidMultimodalPayload.String(),
		},
		{
			name: "image part missing media",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionRun,
				InputParts: []InputPart{
					{Type: InputPartTypeImage},
				},
			},
			wantCode: ErrorCodeInvalidMultimodalPayload.String(),
		},
		{
			name: "image part missing media.uri",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionRun,
				InputParts: []InputPart{
					{
						Type:  InputPartTypeImage,
						Media: &Media{MimeType: "image/png"},
					},
				},
			},
			wantCode: ErrorCodeInvalidMultimodalPayload.String(),
		},
		{
			name: "image part missing media.mime_type",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionRun,
				InputParts: []InputPart{
					{
						Type:  InputPartTypeImage,
						Media: &Media{URI: "file:///a.png"},
					},
				},
			},
			wantCode: ErrorCodeInvalidMultimodalPayload.String(),
		},
		{
			name: "unsupported part type",
			frame: MessageFrame{
				Type:   FrameTypeRequest,
				Action: FrameActionRun,
				InputParts: []InputPart{
					{Type: InputPartType("audio")},
				},
			},
			wantCode: ErrorCodeInvalidMultimodalPayload.String(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFrame(tt.frame)
			if tt.wantNil {
				if err != nil {
					t.Fatalf("expected nil error, got: %#v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			if err.Code != tt.wantCode {
				t.Fatalf("error code mismatch: got %q want %q", err.Code, tt.wantCode)
			}
		})
	}
}
