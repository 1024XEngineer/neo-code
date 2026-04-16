package types

import (
	"testing"
)

func TestNewTextPart(t *testing.T) {
	part := NewTextPart("hello")
	if part.Kind != ContentPartText {
		t.Errorf("expected kind %s, got %s", ContentPartText, part.Kind)
	}
	if part.Text != "hello" {
		t.Errorf("expected text %s, got %s", "hello", part.Text)
	}
}

func TestNewRemoteImagePart(t *testing.T) {
	part := NewRemoteImagePart("https://example.com/image.png")
	if part.Kind != ContentPartImage {
		t.Errorf("expected kind %s, got %s", ContentPartImage, part.Kind)
	}
	if part.Image == nil || part.Image.SourceType != ImageSourceRemote || part.Image.URL != "https://example.com/image.png" {
		t.Errorf("invalid image part: %+v", part)
	}
}

func TestNewSessionAssetImagePart(t *testing.T) {
	part := NewSessionAssetImagePart("asset-1", "image/jpeg")
	if part.Kind != ContentPartImage {
		t.Errorf("expected kind %s, got %s", ContentPartImage, part.Kind)
	}
	if part.Image == nil || part.Image.SourceType != ImageSourceSessionAsset || part.Image.Asset == nil || part.Image.Asset.ID != "asset-1" || part.Image.Asset.MimeType != "image/jpeg" {
		t.Errorf("invalid session asset image part: %+v", part)
	}
}

func TestValidateParts(t *testing.T) {
	tests := []struct {
		name    string
		parts   []ContentPart
		wantErr bool
	}{
		{
			name:    "valid text",
			parts:   []ContentPart{NewTextPart("hello")},
			wantErr: false,
		},
		{
			name:    "text with image payload",
			parts:   []ContentPart{{Kind: ContentPartText, Image: &ImagePart{}}},
			wantErr: true,
		},
		{
			name:    "valid remote image",
			parts:   []ContentPart{NewRemoteImagePart("http://example.com/img.png")},
			wantErr: false,
		},
		{
			name:    "remote image missing url",
			parts:   []ContentPart{{Kind: ContentPartImage, Image: &ImagePart{SourceType: ImageSourceRemote}}},
			wantErr: true,
		},
		{
			name:    "remote image blank url",
			parts:   []ContentPart{{Kind: ContentPartImage, Image: &ImagePart{SourceType: ImageSourceRemote, URL: "   "}}},
			wantErr: true,
		},
		{
			name:    "image missing payload",
			parts:   []ContentPart{{Kind: ContentPartImage}},
			wantErr: true,
		},
		{
			name:    "valid session asset image",
			parts:   []ContentPart{NewSessionAssetImagePart("123", "image/png")},
			wantErr: false,
		},
		{
			name:    "session asset missing asset ID",
			parts:   []ContentPart{{Kind: ContentPartImage, Image: &ImagePart{SourceType: ImageSourceSessionAsset, Asset: &AssetRef{}}}},
			wantErr: true,
		},
		{
			name:    "unsupported image source type",
			parts:   []ContentPart{{Kind: ContentPartImage, Image: &ImagePart{SourceType: ImageSourceType("local_file"), URL: "file://tmp/test.png"}}},
			wantErr: true,
		},
		{
			name:    "unknown kind",
			parts:   []ContentPart{{Kind: "unknown"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateParts(tt.parts)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateParts() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCloneParts(t *testing.T) {
	original := []ContentPart{
		NewTextPart("hello"),
		NewSessionAssetImagePart("asset-123", "image/png"),
	}
	cloned := CloneParts(original)

	if len(cloned) != len(original) {
		t.Fatalf("expected length %d, got %d", len(original), len(cloned))
	}

	cloned[0].Text = "world"
	if original[0].Text == "world" {
		t.Errorf("deep copy failed for text part")
	}

	cloned[1].Image.Asset.ID = "asset-456"
	if original[1].Image.Asset.ID == "asset-456" {
		t.Errorf("deep copy failed for image part asset")
	}
}
