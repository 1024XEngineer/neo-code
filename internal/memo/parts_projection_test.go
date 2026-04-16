package memo

import (
	"testing"

	providertypes "neo-code/internal/provider/types"
)

func TestRenderMemoPartsUsesImagePlaceholder(t *testing.T) {
	t.Parallel()

	parts := []providertypes.ContentPart{
		providertypes.NewTextPart("note:"),
		providertypes.NewRemoteImagePart("https://example.com/img.png"),
	}
	if got := renderMemoParts(parts); got != "note:[Image]" {
		t.Fatalf("renderMemoParts() = %q, want %q", got, "note:[Image]")
	}
}

func TestHasMemoRelevantUserInputRequiresNonEmptyText(t *testing.T) {
	t.Parallel()

	if hasMemoRelevantUserInput([]providertypes.ContentPart{providertypes.NewTextPart("  ")}) {
		t.Fatalf("blank text should not be treated as meaningful input")
	}
	if hasMemoRelevantUserInput([]providertypes.ContentPart{providertypes.NewRemoteImagePart("https://example.com/img.png")}) {
		t.Fatalf("image-only input should not trigger memo extraction")
	}
	if !hasMemoRelevantUserInput([]providertypes.ContentPart{
		providertypes.NewRemoteImagePart("https://example.com/img.png"),
		providertypes.NewTextPart("caption"),
	}) {
		t.Fatalf("non-empty text should be treated as meaningful user input")
	}
}
