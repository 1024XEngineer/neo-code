package config

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

// ProviderIdentity identifies a concrete provider endpoint for discovery and caching.
type ProviderIdentity struct {
	Driver  string `json:"driver"`
	BaseURL string `json:"base_url"`
}

func (i ProviderIdentity) Key() string {
	return i.Driver + "|" + i.BaseURL
}

func NormalizeKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func NormalizeProviderName(name string) string {
	return NormalizeKey(name)
}

func NormalizeProviderDriver(driver string) string {
	return NormalizeKey(driver)
}

func NormalizeProviderBaseURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("provider base_url is empty")
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("provider base_url %q is invalid: %w", raw, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("provider base_url %q must include scheme and host", raw)
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.User = nil

	if cleaned := path.Clean(strings.TrimSpace(parsed.Path)); cleaned == "." {
		parsed.Path = ""
	} else {
		parsed.Path = strings.TrimRight(cleaned, "/")
	}

	return parsed.String(), nil
}

func NewProviderIdentity(driver string, baseURL string) (ProviderIdentity, error) {
	normalizedDriver := NormalizeProviderDriver(driver)
	if normalizedDriver == "" {
		return ProviderIdentity{}, fmt.Errorf("provider driver is empty")
	}

	normalizedBaseURL, err := NormalizeProviderBaseURL(baseURL)
	if err != nil {
		return ProviderIdentity{}, err
	}

	return ProviderIdentity{
		Driver:  normalizedDriver,
		BaseURL: normalizedBaseURL,
	}, nil
}
