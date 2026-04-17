package tools

// ContentSummarizer 将工具结果内容压缩为短摘要，用于 micro-compact 替换旧工具输出。
// content 和 metadata 来自持久化后的 Message 字段，isError 标识原始工具是否报错。
// 返回空字符串表示"无摘要，回退到默认清除行为"。
type ContentSummarizer func(content string, metadata map[string]string, isError bool) string
