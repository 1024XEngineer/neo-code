package decider

import "testing"

func TestInferTaskKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		goal string
		want TaskKind
	}{
		{
			name: "todo plan",
			goal: "请创建 todo 列表并规划后续任务",
			want: TaskKindTodoState,
		},
		{
			name: "workspace write",
			goal: "创建文件 test.txt 并写入 1",
			want: TaskKindWorkspaceWrite,
		},
		{
			name: "review read only",
			goal: "review this implementation and suggest fixes",
			want: TaskKindReadOnly,
		},
		{
			name: "subagent explicit",
			goal: "用 subagent 创建 test1.txt，内容为 1",
			want: TaskKindSubAgent,
		},
		{
			name: "chat answer greeting",
			goal: "你好",
			want: TaskKindChatAnswer,
		},
		{
			name: "bug discussion should not be write",
			goal: "帮我看看这个 bug 怎么修",
			want: TaskKindReadOnly,
		},
		{
			name: "readme update is write",
			goal: "把 README 补一下",
			want: TaskKindWorkspaceWrite,
		},
		{
			name: "todo creation",
			goal: "创建一个 Todo，内容是 1",
			want: TaskKindTodoState,
		},
		{
			name: "todo content contains write text still todo hint",
			goal: "创建一个 Todo，内容是创建 test.txt 内容为 1",
			want: TaskKindTodoState,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := InferTaskKind(tt.goal)
			if got != tt.want {
				t.Fatalf("InferTaskKind(%q) = %q, want %q", tt.goal, got, tt.want)
			}
		})
	}
}

func TestInferTaskIntent(t *testing.T) {
	t.Parallel()

	intent := InferTaskIntent("创建 2.txt 内容为 2")
	if intent.Hint != TaskKindWorkspaceWrite {
		t.Fatalf("hint = %q, want %q", intent.Hint, TaskKindWorkspaceWrite)
	}
	if intent.Confidence <= 0 {
		t.Fatalf("confidence = %v, want > 0", intent.Confidence)
	}
	if len(intent.Reasons) == 0 {
		t.Fatalf("reasons should not be empty")
	}
}
