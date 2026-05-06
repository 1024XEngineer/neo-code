package gateway

import "testing"

func assertACLAllowed(t *testing.T, acl *ControlPlaneACL, source RequestSource, method string, want bool) {
	t.Helper()
	if got := acl.IsAllowed(source, method); got != want {
		t.Fatalf("acl allowed(%s,%s) = %v, want %v", source, method, got, want)
	}
}

func TestStrictACLAllowlist(t *testing.T) {
	acl := NewStrictControlPlaneACL()
	cases := []struct {
		source RequestSource
		method string
		want   bool
	}{
		{source: RequestSourceIPC, method: "gateway.authenticate", want: true},
		{source: RequestSourceIPC, method: "gateway.ping", want: true},
		{source: RequestSourceIPC, method: "wake.openUrl", want: true},
		{source: RequestSourceHTTP, method: "gateway.bindStream", want: true},
		{source: RequestSourceWS, method: "wake.openUrl", want: true},
		{source: RequestSourceSSE, method: "gateway.ping", want: true},
		{source: RequestSourceSSE, method: "wake.openUrl", want: false},
		{source: RequestSourceHTTP, method: "gateway.run", want: true},
		{source: RequestSourceHTTP, method: "gateway.executeSystemTool", want: true},
		{source: RequestSourceHTTP, method: "gateway.activateSessionSkill", want: true},
		{source: RequestSourceHTTP, method: "gateway.deactivateSessionSkill", want: true},
		{source: RequestSourceHTTP, method: "gateway.listSessionSkills", want: true},
		{source: RequestSourceHTTP, method: "gateway.listAvailableSkills", want: true},
		{source: RequestSourceHTTP, method: "session.todos.list", want: true},
		{source: RequestSourceHTTP, method: "runtime.snapshot.get", want: true},
		{source: RequestSourceHTTP, method: "checkpoint.list", want: true},
		{source: RequestSourceHTTP, method: "checkpoint.restore", want: true},
		{source: RequestSourceHTTP, method: "checkpoint.undoRestore", want: true},
		{source: RequestSourceHTTP, method: "checkpoint.diff", want: true},
		{source: RequestSourceUnknown, method: "gateway.ping", want: false},
	}
	for _, tc := range cases {
		assertACLAllowed(t, acl, tc.source, tc.method, tc.want)
	}
}

func TestNormalizeRequestSource(t *testing.T) {
	if got := NormalizeRequestSource(" WS "); got != RequestSourceWS {
		t.Fatalf("normalized source = %q, want %q", got, RequestSourceWS)
	}
	if got := NormalizeRequestSource("custom"); got != RequestSourceUnknown {
		t.Fatalf("normalized source = %q, want %q", got, RequestSourceUnknown)
	}
}

func TestACLModeAndNilBehavior(t *testing.T) {
	var nilACL *ControlPlaneACL
	if mode := nilACL.Mode(); mode != ACLModeStrict {
		t.Fatalf("mode = %q, want %q", mode, ACLModeStrict)
	}
	if !nilACL.IsAllowed(RequestSourceUnknown, "") {
		t.Fatal("nil acl should allow by default")
	}

	acl := NewStrictControlPlaneACL()
	acl.enabled = false
	if !acl.IsAllowed(RequestSourceUnknown, "") {
		t.Fatal("disabled acl should allow all requests")
	}
}

func TestACLModeAndMethodValidationBranches(t *testing.T) {
	acl := NewStrictControlPlaneACL()
	if acl.Mode() != ACLModeStrict {
		t.Fatalf("mode = %q, want %q", acl.Mode(), ACLModeStrict)
	}
	if acl.IsAllowed(RequestSourceIPC, "   ") {
		t.Fatal("empty normalized method should be denied")
	}
}
