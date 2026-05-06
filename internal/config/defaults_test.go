package config

import "testing"

func TestDefaultProvidersIncludesBuiltinProviders(t *testing.T) {
	t.Parallel()

	providers := DefaultProviders()
	if len(providers) != 10 {
		t.Fatalf("expected 10 builtin providers, got %d", len(providers))
	}
	if providers[0].Name != OpenAIName {
		t.Fatalf("expected first provider %q, got %q", OpenAIName, providers[0].Name)
	}
	if providers[1].Name != GeminiName {
		t.Fatalf("expected second provider %q, got %q", GeminiName, providers[1].Name)
	}
}

func TestLoaderDefaultsValidate(t *testing.T) {
	t.Parallel()

	cfg := StaticDefaults()
	cfg.Providers = DefaultProviders()
	cfg.applyStaticDefaults(*StaticDefaults())
	if err := cfg.ValidateSnapshot(); err != nil {
		t.Fatalf("expected loader defaults to validate, got %v", err)
	}
}

func TestStaticDefaultsAreOnlyStaticSkeleton(t *testing.T) {
	t.Parallel()

	cfg := StaticDefaults()
	cfg.applyStaticDefaults(*StaticDefaults())

	if err := cfg.ValidateSnapshot(); err == nil {
		t.Fatal("expected static defaults alone to be insufficient without assembled providers")
	}
}

func TestDefaultConfigWorkdirIsAbsolute(t *testing.T) {
	t.Parallel()

	cfg := StaticDefaults()
	cfg.applyStaticDefaults(*StaticDefaults())
	if cfg.Workdir == "" {
		t.Fatal("expected workdir to be set")
	}
}
