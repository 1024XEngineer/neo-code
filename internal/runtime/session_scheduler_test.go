package runtime

import (
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
