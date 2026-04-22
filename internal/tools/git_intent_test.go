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

func TestAnalyzeBashCommandGitWithoutSubcommandFallsBackToUnknownIntent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
	}{
		{
			name:    "git only",
			command: "git",
		},
		{
			name:    "git global flags only",
			command: "git -c core.editor=vim",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			intent := AnalyzeBashCommand(tt.command)
			if !intent.IsGit {
				t.Fatalf("expected git intent, got %+v", intent)
			}
			if intent.Classification != BashIntentClassificationUnknown {
				t.Fatalf("expected unknown classification, got %q", intent.Classification)
			}
			if !intent.ParseError {
				t.Fatalf("expected parse error marker for git without subcommand")
			}
			if intent.NormalizedIntent != "git" {
				t.Fatalf("expected normalized intent git, got %q", intent.NormalizedIntent)
			}
			if !strings.HasPrefix(intent.PermissionFingerprint, "bash.git.unknown|sha256=") {
				t.Fatalf("unexpected fingerprint %q", intent.PermissionFingerprint)
			}
		})
	}
}

func TestClassifyGitIntentSpecialCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		subcommand string
		flags      []string
		args       []string
		want       string
	}{
		{
			name:       "branch delete is destructive",
			subcommand: "branch",
			flags:      []string{"-D"},
			want:       BashIntentClassificationDestructive,
		},
		{
			name:       "tag list remains read only",
			subcommand: "tag",
			flags:      []string{"--list"},
			want:       BashIntentClassificationReadOnly,
		},
		{
			name:       "tag delete is destructive",
			subcommand: "tag",
			flags:      []string{"--delete"},
			want:       BashIntentClassificationDestructive,
		},
		{
			name:       "reset soft is local mutation",
			subcommand: "reset",
			flags:      []string{"--soft"},
			want:       BashIntentClassificationLocalMutation,
		},
		{
			name:       "branch move flag is local mutation",
			subcommand: "branch",
			flags:      []string{"--move"},
			want:       BashIntentClassificationLocalMutation,
		},
		{
			name:       "tag create with args is local mutation",
			subcommand: "tag",
			args:       []string{"v1.2.3"},
			want:       BashIntentClassificationLocalMutation,
		},
		{
			name:       "clean is destructive",
			subcommand: "clean",
			flags:      []string{"-fd"},
			want:       BashIntentClassificationDestructive,
		},
		{
			name:       "remote command is remote op",
			subcommand: "remote",
			args:       []string{"-v"},
			want:       BashIntentClassificationRemoteOp,
		},
		{
			name:       "checkout dot is destructive",
			subcommand: "checkout",
			args:       []string{"."},
			want:       BashIntentClassificationDestructive,
		},
		{
			name:       "local mutation fallback list",
			subcommand: "stash",
			want:       BashIntentClassificationLocalMutation,
		},
		{
			name:       "unknown subcommand stays unknown",
			subcommand: "foobar",
			want:       BashIntentClassificationUnknown,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := classifyGitIntent(tt.subcommand, tt.flags, tt.args)
			if got != tt.want {
				t.Fatalf("classifyGitIntent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeCommandTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tokens []string
		want   string
	}{
		{
			name:   "empty tokens return empty marker",
			tokens: nil,
			want:   "empty",
		},
		{
			name:   "blank tokens return empty marker",
			tokens: []string{" ", "\t"},
			want:   "empty",
		},
		{
			name:   "normalizes case and spacing",
			tokens: []string{"  Git  ", " STATUS "},
			want:   "git status",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := normalizeCommandTokens(tt.tokens); got != tt.want {
				t.Fatalf("normalizeCommandTokens() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseGitCommandParts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		tokens     []string
		wantOK     bool
		wantSubcmd string
		wantFlags  []string
		wantArgs   []string
	}{
		{
			name:   "too short tokens",
			tokens: []string{"git"},
			wantOK: false,
		},
		{
			name:   "global flag only without subcommand",
			tokens: []string{"git", "--"},
			wantOK: false,
		},
		{
			name:       "parses global flags and subcommand args",
			tokens:     []string{"git", "-c", "core.editor=vim", "--config-env=core.fsmonitor=GIT_FSMONITOR", "status", "--short", "README.md"},
			wantOK:     true,
			wantSubcmd: "status",
			wantFlags:  []string{"--config-env=core.fsmonitor=git_fsmonitor", "--short", "-c=core.editor=vim"},
			wantArgs:   []string{"readme.md"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotSubcmd, gotFlags, gotArgs, gotOK := parseGitCommandParts(tt.tokens)
			if gotOK != tt.wantOK {
				t.Fatalf("parseGitCommandParts() ok = %v, want %v", gotOK, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if gotSubcmd != tt.wantSubcmd {
				t.Fatalf("parseGitCommandParts() subcommand = %q, want %q", gotSubcmd, tt.wantSubcmd)
			}
			if strings.Join(gotFlags, ",") != strings.Join(tt.wantFlags, ",") {
				t.Fatalf("parseGitCommandParts() flags = %v, want %v", gotFlags, tt.wantFlags)
			}
			if strings.Join(gotArgs, ",") != strings.Join(tt.wantArgs, ",") {
				t.Fatalf("parseGitCommandParts() args = %v, want %v", gotArgs, tt.wantArgs)
			}
		})
	}
}

func TestContainsShellControlOperators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{
			name:    "plain git command is safe",
			command: "git status --short",
			want:    false,
		},
		{
			name:    "quoted ampersand is not treated as operator",
			command: `git status "&"`,
			want:    false,
		},
		{
			name:    "operator inside single quote is ignored",
			command: "git status ';'",
			want:    false,
		},
		{
			name:    "operator inside double quote is ignored",
			command: `git status "|"`,
			want:    false,
		},
		{
			name:    "single ampersand is operator",
			command: "git status & git log -1",
			want:    true,
		},
		{
			name:    "command substitution operator",
			command: "git status $(whoami)",
			want:    true,
		},
		{
			name:    "backtick substitution operator",
			command: "git status `whoami`",
			want:    true,
		},
		{
			name:    "semicolon operator",
			command: "git status; git log -1",
			want:    true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := containsShellControlOperators(tt.command); got != tt.want {
				t.Fatalf("containsShellControlOperators() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTokenizeShellCommand(t *testing.T) {
	t.Parallel()

	t.Run("tokenizes quoted arguments", func(t *testing.T) {
		t.Parallel()

		tokens, err := tokenizeShellCommand(`git log --pretty "%h %s"`)
		if err != nil {
			t.Fatalf("tokenizeShellCommand() error = %v", err)
		}
		got := strings.Join(tokens, "|")
		want := "git|log|--pretty|%h %s"
		if got != want {
			t.Fatalf("tokenizeShellCommand() = %q, want %q", got, want)
		}
	})

	t.Run("supports escaped characters inside double quote", func(t *testing.T) {
		t.Parallel()

		tokens, err := tokenizeShellCommand(`git log --grep "hello\\ world"`)
		if err != nil {
			t.Fatalf("tokenizeShellCommand() error = %v", err)
		}
		got := strings.Join(tokens, "|")
		want := "git|log|--grep|hello\\ world"
		if got != want {
			t.Fatalf("tokenizeShellCommand() = %q, want %q", got, want)
		}
	})

	t.Run("returns syntax error on unclosed quote", func(t *testing.T) {
		t.Parallel()

		if _, err := tokenizeShellCommand(`git log "unterminated`); err == nil {
			t.Fatalf("expected syntax error for unterminated quote")
		}
	})
}

func TestNormalizeGitArgs(t *testing.T) {
	t.Parallel()

	flags, args := normalizeGitArgs([]string{"--max-count", "5", "--pretty=oneline", "--", "README.md", "docs/Guide.md"})
	if strings.Join(flags, ",") != "--max-count=5,--pretty=oneline" {
		t.Fatalf("unexpected flags: %v", flags)
	}
	if strings.Join(args, ",") != "readme.md,docs/guide.md" {
		t.Fatalf("unexpected args: %v", args)
	}
}

func TestFlagHelpers(t *testing.T) {
	t.Parallel()

	t.Run("splitGitFlagToken handles key-value and key-only", func(t *testing.T) {
		t.Parallel()

		key, value := splitGitFlagToken("--pretty=oneline")
		if key != "--pretty" || value != "oneline" {
			t.Fatalf("unexpected split result key=%q value=%q", key, value)
		}
		key, value = splitGitFlagToken("--pretty")
		if key != "--pretty" || value != "" {
			t.Fatalf("unexpected split result key=%q value=%q", key, value)
		}
	})

	t.Run("shouldConsumeGitFlagValue recognizes configured flags", func(t *testing.T) {
		t.Parallel()

		if !shouldConsumeGitFlagValue("--max-count", "5") {
			t.Fatalf("expected --max-count to consume value")
		}
		if shouldConsumeGitFlagValue("--oneline", "1") {
			t.Fatalf("expected --oneline not to consume value")
		}
	})

	t.Run("hasGitArgument matches and mismatches", func(t *testing.T) {
		t.Parallel()

		if !hasGitArgument([]string{"HEAD", "."}, ".") {
			t.Fatalf("expected hasGitArgument to match dot")
		}
		if hasGitArgument([]string{"HEAD", "docs"}, "src") {
			t.Fatalf("expected hasGitArgument mismatch")
		}
	})
}

func TestBuildNormalizedGitIntent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		subcommand string
		flags      []string
		args       []string
		want       string
	}{
		{
			name:       "only subcommand",
			subcommand: "status",
			want:       "git status",
		},
		{
			name:       "includes sorted flags and arg count",
			subcommand: "log",
			flags:      []string{"--pretty=oneline", "--max-count=5"},
			args:       []string{"HEAD"},
			want:       "git log flags:--max-count,--pretty args_count:1",
		},
		{
			name:       "ignores blank flags",
			subcommand: "show",
			flags:      []string{" ", "--oneline"},
			want:       "git show flags:--oneline",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := buildNormalizedGitIntent(tt.subcommand, tt.flags, tt.args); got != tt.want {
				t.Fatalf("buildNormalizedGitIntent() = %q, want %q", got, tt.want)
			}
		})
	}
}
