package security

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCapabilitySignerRoundTripAndTamper(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	token := CapabilityToken{
		ID:              "token-1",
		TaskID:          "task-1",
		AgentID:         "agent-1",
		IssuedAt:        now.Add(-time.Minute),
		ExpiresAt:       now.Add(time.Hour),
		AllowedTools:    []string{"filesystem_read_file"},
		AllowedPaths:    []string{"/workspace"},
		NetworkPolicy:   NetworkPolicy{Mode: NetworkPermissionDenyAll},
		WritePermission: WritePermissionWorkspace,
	}

	signer, err := NewCapabilitySigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	signed, err := signer.Sign(token)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	if signed.Signature == "" {
		t.Fatalf("expected non-empty signature")
	}
	if err := signer.Verify(signed); err != nil {
		t.Fatalf("verify signed token: %v", err)
	}

	tampered := signed
	tampered.AllowedTools = []string{"filesystem_write_file"}
	if err := signer.Verify(tampered); err == nil || !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatalf("expected signature mismatch for tampered token, got %v", err)
	}
}

func TestEvaluateCapabilityAction(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	workdir := t.TempDir()
	allowedRoot := filepath.Join(workdir, "allowed")
	token := CapabilityToken{
		ID:              "token-2",
		TaskID:          "task-2",
		AgentID:         "agent-2",
		IssuedAt:        now.Add(-time.Minute),
		ExpiresAt:       now.Add(time.Hour),
		AllowedTools:    []string{"filesystem_read_file", "webfetch"},
		AllowedPaths:    []string{allowedRoot},
		NetworkPolicy:   NetworkPolicy{Mode: NetworkPermissionAllowHosts, AllowedHosts: []string{"example.com"}},
		WritePermission: WritePermissionNone,
	}

	tests := []struct {
		name      string
		action    Action
		wantAllow bool
		wantInErr string
	}{
		{
			name: "allow read in allowed path",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					ToolName:          "filesystem_read_file",
					Resource:          "filesystem_read_file",
					Workdir:           workdir,
					TargetType:        TargetTypePath,
					Target:            "allowed/readme.md",
					SandboxTargetType: TargetTypePath,
					SandboxTarget:     "allowed/readme.md",
				},
			},
			wantAllow: true,
		},
		{
			name: "deny traversal path",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					ToolName:          "filesystem_read_file",
					Resource:          "filesystem_read_file",
					Workdir:           workdir,
					TargetType:        TargetTypePath,
					Target:            "../outside.txt",
					SandboxTargetType: TargetTypePath,
					SandboxTarget:     "../outside.txt",
				},
			},
			wantAllow: false,
			wantInErr: "traversal",
		},
		{
			name: "deny tool miss",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					ToolName: "filesystem_glob",
					Resource: "filesystem_glob",
				},
			},
			wantAllow: false,
			wantInErr: "tool not allowed",
		},
		{
			name: "deny network host miss",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					ToolName:   "webfetch",
					Resource:   "webfetch",
					TargetType: TargetTypeURL,
					Target:     "https://not-example.com/path",
				},
			},
			wantAllow: false,
			wantInErr: "host not allowed",
		},
		{
			name: "deny write by write permission",
			action: Action{
				Type: ActionTypeWrite,
				Payload: ActionPayload{
					ToolName:          "filesystem_read_file",
					Resource:          "filesystem_read_file",
					Workdir:           workdir,
					TargetType:        TargetTypePath,
					Target:            "allowed/readme.md",
					SandboxTargetType: TargetTypePath,
					SandboxTarget:     "allowed/readme.md",
				},
			},
			wantAllow: false,
			wantInErr: "write permission denied",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			allowed, reason := EvaluateCapabilityAction(token, tt.action, now)
			if allowed != tt.wantAllow {
				t.Fatalf("allow=%v, want %v, reason=%q", allowed, tt.wantAllow, reason)
			}
			if tt.wantInErr != "" && !strings.Contains(strings.ToLower(reason), strings.ToLower(tt.wantInErr)) {
				t.Fatalf("expected reason to contain %q, got %q", tt.wantInErr, reason)
			}
		})
	}
}

