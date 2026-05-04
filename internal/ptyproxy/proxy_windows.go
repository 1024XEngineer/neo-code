//go:build windows

package ptyproxy

import (
	"context"
	"errors"
)

var errUnsupportedPlatform = errors.New("ptyproxy: manual shell mode is only supported on unix-like systems")

// RunManualShell 在 Windows 平台返回明确不支持错误，避免误判为静默失败。
func RunManualShell(context.Context, ManualShellOptions) error {
	return errUnsupportedPlatform
}

// SendDiagnoseSignal 在 Windows 平台返回明确不支持错误。
func SendDiagnoseSignal(context.Context, string) error {
	return errUnsupportedPlatform
}

// SendAutoModeSignal 在 Windows 平台返回明确不支持错误。
func SendAutoModeSignal(context.Context, string, bool) error {
	return errUnsupportedPlatform
}

// QueryAutoMode 在 Windows 平台返回明确不支持错误。
func QueryAutoMode(context.Context, string) (bool, error) {
	return false, errUnsupportedPlatform
}
