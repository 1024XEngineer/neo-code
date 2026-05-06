//go:build webembed

package webassets

import (
	"io/fs"
	"neo-code/web"
)

// FS provides access to the embedded web frontend assets.
// Available only when built with -tags webembed.
// The "dist/" prefix is stripped so paths match what the static file handler expects (e.g. "index.html").
var FS fs.FS

func init() {
	if sub, err := fs.Sub(web.DistFS, "dist"); err == nil {
		FS = sub
	}
}

// IsAvailable reports whether embedded web assets are compiled into the binary.
func IsAvailable() bool {
	return FS != nil
}