func TestEnsureCapabilitySubset(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	parent := CapabilityToken{
		ID:              "parent",
		TaskID:          "task",
		AgentID:         "agent-parent",
		IssuedAt:        now.Add(-time.Minute),
		ExpiresAt:       now.Add(2 * time.Hour),
		AllowedTools:    []string{"filesystem_read_file", "webfetch"},
		AllowedPaths:    []string{"/workspace", "/workspace/sub"},
		NetworkPolicy:   NetworkPolicy{Mode: NetworkPermissionAllowHosts, AllowedHosts: []string{"example.com", "*.github.com"}},
		WritePermission: WritePermissionWorkspace,
	}

	tests := []struct {
		name      string
		child     CapabilityToken
		wantError string
	}{
		{
			name: "subset allowed",
			child: CapabilityToken{
				ID:              "child-ok",
				TaskID:          "task",
				AgentID:         "agent-child",
				IssuedAt:        now.Add(-time.Minute),
				ExpiresAt:       now.Add(time.Hour),
				AllowedTools:    []string{"filesystem_read_file"},
				AllowedPaths:    []string{"/workspace/sub"},
				NetworkPolicy:   NetworkPolicy{Mode: NetworkPermissionAllowHosts, AllowedHosts: []string{"example.com"}},
				WritePermission: WritePermissionNone,
			},
		},
		{
			name: "deny broader tool",
			child: CapabilityToken{
				ID:              "child-tool",
				TaskID:          "task",
				AgentID:         "agent-child",
				IssuedAt:        now.Add(-time.Minute),
				ExpiresAt:       now.Add(time.Hour),
				AllowedTools:    []string{"filesystem_read_file", "filesystem_write_file"},
				AllowedPaths:    []string{"/workspace/sub"},
				NetworkPolicy:   NetworkPolicy{Mode: NetworkPermissionAllowHosts, AllowedHosts: []string{"example.com"}},
				WritePermission: WritePermissionNone,
			},
			wantError: "allowed_tools exceeds parent",
		},
		{
			name: "deny longer ttl",
			child: CapabilityToken{
				ID:              "child-ttl",
				TaskID:          "task",
				AgentID:         "agent-child",
				IssuedAt:        now.Add(-time.Minute),
				ExpiresAt:       now.Add(3 * time.Hour),
				AllowedTools:    []string{"filesystem_read_file"},
				AllowedPaths:    []string{"/workspace/sub"},
				NetworkPolicy:   NetworkPolicy{Mode: NetworkPermissionAllowHosts, AllowedHosts: []string{"example.com"}},
				WritePermission: WritePermissionNone,
			},
			wantError: "expires_at exceeds parent",
		},
		{
			name: "deny broader network",
			child: CapabilityToken{
				ID:              "child-net",
				TaskID:          "task",
				AgentID:         "agent-child",
				IssuedAt:        now.Add(-time.Minute),
				ExpiresAt:       now.Add(time.Hour),
				AllowedTools:    []string{"filesystem_read_file"},
				AllowedPaths:    []string{"/workspace/sub"},
				NetworkPolicy:   NetworkPolicy{Mode: NetworkPermissionAllowAll},
				WritePermission: WritePermissionNone,
			},
			wantError: "network policy exceeds parent",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := EnsureCapabilitySubset(parent, tt.child)
			if tt.wantError == "" && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("expected error containing %q, got %v", tt.wantError, err)
				}
			}
		})
	}
}

func TestEvaluateCapabilityForEngine(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	token := CapabilityToken{
		ID:              "token-3",
		TaskID:          "task-3",
		AgentID:         "agent-3",
		IssuedAt:        now.Add(-time.Minute),
		ExpiresAt:       now.Add(time.Hour),
		AllowedTools:    []string{"filesystem_read_file"},
		AllowedPaths:    []string{"/workspace"},
		NetworkPolicy:   NetworkPolicy{Mode: NetworkPermissionDenyAll},
		WritePermission: WritePermissionWorkspace,
	}
	action := Action{
		Type: ActionTypeRead,
		Payload: ActionPayload{
			ToolName:        "filesystem_glob",
			Resource:        "filesystem_glob",
			CapabilityToken: &token,
		},
	}

	result, denied := EvaluateCapabilityForEngine(action, now)
	if !denied {
		t.Fatalf("expected denied result")
	}
	if result.Decision != DecisionDeny {
		t.Fatalf("expected deny decision, got %q", result.Decision)
	}
	if result.Rule == nil || result.Rule.ID != CapabilityRuleID {
		t.Fatalf("expected capability rule id, got %+v", result.Rule)
	}
	if !IsCapabilityDeniedResult(result) {
		t.Fatalf("expected IsCapabilityDeniedResult to be true")
	}
}

func TestNormalizePathKeyPlatformSemantics(t *testing.T) {
	t.Parallel()

	raw := filepath.Join("Workspace", "Sub", "..", "File.txt")
	got := normalizePathKey(raw)
	if got == "" {
		t.Fatalf("expected normalized path key, got empty")
	}

	upper := normalizePathKey(filepath.Join("Workspace", "File.txt"))
	lower := normalizePathKey(filepath.Join("workspace", "file.txt"))

	if runtime.GOOS == "windows" {
		if upper != lower {
			t.Fatalf("windows path key should ignore case: %q vs %q", upper, lower)
		}
		return
	}
	if upper == lower {
		t.Fatalf("non-windows path key should keep case sensitivity: %q vs %q", upper, lower)
	}
}

