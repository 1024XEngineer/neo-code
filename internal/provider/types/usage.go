package types

// Usage 记录本次请求的 token 使用统计。
type Usage struct {
	InputTokens    int  `json:"input_tokens"`
	OutputTokens   int  `json:"output_tokens"`
	TotalTokens    int  `json:"total_tokens"`
	InputObserved  bool `json:"input_observed"`
	OutputObserved bool `json:"output_observed"`
	// CacheReadInputTokens 记录命中 prompt 缓存读取的输入 token 数。
	// Anthropic 的 cache_read_input_tokens、OpenAI 的 cached_tokens 均映射到此字段。
	CacheReadInputTokens int `json:"cache_read_input_tokens,omitempty"`
	// CacheWriteInputTokens 记录写入 prompt 缓存的输入 token 数，仅 Anthropic 显式缓存有该口径。
	CacheWriteInputTokens int `json:"cache_write_input_tokens,omitempty"`
}

// TotalInputTokens 返回计入预算的总输入 token，包含缓存命中与缓存写入部分。
// Anthropic 的 input_tokens 不含缓存相关计数，预算与 compact 必须使用本方法以免低估。
func (u Usage) TotalInputTokens() int {
	return u.InputTokens + u.CacheReadInputTokens + u.CacheWriteInputTokens
}

// CacheHitRate 返回缓存命中的输入占比，无可统计输入时返回 0。
func (u Usage) CacheHitRate() float64 {
	total := u.TotalInputTokens()
	if total <= 0 {
		return 0
	}
	return float64(u.CacheReadInputTokens) / float64(total)
}

// BudgetEstimate 描述 provider 对冻结请求输入 token 的估算结果。
type BudgetEstimate struct {
	EstimatedInputTokens int    `json:"estimated_input_tokens"`
	EstimateSource       string `json:"estimate_source"`
	GatePolicy           string `json:"gate_policy"`
}
