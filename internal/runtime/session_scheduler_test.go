package runtime

import (
	"os"
	"path/filepath"
	"testing"

	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

func TestSessionHasCompactedTranscript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		session agentsession.Session
		want    bool
	}{
		{
			name:    "empty messages",
			session: agentsession.New("empty"),
			want:    false,
		},
		{
			name: "first message compact summary",
			session: agentsession.Session{
				Messages: []providertypes.Message{
					{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("[compact_summary]\ndone:\n- ok")}},
				},
			},
			want: true,
		},
		{
			name: "first message is not assistant",
			session: agentsession.Session{
				Messages: []providertypes.Message{
					{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("[compact_summary]\ndone:\n- ok")}},
				},
			},
			want: false,
		},
		{
			name: "later message compact summary does not count",
			session: agentsession.Session{
				Messages: []providertypes.Message{
					{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("normal reply")}},
					{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("[compact_summary]\ndone:\n- archived")}},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := sessionHasCompactedTranscript(tt.session); got != tt.want {
				t.Fatalf("sessionHasCompactedTranscript() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveWorkdirForSessionAndVerificationProfileBranches(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	child := filepath.Join(base, "child")
	if err := ensureDir(child); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}

	resolved, err := resolveWorkdirForSession(base, "", "child")
	if err != nil {
		t.Fatalf("resolveWorkdirForSession(relative) error = %v", err)
	}
	if resolved != child {
		t.Fatalf("resolved workdir = %q, want %q", resolved, child)
	}

	session := agentsession.New("verification-profile")
	if !establishSessionVerificationProfile(&session) {
		t.Fatal("expected first profile establishment to report changed=true")
	}
	if establishSessionVerificationProfile(&session) {
		t.Fatal("expected second profile establishment to report changed=false")
	}
	if establishSessionVerificationProfile(nil) {
		t.Fatal("nil session should report changed=false")
	}
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}