func TestCapabilitySignerAndTokenBoundaries(t *testing.T) {
	t.Parallel()

	if _, err := NewCapabilitySigner([]byte("short-secret")); err == nil || !strings.Contains(err.Error(), "too short") {
		t.Fatalf("expected short secret error, got %v", err)
	}
	signer, err := NewEphemeralCapabilitySigner()
	if err != nil {
		t.Fatalf("new ephemeral signer: %v", err)
	}
	if signer == nil {
		t.Fatalf("expected non-nil signer")
	}

	now := time.Now().UTC()
	valid := CapabilityToken{
		ID:              "ephemeral",
		TaskID:          "task-e",
		AgentID:         "agent-e",
		IssuedAt:        now.Add(-time.Minute),
		ExpiresAt:       now.Add(time.Hour),
		AllowedTools:    []string{"filesystem_read_file"},
		AllowedPaths:    []string{"/workspace"},
		NetworkPolicy:   NetworkPolicy{Mode: NetworkPermissionDenyAll},
		WritePermission: WritePermissionNone,
	}
	signed, err := signer.Sign(valid)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	if err := signer.Verify(signed); err != nil {
		t.Fatalf("verify signed token: %v", err)
	}

	var nilSigner *CapabilitySigner
	if _, err := nilSigner.Sign(valid); err == nil || !strings.Contains(err.Error(), "signer is nil") {
		t.Fatalf("expected nil signer sign error, got %v", err)
	}
	if err := nilSigner.Verify(signed); err == nil || !strings.Contains(err.Error(), "signer is nil") {
		t.Fatalf("expected nil signer verify error, got %v", err)
	}

	noSignature := valid
	if err := signer.Verify(noSignature); err == nil || !strings.Contains(err.Error(), "signature is empty") {
		t.Fatalf("expected empty signature error, got %v", err)
	}
}

func TestCapabilityTokenValidateShapeAndAt(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	token := CapabilityToken{
		ID:              "token-shape",
		TaskID:          "task-shape",
		AgentID:         "agent-shape",
		IssuedAt:        now.Add(-time.Minute),
		ExpiresAt:       now.Add(time.Hour),
		AllowedTools:    []string{"filesystem_read_file"},
		AllowedPaths:    []string{" /workspace ", "/workspace"},
		NetworkPolicy:   NetworkPolicy{},
		WritePermission: "",
	}
	normalized := token.Normalize()
	if normalized.NetworkPolicy.Mode != NetworkPermissionDenyAll {
		t.Fatalf("expected default network mode deny_all, got %q", normalized.NetworkPolicy.Mode)
	}
	if normalized.WritePermission != WritePermissionNone {
		t.Fatalf("expected default write permission none, got %q", normalized.WritePermission)
	}
	if len(normalized.AllowedPaths) != 1 {
		t.Fatalf("expected deduplicated allowed paths, got %+v", normalized.AllowedPaths)
	}
	if err := token.ValidateShape(); err != nil {
		t.Fatalf("expected valid shape, got %v", err)
	}
	if err := token.ValidateAt(now); err != nil {
		t.Fatalf("expected active token, got %v", err)
	}

	tests := []struct {
		name      string
		mutate    func(CapabilityToken) CapabilityToken
		at        time.Time
		wantInErr string
	}{
		{
			name: "missing id",
			mutate: func(in CapabilityToken) CapabilityToken {
				in.ID = " "
				return in
			},
			at:        now,
			wantInErr: "id is empty",
		},
		{
			name: "missing task",
			mutate: func(in CapabilityToken) CapabilityToken {
				in.TaskID = ""
				return in
			},
			at:        now,
			wantInErr: "task_id is empty",
		},
		{
			name: "missing agent",
			mutate: func(in CapabilityToken) CapabilityToken {
				in.AgentID = ""
				return in
			},
			at:        now,
			wantInErr: "agent_id is empty",
		},
		{
			name: "invalid ttl window",
			mutate: func(in CapabilityToken) CapabilityToken {
				in.ExpiresAt = in.IssuedAt
				return in
			},
			at:        now,
			wantInErr: "must be after",
		},
		{
			name: "empty allowed tools",
			mutate: func(in CapabilityToken) CapabilityToken {
				in.AllowedTools = nil
				return in
			},
			at:        now,
			wantInErr: "allowed_tools is empty",
		},
		{
			name: "invalid network mode",
			mutate: func(in CapabilityToken) CapabilityToken {
				in.NetworkPolicy = NetworkPolicy{Mode: NetworkPermissionMode("bad-mode")}
				return in
			},
			at:        now,
			wantInErr: "invalid network permission",
		},
		{
			name: "allow_hosts requires hosts",
			mutate: func(in CapabilityToken) CapabilityToken {
				in.NetworkPolicy = NetworkPolicy{Mode: NetworkPermissionAllowHosts}
				return in
			},
			at:        now,
			wantInErr: "requires at least one host",
		},
		{
			name: "invalid write permission",
			mutate: func(in CapabilityToken) CapabilityToken {
				in.WritePermission = WritePermissionLevel("bad")
				return in
			},
			at:        now,
			wantInErr: "invalid write permission",
		},
		{
			name: "not active yet",
			mutate: func(in CapabilityToken) CapabilityToken {
				in.IssuedAt = now.Add(time.Minute)
				in.ExpiresAt = now.Add(2 * time.Hour)
				return in
			},
			at:        now,
			wantInErr: "not active yet",
		},
		{
			name: "expired",
			mutate: func(in CapabilityToken) CapabilityToken {
				in.IssuedAt = now.Add(-2 * time.Hour)
				in.ExpiresAt = now.Add(-time.Hour)
				return in
			},
			at:        now,
			wantInErr: "expired",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.mutate(token).ValidateAt(tt.at)
			if err == nil || !strings.Contains(err.Error(), tt.wantInErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantInErr, err)
			}
		})
	}
}

