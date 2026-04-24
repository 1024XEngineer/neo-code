package acceptance

// Config 定义 acceptance 引擎运行期开关。
type Config struct {
	// CompatibilityFallbackEnabled 表示是否允许 verification 关闭时走兼容收尾路径。
	CompatibilityFallbackEnabled bool
}
