package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ModelConfig 定义模型配置结构
type ModelConfig struct {
	Chat      ChatConfig      `yaml:"chat"`
	Embedding EmbeddingConfig `yaml:"embedding"`
}

// ChatConfig 定义聊天模型配置
type ChatConfig struct {
	DefaultModel string        `yaml:"default_model"`
	Models       []ModelDetail `yaml:"models"`
}

// EmbeddingConfig 定义嵌入模型配置
type EmbeddingConfig struct {
	DefaultModel string        `yaml:"default_model"`
	Models       []ModelDetail `yaml:"models"`
}

// ModelDetail 定义单个模型详情
type ModelDetail struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

// GlobalModelConfig 全局模型配置
var GlobalModelConfig *ModelConfig

// LoadModelConfig 从YAML文件加载模型配置
func LoadModelConfig(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read model config file: %w", err)
	}

	config := &ModelConfig{}
	if err := yaml.Unmarshal(data, config); err != nil {
		return fmt.Errorf("failed to parse model config YAML: %w", err)
	}

	GlobalModelConfig = config
	return nil
}

// GetChatModelURL 根据模型名称获取聊天模型URL
func GetChatModelURL(modelName string) (string, bool) {
	if GlobalModelConfig == nil {
		return "", false
	}

	for _, model := range GlobalModelConfig.Chat.Models {
		if model.Name == modelName {
			return model.URL, true
		}
	}
	return "", false
}

// GetEmbeddingModelURL 根据模型名称获取嵌入模型URL
func GetEmbeddingModelURL(modelName string) (string, bool) {
	if GlobalModelConfig == nil {
		return "", false
	}

	for _, model := range GlobalModelConfig.Embedding.Models {
		if model.Name == modelName {
			return model.URL, true
		}
	}
	return "", false
}

// GetDefaultChatModel 获取默认聊天模型
func GetDefaultChatModel() string {
	if GlobalModelConfig == nil {
		return ""
	}
	return GlobalModelConfig.Chat.DefaultModel
}

// GetDefaultEmbeddingModel 获取默认嵌入模型
func GetDefaultEmbeddingModel() string {
	if GlobalModelConfig == nil {
		return ""
	}
	return GlobalModelConfig.Embedding.DefaultModel
}