func TestCapabilityHelperFunctions(t *testing.T) {
	t.Parallel()

	if got := WritePermissionNone.rank(); got != 0 {
		t.Fatalf("rank none=%d", got)
	}
	if got := WritePermissionWorkspace.rank(); got != 1 {
		t.Fatalf("rank workspace=%d", got)
	}
	if got := WritePermissionAny.rank(); got != 2 {
		t.Fatalf("rank any=%d", got)
	}
	if got := WritePermissionLevel("invalid").rank(); got != -1 {
		t.Fatalf("rank invalid=%d", got)
	}
	if err := WritePermissionLevel("invalid").Validate(); err == nil {
		t.Fatalf("expected invalid write permission error")
	}
	if err := NetworkPermissionMode("invalid").Validate(); err == nil {
		t.Fatalf("expected invalid network mode error")
	}

	paths := normalizePathDistinctList([]string{" /a ", "/a", "/a/./b", "", " "})
	if len(paths) < 2 {
		t.Fatalf("expected normalized path list to retain distinct values, got %+v", paths)
	}
	hosts := normalizeLowerDistinctList([]string{" Example.com ", "example.com", "*.GitHub.com"})
	if len(hosts) != 2 || hosts[0] != "*.github.com" || hosts[1] != "example.com" {
		t.Fatalf("unexpected normalized host list: %+v", hosts)
	}
	if !isSubsetExact([]string{"a", "b"}, []string{"a"}) {
		t.Fatalf("expected subset exact true")
	}
	if isSubsetExact([]string{"a"}, []string{"a", "b"}) {
		t.Fatalf("expected subset exact false")
	}
	if !isPathSubset([]string{"/repo"}, []string{"/repo/a"}) {
		t.Fatalf("expected path subset true")
	}
	if isPathSubset([]string{"/repo/a"}, []string{"/repo"}) {
		t.Fatalf("expected path subset false")
	}
	if !allowPathByList([]string{"/repo"}, "/repo/a.txt") {
		t.Fatalf("expected allowPathByList to allow nested path")
	}
	if allowPathByList([]string{"/repo"}, "/tmp/x.txt") {
		t.Fatalf("expected allowPathByList to deny outside path")
	}
	if !matchesCapabilityTool([]string{"filesystem_*"}, "filesystem_read_file", "") {
		t.Fatalf("expected wildcard tool match")
	}
	if matchesCapabilityTool([]string{"["}, "filesystem_read_file", "") {
		t.Fatalf("expected invalid wildcard pattern not to match")
	}
	if !matchesCapabilityHost([]string{"*.example.com"}, "api.example.com") {
		t.Fatalf("expected wildcard host match")
	}
	if matchesCapabilityHost([]string{"example.com"}, "api.example.com") {
		t.Fatalf("expected exact host mismatch")
	}
	if !hasTraversal("../etc/passwd") {
		t.Fatalf("expected traversal detection")
	}
	if hasTraversal("safe/path") {
		t.Fatalf("expected non-traversal path")
	}
}

