package state

import "time"

// 这些常量定义了输入框与粘贴检测在 Update 流程中的基础行为阈值。
const (
	ComposerMinHeight   = 1
	ComposerMaxHeight   = 10
	ComposerPromptWidth = 2
	MouseWheelStepLines = 3
	PasteBurstWindow    = 120 * time.Millisecond
	PasteEnterGuard     = 180 * time.Millisecond
	PasteSessionGuard   = 5 * time.Second
	PasteBurstThreshold = 12
)
