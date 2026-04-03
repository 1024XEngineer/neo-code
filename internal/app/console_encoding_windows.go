//go:build windows

package app

import "golang.org/x/sys/windows"

func platformSetConsoleOutputCodePage(codePage uint32) error {
	return windows.SetConsoleOutputCP(codePage)
}

func platformSetConsoleInputCodePage(codePage uint32) error {
	return windows.SetConsoleCP(codePage)
}
