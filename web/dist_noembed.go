//go:build !webembed

package web

import "embed"

// DistFS is empty when the webembed build tag is not set.
// Use webassets.IsAvailable() to check before serving.
var DistFS embed.FS
