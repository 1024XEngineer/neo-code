package repository

import (
	"fmt"
	"os"
	"path/filepath"

	"go-llm-demo/internal/server/domain"

	"gopkg.in/yaml.v3"
)

type SecurityConfigRepository interface {
	LoadBlackList() (*domain.Config, error)
	LoadWhiteList() (*domain.Config, error)
	LoadYellowList() (*domain.Config, error)
	LoadAll(configDir string) (*domain.Config, *domain.Config, *domain.Config, error)
}

type securityConfigRepositoryImpl struct{}

func NewSecurityConfigRepository() SecurityConfigRepository {
	return &securityConfigRepositoryImpl{}
}

func (r *securityConfigRepositoryImpl) LoadAll(configDir string) (*domain.Config, *domain.Config, *domain.Config, error) {
	blackList, err := r.LoadBlackList()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("加载黑名单失败：%w", err)
	}

	whiteList, err := r.LoadWhiteList()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("加载白名单失败：%w", err)
	}

	yellowList, err := r.LoadYellowList()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("加载黄名单失败：%w", err)
	}

	return blackList, whiteList, yellowList, nil
}

func (r *securityConfigRepositoryImpl) LoadBlackList() (*domain.Config, error) {
	return r.loadConfig("security/blacklist.yaml")
}

func (r *securityConfigRepositoryImpl) LoadWhiteList() (*domain.Config, error) {
	return r.loadConfig("security/whitelist.yaml")
}

func (r *securityConfigRepositoryImpl) LoadYellowList() (*domain.Config, error) {
	return r.loadConfig("security/yellowlist.yaml")
}

func (r *securityConfigRepositoryImpl) loadConfig(relativePath string) (*domain.Config, error) {
	filePath := filepath.Join(relativePath)

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &domain.Config{Rules: []domain.Rule{}}, nil
		}
		return nil, fmt.Errorf("读取配置文件失败：%w", err)
	}

	var config domain.Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("解析 YAML 配置失败：%w", err)
	}

	return &config, nil
}
