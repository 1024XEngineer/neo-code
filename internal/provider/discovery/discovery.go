package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type openAIModelsResponse struct {
	Data []map[string]any `json:"data"`
}

// FetchOpenAICompatibleModels fetches raw model objects from an OpenAI-compatible /models endpoint.
func FetchOpenAICompatibleModels(ctx context.Context, client *http.Client, baseURL string, apiKey string) ([]map[string]any, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("provider discovery: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider discovery: send request: %w", err)
	}
	defer func(body io.ReadCloser) {
		_ = body.Close()
	}(resp.Body)

	if resp.StatusCode >= http.StatusBadRequest {
		data, _ := io.ReadAll(resp.Body)
		body := strings.TrimSpace(string(data))
		if body == "" {
			body = resp.Status
		}
		return nil, fmt.Errorf("provider discovery: %s", body)
	}

	var payload openAIModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("provider discovery: decode response: %w", err)
	}
	return payload.Data, nil
}
