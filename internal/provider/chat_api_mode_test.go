package provider

import "testing"

func TestNormalizeProviderChatAPIMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "empty", input: "", want: ""},
		{name: "chat completions", input: " chat_completions ", want: ChatAPIModeChatCompletions},
		{name: "responses", input: "RESPONSES", want: ChatAPIModeResponses},
		{name: "invalid", input: "other", wantErr: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NormalizeProviderChatAPIMode(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NormalizeProviderChatAPIMode() error = %v, wantErr=%v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Fatalf("NormalizeProviderChatAPIMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultProviderChatAPIMode(t *testing.T) {
	t.Parallel()

	if got := DefaultProviderChatAPIMode(); got != ChatAPIModeChatCompletions {
		t.Fatalf("DefaultProviderChatAPIMode() = %q", got)
	}
}
