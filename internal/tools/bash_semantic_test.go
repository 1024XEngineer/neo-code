package tools

import "testing"

func TestAnalyzeBashCommandClassifiesGitCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		command       string
		wantIsGit     bool
		wantClass     string
		wantSubCmd    string
		wantComposite bool
	}{
		{
			name:       "non git command is unknown",
			command:    "Get-ChildItem",
			wantIsGit:  false,
			wantClass:  BashIntentClassificationUnknown,
			wantSubCmd: "",
		},
		{
			name:       "git status is read only",
			command:    "git status --short",
			wantIsGit:  true,
			wantClass:  BashIntentClassificationReadOnly,
			wantSubCmd: "status",
		},
		{
			name:       "git log is read only",
			command:    "git log --oneline -5",
			wantIsGit:  true,
			wantClass:  BashIntentClassificationReadOnly,
			wantSubCmd: "log",
		},
		{
			name:       "git show is read only",
			command:    "git show HEAD~1",
			wantIsGit:  true,
			wantClass:  BashIntentClassificationReadOnly,
			wantSubCmd: "show",
		},
		{
			name:       "git diff is read only",
			command:    "git diff --name-only",
			wantIsGit:  true,
			wantClass:  BashIntentClassificationReadOnly,
			wantSubCmd: "diff",
		},
		{
			name:       "git cat-file is unknown and must require approval",
			command:    "git cat-file -p HEAD:.env",
			wantIsGit:  true,
			wantClass:  BashIntentClassificationUnknown,
			wantSubCmd: "cat-file",
		},
		{
			name:       "git push is remote",
			command:    "git push origin main",
			wantIsGit:  true,
			wantClass:  BashIntentClassificationRemoteOp,
			wantSubCmd: "push",
		},
		{
			name:       "git commit is local mutation",
			command:    "git commit -m test",
			wantIsGit:  true,
			wantClass:  BashIntentClassificationLocalMutation,
			wantSubCmd: "commit",
		},
		{
			name:       "git reset hard is destructive",
			command:    "git reset --hard HEAD~1",
			wantIsGit:  true,
			wantClass:  BashIntentClassificationDestructive,
			wantSubCmd: "reset",
		},
		{
			name:       "risky config flag becomes unknown",
			command:    "git -c core.pager=cat log",
			wantIsGit:  true,
			wantClass:  BashIntentClassificationUnknown,
			wantSubCmd: "log",
		},
		{
			name:          "composite command downgrades to unknown",
			command:       "git status && git log",
			wantIsGit:     false,
			wantClass:     BashIntentClassificationUnknown,
			wantSubCmd:    "",
			wantComposite: true,
		},
		{
			name:          "command substitution downgrades to unknown",
			command:       "git show $(touch /tmp/pwn)",
			wantIsGit:     false,
			wantClass:     BashIntentClassificationUnknown,
			wantSubCmd:    "",
			wantComposite: true,
		},
		{
			name:       "path masquerade git binary rejected",
			command:    "./git status",
			wantIsGit:  false,
			wantClass:  BashIntentClassificationUnknown,
			wantSubCmd: "",
		},
		{
			name:       "short risky config with attached value becomes unknown",
			command:    "git -ccore.pager=cat log",
			wantIsGit:  true,
			wantClass:  BashIntentClassificationUnknown,
			wantSubCmd: "log",
		},
		{
			name:       "config env equals form becomes unknown",
			command:    "git --config-env=core.pager=PAGER_ENV show HEAD",
			wantIsGit:  true,
			wantClass:  BashIntentClassificationUnknown,
			wantSubCmd: "show",
		},
		{
			name:       "uppercase C does not become risky config",
			command:    "git -C /tmp status",
			wantIsGit:  true,
			wantClass:  BashIntentClassificationReadOnly,
			wantSubCmd: "status",
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
			if got.Subcommand != tt.wantSubCmd {
				t.Fatalf("AnalyzeBashCommand() subcommand = %q, want %q", got.Subcommand, tt.wantSubCmd)
			}
			if got.Composite != tt.wantComposite {
				t.Fatalf("AnalyzeBashCommand() composite = %v, want %v", got.Composite, tt.wantComposite)
			}
			if got.PermissionFingerprint == "" {
				t.Fatalf("AnalyzeBashCommand() permission fingerprint should not be empty")
			}
		})
	}
}

func TestAnalyzeBashCommandFingerprintStable(t *testing.T) {
	t.Parallel()

	first := AnalyzeBashCommand("  git   status   --short ")
	second := AnalyzeBashCommand("git status --short")
	if first.PermissionFingerprint != second.PermissionFingerprint {
		t.Fatalf("permission fingerprint should be stable: %q vs %q", first.PermissionFingerprint, second.PermissionFingerprint)
	}
}
