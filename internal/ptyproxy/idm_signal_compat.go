package ptyproxy

import "os"

// isInterruptSignal 判断输入信号是否应视为 IDM 中断信号。
func isInterruptSignal(signalValue os.Signal) bool {
	return signalValue == os.Interrupt
}

// interruptSignal 返回 IDM 主动触发中断时使用的标准信号值。
func interruptSignal() os.Signal {
	return os.Interrupt
}
