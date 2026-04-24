package acceptance

import "neo-code/internal/runtime/controlplane"

// TerminalStatusFromAcceptance 将 acceptance 决策映射到 runtime 终态枚举。
func TerminalStatusFromAcceptance(status AcceptanceStatus) controlplane.TerminalStatus {
	switch status {
	case AcceptanceAccepted:
		return controlplane.TerminalStatusCompleted
	case AcceptanceFailed:
		return controlplane.TerminalStatusFailed
	case AcceptanceIncomplete:
		return controlplane.TerminalStatusIncomplete
	default:
		return controlplane.TerminalStatusContinue
	}
}
