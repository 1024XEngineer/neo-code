package context

import (
	"path/filepath"
	"strings"

	"neo-code/internal/tools"
)

// defaultPinPatterns 列出关键产物文件的 basename glob 模式，匹配的工具结果不参与微压缩。
var defaultPinPatterns = []string{
	"README*",
	"*.spec.*",
	"*.schema.*",
	"docker-compose*",
	"*migration*",
	"Makefile",
	"go.mod",
	"package.json",
}

// defaultPinToolNames 约束默认 pin 仅对明确修改文件内容的工具生效，避免扩散到读取类或自定义工具。
var defaultPinToolNames = map[string]struct{}{
	tools.ToolNameFilesystemWriteFile: {},
	tools.ToolNameFilesystemEdit:      {},
}

// pinChecker 基于文件路径 glob 模式判断工具结果是否应钉住。
type pinChecker struct {
	patterns []string
}

// NewDefaultPinChecker 返回使用默认钉住模式的 PinChecker。
func NewDefaultPinChecker() MicroCompactPinChecker {
	return &pinChecker{patterns: defaultPinPatterns}
}

// ShouldPin 判断工具结果是否应钉住：从 metadata 中提取文件路径，对 basename 匹配 glob 模式。
func (p *pinChecker) ShouldPin(toolName string, metadata map[string]string) bool {
	if len(metadata) == 0 {
		return false
	}
	if !toolSupportsPinnedRetention(toolName) {
		return false
	}

	path := metadata["relative_path"]
	if path == "" {
		path = metadata["path"]
	}
	if path == "" {
		return false
	}

	base := filepath.Base(path)
	for _, pattern := range p.patterns {
		if matched, _ := filepath.Match(pattern, base); matched {
			return true
		}
	}
	return false
}

// toolSupportsPinnedRetention 判断工具是否允许参与默认 pin 策略，避免非文件修改类工具扩大保留范围。
func toolSupportsPinnedRetention(toolName string) bool {
	_, ok := defaultPinToolNames[strings.TrimSpace(toolName)]
	return ok
}

// isPinnedToolMessage 检查工具消息是否被 pin checker 钉住，被钉住的消息不参与微压缩。
func isPinnedToolMessage(toolName string, metadata map[string]string, checker MicroCompactPinChecker) bool {
	if checker == nil || len(metadata) == 0 {
		return false
	}
	return checker.ShouldPin(toolName, metadata)
}

// toolNameFromCallID 在 toolNames 映射中查找 callID 对应的工具名。
func toolNameFromCallID(callID string, toolNames map[string]string) string {
	return toolNames[strings.TrimSpace(callID)]
}
