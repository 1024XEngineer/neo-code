package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// AppConfiguration 应用配置结构
type AppConfiguration struct {
	App struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	} `yaml:"app"`

	AI struct {
		Provider string `yaml:"provider"`
		APIKey   string `yaml:"api_key"`
		Model    string `yaml:"model"`
	} `yaml:"ai"`

	// 嵌入模型配置
	Embedding struct {
		Provider string `yaml:"provider"`
		APIKey   string `yaml:"api_key"`
		Model    string `yaml:"model"`
	} `yaml:"embedding"`

	// 内存配置
	Memory struct {
		FilePath string  `yaml:"file_path"`
		TopK     int     `yaml:"top_k"`
		MinScore float64 `yaml:"min_score"`
		MaxItems int     `yaml:"max_items"`
	} `yaml:"memory"`

	History struct {
		ShortTermTurns int `yaml:"short_term_turns"`
	} `yaml:"history"`

	Persona struct {
		FilePath string `yaml:"file_path"`
	} `yaml:"persona"`
}

// GlobalAppConfig 全局应用配置
var GlobalAppConfig *AppConfiguration

// LoadAppConfig 从YAML文件加载应用配置
func LoadAppConfig(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read app config file: %w", err)
	}

	config := &AppConfiguration{}
	if err := yaml.Unmarshal(data, config); err != nil {
		return fmt.Errorf("failed to parse app config YAML: %w", err)
	}

	GlobalAppConfig = config
	return nil
}
