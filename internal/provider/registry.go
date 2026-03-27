package provider

import (
	"errors"
	"strings"
)

type Registry struct {
	items map[string]Provider
}

func NewRegistry() *Registry {
	return &Registry{items: map[string]Provider{}}
}

func (r *Registry) Register(provider Provider) {
	if provider == nil {
		return
	}
	r.items[strings.ToLower(provider.Name())] = provider
}

func (r *Registry) Get(name string) (Provider, error) {
	item, ok := r.items[strings.ToLower(name)]
	if !ok {
		return nil, errors.New("provider: not found")
	}
	return item, nil
}