func TestCapabilityNetworkSubsetAndActionHelpers(t *testing.T) {
	t.Parallel()

	if err := ensureNetworkSubset(
		NetworkPolicy{Mode: NetworkPermissionAllowAll},
		NetworkPolicy{Mode: NetworkPermissionAllowHosts, AllowedHosts: []string{"a.com"}},
	); err != nil {
		t.Fatalf("allow_all parent should allow child, got %v", err)
	}
	if err := ensureNetworkSubset(
		NetworkPolicy{Mode: NetworkPermissionDenyAll},
		NetworkPolicy{Mode: NetworkPermissionAllowHosts, AllowedHosts: []string{"a.com"}},
	); err == nil || !strings.Contains(err.Error(), "exceeds parent") {
		t.Fatalf("expected deny_all parent rejection, got %v", err)
	}
	if err := ensureNetworkSubset(
		NetworkPolicy{Mode: NetworkPermissionAllowHosts, AllowedHosts: []string{"a.com"}},
		NetworkPolicy{Mode: NetworkPermissionAllowAll},
	); err == nil || !strings.Contains(err.Error(), "exceeds parent") {
		t.Fatalf("expected child allow_all rejection, got %v", err)
	}
	if err := ensureNetworkSubset(
		NetworkPolicy{Mode: NetworkPermissionAllowHosts, AllowedHosts: []string{"a.com"}},
		NetworkPolicy{Mode: NetworkPermissionAllowHosts, AllowedHosts: []string{"b.com"}},
	); err == nil || !strings.Contains(err.Error(), "allowed_hosts exceeds parent") {
		t.Fatalf("expected child hosts subset rejection, got %v", err)
	}
	if err := ensureNetworkSubset(
		NetworkPolicy{Mode: NetworkPermissionMode("bad-parent")},
		NetworkPolicy{Mode: NetworkPermissionDenyAll},
	); err == nil || !strings.Contains(err.Error(), "invalid parent network policy") {
		t.Fatalf("expected invalid parent mode error, got %v", err)
	}

	if allowed, reason := allowNetworkHost(NetworkPolicy{Mode: NetworkPermissionAllowAll}, "Example.com"); !allowed || reason != "" {
		t.Fatalf("allow_all should allow, got allowed=%v reason=%q", allowed, reason)
	}
	if allowed, reason := allowNetworkHost(NetworkPolicy{Mode: NetworkPermissionDenyAll}, "example.com"); allowed || !strings.Contains(reason, "denies all") {
		t.Fatalf("deny_all should deny, got allowed=%v reason=%q", allowed, reason)
	}
	if allowed, reason := allowNetworkHost(NetworkPolicy{Mode: NetworkPermissionAllowHosts, AllowedHosts: []string{"example.com"}}, "other.com"); allowed || !strings.Contains(reason, "host not allowed") {
		t.Fatalf("allow_hosts miss should deny, got allowed=%v reason=%q", allowed, reason)
	}
	if allowed, reason := allowNetworkHost(NetworkPolicy{Mode: NetworkPermissionMode("invalid")}, "example.com"); allowed || !strings.Contains(reason, "is invalid") {
		t.Fatalf("invalid mode should deny, got allowed=%v reason=%q", allowed, reason)
	}

	if got := resolveActionPath("notes/a.txt", "/workspace"); !strings.HasSuffix(got, "/workspace/notes/a.txt") {
		t.Fatalf("expected resolve relative path under workdir, got %q", got)
	}
	if got := resolveActionPath("", "/workspace"); got != "" {
		t.Fatalf("expected empty resolved path, got %q", got)
	}

	pathAction := Action{
		Type: ActionTypeRead,
		Payload: ActionPayload{
			ToolName:   "filesystem_read_file",
			Resource:   "filesystem_read_file",
			TargetType: TargetTypePath,
			Target:     "a/b.txt",
			Workdir:    "/workspace",
		},
	}
	target, ok, traversal := extractActionPath(pathAction)
	if !ok || traversal || !strings.HasSuffix(target, "/workspace/a/b.txt") {
		t.Fatalf("unexpected extracted path target=%q ok=%v traversal=%v", target, ok, traversal)
	}
	noneTarget, noneOK, noneTraversal := extractActionPath(Action{
		Type: ActionTypeRead,
		Payload: ActionPayload{
			ToolName:   "webfetch",
			Resource:   "webfetch",
			TargetType: TargetTypeURL,
			Target:     "https://example.com",
		},
	})
	if noneOK || noneTraversal || noneTarget != "" {
		t.Fatalf("expected no path extraction for non-path target, got target=%q ok=%v traversal=%v", noneTarget, noneOK, noneTraversal)
	}

	host, ok := extractActionNetworkHost(Action{
		Type: ActionTypeRead,
		Payload: ActionPayload{
			ToolName:   "webfetch",
			Resource:   "webfetch",
			TargetType: TargetTypeURL,
			Target:     "https://API.EXAMPLE.com/x",
		},
	})
	if !ok || host != "api.example.com" {
		t.Fatalf("expected extracted lowercase host, got host=%q ok=%v", host, ok)
	}
	if host, ok := extractActionNetworkHost(Action{
		Type: ActionTypeRead,
		Payload: ActionPayload{
			ToolName:   "filesystem_read_file",
			Resource:   "filesystem_read_file",
			TargetType: TargetTypePath,
			Target:     "/tmp/a.txt",
		},
	}); ok || host != "" {
		t.Fatalf("expected no host for path target, got host=%q ok=%v", host, ok)
	}
}

