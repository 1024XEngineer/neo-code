package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeGatewayDocFixtures 写入文档校验测试所需的示例与文档文件。
func writeGatewayDocFixtures(t *testing.T, examples string, doc string) (string, string) {
	t.Helper()

	tempDir := t.TempDir()
	examplesPath := filepath.Join(tempDir, "gateway-rpc-examples.json")
	docPath := filepath.Join(tempDir, "gateway-rpc-api.md")
	if err := os.WriteFile(examplesPath, []byte(examples), 0o644); err != nil {
		t.Fatalf("write examples: %v", err)
	}
	if err := os.WriteFile(docPath, []byte(doc), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	return examplesPath, docPath
}

func TestCheckGatewayRPCDocConsistency(t *testing.T) {
	t.Run("success when methods and generated path are in doc", func(t *testing.T) {
		examples := `{
  "gateway.bindStream": {},
  "gateway.run": {},
  "common.error": {}
}
`
		doc := strings.Join([]string{
			"# Gateway RPC API",
			"",
			"产物：docs/generated/gateway-rpc-examples.json",
			"",
			"## Method: gateway.bindStream",
			"",
			"## Method: gateway.run",
		}, "\n")
		examplesPath, docPath := writeGatewayDocFixtures(t, examples, doc)

		if err := checkGatewayRPCDocConsistency(examplesPath, docPath); err != nil {
			t.Fatalf("checkGatewayRPCDocConsistency() error = %v", err)
		}
	})

	t.Run("fails when doc misses generated path reference", func(t *testing.T) {
		examples := `{"gateway.run":{}}`
		doc := "## Method: gateway.run\n"
		examplesPath, docPath := writeGatewayDocFixtures(t, examples, doc)

		err := checkGatewayRPCDocConsistency(examplesPath, docPath)
		if err == nil {
			t.Fatal("expected generated path reference error")
		}
		if !strings.Contains(err.Error(), "must reference generated examples file") {
			t.Fatalf("error = %v, want contains %q", err, "must reference generated examples file")
		}
	})

	t.Run("fails when doc misses method sections", func(t *testing.T) {
		examples := `{"gateway.bindStream":{},"gateway.run":{}}`
		doc := strings.Join([]string{
			"docs/generated/gateway-rpc-examples.json",
			"## Method: gateway.run",
		}, "\n")
		examplesPath, docPath := writeGatewayDocFixtures(t, examples, doc)

		err := checkGatewayRPCDocConsistency(examplesPath, docPath)
		if err == nil {
			t.Fatal("expected missing method section error")
		}
		if !strings.Contains(err.Error(), "## Method: gateway.bindStream") {
			t.Fatalf("error = %v, want contains %q", err, "## Method: gateway.bindStream")
		}
	})
}

func TestCollectGatewayMethods(t *testing.T) {
	methods := collectGatewayMethods(map[string]json.RawMessage{
		"common.error":       nil,
		"gateway.run":        nil,
		"gateway.bindStream": nil,
	})

	want := []string{"gateway.bindStream", "gateway.run"}
	if len(methods) != len(want) {
		t.Fatalf("len(methods) = %d, want %d", len(methods), len(want))
	}
	for index := range want {
		if methods[index] != want[index] {
			t.Fatalf("methods[%d] = %q, want %q", index, methods[index], want[index])
		}
	}
}

func TestCollectMissingMethodSections(t *testing.T) {
	missing := collectMissingMethodSections("## Method: gateway.run", []string{"gateway.bindStream", "gateway.run"})
	want := []string{"## Method: gateway.bindStream"}
	if len(missing) != len(want) {
		t.Fatalf("len(missing) = %d, want %d", len(missing), len(want))
	}
	for index := range want {
		if missing[index] != want[index] {
			t.Fatalf("missing[%d] = %q, want %q", index, missing[index], want[index])
		}
	}
}
