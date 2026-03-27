package webfetch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dust/neo-code/internal/tools"
)

func TestToolExecute(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("hello webfetch"))
		case "/fail":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("upstream failed"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tool := New(2 * time.Second)
	if tool.Name() != "webfetch" {
		t.Fatalf("unexpected tool name %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Fatalf("expected non-empty description")
	}
	if tool.Schema()["type"] != "object" {
		t.Fatalf("expected schema object")
	}

	tests := []struct {
		name          string
		args          any
		expectErr     string
		expectContent string
		expectStatus  string
		expectIsError bool
	}{
		{
			name:          "fetch success",
			args:          map[string]string{"url": server.URL + "/ok"},
			expectContent: "hello webfetch",
			expectStatus:  "200 OK",
		},
		{
			name:          "fetch http error",
			args:          map[string]string{"url": server.URL + "/fail"},
			expectErr:     "502 Bad Gateway",
			expectContent: "upstream failed",
			expectStatus:  "502 Bad Gateway",
			expectIsError: true,
		},
		{
			name:      "rejects invalid scheme",
			args:      map[string]string{"url": "ftp://example.com"},
			expectErr: "url must start with http:// or https://",
		},
		{
			name:      "rejects invalid json",
			args:      "{",
			expectErr: "JSON input",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			var raw []byte
			switch value := tt.args.(type) {
			case string:
				raw = []byte(value)
			default:
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatalf("marshal args: %v", err)
				}
				raw = data
			}

			result, execErr := tool.Execute(context.Background(), tools.ToolCallInput{
				Name:      tool.Name(),
				Arguments: raw,
			})

			if tt.expectErr != "" {
				if execErr == nil || !strings.Contains(execErr.Error(), tt.expectErr) {
					t.Fatalf("expected error containing %q, got %v", tt.expectErr, execErr)
				}
			} else if execErr != nil {
				t.Fatalf("unexpected error: %v", execErr)
			}

			if tt.expectContent != "" && !strings.Contains(result.Content, tt.expectContent) {
				t.Fatalf("expected content containing %q, got %q", tt.expectContent, result.Content)
			}
			if tt.expectStatus != "" && result.Metadata["status"] != tt.expectStatus {
				t.Fatalf("expected status %q, got %+v", tt.expectStatus, result.Metadata)
			}
			if result.IsError != tt.expectIsError {
				t.Fatalf("expected IsError=%v, got %v", tt.expectIsError, result.IsError)
			}
		})
	}
}
