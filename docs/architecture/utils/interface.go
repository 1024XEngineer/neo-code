//go:build ignore
// +build ignore

package utils

import "context"

// TokenEstimateInput 是 token 估算输入。
type TokenEstimateInput struct {
	// Model 是目标模型标识。
	Model string
	// Text 是待估算文本。
	Text string
}

// TokenEstimateResult 是 token 估算输出。
type TokenEstimateResult struct {
	// Tokens 是估算 token 数量。
	Tokens int
}

// TokenEstimator [PROPOSED] 是 token 估算契约。
type TokenEstimator interface {
	// Estimate 估算输入文本 token 数量。
	// 输入语义：input 提供模型与文本。
	// 并发约束：应支持并发调用。
	// 生命周期：在上下文预算检查或展示统计时调用。
	// 错误语义：返回模型不支持、输入非法或估算失败错误。
	Estimate(ctx context.Context, input TokenEstimateInput) (TokenEstimateResult, error)
}

// SummaryInput 是摘要辅助输入。
type SummaryInput struct {
	// MaxChars 是摘要最大字符限制。
	MaxChars int
	// Text 是待处理文本。
	Text string
}

// SummaryHelper [PROPOSED] 是摘要与收敛辅助契约。
type SummaryHelper interface {
	// NormalizeSummary 规范化摘要结构。
	// 输入语义：input 为摘要文本与限制。
	// 并发约束：纯函数实现应支持并发。
	// 生命周期：compact 前后摘要处理阶段调用。
	// 错误语义：返回摘要结构不合法或无法收敛错误。
	NormalizeSummary(ctx context.Context, input SummaryInput) (string, error)
}

// TextLimiter [PROPOSED] 是文本截断辅助契约。
type TextLimiter interface {
	// Truncate 将文本裁剪到上限。
	// 输入语义：text 为原文，maxChars 为字符上限。
	// 并发约束：纯函数且线程安全。
	// 生命周期：工具输出与事件 payload 收敛阶段调用。
	// 错误语义：无错误返回，返回裁剪后文本。
	Truncate(text string, maxChars int) string
}

// IDGenerator [PROPOSED] 是标识生成契约。
type IDGenerator interface {
	// NewID 生成带前缀的唯一标识。
	// 输入语义：prefix 为业务前缀，如 run/session。
	// 并发约束：并发调用仍需保证冲突概率可接受。
	// 生命周期：创建会话、运行、transcript 等标识时调用。
	// 错误语义：返回随机源失败或参数非法错误。
	NewID(prefix string) (string, error)
}

// Clock [PROPOSED] 是时间抽象契约。
type Clock interface {
	// Now 返回当前时间。
	// 输入语义：无。
	// 并发约束：线程安全。
	// 生命周期：时间戳写入与排序比较场景调用。
	// 错误语义：无错误返回。
	Now() int64
}

// PathNormalizer [PROPOSED] 是路径规范化契约。
type PathNormalizer interface {
	// Normalize 将输入路径规范化为绝对或项目内相对路径。
	// 输入语义：base 为基准目录，target 为待规范化路径。
	// 并发约束：纯函数且线程安全。
	// 生命周期：会话 workdir、工具参数、安全校验前调用。
	// 错误语义：返回路径不存在、越界或格式非法错误。
	Normalize(base string, target string) (string, error)
}

// RuntimeUtils [PROPOSED] 是运行时通用辅助门面。
type RuntimeUtils interface {
	// Token 返回 token 估算器。
	Token() TokenEstimator
	// Summary 返回摘要辅助器。
	Summary() SummaryHelper
	// Text 返回文本截断器。
	Text() TextLimiter
	// IDs 返回标识生成器。
	IDs() IDGenerator
	// Paths 返回路径规范化器。
	Paths() PathNormalizer
}
