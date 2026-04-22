package tools

import (
	"strings"
	"testing"
)

func TestAnalyzeBashCommandClassifiesGitCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		command     string
		wantIsGit   bool
		wantClass   string
		wantSubcmd  string
		wantPrefix  string
		wantUnknown bool
	}{
		{
			name:       "git status is read only",
			command:    "git status --short --branch",
			wantIsGit:  true,
			wantClass:  BashIntentClassificationReadOnly,
			wantSubcmd: "status",
			wantPrefix: "bash.git|read_only|status",
		},
		{
			name:       "git push is remote op",
			command:    "git push origin main",
			wantIsGit:  true,
			wantClass:  BashIntentClassificationRemoteOp,
			wantSubcmd: "push",
			wantPrefix: "bash.git|remote_op|push",
		},
		{
			name:       "git reset hard is destructive",
			command:    "git reset --hard HEAD~1",
			wantIsGit:  true,
			wantClass:  BashIntentClassificationDestructive,
			wantSubcmd: "reset",
			wantPrefix: "bash.git|destructive|reset",
		},
		{
			name:       "git revert is local mutation",
			command:    "git revert HEAD~1 --no-edit",
			wantIsGit:  true,
			wantClass:  BashIntentClassificationLocalMutation,
			wantSubcmd: "revert",
			wantPrefix: "bash.git|local_mutation|revert",
		},
		{
			name:       "git branch create is local mutation",
			command:    "git branch feature/new-branch",
			wantIsGit:  true,
			wantClass:  BashIntentClassificationLocalMutation,
			wantSubcmd: "branch",
			wantPrefix: "bash.git|local_mutation|branch",
		},
		{
			name:       "git branch list is read only",
			command:    "git branch --list",
			wantIsGit:  true,
			wantClass:  BashIntentClassificationReadOnly,
			wantSubcmd: "branch",
			wantPrefix: "bash.git|read_only|branch",
		},
		{
			name:       "git status with -c config injection is unknown",
			command:    "git -c core.fsmonitor='echo pwn' status",
			wantIsGit:  true,
			wantClass:  BashIntentClassificationUnknown,
			wantSubcmd: "status",
			wantPrefix: "bash.git|unknown|status",
		},
		{
			name:       "git status with config-env is unknown",
			command:    "git --config-env=core.fsmonitor=GIT_FSMONITOR status",
			wantIsGit:  true,
			wantClass:  BashIntentClassificationUnknown,
			wantSubcmd: "status",
			wantPrefix: "bash.git|unknown|status",
		},
		{
			name:        "non git command is unknown",
			command:     "Get-ChildItem -Force",
			wantIsGit:   false,
			wantClass:   BashIntentClassificationUnknown,
			wantPrefix:  "bash.command|sha256=",
			wantUnknown: true,
		},
		{
			name:        "composite command is unknown",
			command:     "git status && git log -1",
			wantIsGit:   false,
			wantClass:   BashIntentClassificationUnknown,
			wantPrefix:  "bash.command.composite",
			wantUnknown: true,
		},
		{
			name:        "single ampersand is composite",
			command:     "git status & git log -1",
			wantIsGit:   false,
			wantClass:   BashIntentClassificationUnknown,
			wantPrefix:  "bash.command.composite",
			wantUnknown: true,
		},
		{
			name:        "command substitution is composite",
			command:     "git status $(whoami)",
			wantIsGit:   false,
			wantClass:   BashIntentClassificationUnknown,
			wantPrefix:  "bash.command.composite",
			wantUnknown: true,
		},
		{
			name:        "backtick substitution is composite",
			command:     "git status `whoami`",
			wantIsGit:   false,
			wantClass:   BashIntentClassificationUnknown,
			wantPrefix:  "bash.command.composite",
			wantUnknown: true,
		},
		{
			name:        "parse error command is unknown",
			command:     "git status \"unterminated",
			wantIsGit:   false,
			wantClass:   BashIntentClassificationUnknown,
			wantPrefix:  "bash.command.parse_error",
			wantUnknown: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := AnalyzeBashCommand(tt.command)
			if got.IsGit != tt.wantIsGit {
				t.Fatalf("AnalyzeBashCommand() is_git = %v, want %v", got.IsGit, tt.wantIsGit)
			}
			if got.Classification != tt.wantClass {
				t.Fatalf("AnalyzeBashCommand() classification = %q, want %q", got.Classification, tt.wantClass)
			}
			if got.Subcommand != tt.wantSubcmd {
				t.Fatalf("AnalyzeBashCommand() subcommand = %q, want %q", got.Subcommand, tt.wantSubcmd)
			}
			if tt.wantPrefix != "" && !strings.HasPrefix(got.PermissionFingerprint, tt.wantPrefix) {
				t.Fatalf("AnalyzeBashCommand() fingerprint = %q, want prefix %q", got.PermissionFingerprint, tt.wantPrefix)
			}
			if tt.wantUnknown && got.NormalizedIntent == "" {
				t.Fatalf("expected normalized intent for unknown command")
			}
		})
	}
}

func TestAnalyzeBashCommandNormalizesGitFlagOrderAndQuotes(t *testing.T) {
	t.Parallel()

	first := AnalyzeBashCommand(`git log --max-count "5" --oneline`)
	second := AnalyzeBashCommand(`git log --oneline --max-count='5'`)

	if first.Classification != BashIntentClassificationReadOnly || second.Classification != BashIntentClassificationReadOnly {
		t.Fatalf("expected read_only classification, got %q and %q", first.Classification, second.Classification)
	}
	if first.PermissionFingerprint != second.PermissionFingerprint {
		t.Fatalf("expected same fingerprint, got %q vs %q", first.PermissionFingerprint, second.PermissionFingerprint)
	}
	if first.NormalizedIntent != second.NormalizedIntent {
		t.Fatalf("expected same normalized intent, got %q vs %q", first.NormalizedIntent, second.NormalizedIntent)
	}
}

func TestAnalyzeBashCommandFingerprintDoesNotLeakRawArguments(t *testing.T) {
	t.Parallel()

	intent := AnalyzeBashCommand(`git clone https://token-123456@example.com/private/repo.git`)
	if !intent.IsGit || intent.Classification != BashIntentClassificationRemoteOp {
		t.Fatalf("expected git remote classification, got %+v", intent)
	}
	if strings.Contains(intent.PermissionFingerprint, "token-123456") {
		t.Fatalf("expected fingerprint to avoid leaking raw arguments, got %q", intent.PermissionFingerprint)
	}
	if strings.Contains(intent.NormalizedIntent, "token-123456") {
		t.Fatalf("expected normalized intent to avoid leaking raw arguments, got %q", intent.NormalizedIntent)
	}
	if !strings.Contains(intent.NormalizedIntent, "args_count:1") {
		t.Fatalf("expected normalized intent to contain redacted args count, got %q", intent.NormalizedIntent)
	}
}
