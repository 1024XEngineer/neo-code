package discovery

import (
	"testing"

	"neo-code/internal/provider"
)

func TestExtractRawModels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload any
		profile string
		wantLen int
		wantErr bool
	}{
		{
			name: "openai data array",
			payload: map[string]any{
				"data": []any{
					map[string]any{"id": "gpt-4.1"},
				},
			},
			profile: provider.DiscoveryResponseProfileOpenAI,
			wantLen: 1,
		},
		{
			name: "openai nested data models array",
			payload: map[string]any{
				"data": map[string]any{
					"models": []any{
						map[string]any{"id": "qwen-plus"},
					},
				},
			},
			profile: provider.DiscoveryResponseProfileOpenAI,
			wantLen: 1,
		},
		{
			name: "generic profile supports case-insensitive list keys",
			payload: map[string]any{
				"Data": []any{
					map[string]any{"id": "qwen-max"},
				},
			},
			profile: provider.DiscoveryResponseProfileGeneric,
			wantLen: 1,
		},
		{
			name: "gemini models array",
			payload: map[string]any{
				"models": []any{
					map[string]any{"name": "models/gemini-2.5-flash"},
				},
			},
			profile: provider.DiscoveryResponseProfileGemini,
			wantLen: 1,
		},
		{
			name: "generic results array",
			payload: map[string]any{
				"results": []any{
					map[string]any{"id": "custom-model"},
				},
			},
			profile: provider.DiscoveryResponseProfileGeneric,
			wantLen: 1,
		},
		{
			name: "root array payload",
			payload: []any{
				map[string]any{"id": "m1"},
				map[string]any{"id": "m2"},
			},
			profile: provider.DiscoveryResponseProfileGeneric,
			wantLen: 2,
		},
		{
			name: "single model object payload",
			payload: map[string]any{
				"id":   "m1",
				"name": "Model 1",
			},
			profile: provider.DiscoveryResponseProfileOpenAI,
			wantLen: 1,
		},
		{
			name: "single object with name only should not be treated as model",
			payload: map[string]any{
				"name": "not-a-model",
			},
			profile: provider.DiscoveryResponseProfileOpenAI,
			wantErr: true,
		},
		{
			name: "unsupported payload type",
			payload: map[string]any{
				"data": "unexpected",
			},
			profile: provider.DiscoveryResponseProfileOpenAI,
			wantErr: true,
		},
		{
			name: "missing supported list key",
			payload: map[string]any{
				"object": "list",
			},
			profile: provider.DiscoveryResponseProfileOpenAI,
			wantErr: true,
		},
		{
			name: "openai profile falls back to generic keys for items payload",
			payload: map[string]any{
				"items": []any{
					map[string]any{"id": "m1"},
				},
			},
			profile: provider.DiscoveryResponseProfileOpenAI,
			wantLen: 1,
		},
		{
			name: "generic profile accepts items payload",
			payload: map[string]any{
				"items": []any{
					map[string]any{"id": "m1"},
				},
			},
			profile: provider.DiscoveryResponseProfileGeneric,
			wantLen: 1,
		},
		{
			name: "string list under models key",
			payload: map[string]any{
				"models": []any{"model-a", "model-b"},
			},
			profile: provider.DiscoveryResponseProfileOpenAI,
			wantLen: 2,
		},
		{
			name: "deep nested unknown container keys",
			payload: map[string]any{
				"result": map[string]any{
					"payload": map[string]any{
						"records": []any{
							map[string]any{"model_id": "qwen-max"},
						},
					},
				},
			},
			profile: provider.DiscoveryResponseProfileOpenAI,
			wantLen: 1,
		},
		{
			name: "deep fallback skips non-model string arrays",
			payload: map[string]any{
				"errors": []any{"bad request", "permission denied"},
			},
			profile: provider.DiscoveryResponseProfileOpenAI,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ExtractRawModels(tt.payload, tt.profile)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ExtractRawModels() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(got) != tt.wantLen {
				t.Fatalf("ExtractRawModels() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}
