package webfetch

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
	"neo-code/internal/config"
	"neo-code/internal/tools"
)

func executionContextAllowLoopback() context.Context {
	return context.WithValue(context.Background(), bypassTargetValidationKey, true)
}

func TestToolExecute(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`
				<html>
					<head>
						<title>Example Domain</title>
						<script>ignored()</script>
						<style>body { display:none; }</style>
					</head>
					<body>
						<main>
							<h1>Example Domain</h1>
							<p>This domain is for use in illustrative examples.</p>
							<noscript>skip me</noscript>
						</main>
					</body>
				</html>
			`))
		case "/html-empty":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><title>Blank</title></head><body><script>ignored()</script></body></html>`))
		case "/plain":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("hello webfetch"))
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/image":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("\x89PNG\x0d\x0a\x1a\x0a"))
		case "/octet":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("binary payload"))
		case "/large":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("0123456789abcdef"))
		case "/fail":
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("upstream failed"))
		case "/redirect":
			http.Redirect(w, r, "https://example.com/blocked", http.StatusFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	closedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := closedServer.URL
	closedServer.Close()

	defaultConfig := Config{
		Timeout:               2 * time.Second,
		MaxResponseBytes:      config.DefaultWebFetchMaxResponseBytes,
		SupportedContentTypes: config.DefaultWebFetchSupportedContentTypes(),
	}
	if New(defaultConfig).Name() != "webfetch" {
		t.Fatalf("unexpected tool name %q", New(defaultConfig).Name())
	}
	if New(defaultConfig).Description() == "" {
		t.Fatalf("expected non-empty description")
	}
	if New(defaultConfig).Schema()["type"] != "object" {
		t.Fatalf("expected schema object")
	}

	tests := []struct {
		name            string
		args            any
		toolConfig      Config
		expectErr       string
		expectContent   []string
		rejectContent   []string
		expectStatus    string
		expectType      string
		expectIsError   bool
		expectTruncated bool
	}{
		{
			name:       "extracts html body text",
			args:       map[string]string{"url": server.URL + "/html"},
			toolConfig: defaultConfig,
			expectContent: []string{
				"url: " + server.URL + "/html",
				"status: 200 OK",
				"content_type: text/html",
				"title: Example Domain",
				"Example Domain This domain is for use in illustrative examples.",
			},
			rejectContent: []string{"ignored()", "display:none", "skip me"},
			expectStatus:  "200 OK",
			expectType:    "text/html",
		},
		{
			name:       "rejects empty html content",
			args:       map[string]string{"url": server.URL + "/html-empty"},
			toolConfig: defaultConfig,
			expectErr:  reasonEmptyContent,
			expectContent: []string{
				"tool error",
				"tool: webfetch",
				"content_type: text/html",
				"reason: " + reasonEmptyContent,
			},
			expectStatus:  "200 OK",
			expectType:    "text/html",
			expectIsError: true,
		},
		{
			name:       "returns plain text response",
			args:       map[string]string{"url": server.URL + "/plain"},
			toolConfig: defaultConfig,
			expectContent: []string{
				"content_type: text/plain",
				"hello webfetch",
			},
			expectStatus: "200 OK",
			expectType:   "text/plain",
		},
		{
			name:       "returns json as text",
			args:       map[string]string{"url": server.URL + "/json"},
			toolConfig: defaultConfig,
			expectContent: []string{
				"content_type: application/json",
				`{"ok":true}`,
			},
			expectStatus: "200 OK",
			expectType:   "application/json",
		},
		{
			name:       "rejects unsupported image content type",
			args:       map[string]string{"url": server.URL + "/image"},
			toolConfig: defaultConfig,
			expectErr:  reasonUnsupportedType,
			expectContent: []string{
				"tool error",
				"tool: webfetch",
				"content_type: image/png",
				"reason: " + reasonUnsupportedType,
			},
			expectStatus:  "200 OK",
			expectType:    "image/png",
			expectIsError: true,
		},
		{
			name:       "rejects unsupported octet stream",
			args:       map[string]string{"url": server.URL + "/octet"},
			toolConfig: defaultConfig,
			expectErr:  reasonUnsupportedType,
			expectContent: []string{
				"tool error",
				"tool: webfetch",
				"reason: " + reasonUnsupportedType,
			},
			expectStatus:  "200 OK",
			expectType:    "application/octet-stream",
			expectIsError: true,
		},
		{
			name: "marks truncated text responses",
			args: map[string]string{"url": server.URL + "/large"},
			toolConfig: Config{
				Timeout:               2 * time.Second,
				MaxResponseBytes:      8,
				SupportedContentTypes: config.DefaultWebFetchSupportedContentTypes(),
			},
			expectContent: []string{
				"truncated: true",
				"01234567",
			},
			expectStatus:    "200 OK",
			expectType:      "text/plain",
			expectTruncated: true,
		},
		{
			name:       "formats http errors consistently",
			args:       map[string]string{"url": server.URL + "/fail"},
			toolConfig: defaultConfig,
			expectErr:  "unexpected HTTP status 502 Bad Gateway",
			expectContent: []string{
				"tool error",
				"tool: webfetch",
				"status: 502 Bad Gateway",
				"content_type: text/plain",
				"reason: unexpected HTTP status 502 Bad Gateway",
				"upstream failed",
			},
			expectStatus:  "502 Bad Gateway",
			expectType:    "text/plain",
			expectIsError: true,
		},
		{
			name:       "blocks redirect responses",
			args:       map[string]string{"url": server.URL + "/redirect"},
			toolConfig: defaultConfig,
			expectErr:  "redirect blocked",
			expectContent: []string{
				"tool error",
				"tool: webfetch",
				"reason: " + reasonRedirectNotAllowed,
				"https://example.com/blocked",
			},
			expectStatus:  "302 Found",
			expectIsError: true,
		},
		{
			name:       "rejects invalid scheme",
			args:       map[string]string{"url": "ftp://example.com"},
			toolConfig: defaultConfig,
			expectErr:  "url must start with http:// or https://",
			expectContent: []string{
				"tool error",
				"tool: webfetch",
				"reason: " + reasonInvalidURL,
			},
			expectIsError: true,
		},
		{
			name:       "rejects invalid json",
			args:       "{",
			toolConfig: defaultConfig,
			expectErr:  "JSON input",
			expectContent: []string{
				"tool error",
				"tool: webfetch",
				"reason: " + reasonInvalidArguments,
			},
			expectIsError: true,
		},
		{
			name:       "returns request failure when server is unavailable",
			args:       map[string]string{"url": closedURL},
			toolConfig: defaultConfig,
			expectErr:  "webfetch: fetch",
			expectContent: []string{
				"tool error",
				"tool: webfetch",
				"reason: " + reasonRequestFailed,
			},
			expectIsError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tool := New(tt.toolConfig)

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

			result, execErr := tool.Execute(executionContextAllowLoopback(), tools.ToolCallInput{
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

			for _, want := range tt.expectContent {
				if !strings.Contains(result.Content, want) {
					t.Fatalf("expected content containing %q, got %q", want, result.Content)
				}
			}
			for _, reject := range tt.rejectContent {
				if strings.Contains(result.Content, reject) {
					t.Fatalf("expected content not to contain %q, got %q", reject, result.Content)
				}
			}
			if tt.expectStatus != "" && result.Metadata["status"] != tt.expectStatus {
				t.Fatalf("expected status %q, got %+v", tt.expectStatus, result.Metadata)
			}
			if tt.expectType != "" && result.Metadata["content_type"] != tt.expectType {
				t.Fatalf("expected content_type %q, got %+v", tt.expectType, result.Metadata)
			}
			if truncated, ok := result.Metadata["truncated"].(bool); !ok || truncated != tt.expectTruncated {
				t.Fatalf("expected truncated=%v, got %+v", tt.expectTruncated, result.Metadata)
			}
			if result.IsError != tt.expectIsError {
				t.Fatalf("expected IsError=%v, got %v", tt.expectIsError, result.IsError)
			}
		})
	}
}

func TestToolDefaultsAndContentTypeFallback(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fallback text body"))
	}))
	defer server.Close()

	tool := New(Config{})
	args, err := json.Marshal(map[string]string{"url": server.URL})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	result, execErr := tool.Execute(executionContextAllowLoopback(), tools.ToolCallInput{
		Name:      tool.Name(),
		Arguments: args,
	})
	if execErr != nil {
		t.Fatalf("unexpected error: %v", execErr)
	}
	if result.Metadata["content_type"] != "text/plain" {
		t.Fatalf("expected detected text/plain content type, got %+v", result.Metadata)
	}
	if !strings.Contains(result.Content, "fallback text body") {
		t.Fatalf("expected fallback body content, got %q", result.Content)
	}
}

func TestHTMLHelpers(t *testing.T) {
	t.Parallel()

	text, title, err := extractHTMLContent([]byte(`<div>hello <span>world</span></div>`))
	if err != nil {
		t.Fatalf("extractHTMLContent() error = %v", err)
	}
	if text != "hello world" {
		t.Fatalf("expected extracted text %q, got %q", "hello world", text)
	}
	if title != "" {
		t.Fatalf("expected empty title, got %q", title)
	}

	if findElement(nil, "body") != nil {
		t.Fatalf("expected nil findElement result for nil root")
	}
	if shouldSkipNode(nil) {
		t.Fatalf("expected shouldSkipNode(nil) to be false")
	}
	if normalizeWhitespace(" hello \n  world ") != "hello world" {
		t.Fatalf("expected normalizeWhitespace to collapse whitespace")
	}

	titleNode := &html.Node{Type: html.ElementNode, Data: "title"}
	titleNode.AppendChild(&html.Node{Type: html.TextNode, Data: "Example"})
	if textContent(titleNode) != "Example " {
		t.Fatalf("expected title text content, got %q", textContent(titleNode))
	}
}

func TestToolExecuteBlocksLocalAndPrivateTargets(t *testing.T) {
	t.Parallel()

	tool := New(Config{
		Timeout:               2 * time.Second,
		MaxResponseBytes:      config.DefaultWebFetchMaxResponseBytes,
		SupportedContentTypes: config.DefaultWebFetchSupportedContentTypes(),
	})

	tests := []struct {
		name string
		url  string
	}{
		{name: "localhost", url: "http://localhost/internal"},
		{name: "localhost subdomain", url: "http://api.localhost/internal"},
		{name: "loopback ipv4", url: "http://127.0.0.1/internal"},
		{name: "link local metadata", url: "http://169.254.169.254/latest/meta-data"},
		{name: "private network", url: "http://10.1.2.3/internal"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			raw, err := json.Marshal(map[string]string{"url": tc.url})
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}

			result, execErr := tool.Execute(context.Background(), tools.ToolCallInput{
				Name:      tool.Name(),
				Arguments: raw,
			})
			if execErr == nil || !strings.Contains(execErr.Error(), "target host is blocked") {
				t.Fatalf("expected blocked target error, got %v", execErr)
			}
			if !result.IsError {
				t.Fatalf("expected error result for blocked target")
			}
			if !strings.Contains(result.Content, "reason: "+reasonInvalidURL) {
				t.Fatalf("expected invalid url reason, got %q", result.Content)
			}
		})
	}
}

func TestFetchBlocksLoopbackAtDial(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	tool := New(Config{
		Timeout:               2 * time.Second,
		MaxResponseBytes:      config.DefaultWebFetchMaxResponseBytes,
		SupportedContentTypes: config.DefaultWebFetchSupportedContentTypes(),
	})

	_, err := tool.fetch(context.Background(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "target host is blocked") {
		t.Fatalf("expected loopback dial to be blocked, got %v", err)
	}
}

func TestResolveDialAddressesPrefersAllowedSet(t *testing.T) {
	originalLookup := lookupIPAddr
	t.Cleanup(func() {
		lookupIPAddr = originalLookup
	})
	lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		return []net.IPAddr{
			{IP: net.ParseIP("203.0.113.1")},
			{IP: net.ParseIP("127.0.0.1")},
			{IP: net.ParseIP("198.51.100.2")},
		}, nil
	}

	addresses, err := resolveDialAddresses(context.Background(), "example.com:443")
	if err != nil {
		t.Fatalf("resolveDialAddresses() error = %v", err)
	}
	if len(addresses) != 2 {
		t.Fatalf("expected 2 allowed addresses, got %v", addresses)
	}
	if addresses[0] != "203.0.113.1:443" || addresses[1] != "198.51.100.2:443" {
		t.Fatalf("unexpected addresses order: %v", addresses)
	}
}

func TestResolveDialAddressesBlockedOrEmpty(t *testing.T) {
	t.Run("empty dns result", func(t *testing.T) {
		originalLookup := lookupIPAddr
		t.Cleanup(func() {
			lookupIPAddr = originalLookup
		})
		lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return nil, nil
		}

		_, err := resolveDialAddresses(context.Background(), "example.com:80")
		if err == nil || !strings.Contains(err.Error(), "empty result") {
			t.Fatalf("expected empty result error, got %v", err)
		}
	})

	t.Run("all blocked", func(t *testing.T) {
		originalLookup := lookupIPAddr
		t.Cleanup(func() {
			lookupIPAddr = originalLookup
		})
		lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		}

		_, err := resolveDialAddresses(context.Background(), "example.com:80")
		if err == nil || !strings.Contains(err.Error(), "target host is blocked") {
			t.Fatalf("expected blocked target error, got %v", err)
		}
	})
}

func TestDialFirstReachable(t *testing.T) {
	t.Parallel()

	t.Run("fallback to later address", func(t *testing.T) {
		var called []string
		connA, connB := net.Pipe()
		t.Cleanup(func() {
			_ = connA.Close()
			_ = connB.Close()
		})

		conn, err := dialFirstReachable(context.Background(), func(ctx context.Context, network, address string) (net.Conn, error) {
			called = append(called, address)
			if address != "198.51.100.2:443" {
				return nil, errors.New("dial failed")
			}
			return connA, nil
		}, "tcp", []string{"203.0.113.1:443", "192.0.2.1:443", "198.51.100.2:443"})
		if err != nil {
			t.Fatalf("dialFirstReachable() error = %v", err)
		}
		_ = conn.Close()
		if strings.Join(called, ",") != "203.0.113.1:443,192.0.2.1:443,198.51.100.2:443" {
			t.Fatalf("unexpected dial sequence: %v", called)
		}
	})

	t.Run("return last error", func(t *testing.T) {
		_, err := dialFirstReachable(context.Background(), func(ctx context.Context, network, address string) (net.Conn, error) {
			return nil, errors.New("last dial error")
		}, "tcp", []string{"198.51.100.2:443"})
		if err == nil || !strings.Contains(err.Error(), "last dial error") {
			t.Fatalf("expected last dial error, got %v", err)
		}
	})
}
