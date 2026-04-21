package session

import "testing"

func TestDefaultAssetPolicy(t *testing.T) {
	t.Parallel()

	policy := DefaultAssetPolicy()
	if policy.MaxSessionAssetBytes != MaxSessionAssetBytes {
		t.Fatalf("expected default max session asset bytes %d, got %d", MaxSessionAssetBytes, policy.MaxSessionAssetBytes)
	}
}

func TestNormalizeAssetPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   AssetPolicy
		want int64
	}{
		{
			name: "non-positive uses default",
			in:   AssetPolicy{MaxSessionAssetBytes: 0},
			want: MaxSessionAssetBytes,
		},
		{
			name: "caps at hard limit",
			in:   AssetPolicy{MaxSessionAssetBytes: MaxSessionAssetBytes + 1},
			want: MaxSessionAssetBytes,
		},
		{
			name: "keeps valid value",
			in:   AssetPolicy{MaxSessionAssetBytes: 1024},
			want: 1024,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeAssetPolicy(tt.in)
			if got.MaxSessionAssetBytes != tt.want {
				t.Fatalf("NormalizeAssetPolicy(%+v) max=%d, want=%d", tt.in, got.MaxSessionAssetBytes, tt.want)
			}
		})
	}
}
