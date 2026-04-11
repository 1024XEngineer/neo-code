//go:build ignore
// +build ignore
// 说明：本文件为架构契约定义，仅用于文档与校验，不参与生产编译。

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

// SummaryInput 是摘要规范化输入。
type SummaryInput struct {
	// MaxChars 是摘要最大字符限制。
	MaxChars int
	// Text 是待处理文本。
	Text string
}

// PathNormalizeInput 是路径规范化输入。
type PathNormalizeInput struct {
	// Base 是基准目录。
	Base string
	// Target 是待规范化路径。
	Target string
	// AllowedRoots 是允许访问的根目录集合。
	AllowedRoots []string
}

// PathNormalizeResult 是路径规范化输出。
type PathNormalizeResult struct {
	// NormalizedPath 是规范化后的路径。
	NormalizedPath string
	// IsInsideAllowedRoots 表示规范化结果是否在允许根目录内。
	IsInsideAllowedRoots bool
}

// TokenEstimator 定义 token 估算契约。
type TokenEstimator interface {
	// Estimate 估算输入文本 token 数量。
	// 职责：为上下文预算检查与展示统计提供稳定估算能力。
	// 输入语义：input 指定目标模型与待估算文本。
	// 并发约束：实现必须支持并发调用且无共享可变状态竞争。
	// 生命周期：在预构建预算检查、compact 决策、用量展示阶段调用。
	// 错误语义：返回模型不支持、输入非法或估算失败错误。
	Estimate(ctx context.Context, input TokenEstimateInput) (TokenEstimateResult, error)
}

// SummaryHelper 定义摘要规范化契约。
type SummaryHelper interface {
	// NormalizeSummary 对摘要文本进行结构收敛。
	// 职责：把摘要收敛到可回灌、可存储、可审计的稳定结构。
	// 输入语义：input.MaxChars 表示输出上限，input.Text 为待处理文本。
	// 并发约束：实现必须支持并发调用并避免全局状态污染。
	// 生命周期：在 compact 前后、会话摘要写入前调用。
	// 错误语义：返回摘要结构非法、无法收敛或参数非法错误。
	NormalizeSummary(ctx context.Context, input SummaryInput) (string, error)
}

// TextLimiter 定义文本截断契约。
type TextLimiter interface {
	// Truncate 将文本裁剪到给定上限。
	// 职责：限制长文本进入上下文、事件载荷与日志。
	// 输入语义：text 为原文，maxChars 为字符上限。
	// 并发约束：纯函数实现，线程安全。
	// 生命周期：工具输出收敛、事件输出收敛、摘要前置处理阶段调用。
	// 错误语义：该接口不返回错误，调用方通过返回值判断截断结果。
	Truncate(text string, maxChars int) string
}

// IDGenerator 定义标识生成契约。
type IDGenerator interface {
	// NewID 生成带前缀的唯一标识。
	// 职责：统一 run/session/transcript/request 等标识生成规则。
	// 输入语义：prefix 是业务前缀，例如 run、session、req。
	// 并发约束：并发生成时仍需保证冲突概率可接受。
	// 生命周期：创建会话、运行、请求、转录记录时调用。
	// 错误语义：返回随机源失败或参数非法错误。
	NewID(prefix string) (string, error)
}

// Clock 定义时间抽象契约。
type Clock interface {
	// NowUnix 返回当前 Unix 秒时间戳。
	// 职责：提供统一可替换时间源，便于测试与重放。
	// 输入语义：无输入参数。
	// 并发约束：必须线程安全。
	// 生命周期：事件时间戳、会话更新时间、排序键生成阶段调用。
	// 错误语义：该接口不返回错误。
	NowUnix() int64
}

// PathNormalizer 定义路径规范化契约。
type PathNormalizer interface {
	// Normalize 规范化路径并执行允许根目录判定。
	// 职责：统一路径清洗、越界防护与平台差异消解。
	// 输入语义：input.Base 为基准目录，input.Target 为目标路径，input.AllowedRoots 为访问边界。
	// 并发约束：实现必须支持并发调用。
	// 生命周期：工作目录切换、工具参数解析、安全校验阶段调用。
	// 错误语义：返回路径不存在、越界访问或格式非法错误。
	Normalize(ctx context.Context, input PathNormalizeInput) (PathNormalizeResult, error)
}

// PayloadCodec 定义轻量序列化契约。
type PayloadCodec interface {
	// Marshal 将结构化负载编码为字节序列。
	// 职责：为事件传输、快照写入和调试输出提供统一编码入口。
	// 输入语义：value 为待编码对象。
	// 并发约束：实现必须线程安全。
	// 生命周期：事件发送、持久化与跨层调试阶段调用。
	// 错误语义：返回编码失败或不支持类型错误。
	Marshal(value any) ([]byte, error)
	// Unmarshal 将字节序列解码到目标对象。
	// 职责：恢复事件负载或外部输入结构。
	// 输入语义：data 为原始字节，target 为解码目标指针。
	// 并发约束：实现必须线程安全。
	// 生命周期：事件消费、快照恢复、协议解析阶段调用。
	// 错误语义：返回解码失败、类型不匹配或目标非法错误。
	Unmarshal(data []byte, target any) error
}

// Registry 定义 utils 模块唯一主契约。
type Registry interface {
	// Token 返回 token 估算能力。
	// 职责：提供统一 token 估算入口。
	// 输入语义：无输入参数，返回已装配的能力实现。
	// 并发约束：返回值应可并发安全复用。
	// 生命周期：运行时初始化后全生命周期可用。
	// 错误语义：该方法不返回错误，初始化失败应在构建阶段暴露。
	Token() TokenEstimator
	// Summary 返回摘要规范化能力。
	// 职责：提供统一摘要处理入口。
	// 输入语义：无输入参数，返回已装配能力。
	// 并发约束：返回值应支持并发调用。
	// 生命周期：全生命周期可复用。
	// 错误语义：方法本身不返回错误。
	Summary() SummaryHelper
	// Text 返回文本截断能力。
	// 职责：提供统一文本收敛入口。
	// 输入语义：无输入参数。
	// 并发约束：返回值应支持并发调用。
	// 生命周期：全生命周期可复用。
	// 错误语义：方法本身不返回错误。
	Text() TextLimiter
	// IDs 返回标识生成能力。
	// 职责：提供统一 ID 生成入口。
	// 输入语义：无输入参数。
	// 并发约束：返回值应支持并发调用。
	// 生命周期：全生命周期可复用。
	// 错误语义：方法本身不返回错误。
	IDs() IDGenerator
	// Clock 返回时间抽象能力。
	// 职责：提供统一时间源入口。
	// 输入语义：无输入参数。
	// 并发约束：返回值应支持并发调用。
	// 生命周期：全生命周期可复用。
	// 错误语义：方法本身不返回错误。
	Clock() Clock
	// Paths 返回路径规范化能力。
	// 职责：提供统一路径规范化与边界校验入口。
	// 输入语义：无输入参数。
	// 并发约束：返回值应支持并发调用。
	// 生命周期：全生命周期可复用。
	// 错误语义：方法本身不返回错误。
	Paths() PathNormalizer
	// Codec 返回轻量序列化能力。
	// 职责：提供统一负载编码解码入口。
	// 输入语义：无输入参数。
	// 并发约束：返回值应支持并发调用。
	// 生命周期：全生命周期可复用。
	// 错误语义：方法本身不返回错误。
	Codec() PayloadCodec
}