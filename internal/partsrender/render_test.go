package partsrender

import (
	"testing"

	providertypes "neo-code/internal/provider/types"
)

func TestRenderCompactPromptParts(t *testing.T) {
	t.Parallel()

	parts := []providertypes.ContentPart{
		providertypes.NewTextPart("before"),
		providertypes.NewRemoteImagePart("https://example.com/a.png"),
		providertypes.NewSessionAssetImagePart("asset-1", "image/png"),
	}

	got := RenderCompactPromptParts(parts)
	want := "before[Image:remote] https://example.com/a.png[Image:session_asset] asset-1 (image/png)"
	if got != want {
		t.Fatalf("RenderCompactPromptParts() = %q, want %q", got, want)
	}
}

func TestRenderTranscriptParts(t *testing.T) {
	t.Parallel()

	parts := []providertypes.ContentPart{
		providertypes.NewTextPart("look "),
		providertypes.NewRemoteImagePart("https://example.com/a.png?token=secret"),
		providertypes.NewSessionAssetImagePart("asset-2", ""),
	}

	got := RenderTranscriptParts(parts)
	want := "look [Image:remote][Image:session_asset]"
	if got != want {
		t.Fatalf("RenderTranscriptParts() = %q, want %q", got, want)
	}
}

func TestRenderDisplayParts(t *testing.T) {
	t.Parallel()

	parts := []providertypes.ContentPart{
		providertypes.NewTextPart("look"),
		providertypes.NewRemoteImagePart("https://example.com/a.png"),
	}

	got := RenderDisplayParts(parts)
	want := "look[Image]"
	if got != want {
		t.Fatalf("RenderDisplayParts() = %q, want %q", got, want)
	}
}

func TestRenderPartsFallbackForUnknownImageSource(t *testing.T) {
	t.Parallel()

	parts := []providertypes.ContentPart{
		{
			Kind: providertypes.ContentPartImage,
			Image: &providertypes.ImagePart{
				SourceType: providertypes.ImageSourceType("unknown"),
			},
		},
	}

	if got := RenderDisplayParts(parts); got != "[Image]" {
		t.Fatalf("RenderDisplayParts() fallback = %q, want %q", got, "[Image]")
	}
}

func TestRenderCompactPromptPartsImageFallbackBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		parts []providertypes.ContentPart
		want  string
	}{
		{
			name: "nil image payload",
			parts: []providertypes.ContentPart{{
				Kind:  providertypes.ContentPartImage,
				Image: nil,
			}},
			want: "[Image]",
		},
		{
			name: "remote image with empty url",
			parts: []providertypes.ContentPart{{
				Kind: providertypes.ContentPartImage,
				Image: &providertypes.ImagePart{
					SourceType: providertypes.ImageSourceRemote,
					URL:        "  ",
				},
			}},
			want: "[Image]",
		},
		{
			name: "session asset without asset payload",
			parts: []providertypes.ContentPart{{
				Kind: providertypes.ContentPartImage,
				Image: &providertypes.ImagePart{
					SourceType: providertypes.ImageSourceSessionAsset,
					Asset:      nil,
				},
			}},
			want: "[Image]",
		},
		{
			name: "session asset without id",
			parts: []providertypes.ContentPart{{
				Kind: providertypes.ContentPartImage,
				Image: &providertypes.ImagePart{
					SourceType: providertypes.ImageSourceSessionAsset,
					Asset:      &providertypes.AssetRef{ID: " ", MimeType: "image/png"},
				},
			}},
			want: "[Image]",
		},
		{
			name: "session asset without mime",
			parts: []providertypes.ContentPart{{
				Kind: providertypes.ContentPartImage,
				Image: &providertypes.ImagePart{
					SourceType: providertypes.ImageSourceSessionAsset,
					Asset:      &providertypes.AssetRef{ID: "asset-3", MimeType: ""},
				},
			}},
			want: "[Image:session_asset] asset-3",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := RenderCompactPromptParts(tc.parts); got != tc.want {
				t.Fatalf("RenderCompactPromptParts() = %q, want %q", got, tc.want)
			}
		})
	}
}
