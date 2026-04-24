# Compatibility Fallback Lifecycle

## 为什么 fallback 只能短期存在
- completion-only 旧路径会绕过 verifier gate，破坏双门控与单一裁决层。
- 长期保留会形成双真源，增加状态不一致风险。

## 触发条件
- `runtime.verification.enabled=false` 时进入兼容路径。

## 事件与日志要求
- 兼容路径必须输出结构化 stop reason：`compatibility_fallback`。
- acceptance 事件中保留内部摘要，标记这是 fallback 行为。

## 移除条件
- 验收与 verifier 在主链路稳定后，移除 `enabled=false` 作为常规运行路径。
- 仅保留灰度发布窗口内的短期开关。

## 禁止长期双轨原因
- 终态语义会被分裂为“old completion-only”与“new dual-gate”两套规则。
- TUI / runtime / persistence 难以保证统一解释，容易引发回归。

