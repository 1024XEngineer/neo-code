package provider

import "testing"

func TestDefaultRequestAssetBudget(t *testing.T) {
	t.Parallel()

	budget := DefaultRequestAssetBudget()
	if budget.MaxSessionAssetsTotalBytes != MaxSessionAssetsTotalBytes {
		t.Fatalf("expected default budget %d, got %d", MaxSessionAssetsTotalBytes, budget.MaxSessionAssetsTotalBytes)
	}
}

func TestNormalizeRequestAssetBudget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		budget               RequestAssetBudget
		maxSessionAssetBytes int64
		want                 int64
	}{
		{
			name:                 "uses default when configured total is non-positive",
			budget:               RequestAssetBudget{MaxSessionAssetsTotalBytes: 0},
			maxSessionAssetBytes: 1024,
			want:                 MaxSessionAssetsTotalBytes,
		},
		{
			name:                 "caps at hard limit",
			budget:               RequestAssetBudget{MaxSessionAssetsTotalBytes: MaxSessionAssetsTotalBytes + 1},
			maxSessionAssetBytes: 1024,
			want:                 MaxSessionAssetsTotalBytes,
		},
		{
			name:                 "raises budget to single asset limit",
			budget:               RequestAssetBudget{MaxSessionAssetsTotalBytes: 1024},
			maxSessionAssetBytes: 2048,
			want:                 2048,
		},
		{
			name:                 "keeps configured total within bounds",
			budget:               RequestAssetBudget{MaxSessionAssetsTotalBytes: 4096},
			maxSessionAssetBytes: 1024,
			want:                 4096,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeRequestAssetBudget(tt.budget, tt.maxSessionAssetBytes)
			if got.MaxSessionAssetsTotalBytes != tt.want {
				t.Fatalf(
					"NormalizeRequestAssetBudget(%+v, %d) total=%d, want=%d",
					tt.budget,
					tt.maxSessionAssetBytes,
					got.MaxSessionAssetsTotalBytes,
					tt.want,
				)
			}
		})
	}
}
