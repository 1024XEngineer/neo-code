package gateway

import (
	"context"
	"strings"
	"testing"
)

func TestConnectionContextRoundTrip(t *testing.T) {
	connectionID := NewConnectionID()
	if !strings.HasPrefix(string(connectionID), "cid_") {
		t.Fatalf("connection id = %q, want prefix %q", connectionID, "cid_")
	}

	ctx := WithConnectionID(context.Background(), connectionID)
	resolvedID, exists := ConnectionIDFromContext(ctx)
	if !exists {
		t.Fatal("connection id should exist in context")
	}
	if resolvedID != connectionID {
		t.Fatalf("resolved connection id = %q, want %q", resolvedID, connectionID)
	}
}

func TestStreamRelayContextRoundTrip(t *testing.T) {
	relay := NewStreamRelay(StreamRelayOptions{})
	ctx := WithStreamRelay(context.Background(), relay)

	resolvedRelay, exists := StreamRelayFromContext(ctx)
	if !exists {
		t.Fatal("stream relay should exist in context")
	}
	if resolvedRelay != relay {
		t.Fatal("resolved relay should match original relay")
	}
}

func TestParseStreamChannel(t *testing.T) {
	if channel, ok := ParseStreamChannel("ws"); !ok || channel != StreamChannelWS {
		t.Fatalf("parse channel = %q ok=%v, want %q true", channel, ok, StreamChannelWS)
	}

	if _, ok := ParseStreamChannel("tcp"); ok {
		t.Fatal("invalid channel should be rejected")
	}
}
