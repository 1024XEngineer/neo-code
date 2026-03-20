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

func NewRoleService(repo domain.RoleRepository, defaultPath string) domain.RoleService {
	return &roleServiceImpl{
		repo:        repo,
		defaultPath: defaultPath,
	}
}

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

func (s *roleServiceImpl) List(ctx context.Context) ([]domain.Role, error) {
	return s.repo.List(ctx)
}

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
