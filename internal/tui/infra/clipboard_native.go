//go:build windows || darwin

package infra

import "golang.design/x/clipboard"

var clipboardInitialized bool

func initClipboard() error {
	if clipboardInitialized {
		return nil
	}
	err := clipboard.Init()
	if err != nil {
		return err
	}
	clipboardInitialized = true
	return nil
}

func CopyText(text string) error {
	if err := initClipboard(); err != nil {
		return err
	}
	clipboard.Write(clipboard.FmtText, []byte(text))
	return nil
}

func ReadClipboardText() (string, error) {
	if err := initClipboard(); err != nil {
		return "", err
	}
	data := clipboard.Read(clipboard.FmtText)
	return string(data), nil
}

func ReadClipboardImage() ([]byte, error) {
	if err := initClipboard(); err != nil {
		return nil, err
	}
	data := clipboard.Read(clipboard.FmtImage)
	if len(data) == 0 {
		return nil, nil
	}
	return data, nil
}
