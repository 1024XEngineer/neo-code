//go:build webembed

package web

import "embed"

//go:embed all:dist
var DistFS embed.FS
