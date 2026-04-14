package handlers

import (
	"testing"

	"neo-code/internal/gateway/protocol"
)

func TestWakeOpenURLHandlerHandleSuccess(t *testing.T) {
	handler := NewWakeOpenURLHandler()
	result, err := handler.Handle(protocol.WakeIntent{
		Action: protocol.WakeActionReview,
		Params: map[string]string{
			"path": "README.md",
		},
	})
	if err != nil {
		t.Fatalf("handle wake intent: %v", err)
	}
	if result.Action != protocol.WakeActionReview {
		t.Fatalf("result action = %q, want %q", result.Action, protocol.WakeActionReview)
	}
	if result.Params["path"] != "README.md" {
		t.Fatalf("result params[path] = %q, want %q", result.Params["path"], "README.md")
	}
}

func TestWakeOpenURLHandlerHandleInvalidAction(t *testing.T) {
	handler := NewWakeOpenURLHandler()
	_, err := handler.Handle(protocol.WakeIntent{
		Action: "open",
		Params: map[string]string{
			"path": "README.md",
		},
	})
	if err == nil {
		t.Fatal("expected invalid action error")
	}
	if err.Code != WakeErrorCodeInvalidAction {
		t.Fatalf("error code = %q, want %q", err.Code, WakeErrorCodeInvalidAction)
	}
}

func TestWakeOpenURLHandlerHandleMissingPath(t *testing.T) {
	handler := NewWakeOpenURLHandler()
	_, err := handler.Handle(protocol.WakeIntent{
		Action: protocol.WakeActionReview,
	})
	if err == nil {
		t.Fatal("expected missing path error")
	}
	if err.Code != WakeErrorCodeMissingRequiredField {
		t.Fatalf("error code = %q, want %q", err.Code, WakeErrorCodeMissingRequiredField)
	}
}

func TestWakeOpenURLHandlerHandleUnsafePath(t *testing.T) {
	testCases := []string{
		"../../etc/passwd",
		"/etc/passwd",
		"..\\Windows\\system32",
	}

	handler := NewWakeOpenURLHandler()
	for _, path := range testCases {
		_, err := handler.Handle(protocol.WakeIntent{
			Action: protocol.WakeActionReview,
			Params: map[string]string{
				"path": path,
			},
		})
		if err == nil {
			t.Fatalf("path %q: expected unsafe path error", path)
		}
		if err.Code != WakeErrorCodeUnsafePath {
			t.Fatalf("path %q: error code = %q, want %q", path, err.Code, WakeErrorCodeUnsafePath)
		}
	}
}

func TestIsSafeReviewPath(t *testing.T) {
	testCases := []struct {
		name string
		path string
		want bool
	}{
		{name: "relative file", path: "README.md", want: true},
		{name: "relative nested path", path: "docs/spec/design.md", want: true},
		{name: "contains double dot in segment", path: "docs/v1..2/spec.md", want: true},
		{name: "parent traversal", path: "../secret.txt", want: false},
		{name: "parent traversal nested", path: "a/../../secret.txt", want: false},
		{name: "absolute unix path", path: "/tmp/file", want: false},
		{name: "empty", path: "", want: false},
		{name: "dot current dir", path: ".", want: false},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := isSafeReviewPath(tc.path); got != tc.want {
				t.Fatalf("isSafeReviewPath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestCloneParams(t *testing.T) {
	original := map[string]string{"path": "README.md"}
	cloned := cloneParams(original)
	cloned["path"] = "docs/README.md"
	if original["path"] != "README.md" {
		t.Fatalf("original map should remain unchanged, got %q", original["path"])
	}
	if cloneParams(nil) != nil {
		t.Fatal("cloneParams(nil) should return nil")
	}
}

func TestWakeErrorError(t *testing.T) {
	if (*WakeError)(nil).Error() != "" {
		t.Fatal("nil wake error string should be empty")
	}
	if (&WakeError{Message: "boom"}).Error() != "boom" {
		t.Fatal("wake error string should be message text")
	}
}
