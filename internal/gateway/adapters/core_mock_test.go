package adapters

import (
	"context"
	"testing"
)

func TestCoreMockPing(t *testing.T) {
	mock := NewCoreMock()
	pong, err := mock.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	if pong != "Pong" {
		t.Fatalf("Ping() = %s, want Pong", pong)
	}
}

func TestCoreMockPingCustomMessage(t *testing.T) {
	mock := NewCoreMockWithPong("Custom")
	pong, err := mock.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	if pong != "Custom" {
		t.Fatalf("Ping() = %s, want Custom", pong)
	}
}

func TestCoreMockPingNilReceiver(t *testing.T) {
	var mock *CoreMock
	pong, err := mock.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	if pong != "Pong" {
		t.Fatalf("Ping() = %s, want Pong", pong)
	}
}
