package protocol

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestEncodeAndDecodeWakeStartupInput(t *testing.T) {
	encoded, err := EncodeWakeStartupInput(WakeStartupInput{
		Text:    "  hello world  ",
		Workdir: "  C:\\repo\\neo-code  ",
	})
	if err != nil {
		t.Fatalf("EncodeWakeStartupInput() error = %v", err)
	}
	if strings.TrimSpace(encoded) == "" {
		t.Fatal("expected non-empty encoded payload")
	}

	decoded, err := DecodeWakeStartupInput(encoded)
	if err != nil {
		t.Fatalf("DecodeWakeStartupInput() error = %v", err)
	}
	if decoded.Text != "hello world" {
		t.Fatalf("decoded text = %q, want %q", decoded.Text, "hello world")
	}
	if decoded.Workdir != "C:\\repo\\neo-code" {
		t.Fatalf("decoded workdir = %q, want %q", decoded.Workdir, "C:\\repo\\neo-code")
	}
}

func TestEncodeWakeStartupInputRejectsEmptyText(t *testing.T) {
	_, err := EncodeWakeStartupInput(WakeStartupInput{Text: "   "})
	if err == nil {
		t.Fatal("expected empty text error")
	}
}

func TestDecodeWakeStartupInputRejectsInvalidPayload(t *testing.T) {
	_, err := DecodeWakeStartupInput("not-base64")
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestDecodeWakeStartupInputRejectsMissingText(t *testing.T) {
	raw := `{"text":"   ","workdir":"/tmp/repo"}`
	encoded := base64.RawURLEncoding.EncodeToString([]byte(raw))
	_, err := DecodeWakeStartupInput(encoded)
	if err == nil {
		t.Fatal("expected missing text error")
	}
}