func TestCapabilityWorkspaceAndEngineDeniedHelpers(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	token := CapabilityToken{
		ID:              "workspace-token",
		TaskID:          "task-workspace",
		AgentID:         "agent-workspace",
		IssuedAt:        now.Add(-time.Minute),
		ExpiresAt:       now.Add(time.Hour),
		AllowedTools:    []string{"filesystem_read_file"},
		AllowedPaths:    []string{"/workspace/allowed"},
		NetworkPolicy:   NetworkPolicy{Mode: NetworkPermissionDenyAll},
		WritePermission: WritePermissionWorkspace,
	}

	allowedAction := Action{
		Type: ActionTypeRead,
		Payload: ActionPayload{
			ToolName:          "filesystem_read_file",
			Resource:          "filesystem_read_file",
			TargetType:        TargetTypePath,
			Target:            "/workspace/allowed/a.txt",
			SandboxTargetType: TargetTypePath,
			SandboxTarget:     "/workspace/allowed/a.txt",
			CapabilityToken:   &token,
		},
	}
	if err := ValidateCapabilityForWorkspace(allowedAction); err != nil {
		t.Fatalf("expected workspace capability allow, got %v", err)
	}

	deniedAction := allowedAction
	deniedAction.Payload.SandboxTarget = "/workspace/blocked/a.txt"
	if err := ValidateCapabilityForWorkspace(deniedAction); err == nil || !strings.Contains(err.Error(), "path not allowed") {
		t.Fatalf("expected workspace capability path denial, got %v", err)
	}

	traversalAction := allowedAction
	traversalAction.Payload.SandboxTarget = "../blocked.txt"
	if err := ValidateCapabilityForWorkspace(traversalAction); err == nil || !strings.Contains(err.Error(), "traversal") {
		t.Fatalf("expected workspace traversal denial, got %v", err)
	}

	noPathAction := Action{
		Type: ActionTypeRead,
		Payload: ActionPayload{
			ToolName:        "webfetch",
			Resource:        "webfetch",
			TargetType:      TargetTypeURL,
			Target:          "https://example.com",
			CapabilityToken: &token,
		},
	}
	if err := ValidateCapabilityForWorkspace(noPathAction); err != nil {
		t.Fatalf("expected non-path action to skip workspace check, got %v", err)
	}
	if err := ValidateCapabilityForWorkspace(Action{
		Type: ActionTypeRead,
		Payload: ActionPayload{
			ToolName: "filesystem_read_file",
			Resource: "filesystem_read_file",
		},
	}); err != nil {
		t.Fatalf("expected action without token to pass, got %v", err)
	}

	if allowed, reason := EvaluateCapabilityAction(token, Action{
		Type: ActionType("invalid"),
		Payload: ActionPayload{
			ToolName: "filesystem_read_file",
			Resource: "filesystem_read_file",
		},
	}, now); allowed || !strings.Contains(reason, "invalid action type") {
		t.Fatalf("expected invalid action denial, got allowed=%v reason=%q", allowed, reason)
	}
	if allowed, reason := EvaluateCapabilityAction(token, allowedAction, now.Add(2*time.Hour)); allowed || !strings.Contains(reason, "expired") {
		t.Fatalf("expected expired denial, got allowed=%v reason=%q", allowed, reason)
	}

	emptyReasonResult := capabilityDeniedResult(allowedAction, " ")
	if emptyReasonResult.Reason != "capability token denied" {
		t.Fatalf("expected default deny reason, got %q", emptyReasonResult.Reason)
	}
	if IsCapabilityDeniedResult(CheckResult{}) {
		t.Fatalf("expected no capability deny for empty result")
	}
	if IsCapabilityDeniedResult(CheckResult{
		Rule: &Rule{ID: "another-rule"},
	}) {
		t.Fatalf("expected non-capability rule id to return false")
	}
}

func TestCapabilitySubsetAndEngineBranches(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	parent := CapabilityToken{
		ID:              "parent-branch",
		TaskID:          "task-branch",
		AgentID:         "agent-parent-branch",
		IssuedAt:        now.Add(-time.Minute),
		ExpiresAt:       now.Add(2 * time.Hour),
		AllowedTools:    []string{"filesystem_read_file", "webfetch"},
		AllowedPaths:    []string{"/workspace"},
		NetworkPolicy:   NetworkPolicy{Mode: NetworkPermissionAllowHosts, AllowedHosts: []string{"example.com", "api.example.com"}},
		WritePermission: WritePermissionWorkspace,
	}

	tests := []struct {
		name      string
		child     CapabilityToken
		wantInErr string
	}{
		{
			name: "child deny_all network is subset",
			child: CapabilityToken{
				ID:              "child-deny-all",
				TaskID:          "task-branch",
				AgentID:         "agent-child-branch",
				IssuedAt:        now.Add(-time.Minute),
				ExpiresAt:       now.Add(time.Hour),
				AllowedTools:    []string{"filesystem_read_file"},
				AllowedPaths:    []string{"/workspace/sub"},
				NetworkPolicy:   NetworkPolicy{Mode: NetworkPermissionDenyAll},
				WritePermission: WritePermissionNone,
			},
		},
		{
			name: "write permission exceeds parent",
			child: CapabilityToken{
				ID:              "child-write",
				TaskID:          "task-branch",
				AgentID:         "agent-child-branch",
				IssuedAt:        now.Add(-time.Minute),
				ExpiresAt:       now.Add(time.Hour),
				AllowedTools:    []string{"filesystem_read_file"},
				AllowedPaths:    []string{"/workspace/sub"},
				NetworkPolicy:   NetworkPolicy{Mode: NetworkPermissionAllowHosts, AllowedHosts: []string{"example.com"}},
				WritePermission: WritePermissionAny,
			},
			wantInErr: "write permission exceeds parent",
		},
		{
			name: "path exceeds parent",
			child: CapabilityToken{
				ID:              "child-path",
				TaskID:          "task-branch",
				AgentID:         "agent-child-branch",
				IssuedAt:        now.Add(-time.Minute),
				ExpiresAt:       now.Add(time.Hour),
				AllowedTools:    []string{"filesystem_read_file"},
				AllowedPaths:    []string{"/outside"},
				NetworkPolicy:   NetworkPolicy{Mode: NetworkPermissionAllowHosts, AllowedHosts: []string{"example.com"}},
				WritePermission: WritePermissionNone,
			},
			wantInErr: "allowed_paths exceeds parent",
		},
		{
			name: "invalid parent shape",
			child: CapabilityToken{
				ID:              "child-ok",
				TaskID:          "task-branch",
				AgentID:         "agent-child-branch",
				IssuedAt:        now.Add(-time.Minute),
				ExpiresAt:       now.Add(time.Hour),
				AllowedTools:    []string{"filesystem_read_file"},
				AllowedPaths:    []string{"/workspace/sub"},
				NetworkPolicy:   NetworkPolicy{Mode: NetworkPermissionDenyAll},
				WritePermission: WritePermissionNone,
			},
			wantInErr: "invalid parent capability token",
		},
		{
			name: "invalid child shape",
			child: CapabilityToken{
				ID:              " ",
				TaskID:          "task-branch",
				AgentID:         "agent-child-branch",
				IssuedAt:        now.Add(-time.Minute),
				ExpiresAt:       now.Add(time.Hour),
				AllowedTools:    []string{"filesystem_read_file"},
				AllowedPaths:    []string{"/workspace/sub"},
				NetworkPolicy:   NetworkPolicy{Mode: NetworkPermissionDenyAll},
				WritePermission: WritePermissionNone,
			},
			wantInErr: "invalid child capability token",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parentToken := parent
			if tt.name == "invalid parent shape" {
				parentToken.ID = ""
			}
			err := EnsureCapabilitySubset(parentToken, tt.child)
			if tt.wantInErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantInErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantInErr, err)
			}
		})
	}

	action := Action{
		Type: ActionTypeRead,
		Payload: ActionPayload{
			ToolName:   "filesystem_read_file",
			Resource:   "filesystem_read_file",
			TargetType: TargetTypePath,
			Target:     "/workspace/notes.txt",
		},
	}

	allowToken := parent
	allowToken.ID = "allow-token"
	allowToken.AgentID = "agent-allow"
	allowToken.AllowedPaths = []string{"/workspace"}
	allowToken.NetworkPolicy = NetworkPolicy{Mode: NetworkPermissionDenyAll}
	action.Payload.CapabilityToken = &allowToken

	result, denied := EvaluateCapabilityForEngine(action, now)
	if denied || result.Decision != "" {
		t.Fatalf("expected capability allow to bypass deny result, got denied=%v result=%+v", denied, result)
	}

	result, denied = EvaluateCapabilityForEngine(Action{
		Type: ActionTypeRead,
		Payload: ActionPayload{
			ToolName: "filesystem_read_file",
			Resource: "filesystem_read_file",
		},
	}, now)
	if denied || result.Decision != "" {
		t.Fatalf("expected no token to bypass capability check, got denied=%v result=%+v", denied, result)
	}
}

