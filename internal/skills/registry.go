package skills

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// MemoryRegistry is an in-memory skills registry backed by one loader.
type MemoryRegistry struct {
	loader Loader

	mu     sync.RWMutex
	loaded bool
	byID   map[string]Skill
	issues []LoadIssue
}

// NewRegistry creates a registry from one loader.
func NewRegistry(loader Loader) *MemoryRegistry {
	return &MemoryRegistry{
		loader: loader,
		byID:   map[string]Skill{},
		issues: []LoadIssue{},
	}
}

// Refresh reloads all skills from the loader and rebuilds the in-memory index.
func (r *MemoryRegistry) Refresh(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("skills: registry is nil")
	}
	if r.loader == nil {
		return fmt.Errorf("skills: loader is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	snapshot, err := r.loader.Load(ctx)
	if err != nil {
		if errors.Is(err, ErrSkillRootNotFound) {
			r.mu.Lock()
			r.loaded = true
			r.byID = map[string]Skill{}
			r.issues = nil
			r.mu.Unlock()
			return nil
		}
		r.mu.Lock()
		r.issues = []LoadIssue{{
			Code:    IssueRefreshFailed,
			Message: "skills refresh failed",
			Err:     err,
		}}
		r.mu.Unlock()
		return err
	}

	index := make(map[string]Skill, len(snapshot.Skills))
	issues := append([]LoadIssue(nil), snapshot.Issues...)
	for _, skill := range snapshot.Skills {
		key := normalizeSkillID(skill.Descriptor.ID)
		if key == "" {
			issues = append(issues, LoadIssue{
				Code:    IssueInvalidMetadata,
				Path:    skill.Descriptor.Source.FilePath,
				Message: "empty skill id after normalization",
			})
			continue
		}
		if existing, ok := index[key]; ok {
			issues = append(issues, LoadIssue{
				Code:    IssueDuplicateID,
				Path:    skill.Descriptor.Source.FilePath,
				SkillID: skill.Descriptor.ID,
				Message: fmt.Sprintf(
					"duplicate skill id %q (already loaded from %s)",
					skill.Descriptor.ID,
					existing.Descriptor.Source.FilePath,
				),
			})
			continue
		}
		index[key] = skill
	}

	r.mu.Lock()
	r.loaded = true
	r.byID = index
	r.issues = issues
	r.mu.Unlock()
	return nil
}

// List returns descriptors that match input filters.
func (r *MemoryRegistry) List(ctx context.Context, input ListInput) ([]Descriptor, error) {
	if err := r.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mu.RLock()
	descriptors := make([]Descriptor, 0, len(r.byID))
	for _, skill := range r.byID {
		descriptor := skill.Descriptor
		if !allowBySource(input, descriptor) {
			continue
		}
		if !allowByScope(input, descriptor) {
			continue
		}
		descriptors = append(descriptors, descriptor)
	}
	r.mu.RUnlock()

	sort.Slice(descriptors, func(i, j int) bool {
		return normalizeSkillID(descriptors[i].ID) < normalizeSkillID(descriptors[j].ID)
	})
	return descriptors, nil
}

// Get returns one skill descriptor and content by id.
func (r *MemoryRegistry) Get(ctx context.Context, id string) (Descriptor, Content, error) {
	if err := r.ensureLoaded(ctx); err != nil {
		return Descriptor{}, Content{}, err
	}
	if err := ctx.Err(); err != nil {
		return Descriptor{}, Content{}, err
	}

	key := normalizeSkillID(id)
	if key == "" {
		return Descriptor{}, Content{}, fmt.Errorf("%w: id is empty", ErrSkillNotFound)
	}

	r.mu.RLock()
	skill, ok := r.byID[key]
	r.mu.RUnlock()
	if !ok {
		return Descriptor{}, Content{}, fmt.Errorf("%w: %s", ErrSkillNotFound, id)
	}
	return skill.Descriptor, skill.Content, nil
}

// Issues returns the latest non-fatal loader/registry issues.
func (r *MemoryRegistry) Issues() []LoadIssue {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]LoadIssue(nil), r.issues...)
}

func (r *MemoryRegistry) ensureLoaded(ctx context.Context) error {
	r.mu.RLock()
	loaded := r.loaded
	r.mu.RUnlock()
	if loaded {
		return nil
	}
	return r.Refresh(ctx)
}

func normalizeSkillID(id string) string {
	normalized := strings.ToLower(strings.TrimSpace(id))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	return strings.Trim(normalized, "-")
}
