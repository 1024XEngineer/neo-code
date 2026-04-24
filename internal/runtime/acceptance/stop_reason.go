package acceptance

import "neo-code/internal/runtime/controlplane"

// StopReason 复用控制面统一停止原因枚举，避免 acceptance 层引入平行真源。
type StopReason = controlplane.StopReason
