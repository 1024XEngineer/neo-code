package domain

type SecurityChecker interface {
	Check(toolType string, target string) Action
}

type Action string

const (
	ActionDeny  Action = "deny"
	ActionAllow Action = "allow"
	ActionAsk   Action = "ask"
)

type Rule struct {
	Target  string `yaml:"target,omitempty"`
	Command string `yaml:"command,omitempty"`
	Domain  string `yaml:"domain,omitempty"`
	R       string `yaml:"r,omitempty"`
	W       string `yaml:"w,omitempty"`
	X       string `yaml:"x,omitempty"`
	N       string `yaml:"n,omitempty"`
}

type Config struct {
	Rules []Rule `yaml:"rules"`
}

type SecurityConfigRepository interface {
	LoadAll(configDir string) (*Config, *Config, *Config, error)
}
