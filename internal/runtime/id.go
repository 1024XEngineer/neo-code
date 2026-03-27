package runtime

import (
	"crypto/rand"
	"encoding/hex"
)

func newID(prefix string) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return prefix + "_" + hex.EncodeToString(buf)
}
