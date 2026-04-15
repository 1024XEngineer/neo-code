package infra

import (
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"strings"
)

var supportedImageMimes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
	"image/gif":  true,
}

func GetFileInfo(path string) (fs.FileInfo, error) {
	return os.Stat(path)
}

func DetectImageMimeType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	}

	detected := mime.TypeByExtension(ext)
	if detected != "" {
		return detected
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	if len(data) >= 8 {
		if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
			return "image/png"
		}
		if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
			return "image/jpeg"
		}
		if len(data) >= 12 {
			if string(data[0:4]) == "GIF8" {
				return "image/gif"
			}
			if string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
				return "image/webp"
			}
		}
	}

	return ""
}

func IsSupportedImageFormat(path string) bool {
	mimeType := DetectImageMimeType(path)
	return supportedImageMimes[mimeType]
}

func ReadImageFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
