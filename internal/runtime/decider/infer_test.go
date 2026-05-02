package decider

import (
	"testing"

	runtimefacts "neo-code/internal/runtime/facts"
)

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
			goal: "review README.md 并总结风险",
			want: TaskKindReadOnly,
		},
		{
			name: "mixed write and review",
			goal: "edit main.go then review changes",
			want: TaskKindMixed,
		},
		{
			name: "subagent explicit",
			goal: "用 subagent 创建 test1.txt，内容为 1",
			want: TaskKindSubAgent,
		},
		{
			name: "chat answer fallback",
			goal: "什么是 NeoCode",
			want: TaskKindChatAnswer,
		},
		{
			name: "greeting chat answer",
			goal: "你好",
			want: TaskKindChatAnswer,
		},
		{
			name: "bug fix discussion should not be workspace write",
			goal: "帮我看看这个 bug 怎么修",
			want: TaskKindReadOnly,
		},
		{
			name: "review implementation should be read only",
			goal: "review this implementation and suggest fixes",
			want: TaskKindReadOnly,
		},
		{
			name: "update readme should be workspace write",
			goal: "把 README 补一下",
			want: TaskKindWorkspaceWrite,
		},
		{
			name: "todo creation should be todo state",
			goal: "创建一个 Todo，内容是 1",
			want: TaskKindTodoState,
		},
		{
			name: "todo content contains write target should still be todo state hint",
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

func TestDeriveEffectiveTaskKindFactsOverrideHint(t *testing.T) {
	t.Parallel()

	if got := DeriveEffectiveTaskKind(TaskKindChatAnswer, runtimefacts.RuntimeFacts{
		Files: runtimefacts.FileFacts{
			Written: []runtimefacts.FileWriteFact{{Path: "a.txt", WorkspaceWrite: true}},
		},
	}, TodoSnapshot{}); got != TaskKindWorkspaceWrite {
		t.Fatalf("effective kind = %q, want %q", got, TaskKindWorkspaceWrite)
	}

	if got := DeriveEffectiveTaskKind(TaskKindWorkspaceWrite, runtimefacts.RuntimeFacts{
		Commands: runtimefacts.CommandFacts{
			Executed: []runtimefacts.CommandFact{{Tool: "bash", Command: "ls", Succeeded: true}},
		},
	}, TodoSnapshot{}); got != TaskKindReadOnly {
		t.Fatalf("effective kind = %q, want %q", got, TaskKindReadOnly)
	}

	if got := DeriveEffectiveTaskKind(TaskKindWorkspaceWrite, runtimefacts.RuntimeFacts{
		Verification: runtimefacts.VerificationFacts{
			Passed: []runtimefacts.VerificationFact{{Tool: "filesystem_write_file", Scope: "artifact:2.txt"}},
		},
	}, TodoSnapshot{}); got != TaskKindWorkspaceWrite {
		t.Fatalf("effective kind = %q, want %q when artifact verification exists", got, TaskKindWorkspaceWrite)
	}
}
