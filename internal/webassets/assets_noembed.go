//go:build !webembed

package webassets

import "io/fs"

// FS is nil when the webembed build tag is not set.
var FS fs.FS = nil

// IsAvailable reports whether embedded web assets are compiled into the binary.
func IsAvailable() bool {
	return false
}
