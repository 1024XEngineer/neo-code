package types

import "testing"

func TestDefaultSessionAssetLimits(t *testing.T) {
	t.Parallel()

	got := DefaultSessionAssetLimits()
	if got.MaxSessionAssetBytes != MaxSessionAssetBytes {
		t.Fatalf("expected MaxSessionAssetBytes=%d, got %d", MaxSessionAssetBytes, got.MaxSessionAssetBytes)
	}
	if got.MaxSessionAssetsTotalBytes != MaxSessionAssetsTotalBytes {
		t.Fatalf(
			"expected MaxSessionAssetsTotalBytes=%d, got %d",
			MaxSessionAssetsTotalBytes,
			got.MaxSessionAssetsTotalBytes,
		)
	}
}

func TestNormalizeSessionAssetLimits(t *testing.T) {
	t.Parallel()

	t.Run("defaults when empty", func(t *testing.T) {
		t.Parallel()

		got := NormalizeSessionAssetLimits(SessionAssetLimits{})
		if got.MaxSessionAssetBytes != MaxSessionAssetBytes {
			t.Fatalf("expected default MaxSessionAssetBytes=%d, got %d", MaxSessionAssetBytes, got.MaxSessionAssetBytes)
		}
		if got.MaxSessionAssetsTotalBytes != MaxSessionAssetsTotalBytes {
			t.Fatalf(
				"expected default MaxSessionAssetsTotalBytes=%d, got %d",
				MaxSessionAssetsTotalBytes,
				got.MaxSessionAssetsTotalBytes,
			)
		}
	})

	t.Run("defaults when explicit zero values are provided", func(t *testing.T) {
		t.Parallel()

		got := NormalizeSessionAssetLimits(SessionAssetLimits{
			MaxSessionAssetBytes:       0,
			MaxSessionAssetsTotalBytes: 0,
		})
		if got.MaxSessionAssetBytes != MaxSessionAssetBytes {
			t.Fatalf("expected default MaxSessionAssetBytes=%d, got %d", MaxSessionAssetBytes, got.MaxSessionAssetBytes)
		}
		if got.MaxSessionAssetsTotalBytes != MaxSessionAssetsTotalBytes {
			t.Fatalf(
				"expected default MaxSessionAssetsTotalBytes=%d, got %d",
				MaxSessionAssetsTotalBytes,
				got.MaxSessionAssetsTotalBytes,
			)
		}
	})

	t.Run("clamps to hard max", func(t *testing.T) {
		t.Parallel()

		got := NormalizeSessionAssetLimits(SessionAssetLimits{
			MaxSessionAssetBytes:       MaxSessionAssetBytes + 1,
			MaxSessionAssetsTotalBytes: MaxSessionAssetsTotalBytes + 1,
		})
		if got.MaxSessionAssetBytes != MaxSessionAssetBytes {
			t.Fatalf("expected clamped MaxSessionAssetBytes=%d, got %d", MaxSessionAssetBytes, got.MaxSessionAssetBytes)
		}
		if got.MaxSessionAssetsTotalBytes != MaxSessionAssetsTotalBytes {
			t.Fatalf(
				"expected clamped MaxSessionAssetsTotalBytes=%d, got %d",
				MaxSessionAssetsTotalBytes,
				got.MaxSessionAssetsTotalBytes,
			)
		}
	})

	t.Run("raises total to single limit", func(t *testing.T) {
		t.Parallel()

		got := NormalizeSessionAssetLimits(SessionAssetLimits{
			MaxSessionAssetBytes:       1024,
			MaxSessionAssetsTotalBytes: 512,
		})
		if got.MaxSessionAssetsTotalBytes != 1024 {
			t.Fatalf("expected total limit promoted to 1024, got %d", got.MaxSessionAssetsTotalBytes)
		}
	})
}
