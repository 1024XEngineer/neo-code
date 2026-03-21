package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"go-llm-demo/internal/server/domain"
)

type roleServiceImpl struct {
	repo        domain.RoleRepository
	activeRole  *domain.Role
	mu          sync.RWMutex
	defaultPath string
}

// NewRoleService 使用给定仓储创建角色服务。
func NewRoleService(repo domain.RoleRepository, defaultPath string) domain.RoleService {
	return &roleServiceImpl{
		repo:        repo,
		defaultPath: defaultPath,
	}
}

// GetActivePrompt 返回当前激活角色的提示词，必要时回退到默认文件。
func (s *roleServiceImpl) GetActivePrompt(ctx context.Context) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.activeRole != nil {
		return s.activeRole.Prompt, nil
	}

	if s.defaultPath != "" {
		return s.loadFromFile(s.defaultPath)
	}

	return "", nil
}

// SetActive 将指定角色设为当前激活角色。
func (s *roleServiceImpl) SetActive(ctx context.Context, roleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	role, err := s.repo.GetByID(ctx, roleID)
	if err != nil {
		return err
	}

	s.activeRole = role
	return nil
}

// List 返回所有已存储的角色。
func (s *roleServiceImpl) List(ctx context.Context) ([]domain.Role, error) {
	return s.repo.List(ctx)
}

// Create 创建并保存一个新角色后返回它。
func (s *roleServiceImpl) Create(ctx context.Context, name, desc, prompt string) (*domain.Role, error) {
	role := &domain.Role{
		ID:          fmt.Sprintf("role_%d", time.Now().UnixNano()),
		Name:        name,
		Description: desc,
		Prompt:      prompt,
	}

	if err := s.repo.Save(ctx, role); err != nil {
		return nil, err
	}

	return role, nil
}

// Delete 删除指定角色，并在其处于激活状态时一并清除。
func (s *roleServiceImpl) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	if s.activeRole != nil && s.activeRole.ID == id {
		s.activeRole = nil
	}
	s.mu.Unlock()

	return s.repo.Delete(ctx, id)
}

func (s *roleServiceImpl) loadFromFile(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	return strings.TrimSpace(string(data)), nil
}
