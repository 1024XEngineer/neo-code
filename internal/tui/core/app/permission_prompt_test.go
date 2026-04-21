package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"

	agentruntime "neo-code/internal/tui/services"
)

func TestNormalizePermissionPromptSelectionWrap(t *testing.T) {
	if got := normalizePermissionPromptSelection(-1); got != len(permissionPromptOptions)-1 {
		t.Fatalf("expected -1 to wrap to last index, got %d", got)
	}
	if got := normalizePermissionPromptSelection(len(permissionPromptOptions)); got != 0 {
		t.Fatalf("expected overflow index to wrap to 0, got %d", got)
	}
}

func TestNormalizePermissionPromptSelectionEmptyOptions(t *testing.T) {
	original := permissionPromptOptions
	permissionPromptOptions = nil
	defer func() { permissionPromptOptions = original }()

	if got := normalizePermissionPromptSelection(99); got != 0 {
		t.Fatalf("expected empty options to return 0, got %d", got)
	}
}

func TestPermissionPromptOptionAt(t *testing.T) {
	option := permissionPromptOptionAt(-1)
	if option.Decision != agentruntime.DecisionReject {
		t.Fatalf("expected wrapped option to be reject, got %q", option.Decision)
	}
}

func TestParsePermissionShortcut(t *testing.T) {
	tests := map[string]agentruntime.PermissionResolutionDecision{
		"y":      agentruntime.DecisionAllowOnce,
		"once":   agentruntime.DecisionAllowOnce,
		"a":      agentruntime.DecisionAllowSession,
		"always": agentruntime.DecisionAllowSession,
		"n":      agentruntime.DecisionReject,
		"deny":   agentruntime.DecisionReject,
	}
	for input, want := range tests {
		got, ok := parsePermissionShortcut(input)
		if !ok || got != want {
			t.Fatalf("parsePermissionShortcut(%q) = (%q,%v), want (%q,true)", input, got, ok, want)
		}
	}
	if _, ok := parsePermissionShortcut("unknown"); ok {
		t.Fatalf("expected unknown shortcut to fail")
	}
}

func TestFormatPermissionPromptLines(t *testing.T) {
	lines := formatPermissionPromptLines(permissionPromptState{
		Request: agentruntime.PermissionRequestPayload{
			ToolName:  "bash",
			Operation: "exec",
			Target:    "git status",
		},
		Selected:   1,
		Submitting: true,
	})
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Permission request") {
		t.Fatalf("expected prompt header, got %q", joined)
	}
	if !strings.Contains(joined, "> Allow session") {
		t.Fatalf("expected selected option marker, got %q", joined)
	}
	if !strings.Contains(joined, "Submitting permission decision") {
		t.Fatalf("expected submitting hint, got %q", joined)
	}
}

func TestRenderPermissionPrompt(t *testing.T) {
	app := App{
		appComponents: appComponents{input: textarea.New()},
		appRuntimeState: appRuntimeState{
			pendingPermission: &permissionPromptState{
				Request: agentruntime.PermissionRequestPayload{
					ToolName: "bash",
					Target:   "git status",
				},
				Selected: 0,
			},
			layoutCached: true,
			cachedWidth:  128,
			cachedHeight: 40,
		},
	}
	rendered := app.renderPermissionPrompt()
	if !strings.Contains(rendered, "Permission request") {
		t.Fatalf("expected rendered permission prompt, got %q", rendered)
	}

	app.pendingPermission = nil
	app.input.SetValue("plain input")
	rendered = app.renderPermissionPrompt()
	if !strings.Contains(rendered, "plain input") {
		t.Fatalf("expected fallback to input view, got %q", rendered)
	}
}

func TestParsePermissionPayloadHelpers(t *testing.T) {
	req := agentruntime.PermissionRequestPayload{RequestID: "perm-1"}
	if got, ok := parsePermissionRequestPayload(req); !ok || got.RequestID != "perm-1" {
		t.Fatalf("unexpected parsePermissionRequestPayload result: %+v ok=%v", got, ok)
	}
	if _, ok := parsePermissionRequestPayload((*agentruntime.PermissionRequestPayload)(nil)); ok {
		t.Fatalf("expected nil request pointer to fail parsing")
	}
	reqPtr := &agentruntime.PermissionRequestPayload{RequestID: "perm-1-ptr"}
	if got, ok := parsePermissionRequestPayload(reqPtr); !ok || got.RequestID != "perm-1-ptr" {
		t.Fatalf("unexpected pointer parsePermissionRequestPayload result: %+v ok=%v", got, ok)
	}
	if _, ok := parsePermissionRequestPayload("bad"); ok {
		t.Fatalf("expected unsupported request payload type to fail parsing")
	}

	resolved := agentruntime.PermissionResolvedPayload{RequestID: "perm-2"}
	if got, ok := parsePermissionResolvedPayload(resolved); !ok || got.RequestID != "perm-2" {
		t.Fatalf("unexpected parsePermissionResolvedPayload result: %+v ok=%v", got, ok)
	}
	if _, ok := parsePermissionResolvedPayload((*agentruntime.PermissionResolvedPayload)(nil)); ok {
		t.Fatalf("expected nil resolved pointer to fail parsing")
	}
	resolvedPtr := &agentruntime.PermissionResolvedPayload{RequestID: "perm-2-ptr"}
	if got, ok := parsePermissionResolvedPayload(resolvedPtr); !ok || got.RequestID != "perm-2-ptr" {
		t.Fatalf("unexpected pointer parsePermissionResolvedPayload result: %+v ok=%v", got, ok)
	}
	if _, ok := parsePermissionResolvedPayload(123); ok {
		t.Fatalf("expected unsupported resolved payload type to fail parsing")
	}
}

func TestSanitizePermissionDisplayText(t *testing.T) {
	got := sanitizePermissionDisplayText("bash\x1b[31m\n./demo\t\u202egit status")
	if strings.ContainsRune(got, '\x1b') {
		t.Fatalf("expected escape characters to be removed, got %q", got)
	}
	if strings.Contains(got, "\u202e") {
		t.Fatalf("expected format control characters to be removed, got %q", got)
	}
	if !strings.Contains(got, "bash [31m ./demo git status") {
		t.Fatalf("expected printable content to remain, got %q", got)
	}
}

func TestRenderPromptWithPendingPermission(t *testing.T) {
	input := textarea.New()
	input.SetValue("normal message")

	app := App{
		appComponents: appComponents{
			input: input,
		},
		styles: newStyles(),
		appRuntimeState: appRuntimeState{
			pendingPermission: &permissionPromptState{
				Request:  agentruntime.PermissionRequestPayload{ToolName: "bash", Target: "git status"},
				Selected: 0,
			},
			layoutCached: true,
			cachedWidth:  128,
			cachedHeight: 40,
		},
	}
	rendered := app.renderPrompt(80)
	if !strings.Contains(rendered, "Permission request") {
		t.Fatalf("expected permission prompt rendering branch, got %q", rendered)
	}

	app.pendingPermission = nil
	rendered = app.renderPrompt(80)
	if !strings.Contains(rendered, "normal message") {
		t.Fatalf("expected normal input rendering branch, got %q", rendered)
	}
}
