package infra

import (
	"errors"
	"os"
	"strings"
)

var errClipboardImageUnsupported = errors.New("clipboard image is not supported on this platform")

func SaveImageToTempFile(data []byte, prefix string) (string, error) {
	pattern := "image-*.png"
	if cleaned := sanitizeTempPrefix(prefix); cleaned != "" {
		pattern = cleaned + "-*.png"
	}

	tempDir := strings.TrimSpace(os.Getenv("TMPDIR"))
	f, err := os.CreateTemp(tempDir, pattern)
	if err != nil {
		return "", err
	}
	tmpFile := f.Name()
	_ = f.Close()
	if err = os.WriteFile(tmpFile, data, 0o600); err != nil {
		_ = os.Remove(tmpFile)
		return "", err
	}

	return tmpFile, nil
}

// sanitizeTempPrefix 过滤临时文件名前缀中的不安全字符，避免路径注入与非法命名。
func sanitizeTempPrefix(prefix string) string {
	if prefix == "" {
		return ""
	}

	buf := make([]rune, 0, len(prefix))
	for _, r := range prefix {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			buf = append(buf, r)
		}
	}
	return string(buf)
}
