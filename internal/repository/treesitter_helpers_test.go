package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func TestTreeSitterHelpersAndIndexerBranches(t *testing.T) {
	t.Parallel()

	t.Run("helper functions", func(t *testing.T) {
		t.Parallel()

		if captureNameToKind("definition.function") != "function" ||
			captureNameToKind("definition.method") != "method" ||
			captureNameToKind("definition.class") != "class" ||
			captureNameToKind("definition.type") != "type" ||
			captureNameToKind("definition.variable") != "variable" ||
			captureNameToKind("definition.interface") != "interface" ||
			captureNameToKind("definition.constant") != "constant" ||
			captureNameToKind("other") != "unknown" {
			t.Fatal("captureNameToKind() mismatch")
		}

		entry := grammars.DetectLanguageByName("python")
		if entry == nil {
			t.Fatal("expected python grammar entry")
		}
		lang := entry.Language()
		parser := gotreesitter.NewParser(lang)
		tree, err := parser.Parse([]byte("def hello(name):\n    return name\n"))
		if err != nil {
			t.Fatalf("parse helper source: %v", err)
		}
		if got := extractTreeSitterSignature([]byte("def hello(name):\n    return name\n"), tree.RootNode().Children()[0]); got != "def hello(name):" {
			t.Fatalf("extractTreeSitterSignature() = %q", got)
		}
		oversized := "def " + strings.Repeat("a", maxSignatureLength+20) + "():\n    pass\n"
		tree, err = parser.Parse([]byte(oversized))
		if err != nil {
			t.Fatalf("parse oversized helper source: %v", err)
		}
		if got := extractTreeSitterSignature([]byte(oversized), tree.RootNode().Children()[0]); len(got) != maxSignatureLength {
			t.Fatalf("expected signature truncation, got len=%d", len(got))
		}
		if got := extractLineSignature("a\nb\nc", 2); got != "b" {
			t.Fatalf("extractLineSignature() = %q", got)
		}
		if got := extractLineSignature("a", 9); got != "" {
			t.Fatalf("expected empty out-of-range signature, got %q", got)
		}
	})

	t.Run("read helper, search helper, and index lifecycle", func(t *testing.T) {
		t.Parallel()

		workspace := t.TempDir()
		mustWriteRepositoryFile(t, filepath.Join(workspace, "main.py"), "def hello(name):\n    return name\n")
		mustWriteRepositoryFile(t, filepath.Join(workspace, "main.go"), "package main\nfunc SkipMe() {}\n")
		mustWriteRepositoryFile(t, filepath.Join(workspace, "plain.txt"), "hello only text\n")

		content, ok := readRetrievalTextWithReader(workspace, filepath.Join(workspace, "main.py"), readFile)
		if !ok || !strings.Contains(content, "hello") {
			t.Fatalf("readRetrievalTextWithReader() = (%q, %v)", content, ok)
		}
		if _, ok := readRetrievalTextWithReader(workspace, filepath.Join(workspace, "missing.py"), readFile); ok {
			t.Fatal("expected missing file to be skipped")
		}

		hits, err := searchSymbolsWithTreeSitter(context.Background(), workspace, workspace, "hello", readFile, 10)
		if err != nil || len(hits) == 0 {
			t.Fatalf("searchSymbolsWithTreeSitter() = (%+v, %v)", hits, err)
		}
		if hits[0].Kind == "" || !strings.Contains(hits[0].Signature, "def hello") {
			t.Fatalf("unexpected tree-sitter hit: %+v", hits[0])
		}

		idx := NewTreeSitterIndexer()
		if err := idx.EnsureBuilt(context.Background(), workspace, workspace, readFile); err != nil {
			t.Fatalf("EnsureBuilt() error = %v", err)
		}
		if !idx.isBuilt() || idx.getRoot() != workspace {
			t.Fatalf("unexpected index state after build")
		}
		if got := idx.Search("hello", 1); len(got) != 1 {
			t.Fatalf("expected limited search result, got %+v", got)
		}
		idx.Close()
		if idx.isBuilt() || len(idx.Search("hello", 10)) != 0 {
			t.Fatalf("expected close to reset index state")
		}
	})

	t.Run("refresh replace and delete branches", func(t *testing.T) {
		t.Parallel()

		workspace := t.TempDir()
		pyPath := filepath.Join(workspace, "mod.py")
		mustWriteRepositoryFile(t, pyPath, "def before():\n    pass\n")

		idx := NewTreeSitterIndexer()
		if err := idx.EnsureBuilt(context.Background(), workspace, workspace, readFile); err != nil {
			t.Fatalf("EnsureBuilt() error = %v", err)
		}
		if len(idx.Search("before", 10)) == 0 {
			t.Fatal("expected initial symbol in index")
		}

		if err := os.WriteFile(pyPath, []byte("def after():\n    pass\n"), 0o644); err != nil {
			t.Fatalf("rewrite file: %v", err)
		}
		if err := idx.Refresh(context.Background(), workspace, workspace, readFile); err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}
		if len(idx.Search("before", 10)) != 0 || len(idx.Search("after", 10)) == 0 {
			t.Fatalf("expected refresh to replace indexed entries")
		}

		goPath := filepath.Join(workspace, "skip.go")
		mustWriteRepositoryFile(t, goPath, "package main\nfunc Ignore(){}\n")
		if err := idx.replaceFile(context.Background(), workspace, goPath, readFile); err != nil {
			t.Fatalf("replaceFile(go) error = %v", err)
		}

		if err := os.Remove(pyPath); err != nil {
			t.Fatalf("remove file: %v", err)
		}
		if err := idx.Refresh(context.Background(), workspace, workspace, readFile); err != nil {
			t.Fatalf("Refresh(delete) error = %v", err)
		}
		if len(idx.Search("after", 10)) != 0 {
			t.Fatalf("expected deleted file entries to be removed")
		}
	})
}
