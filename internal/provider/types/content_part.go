package types

import (
	"errors"
	"strings"
)

// ContentPartKind defines the type of a content part.
type ContentPartKind string

const (
	// ContentPartText represents a text part.
	ContentPartText ContentPartKind = "text"
	// ContentPartImage represents an image part.
	ContentPartImage ContentPartKind = "image"
)

// ImageSourceType defines the source of an image.
type ImageSourceType string

const (
	// ImageSourceRemote indicates the image is from a remote URL.
	ImageSourceRemote ImageSourceType = "remote"
	// ImageSourceSessionAsset indicates the image is stored locally in the session.
	ImageSourceSessionAsset ImageSourceType = "session_asset"
)

// AssetRef references a locally stored asset.
type AssetRef struct {
	ID       string `json:"id"`
	MimeType string `json:"mime_type"`
}

// ImagePart contains the payload for an image content part.
type ImagePart struct {
	SourceType ImageSourceType `json:"source_type"`
	URL        string          `json:"url,omitempty"`
	Asset      *AssetRef       `json:"asset,omitempty"`
}

// ContentPart represents a single piece of multi-modal content.
type ContentPart struct {
	Kind  ContentPartKind `json:"kind"`
	Text  string          `json:"text,omitempty"`
	Image *ImagePart      `json:"image,omitempty"`
}

// NewTextPart creates a new text content part.
func NewTextPart(text string) ContentPart {
	return ContentPart{
		Kind: ContentPartText,
		Text: text,
	}
}

// NewRemoteImagePart creates a new image content part from a remote URL.
func NewRemoteImagePart(url string) ContentPart {
	return ContentPart{
		Kind: ContentPartImage,
		Image: &ImagePart{
			SourceType: ImageSourceRemote,
			URL:        url,
		},
	}
}

// NewSessionAssetImagePart creates a new image content part from a session asset.
func NewSessionAssetImagePart(id, mimeType string) ContentPart {
	return ContentPart{
		Kind: ContentPartImage,
		Image: &ImagePart{
			SourceType: ImageSourceSessionAsset,
			Asset: &AssetRef{
				ID:       id,
				MimeType: mimeType,
			},
		},
	}
}

// ValidateParts checks if the given parts are valid.
func ValidateParts(parts []ContentPart) error {
	for _, part := range parts {
		switch part.Kind {
		case ContentPartText:
			if part.Image != nil {
				return errors.New("text part cannot contain image payload")
			}
		case ContentPartImage:
			image := part.Image
			if image == nil {
				return errors.New("image part must contain image payload")
			}

			switch image.SourceType {
			case ImageSourceRemote:
				if strings.TrimSpace(image.URL) == "" {
					return errors.New("remote image part must contain url")
				}
			case ImageSourceSessionAsset:
				if image.Asset == nil || image.Asset.ID == "" {
					return errors.New("session asset image part must contain asset ID")
				}
			default:
				return errors.New("image part contains unsupported source type")
			}
		default:
			return errors.New("unknown content part kind")
		}
	}

	return nil
}

// CloneParts creates a deep copy of a slice of ContentPart.
func CloneParts(parts []ContentPart) []ContentPart {
	if parts == nil {
		return nil
	}
	res := make([]ContentPart, len(parts))
	for i, p := range parts {
		clone := p
		if p.Image != nil {
			imgClone := *p.Image
			if p.Image.Asset != nil {
				assetClone := *p.Image.Asset
				imgClone.Asset = &assetClone
			}
			clone.Image = &imgClone
		}
		res[i] = clone
	}
	return res
}
