package ptyproxy

import (
	"strings"
)

var autoTriggerCommandExemptions = map[string]struct{}{
	"grep":  {},
	"find":  {},
	"test":  {},
	"false": {},
}

// ShouldTriggerAutoDiagnosis 根据退出码、命令词和输出质量判断是否应进入自动诊断。
func ShouldTriggerAutoDiagnosis(exitCode int, commandText string, outputText string) bool {
	if exitCode == 0 {
		return false
	}
	if exitCode == 130 || exitCode == 137 {
		return false
	}
	if isCommandExempted(commandText) {
		return false
	}
	return hasMeaningfulOutput(outputText)
}

// isCommandExempted 判断命令首词是否处于本地豁免名单。
func isCommandExempted(commandText string) bool {
	trimmed := strings.TrimSpace(commandText)
	if trimmed == "" {
		return false
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}
	verb := strings.ToLower(strings.TrimSpace(fields[0]))
	_, exempted := autoTriggerCommandExemptions[verb]
	return exempted
}

// hasMeaningfulOutput 判断输出是否包含足够有效的报错信息，避免空输出误触发。
func hasMeaningfulOutput(outputText string) bool {
	trimmed := strings.TrimSpace(outputText)
	if len(trimmed) < 12 {
		return false
	}
	lower := strings.ToLower(trimmed)
	keywords := []string{
		"error",
		"failed",
		"fatal",
		"exception",
		"denied",
		"not found",
		"no such file",
		"timeout",
	}
	for _, keyword := range keywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	// 关键字缺失时，只要输出长度足够大也认为有排查价值。
	return len(lower) >= 48
}
