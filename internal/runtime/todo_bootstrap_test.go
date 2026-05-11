package runtime

import (
	"context"
	"strings"
	"testing"

	agentcontext "neo-code/internal/context"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

func TestShouldInjectTodoBootstrapReminder(t *testing.T) {
	t.Parallel()

	required := true
	cases := []struct {
		name  string
		state runState
		want  bool
	}{
		{
			name: "direct build without plan or todos injects",
			state: runState{
				session:         agentsession.Session{AgentMode: agentsession.AgentModeBuild},
				planningEnabled: true,
			},
			want: true,
		},
		{
			name: "direct build does not inspect user text",
			state: runState{
				session:         agentsession.Session{AgentMode: agentsession.AgentModeBuild},
				userGoal:        "你好",
				planningEnabled: true,
			},
			want: true,
		},
		{
			name: "active plan skips",
			state: runState{
				session: agentsession.Session{
					AgentMode: agentsession.AgentModeBuild,
					CurrentPlan: &agentsession.PlanArtifact{
						Status: agentsession.PlanStatusApproved,
					},
				},
				userGoal:        "请分析项目并写文档",
				planningEnabled: true,
			},
			want: false,
		},
		{
			name: "existing todo skips",
			state: runState{
				session: agentsession.Session{
					AgentMode: agentsession.AgentModeBuild,
					Todos: []agentsession.TodoItem{{
						ID:       "todo-1",
						Content:  "existing",
						Status:   agentsession.TodoStatusPending,
						Required: &required,
					}},
				},
				userGoal:        "请分析项目并写文档",
				planningEnabled: true,
			},
			want: false,
		},
		{
			name: "plan mode skips",
			state: runState{
				session:         agentsession.Session{AgentMode: agentsession.AgentModePlan},
				userGoal:        "请分析项目并写文档",
				planningEnabled: true,
			},
			want: false,
		},
		{
			name: "legacy non planning run skips",
			state: runState{
				session:  agentsession.Session{AgentMode: agentsession.AgentModeBuild},
				userGoal: "edit file",
			},
			want: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := shouldInjectTodoBootstrapReminder(&tc.state)
			if got != tc.want {
				t.Fatalf("shouldInjectTodoBootstrapReminder() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestServiceRunDirectBuildInjectsTodoBootstrapReminder(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	builder := &stubContextBuilder{
		buildFn: func(ctx context.Context, input agentcontext.BuildInput) (agentcontext.BuildResult, error) {
			return agentcontext.BuildResult{
				SystemPrompt: "stub system prompt",
				Messages:     append([]providertypes.Message(nil), input.Messages...),
			}, nil
		},
	}
	scripted := &scriptedProvider{
		responses: []scriptedResponse{
			{
				Message: providertypes.Message{
					Role: providertypes.RoleAssistant,
					Parts: []providertypes.ContentPart{
						providertypes.NewTextPart(`{"task_completion":{"completed":true}}` + "\n完成。"),
					},
				},
				FinishReason: "stop",
			},
		},
	}

	service := NewWithFactory(manager, tools.NewRegistry(), store, &scriptedProviderFactory{provider: scripted}, builder)
	if err := service.Run(context.Background(), UserInput{
		RunID: "run-direct-build-todo-bootstrap",
		Mode:  string(agentsession.AgentModeBuild),
		Parts: []providertypes.ContentPart{providertypes.NewTextPart("请分析项目并写文档")},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	saved := onlySession(t, store)
	foundPersistedReminder := false
	for _, message := range saved.Messages {
		if message.Role == providertypes.RoleSystem &&
			strings.Contains(renderPartsForTest(message.Parts), todoBootstrapRequiredReason) {
			foundPersistedReminder = true
			break
		}
	}
	if !foundPersistedReminder {
		t.Fatalf("expected persisted todo bootstrap reminder, messages=%+v", saved.Messages)
	}
	if len(scripted.requests) == 0 {
		t.Fatalf("expected provider request")
	}
	foundRequestReminder := false
	for _, message := range scripted.requests[0].Messages {
		if message.Role == providertypes.RoleSystem &&
			strings.Contains(renderPartsForTest(message.Parts), todoBootstrapRequiredReason) {
			foundRequestReminder = true
			break
		}
	}
	if !foundRequestReminder {
		t.Fatalf("expected provider request to include todo bootstrap reminder, messages=%+v", scripted.requests[0].Messages)
	}
}
