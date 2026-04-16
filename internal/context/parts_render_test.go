package context

import (
	"testing"

	providertypes "neo-code/internal/provider/types"
)

func TestRenderCompactPromptPartsIncludesImageSourceDetails(t *testing.T) {
	t.Parallel()

	parts := []providertypes.ContentPart{
		providertypes.NewTextPart("before"),
		providertypes.NewRemoteImagePart("https://example.com/a.png"),
		providertypes.NewSessionAssetImagePart("asset-1", "image/png"),
	}
	got := renderCompactPromptParts(parts)
	want := "before[Image:remote] https://example.com/a.png[Image:session_asset] asset-1 (image/png)"
	if got != want {
		t.Fatalf("renderCompactPromptParts() = %q, want %q", got, want)
	}
}

func TestRenderDisplayPartsUsesSafeImagePlaceholder(t *testing.T) {
	t.Parallel()

	parts := []providertypes.ContentPart{
		providertypes.NewTextPart("look"),
		providertypes.NewRemoteImagePart("https://example.com/a.png"),
	}
	if got := renderDisplayParts(parts); got != "look[Image]" {
		t.Fatalf("renderDisplayParts() = %q, want %q", got, "look[Image]")
	}
}

func TestHasRenderablePartsTreatsImageAsMeaningfulInput(t *testing.T) {
	t.Parallel()

	if hasRenderableParts([]providertypes.ContentPart{providertypes.NewTextPart("   ")}) {
		t.Fatalf("blank text part should not be renderable")
	}
	if !hasRenderableParts([]providertypes.ContentPart{providertypes.NewSessionAssetImagePart("asset-1", "image/jpeg")}) {
		t.Fatalf("image part should be renderable")
	}
}
