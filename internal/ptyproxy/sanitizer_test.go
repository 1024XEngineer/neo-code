package ptyproxy

import (
	"strings"
	"testing"
)

func TestSanitizeDiagnosisTextANSIAndCRFold(t *testing.T) {
	raw := "step1\rstep2\n\x1b[31merror\x1b[0m happened"
	got := SanitizeDiagnosisText(raw, 1024)
	if strings.Contains(got, "\x1b[31m") {
		t.Fatalf("ansi should be removed: %q", got)
	}
	if strings.Contains(got, "step1") {
		t.Fatalf("carriage-return overwritten text should be folded: %q", got)
	}
	if !strings.Contains(got, "step2") {
		t.Fatalf("want folded line tail kept: %q", got)
	}
}

func TestSanitizeDiagnosisTextRedactsSecrets(t *testing.T) {
	raw := strings.Join([]string{
		"AKIAABCDEFGHIJKLMNOP",
		"Bearer super-secret-token",
		"password=abc123",
		"api_key: key-xyz",
		"-----BEGIN PRIVATE KEY-----abc-----END PRIVATE KEY-----",
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTYifQ.signaturepart",
	}, "\n")
	got := SanitizeDiagnosisText(raw, 4096)
	for _, leaked := range []string{
		"AKIAABCDEFGHIJKLMNOP",
		"super-secret-token",
		"abc123",
		"key-xyz",
		"signaturepart",
	} {
		if strings.Contains(got, leaked) {
			t.Fatalf("secret leaked %q in %q", leaked, got)
		}
	}
	if !strings.Contains(got, "[REDACTED]") && !strings.Contains(got, "[JWT_REDACTED]") {
		t.Fatalf("redaction marker missing: %q", got)
	}
}

func TestSanitizeDiagnosisTextTruncatesBeforeRedactionStage(t *testing.T) {
	// 构造超长文本，验证结果长度受 maxBytes 限制并保持可解析。
	raw := strings.Repeat("x", 8192) + "\npassword=abcdef"
	got := SanitizeDiagnosisText(raw, 256)
	if len(got) > 256+len(truncationMarker) {
		t.Fatalf("sanitized payload too large: %d", len(got))
	}
	if strings.Contains(got, "abcdef") {
		t.Fatalf("secret should be redacted even after truncation: %q", got)
	}
}

func TestSanitizerHelperBranches(t *testing.T) {
	if got := SanitizeDiagnosisText("", 0); got != "" {
		t.Fatalf("SanitizeDiagnosisText(empty) = %q, want empty", got)
	}
	if got := stripANSI(" "); got != "" {
		t.Fatalf("stripANSI(whitespace) = %q, want empty", got)
	}
	if got := foldCarriageReturns(""); got != "" {
		t.Fatalf("foldCarriageReturns(empty) = %q, want empty", got)
	}
	normalized := normalizeWhitespace("a \t\tb\u0001\u0002\n c")
	if normalized != "a b\n c" {
		t.Fatalf("normalizeWhitespace() = %q, want %q", normalized, "a b\n c")
	}
	if got := redactSensitive(""); got != "" {
		t.Fatalf("redactSensitive(empty) = %q, want empty", got)
	}
}
