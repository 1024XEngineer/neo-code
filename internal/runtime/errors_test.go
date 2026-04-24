package runtime

import (
	"bytes"
	"errors"
	"log"
	"testing"

	"neo-code/internal/provider"
)

func TestHandleRunErrorProviderErrorDoesNotWriteStdLog(t *testing.T) {
	service := &Service{}
	providerErr := &provider.ProviderError{
		StatusCode: 401,
		Code:       "auth_failed",
		Message:    "Incorrect API key provided",
		Retryable:  false,
	}

	var buf bytes.Buffer
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	oldPrefix := log.Prefix()
	log.SetOutput(&buf)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
		log.SetPrefix(oldPrefix)
	})

	err := service.handleRunError(providerErr)
	if !errors.Is(err, providerErr) {
		t.Fatalf("expected provider error passthrough, got %v", err)
	}
	if got := buf.String(); got != "" {
		t.Fatalf("expected no std log output, got %q", got)
	}

}
