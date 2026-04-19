package provider

import (
	"strings"
	"testing"
)

func TestResolveChatEndpointPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "empty means direct",
			input: "",
			want:  "",
		},
		{
			name:  "slash means direct",
			input: "/",
			want:  "",
		},
		{
			name:  "relative path is normalized",
			input: "chat/completions",
			want:  "/chat/completions",
		},
		{
			name:    "absolute url is rejected",
			input:   "https://api.example.com/v1/chat/completions",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ResolveChatEndpointPath(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveChatEndpointPath() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ResolveChatEndpointPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveChatEndpointURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		path    string
		want    string
		wantErr string
	}{
		{
			name:    "appends chat endpoint path",
			baseURL: "https://api.example.com/v1",
			path:    "/chat/completions",
			want:    "https://api.example.com/v1/chat/completions",
		},
		{
			name:    "direct mode with slash",
			baseURL: "https://api.example.com/v1/text/chatcompletion_v2",
			path:    "/",
			want:    "https://api.example.com/v1/text/chatcompletion_v2",
		},
		{
			name:    "direct mode with empty path",
			baseURL: "https://api.example.com/v1/text/chatcompletion_v2",
			path:    "",
			want:    "https://api.example.com/v1/text/chatcompletion_v2",
		},
		{
			name:    "appends non-slash-prefixed path and trims base slash",
			baseURL: " https://api.example.com/v1/ ",
			path:    "chat/completions",
			want:    "https://api.example.com/v1/chat/completions",
		},
		{
			name:    "rejects absolute chat endpoint path",
			baseURL: "https://api.example.com/v1",
			path:    "https://api.example.com/chat/completions",
			wantErr: "chat endpoint path",
		},
		{
			name:    "invalid base url in direct mode returns actionable error",
			baseURL: "api.example.com/v1",
			path:    "/",
			wantErr: "direct chat endpoint mode",
		},
		{
			name:    "invalid base url in append mode returns base url error",
			baseURL: "api.example.com/v1",
			path:    "/chat/completions",
			wantErr: "base_url is invalid",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ResolveChatEndpointURL(tt.baseURL, tt.path)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if got != "" {
					t.Fatalf("expected empty url on error, got %q", got)
				}
				if want := tt.wantErr; want != "" && !strings.Contains(err.Error(), want) {
					t.Fatalf("expected error containing %q, got %v", want, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveChatEndpointURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ResolveChatEndpointURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
