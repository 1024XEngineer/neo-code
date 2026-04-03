//go:build !windows

package app

func platformSetConsoleOutputCodePage(codePage uint32) error {
	return nil
}

func platformSetConsoleInputCodePage(codePage uint32) error {
	return nil
}
