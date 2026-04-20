package tui

import "regexp"

type copyCodeButtonBinding struct {
	ID   int
	Code string
}

var copyCodeANSIPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