func TestCapabilitySignerShapeFailuresAndNetworkHostParsing(t *testing.T) {
	t.Parallel()

	signer, err := NewCapabilitySigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	now := time.Now().UTC()
	valid := CapabilityToken{
		ID:              "sign-shape",
		TaskID:          "task-sign-shape",
		AgentID:         "agent-sign-shape",
		IssuedAt:        now.Add(-time.Minute),
		ExpiresAt:       now.Add(time.Hour),
		AllowedTools:    []string{"filesystem_read_file"},
		AllowedPaths:    []string{"/workspace"},
		NetworkPolicy:   NetworkPolicy{Mode: NetworkPermissionDenyAll},
		WritePermission: WritePermissionNone,
	}

	invalidForSign := valid
	invalidForSign.AllowedTools = nil
	if _, err := signer.Sign(invalidForSign); err == nil || !strings.Contains(err.Error(), "allowed_tools is empty") {
		t.Fatalf("expected sign shape validation failure, got %v", err)
	}

	signed, err := signer.Sign(valid)
	if err != nil {
		t.Fatalf("sign valid token: %v", err)
	}
	invalidForVerify := signed
	invalidForVerify.TaskID = ""
	if err := signer.Verify(invalidForVerify); err == nil || !strings.Contains(err.Error(), "task_id is empty") {
		t.Fatalf("expected verify shape validation failure, got %v", err)
	}

	if host, ok := extractActionNetworkHost(Action{
		Type: ActionTypeRead,
		Payload: ActionPayload{
			ToolName:   "webfetch",
			Resource:   "webfetch",
			TargetType: TargetTypeURL,
			Target:     "://bad-url",
		},
	}); ok || host != "" {
		t.Fatalf("expected invalid URL parsing to fail, got host=%q ok=%v", host, ok)
	}
	if host, ok := extractActionNetworkHost(Action{
		Type: ActionTypeRead,
		Payload: ActionPayload{
			ToolName:   "webfetch",
			Resource:   "webfetch",
			TargetType: TargetTypeURL,
			Target:     "https://",
		},
	}); ok || host != "" {
		t.Fatalf("expected empty host URL to fail, got host=%q ok=%v", host, ok)
	}
	if host, ok := extractActionNetworkHost(Action{
		Type: ActionTypeRead,
		Payload: ActionPayload{
			ToolName:   "webfetch",
			Resource:   "webfetch",
			TargetType: TargetTypePath,
			Target:     "https://example.com",
		},
	}); !ok || host != "example.com" {
		t.Fatalf("expected webfetch resource fallback to parse host, got host=%q ok=%v", host, ok)
	}
}

