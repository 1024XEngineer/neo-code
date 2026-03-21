package repository

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go-llm-demo/internal/server/domain"
)

type FileRoleStore struct {
	path string
	mu   sync.Mutex
}

// NewFileRoleStore 创建一个基于文件的角色存储。
func NewFileRoleStore(path string) *FileRoleStore {
	return &FileRoleStore{path: path}
}

// GetByID 返回指定 ID 对应的角色。
func (s *FileRoleStore) GetByID(ctx context.Context, id string) (*domain.Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	roles, err := s.readAllLocked()
	if err != nil {
		return nil, err
	}

	for _, role := range roles {
		if role.ID == id {
			return &role, nil
		}
	}

	return nil, errors.New("role not found")
}

// GetByName 返回指定名称对应的角色。
func (s *FileRoleStore) GetByName(ctx context.Context, name string) (*domain.Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	roles, err := s.readAllLocked()
	if err != nil {
		return nil, err
	}

	for _, role := range roles {
		if role.Name == name {
			return &role, nil
		}
	}

	return nil, errors.New("role not found")
}

// List 返回所有已存储的角色。
func (s *FileRoleStore) List(ctx context.Context) ([]domain.Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	roles, err := s.readAllLocked()
	if err != nil {
		return nil, err
	}

	cloned := make([]domain.Role, len(roles))
	copy(cloned, roles)
	return cloned, nil
}

// Save 在存储中创建或更新角色。
func (s *FileRoleStore) Save(ctx context.Context, role *domain.Role) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	roles, err := s.readAllLocked()
	if err != nil {
		return err
	}

	found := false
	for i, r := range roles {
		if r.ID == role.ID {
			role.UpdatedAt = time.Now().UTC()
			roles[i] = *role
			found = true
			break
		}
	}

	if !found {
		role.CreatedAt = time.Now().UTC()
		role.UpdatedAt = time.Now().UTC()
		roles = append(roles, *role)
	}

	return s.writeAllLocked(roles)
}

// Delete 删除指定 ID 对应的角色。
func (s *FileRoleStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	roles, err := s.readAllLocked()
	if err != nil {
		return err
	}

	newRoles := make([]domain.Role, 0, len(roles))
	for _, role := range roles {
		if role.ID != id {
			newRoles = append(newRoles, role)
		}
	}

	if len(newRoles) == len(roles) {
		return errors.New("role not found")
	}

	return s.writeAllLocked(newRoles)
}

func (s *FileRoleStore) readAllLocked() ([]domain.Role, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []domain.Role{}, nil
		}
		return nil, err
	}

	if len(data) == 0 {
		return []domain.Role{}, nil
	}

	var roles []domain.Role
	if err := json.Unmarshal(data, &roles); err != nil {
		return nil, err
	}
	return roles, nil
}

func (s *FileRoleStore) writeAllLocked(roles []domain.Role) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(roles, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, data, 0o644)
}
