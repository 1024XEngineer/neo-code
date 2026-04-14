package gateway

import (
	"strings"
	"testing"
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