func TestCapabilityLowLevelBranchCoverage(t *testing.T) {
	t.Parallel()

	if hasTraversal("") {
		t.Fatalf("empty path should not be traversal")
	}
	if !hasTraversal("a/../b") {
		t.Fatalf("path containing '/../' should be traversal")
	}
	if !hasTraversal("a\\..\\b") {
		t.Fatalf("path containing '\\..\\' should be traversal")
	}
	if !allowPathByList([]string{"/repo"}, "/repo") {
		t.Fatalf("expected exact path allow")
	}
	if allowPathByList(nil, "/repo") {
		t.Fatalf("expected empty allowlist to deny")
	}
	if !allowPathByList([]string{"", "/repo"}, "/repo/a") {
		t.Fatalf("expected empty allowlist entry to be skipped")
	}
	if !matchesCapabilityTool([]string{"webfetch"}, "ignored", "webfetch") {
		t.Fatalf("expected resource exact match")
	}
	if matchesCapabilityTool(nil, "filesystem_read_file", "filesystem_read_file") {
		t.Fatalf("expected empty allowlist to return false")
	}
	if !matchesCapabilityTool([]string{"web*"}, "ignored", "webfetch") {
		t.Fatalf("expected resource wildcard match")
	}
	if !matchesCapabilityHost([]string{"example.com"}, "example.com") {
		t.Fatalf("expected exact host match")
	}
	if normalizePathKey(" ") != "" {
		t.Fatalf("expected empty normalized path for blank input")
	}
	if !isPathSubset([]string{"/a"}, nil) {
		t.Fatalf("empty child should always be subset")
	}
	if isPathSubset(nil, []string{"/a"}) {
		t.Fatalf("empty parent should not contain non-empty child")
	}
	if allowed, reason := allowNetworkHost(NetworkPolicy{
		Mode:         NetworkPermissionAllowHosts,
		AllowedHosts: []string{"example.com"},
	}, "example.com"); !allowed || reason != "" {
		t.Fatalf("allow_hosts exact hit should allow, got allowed=%v reason=%q", allowed, reason)
	}
	if values := normalizeLowerDistinctList([]string{"", " ", "Example.com"}); len(values) != 1 || values[0] != "example.com" {
		t.Fatalf("expected blank values to be ignored, got %+v", values)
	}
	if err := ensureNetworkSubset(
		NetworkPolicy{Mode: NetworkPermissionDenyAll},
		NetworkPolicy{Mode: NetworkPermissionDenyAll},
	); err != nil {
		t.Fatalf("expected deny_all child under deny_all parent, got %v", err)
	}

	now := time.Now().UTC()
	token := CapabilityToken{
		ID:              "validate-at-zero-now",
		TaskID:          "task-validate-at-zero-now",
		AgentID:         "agent-validate-at-zero-now",
		IssuedAt:        now.Add(-time.Minute),
		ExpiresAt:       now.Add(time.Hour),
		AllowedTools:    []string{"filesystem_read_file"},
		AllowedPaths:    []string{"/workspace"},
		NetworkPolicy:   NetworkPolicy{Mode: NetworkPermissionDenyAll},
		WritePermission: WritePermissionNone,
	}
	if err := token.ValidateAt(time.Time{}); err != nil {
		t.Fatalf("zero now should fallback to current time, got %v", err)
	}
	token.IssuedAt = time.Time{}
	if err := token.ValidateShape(); err == nil || !strings.Contains(err.Error(), "issued_at/expires_at is required") {
		t.Fatalf("expected required issued/expires validation error, got %v", err)
	}

	denyPathToken := CapabilityToken{
		ID:              "deny-path-token",
		TaskID:          "task-deny-path",
		AgentID:         "agent-deny-path",
		IssuedAt:        now.Add(-time.Minute),
		ExpiresAt:       now.Add(time.Hour),
		AllowedTools:    []string{"filesystem_read_file"},
		AllowedPaths:    []string{"/workspace/allowed"},
		NetworkPolicy:   NetworkPolicy{Mode: NetworkPermissionDenyAll},
		WritePermission: WritePermissionWorkspace,
	}
	if allowed, reason := EvaluateCapabilityAction(denyPathToken, Action{
		Type: ActionTypeRead,
		Payload: ActionPayload{
			ToolName:          "filesystem_read_file",
			Resource:          "filesystem_read_file",
			TargetType:        TargetTypePath,
			Target:            "/workspace/blocked/a.txt",
			SandboxTargetType: TargetTypePath,
			SandboxTarget:     "/workspace/blocked/a.txt",
		},
	}, now); allowed || !strings.Contains(reason, "path not allowed") {
		t.Fatalf("expected EvaluateCapabilityAction path denial, got allowed=%v reason=%q", allowed, reason)
	}

	if target, ok, traversal := extractActionPath(Action{
		Type: ActionTypeRead,
		Payload: ActionPayload{
			ToolName:   "filesystem_read_file",
			Resource:   "filesystem_read_file",
			TargetType: TargetTypePath,
		},
	}); ok || traversal || target != "" {
		t.Fatalf("expected empty path target extraction, got target=%q ok=%v traversal=%v", target, ok, traversal)
	}
	if host, ok := extractActionNetworkHost(Action{
		Type: ActionTypeRead,
		Payload: ActionPayload{
			ToolName:   "webfetch",
			Resource:   "webfetch",
			TargetType: TargetTypeURL,
			Target:     "",
		},
	}); ok || host != "" {
		t.Fatalf("expected empty URL target to skip host extraction, got host=%q ok=%v", host, ok)
	}
}
