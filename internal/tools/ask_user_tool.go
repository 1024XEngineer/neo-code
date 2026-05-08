package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AskUserRequest 描述一次 ask_user 请求。
type AskUserRequest struct {
	QuestionID string          `json:"question_id"`
	Title      string          `json:"title"`
	Description string         `json:"description"`
	Kind       string          `json:"kind"`
	Options    []AskUserOption `json:"options,omitempty"`
	Required   bool            `json:"required"`
	AllowSkip  bool            `json:"allow_skip"`
	MaxChoices int             `json:"max_choices,omitempty"`
	TimeoutSec int             `json:"timeout_sec,omitempty"`
}

// AskUserOption 描述 ask_user 选项。
type AskUserOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// AskUserResult 描述 ask_user 的解析后结果。
type AskUserResult struct {
	Status     string   `json:"status"`
	QuestionID string   `json:"question_id,omitempty"`
	Values     []string `json:"values,omitempty"`
	Message    string   `json:"message,omitempty"`
}

// AskUserBroker 定义 ask_user 工具用于挂起等待用户回答的最小接口。
type AskUserBroker interface {
	Open(ctx context.Context, request AskUserRequest) (string, AskUserResult, error)
}

type askUserTool struct {
	broker   AskUserBroker
	baseDesc string
}

// AskUserBrokerSetter allows late-binding an AskUserBroker after tool construction.
type AskUserBrokerSetter interface {
	SetAskUserBroker(broker AskUserBroker)
}

const askUserDescription = "Ask the user a question and wait for their response. Only available in plan mode."

// NewAskUserTool creates a new ask_user tool with the given broker.
// The broker may be nil and set later via AskUserBrokerSetter.
func NewAskUserTool(broker AskUserBroker) Tool {
	return &askUserTool{
		broker:   broker,
		baseDesc: askUserDescription,
	}
}

func (t *askUserTool) SetAskUserBroker(broker AskUserBroker) {
	t.broker = broker
}

func (t *askUserTool) Name() string {
	return ToolNameAskUser
}

func (t *askUserTool) Description() string {
	return t.baseDesc
}

func (t *askUserTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question_id": map[string]any{
				"type":        "string",
				"description": "Unique identifier for this question, used to correlate the answer.",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Short title for the question.",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Detailed description or prompt shown to the user.",
			},
			"kind": map[string]any{
				"type":        "string",
				"description": "Question kind: text (free-form), single_choice (pick one), or multi_choice (pick many).",
				"enum":        []string{"text", "single_choice", "multi_choice"},
			},
			"options": map[string]any{
				"type":        "array",
				"description": "Available options for choice-based questions.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"label":       map[string]any{"type": "string", "description": "Option label."},
						"description": map[string]any{"type": "string", "description": "Optional description of this option."},
					},
					"required": []string{"label"},
				},
			},
			"required": map[string]any{
				"type":        "boolean",
				"description": "Whether the user must answer before the session can proceed.",
			},
			"allow_skip": map[string]any{
				"type":        "boolean",
				"description": "Whether the user can skip this question.",
			},
			"max_choices": map[string]any{
				"type":        "integer",
				"description": "Maximum number of choices for multi_choice kind.",
			},
			"timeout_sec": map[string]any{
				"type":        "integer",
				"description": "Timeout in seconds to wait for the user's answer.",
			},
		},
		"required": []string{"question_id", "title", "kind"},
	}
}

func (t *askUserTool) MicroCompactPolicy() MicroCompactPolicy {
	return MicroCompactPolicyPreserveHistory
}

func (t *askUserTool) Execute(ctx context.Context, call ToolCallInput) (ToolResult, error) {
	if t.broker == nil {
		return NewErrorResult(ToolNameAskUser, "ask_user broker not available", "ask_user broker is nil", nil), fmt.Errorf("tools: ask_user broker is nil")
	}

	request, err := parseAskUserRequest(call.Arguments)
	if err != nil {
		return NewErrorResult(ToolNameAskUser, "invalid ask_user arguments", err.Error(), nil), err
	}

	// emit user_question_requested before blocking
	if call.AskUserEventEmitter != nil {
		call.AskUserEventEmitter("user_question_requested", request)
	}

	requestID, result, err := t.broker.Open(ctx, request)

	// emit resolved event
	if call.AskUserEventEmitter != nil {
		call.AskUserEventEmitter("user_question_"+result.Status, map[string]any{
			"request_id":  requestID,
			"question_id": request.QuestionID,
			"status":      result.Status,
			"values":      result.Values,
			"message":     result.Message,
		})
	}

	if err != nil {
		resultJSON, _ := json.Marshal(AskUserResult{
			Status:     result.Status,
			QuestionID: request.QuestionID,
			Message:    err.Error(),
		})
		return ToolResult{
			ToolCallID: call.ID,
			Name:       ToolNameAskUser,
			Content:    string(resultJSON),
			IsError:    false,
			Facts:      ToolExecutionFacts{WorkspaceWrite: false},
		}, err
	}

	resultJSON, _ := json.Marshal(result)
	return ToolResult{
		ToolCallID: call.ID,
		Name:       ToolNameAskUser,
		Content:    string(resultJSON),
		IsError:    false,
		Facts:      ToolExecutionFacts{WorkspaceWrite: false},
	}, nil
}

func parseAskUserRequest(raw []byte) (AskUserRequest, error) {
	var req AskUserRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return AskUserRequest{}, fmt.Errorf("tools: invalid ask_user arguments: %w", err)
	}
	req.QuestionID = strings.TrimSpace(req.QuestionID)
	if req.QuestionID == "" {
		return AskUserRequest{}, fmt.Errorf("tools: ask_user question_id is required")
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		return AskUserRequest{}, fmt.Errorf("tools: ask_user title is required")
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	switch kind {
	case "text", "single_choice", "multi_choice":
		req.Kind = kind
	default:
		return AskUserRequest{}, fmt.Errorf("tools: ask_user kind must be text, single_choice, or multi_choice")
	}
	if req.MaxChoices < 0 {
		req.MaxChoices = 0
	}
	if req.TimeoutSec <= 0 {
		req.TimeoutSec = 300
	}
	if req.TimeoutSec > 3600 {
		req.TimeoutSec = 3600
	}
	return req, nil
}

// defaultAskUserTimeout 是 ask_user 等待用户响应的默认超时。
const defaultAskUserTimeout = 5 * time.Minute
