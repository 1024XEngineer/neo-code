package gateway

import "testing"

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
		{source: RequestSourceUnknown, method: "gateway.ping", want: false},
	}
	for _, tc := range cases {
		got := acl.IsAllowed(tc.source, tc.method)
		if got != tc.want {
			t.Fatalf("acl allowed(%s,%s) = %v, want %v", tc.source, tc.method, got, tc.want)
		}
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
