package gateway

import (
	"context"
	"log"
	"os"
	"testing"
)

type stubTokenAuthenticator struct {
	token string
}

func (a stubTokenAuthenticator) ValidateToken(token string) bool {
	return token == a.token
}

func (a stubTokenAuthenticator) ResolveSubjectID(token string) (string, bool) {
	if !a.ValidateToken(token) {
		return "", false
	}
	return "local_admin", true
}

func TestConnectionAuthState(t *testing.T) {
	state := NewConnectionAuthState()
	if state.IsAuthenticated() {
		t.Fatal("new state should be unauthenticated")
	}
	state.MarkAuthenticated("local_admin")
	if !state.IsAuthenticated() {
		t.Fatal("state should be authenticated")
	}
}

func TestRequestContextHelpers(t *testing.T) {
	ctx := context.Background()
	ctx = WithRequestSource(ctx, RequestSourceHTTP)
	ctx = WithRequestToken(ctx, " token-1 ")

	state := NewConnectionAuthState()
	ctx = WithConnectionAuthState(ctx, state)

	authenticator := stubTokenAuthenticator{token: "token-1"}
	ctx = WithTokenAuthenticator(ctx, authenticator)
	ctx = WithRequestACL(ctx, NewStrictControlPlaneACL())
	metrics := NewGatewayMetrics()
	ctx = WithGatewayMetrics(ctx, metrics)
	logger := log.New(os.Stderr, "", 0)
	ctx = WithGatewayLogger(ctx, logger)

	if source := RequestSourceFromContext(ctx); source != RequestSourceHTTP {
		t.Fatalf("source = %q, want %q", source, RequestSourceHTTP)
	}
	if token := RequestTokenFromContext(ctx); token != "token-1" {
		t.Fatalf("token = %q, want %q", token, "token-1")
	}
	if loadedState, ok := ConnectionAuthStateFromContext(ctx); !ok || loadedState != state {
		t.Fatal("expected to load connection auth state")
	}
	if loadedAuthenticator, ok := TokenAuthenticatorFromContext(ctx); !ok || !loadedAuthenticator.ValidateToken("token-1") {
		t.Fatal("expected to load token authenticator")
	}
	if acl, ok := RequestACLFromContext(ctx); !ok || acl == nil {
		t.Fatal("expected to load acl")
	}
	if loadedMetrics, ok := GatewayMetricsFromContext(ctx); !ok || loadedMetrics != metrics {
		t.Fatal("expected to load metrics")
	}
	if loadedLogger, ok := GatewayLoggerFromContext(ctx); !ok || loadedLogger != logger {
		t.Fatal("expected to load logger")
	}
}

func TestRequestContextNilAndTypeMismatchBranches(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		if source := RequestSourceFromContext(nil); source != RequestSourceUnknown {
			t.Fatalf("source = %q, want %q", source, RequestSourceUnknown)
		}
		if token := RequestTokenFromContext(nil); token != "" {
			t.Fatalf("token = %q, want empty", token)
		}
		if _, ok := ConnectionAuthStateFromContext(nil); ok {
			t.Fatal("expected missing auth state")
		}
		if _, ok := TokenAuthenticatorFromContext(nil); ok {
			t.Fatal("expected missing authenticator")
		}
		if _, ok := RequestACLFromContext(nil); ok {
			t.Fatal("expected missing acl")
		}
		if _, ok := GatewayMetricsFromContext(nil); ok {
			t.Fatal("expected missing metrics")
		}
		if _, ok := GatewayLoggerFromContext(nil); ok {
			t.Fatal("expected missing logger")
		}
	})

	t.Run("nil context in with helpers", func(t *testing.T) {
		ctx := WithRequestSource(nil, " WS ")
		ctx = WithRequestToken(ctx, " token ")
		ctx = WithConnectionAuthState(ctx, NewConnectionAuthState())
		ctx = WithTokenAuthenticator(ctx, stubTokenAuthenticator{token: "token"})
		ctx = WithRequestACL(ctx, NewStrictControlPlaneACL())
		ctx = WithGatewayMetrics(ctx, NewGatewayMetrics())
		ctx = WithGatewayLogger(ctx, log.New(os.Stderr, "", 0))

		if source := RequestSourceFromContext(ctx); source != RequestSourceWS {
			t.Fatalf("source = %q, want %q", source, RequestSourceWS)
		}
		if token := RequestTokenFromContext(ctx); token != "token" {
			t.Fatalf("token = %q, want %q", token, "token")
		}
	})

	t.Run("type mismatch", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), requestSourceContextKey{}, 1)
		ctx = context.WithValue(ctx, requestTokenContextKey{}, 2)
		ctx = context.WithValue(ctx, connectionAuthStateContextKey{}, "state")
		ctx = context.WithValue(ctx, tokenAuthenticatorContextKey{}, "auth")
		ctx = context.WithValue(ctx, requestACLContextKey{}, "acl")
		ctx = context.WithValue(ctx, gatewayMetricsContextKey{}, "metrics")
		ctx = context.WithValue(ctx, gatewayLoggerContextKey{}, "logger")

		if source := RequestSourceFromContext(ctx); source != RequestSourceUnknown {
			t.Fatalf("source = %q, want %q", source, RequestSourceUnknown)
		}
		if token := RequestTokenFromContext(ctx); token != "" {
			t.Fatalf("token = %q, want empty", token)
		}
		if _, ok := ConnectionAuthStateFromContext(ctx); ok {
			t.Fatal("expected type mismatch for auth state")
		}
		if _, ok := TokenAuthenticatorFromContext(ctx); ok {
			t.Fatal("expected type mismatch for authenticator")
		}
		if _, ok := RequestACLFromContext(ctx); ok {
			t.Fatal("expected type mismatch for acl")
		}
		if _, ok := GatewayMetricsFromContext(ctx); ok {
			t.Fatal("expected type mismatch for metrics")
		}
		if _, ok := GatewayLoggerFromContext(ctx); ok {
			t.Fatal("expected type mismatch for logger")
		}
	})
}

func TestConnectionAuthStateNilReceiver(t *testing.T) {
	var state *ConnectionAuthState
	state.MarkAuthenticated("local_admin")
	if state.IsAuthenticated() {
		t.Fatal("nil state should remain unauthenticated")
	}
}

func TestRequestContextWithHelpersOnNilContextIndividually(t *testing.T) {
	if token := RequestTokenFromContext(WithRequestToken(nil, " token-2 ")); token != "token-2" {
		t.Fatalf("token = %q, want %q", token, "token-2")
	}
	if _, ok := ConnectionAuthStateFromContext(WithConnectionAuthState(nil, NewConnectionAuthState())); !ok {
		t.Fatal("expected auth state to be attached on nil context")
	}
	if _, ok := TokenAuthenticatorFromContext(WithTokenAuthenticator(nil, stubTokenAuthenticator{token: "t"})); !ok {
		t.Fatal("expected authenticator to be attached on nil context")
	}
	if _, ok := RequestACLFromContext(WithRequestACL(nil, NewStrictControlPlaneACL())); !ok {
		t.Fatal("expected acl to be attached on nil context")
	}
	if _, ok := GatewayMetricsFromContext(WithGatewayMetrics(nil, NewGatewayMetrics())); !ok {
		t.Fatal("expected metrics to be attached on nil context")
	}
	if _, ok := GatewayLoggerFromContext(WithGatewayLogger(nil, log.New(os.Stderr, "", 0))); !ok {
		t.Fatal("expected logger to be attached on nil context")
	}
}
