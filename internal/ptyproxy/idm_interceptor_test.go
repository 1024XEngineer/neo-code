//go:build !windows

package ptyproxy

import "testing"

func TestRouteIDMInput(t *testing.T) {
	t.Run("native command passthrough", func(t *testing.T) {
		decision := routeIDMInput("ls -la")
		if decision.Kind != idmRoutePassThrough {
			t.Fatalf("kind = %q, want %q", decision.Kind, idmRoutePassThrough)
		}
		if decision.Payload != "ls -la" {
			t.Fatalf("payload = %q, want %q", decision.Payload, "ls -la")
		}
	})

	t.Run("exit command", func(t *testing.T) {
		decision := routeIDMInput(" exit ")
		if decision.Kind != idmRouteExit {
			t.Fatalf("kind = %q, want %q", decision.Kind, idmRouteExit)
		}
	})

	t.Run("ai route", func(t *testing.T) {
		decision := routeIDMInput("@ai 为什么失败")
		if decision.Kind != idmRouteAskAI {
			t.Fatalf("kind = %q, want %q", decision.Kind, idmRouteAskAI)
		}
		if decision.Payload != "为什么失败" {
			t.Fatalf("payload = %q, want %q", decision.Payload, "为什么失败")
		}
	})

	t.Run("escaped ai route", func(t *testing.T) {
		decision := routeIDMInput("\\@ai literal")
		if decision.Kind != idmRoutePassThrough {
			t.Fatalf("kind = %q, want %q", decision.Kind, idmRoutePassThrough)
		}
		if decision.Payload != "@ai literal" {
			t.Fatalf("payload = %q, want %q", decision.Payload, "@ai literal")
		}
	})

	t.Run("empty input", func(t *testing.T) {
		decision := routeIDMInput("")
		if decision.Kind != idmRoutePassThrough {
			t.Fatalf("kind = %q, want %q", decision.Kind, idmRoutePassThrough)
		}
		if decision.Payload != "" {
			t.Fatalf("payload = %q, want empty", decision.Payload)
		}
	})

	t.Run("plain @ai without space should not intercept", func(t *testing.T) {
		decision := routeIDMInput("@ai")
		if decision.Kind != idmRoutePassThrough {
			t.Fatalf("kind = %q, want %q", decision.Kind, idmRoutePassThrough)
		}
		if decision.Payload != "@ai" {
			t.Fatalf("payload = %q, want @ai", decision.Payload)
		}
	})
}
