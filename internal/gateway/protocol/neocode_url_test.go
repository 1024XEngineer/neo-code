package protocol

import (
	"errors"
	"net/url"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseNeoCodeURLSuccess(t *testing.T) {
	workdir := testAbsoluteWorkdir()
	intent, err := ParseNeoCodeURL(
		"neocode://review?path=README.md&session_id=s-1&workdir=" + url.QueryEscape(workdir) + "&mode=fast",
	)
	if err != nil {
		t.Fatalf("parse neocode url: %v", err)
	}
	if intent.Action != WakeActionReview {
		t.Fatalf("action = %q, want %q", intent.Action, WakeActionReview)
	}
	if intent.SessionID != "s-1" {
		t.Fatalf("session_id = %q, want %q", intent.SessionID, "s-1")
	}
	if intent.Workdir != filepath.Clean(workdir) {
		t.Fatalf("workdir = %q, want %q", intent.Workdir, filepath.Clean(workdir))
	}
	if got := intent.Params["path"]; got != "README.md" {
		t.Fatalf("params[path] = %q, want %q", got, "README.md")
	}
	if got := intent.Params["mode"]; got != "fast" {
		t.Fatalf("params[mode] = %q, want %q", got, "fast")
	}
	if intent.RawURL == "" {
		t.Fatal("raw_url should not be empty")
	}
}

func TestParseNeoCodeURLWithActionInPath(t *testing.T) {
	intent, err := ParseNeoCodeURL("neocode:///review?path=README.md")
	if err != nil {
		t.Fatalf("parse neocode url: %v", err)
	}
	if intent.Action != WakeActionReview {
		t.Fatalf("action = %q, want %q", intent.Action, WakeActionReview)
	}
}

func TestParseNeoCodeURLSanitizesWorkdir(t *testing.T) {
	workdir := testAbsoluteWorkdir() + string(filepath.Separator) + "."
	intent, err := ParseNeoCodeURL("neocode://review?path=README.md&workdir=" + url.QueryEscape(workdir))
	if err != nil {
		t.Fatalf("parse neocode url: %v", err)
	}
	if intent.Workdir != filepath.Clean(workdir) {
		t.Fatalf("workdir = %q, want %q", intent.Workdir, filepath.Clean(workdir))
	}
}

func TestParseNeoCodeURLAllowsDotDotInPathSegmentName(t *testing.T) {
	workdir := testAbsoluteWorkdirWithDotDotSegment()
	intent, err := ParseNeoCodeURL("neocode://review?path=README.md&workdir=" + url.QueryEscape(workdir))
	if err != nil {
		t.Fatalf("parse neocode url: %v", err)
	}
	if intent.Workdir != filepath.Clean(workdir) {
		t.Fatalf("workdir = %q, want %q", intent.Workdir, filepath.Clean(workdir))
	}
}

func TestParseNeoCodeURLInvalidCases(t *testing.T) {
	tests := []struct {
		name     string
		rawURL   string
		wantCode string
	}{
		{
			name:     "empty url",
			rawURL:   "   ",
			wantCode: ParseErrorCodeMissingRequiredField,
		},
		{
			name:     "invalid format",
			rawURL:   "://bad",
			wantCode: ParseErrorCodeInvalidURL,
		},
		{
			name:     "invalid scheme",
			rawURL:   "http://review?path=README.md",
			wantCode: ParseErrorCodeInvalidScheme,
		},
		{
			name:     "missing action",
			rawURL:   "neocode://",
			wantCode: ParseErrorCodeMissingRequiredField,
		},
		{
			name:     "unsafe workdir path",
			rawURL:   "neocode://review?path=README.md&workdir=../../etc",
			wantCode: ParseErrorCodeUnsafePath,
		},
		{
			name:     "non absolute workdir path",
			rawURL:   "neocode://review?path=README.md&workdir=workspace/project",
			wantCode: ParseErrorCodeUnsafePath,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseNeoCodeURL(tt.rawURL)
			if err == nil {
				t.Fatal("expected parse error")
			}

			var parseErr *ParseError
			if !errors.As(err, &parseErr) {
				t.Fatalf("error type = %T, want *ParseError", err)
			}
			if parseErr.Code != tt.wantCode {
				t.Fatalf("parse error code = %q, want %q", parseErr.Code, tt.wantCode)
			}
		})
	}
}

func testAbsoluteWorkdir() string {
	if runtime.GOOS == "windows" {
		return `C:\workspace\neo-code`
	}
	return "/tmp/workspace/neo-code"
}

func testAbsoluteWorkdirWithDotDotSegment() string {
	if runtime.GOOS == "windows" {
		return `C:\data\v1..2\repo`
	}
	return "/tmp/a..b/project"
}

func TestIsSupportedWakeAction(t *testing.T) {
	if !IsSupportedWakeAction("review") {
		t.Fatal("review should be supported")
	}
	if !IsSupportedWakeAction("run") {
		t.Fatal("run should be supported")
	}
	if IsSupportedWakeAction("open") {
		t.Fatal("open should not be supported")
	}
}

func TestParseNeoCodeURLRunAction(t *testing.T) {
	intent, err := ParseNeoCodeURL("neocode://run?prompt=write%20hello")
	if err != nil {
		t.Fatalf("parse neocode url: %v", err)
	}
	if intent.Action != WakeActionRun {
		t.Fatalf("action = %q, want %q", intent.Action, WakeActionRun)
	}
	if got := intent.Params["prompt"]; got != "write hello" {
		t.Fatalf("params[prompt] = %q, want %q", got, "write hello")
	}
}

func TestParseErrorError(t *testing.T) {
	if (*ParseError)(nil).Error() != "" {
		t.Fatal("nil parse error string should be empty")
	}
	if (&ParseError{Message: "bad"}).Error() != "bad" {
		t.Fatal("parse error string should be message text")
	}
}

func TestResolveActionAndQueryHelpers(t *testing.T) {
	if resolveAction(nil) != "" {
		t.Fatal("resolveAction(nil) should return empty string")
	}

	actionFromPath := resolveAction(&url.URL{Path: "/review/sub"})
	if actionFromPath != "review" {
		t.Fatalf("action from path = %q, want %q", actionFromPath, "review")
	}

	params := flattenQueryValues(url.Values{
		"path":  {"README.md", "docs/README.md"},
		"":      {"ignored"},
		"empty": {},
	})
	if params["path"] != "docs/README.md" {
		t.Fatalf("params[path] = %q, want %q", params["path"], "docs/README.md")
	}
	if _, exists := params[""]; exists {
		t.Fatal("empty key should be ignored")
	}
	if params["empty"] != "" {
		t.Fatalf("params[empty] = %q, want empty string", params["empty"])
	}

	if popQueryParam(nil, "session_id") != "" {
		t.Fatal("popQueryParam(nil) should return empty string")
	}
	if popQueryParam(params, "missing") != "" {
		t.Fatal("popQueryParam missing key should return empty string")
	}
	if popQueryParam(params, "path") != "docs/README.md" {
		t.Fatal("popQueryParam should return existing value")
	}
}
