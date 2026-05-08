//go:build windows

package ptyproxy

import (
	"strings"
	"time"
)

const diagnoseCallTimeout = windowsDiagCallTimeout

var proxyOutputLineEndingNormalizer = strings.NewReplacer(
	"\r\n", "\r\n",
	"\r", "\r\n",
	"\n", "\r\n",
)

const windowsProcessStillActive = uint32(259)

func normalizeDurationFloor(value time.Duration, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}
