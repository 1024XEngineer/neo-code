package shared

import (
	"errors"
	"net/http"
	"strings"

	"neo-code/internal/provider"
)

func ValidateRuntimeConfig(cfg provider.RuntimeConfig) error {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return errors.New("openai provider: base url is empty")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return errors.New("openai provider: api key is empty")
	}
	return nil
}

func Endpoint(baseURL string, path string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if path == "" {
		return baseURL
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return baseURL + path
}

func SetBearerAuthorization(header http.Header, apiKey string) {
	if header == nil {
		return
	}

	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return
	}

	header.Set("Authorization", "Bearer "+apiKey)
}
