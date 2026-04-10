package gateway

import "testing"

func TestStableErrorCodes(t *testing.T) {
	codes := []ErrorCode{
		ErrorCodeInvalidFrame,
		ErrorCodeInvalidAction,
		ErrorCodeInvalidMultimodalPayload,
		ErrorCodeMissingRequiredField,
		ErrorCodeUnsupportedAction,
		ErrorCodeInternalError,
	}

	for _, code := range codes {
		if !IsStableErrorCode(code.String()) {
			t.Fatalf("expected code %q to be stable", code)
		}
	}

	if IsStableErrorCode("unknown_code") {
		t.Fatalf("unknown code should not be stable")
	}
}

func TestNewMissingRequiredFieldError(t *testing.T) {
	err := NewMissingRequiredFieldError("session_id")
	if err == nil {
		t.Fatalf("expected non-nil error")
	}
	if err.Code != ErrorCodeMissingRequiredField.String() {
		t.Fatalf("error code mismatch: got %q", err.Code)
	}
	if err.Message == "" {
		t.Fatalf("error message should not be empty")
	}
}
