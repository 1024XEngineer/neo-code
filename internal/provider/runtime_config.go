package provider

// RuntimeConfig 表示 provider 构建与模型发现使用的最小运行时输入。
type RuntimeConfig struct {
	Name           string
	Driver         string
	BaseURL        string
	DefaultModel   string
	APIKey         string
	APIStyle       string
	DeploymentMode string
	APIVersion     string
}
