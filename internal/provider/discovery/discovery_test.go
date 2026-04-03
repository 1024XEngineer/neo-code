package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchOpenAICompatibleModels(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-test","context_window":128000,"extra":"kept"}]}`))
	}))
	defer server.Close()

	models, err := FetchOpenAICompatibleModels(context.Background(), server.Client(), server.URL, "test-key")
	if err != nil {
		t.Fatalf("FetchOpenAICompatibleModels() error = %v", err)
	}
	if len(models) != 1 || models[0]["id"] != "gpt-test" {
		t.Fatalf("unexpected models payload: %+v", models)
	}
	if models[0]["extra"] != "kept" {
		t.Fatalf("expected unknown fields to remain in raw payload, got %+v", models[0])
	}
}

func TestFetchOpenAICompatibleModelsHTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("gateway failed"))
	}))
	defer server.Close()

	_, err := FetchOpenAICompatibleModels(context.Background(), server.Client(), server.URL, "test-key")
	if err == nil || !strings.Contains(err.Error(), "gateway failed") {
		t.Fatalf("expected gateway failure error, got %v", err)
	}
}
