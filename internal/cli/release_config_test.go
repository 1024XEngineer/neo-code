package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type goreleaserBuild struct {
	ID   string   `yaml:"id"`
	Tags []string `yaml:"tags"`
}

type goreleaserConfig struct {
	Builds []goreleaserBuild `yaml:"builds"`
}

// repoRootForReleaseConfigTest 解析仓库根目录，供发布配置回归测试读取工作流与 GoReleaser 文件。
func repoRootForReleaseConfigTest(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
}

func TestGoReleaserEmbedsWebAssetsOnlyForNeoCode(t *testing.T) {
	repoRoot := repoRootForReleaseConfigTest(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, ".goreleaser.yaml"))
	if err != nil {
		t.Fatalf("read .goreleaser.yaml: %v", err)
	}

	var cfg goreleaserConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal .goreleaser.yaml: %v", err)
	}

	builds := make(map[string]goreleaserBuild, len(cfg.Builds))
	for _, build := range cfg.Builds {
		builds[build.ID] = build
	}

	neocodeBuild, ok := builds["neocode"]
	if !ok {
		t.Fatal("missing neocode build in .goreleaser.yaml")
	}
	if !slicesContains(neocodeBuild.Tags, "webembed") {
		t.Fatalf("neocode build tags = %v, want webembed", neocodeBuild.Tags)
	}

	gatewayBuild, ok := builds["neocode-gateway"]
	if !ok {
		t.Fatal("missing neocode-gateway build in .goreleaser.yaml")
	}
	if slicesContains(gatewayBuild.Tags, "webembed") {
		t.Fatalf("neocode-gateway build tags = %v, want no webembed", gatewayBuild.Tags)
	}
}

func TestReleaseWorkflowBuildsWebDistBeforeGoReleaser(t *testing.T) {
	repoRoot := repoRootForReleaseConfigTest(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	content := string(raw)

	for _, expected := range []string{
		"actions/setup-node@v4",
		"npm ci",
		"npm run build",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("release workflow missing %q", expected)
		}
	}
}

func slicesContains(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}
