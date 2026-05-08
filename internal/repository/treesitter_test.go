package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTreeSitterIndexerPythonDefinitions(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	src := `def hello(name):
    return "hi " + name

class MyClass:
    def method(self):
        pass
`
	if err := os.WriteFile(filepath.Join(workspace, "main.py"), []byte(src), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewTreeSitterIndexer()
	if err := idx.EnsureBuilt(context.Background(), workspace, workspace, readFile); err != nil {
		t.Fatalf("EnsureBuilt: %v", err)
	}

	hits := idx.Search("hello", 10)
	if len(hits) == 0 {
		t.Fatal("expected to find 'hello'")
	}
	if hits[0].Kind != "function" {
		t.Fatalf("expected function kind, got %q", hits[0].Kind)
	}
	if !strings.Contains(hits[0].Signature, "def hello") {
		t.Fatalf("expected signature containing 'def hello', got %q", hits[0].Signature)
	}

	hits = idx.Search("MyClass", 10)
	if len(hits) == 0 {
		t.Fatal("expected to find 'MyClass'")
	}
	if hits[0].Kind != "class" {
		t.Fatalf("expected class kind, got %q", hits[0].Kind)
	}

	hits = idx.Search("method", 10)
	if len(hits) == 0 {
		t.Fatal("expected to find 'method'")
	}
	// Python Tree-sitter grammar tags query categorizes def inside class as
	// definition.function, not definition.method. This is a grammar-level
	// limitation — the index correctly reflects what the grammar reports.
	if hits[0].Kind != "function" && hits[0].Kind != "method" {
		t.Fatalf("expected function/method kind, got %q", hits[0].Kind)
	}
}

func TestTreeSitterIndexerTypeScriptDefinitions(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	src := `function greet(name: string): string {
    return "hello " + name;
}

class Person {
    constructor(public name: string) {}
    sayHi(): void {}
}
`
	if err := os.WriteFile(filepath.Join(workspace, "main.ts"), []byte(src), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewTreeSitterIndexer()
	if err := idx.EnsureBuilt(context.Background(), workspace, workspace, readFile); err != nil {
		t.Fatalf("EnsureBuilt: %v", err)
	}

	hits := idx.Search("greet", 10)
	if len(hits) == 0 {
		t.Fatal("expected to find 'greet'")
	}
	if hits[0].Kind != "function" {
		t.Fatalf("expected function kind, got %q", hits[0].Kind)
	}

	hits = idx.Search("sayHi", 10)
	if len(hits) == 0 {
		t.Fatal("expected to find 'sayHi'")
	}
	if hits[0].Kind != "method" {
		t.Fatalf("expected method kind, got %q", hits[0].Kind)
	}
}

func TestTreeSitterIndexerJavaDefinitions(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	src := `public class Main {
    public String greet(String name) {
        return "hello " + name;
    }
}
`
	if err := os.WriteFile(filepath.Join(workspace, "Main.java"), []byte(src), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewTreeSitterIndexer()
	if err := idx.EnsureBuilt(context.Background(), workspace, workspace, readFile); err != nil {
		t.Fatalf("EnsureBuilt: %v", err)
	}

	hits := idx.Search("Main", 10)
	if len(hits) == 0 {
		t.Fatal("expected to find 'Main'")
	}
	if hits[0].Kind != "class" {
		t.Fatalf("expected class kind, got %q", hits[0].Kind)
	}
}

func TestTreeSitterIndexerRustDefinitions(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	src := `fn hello(name: &str) -> String {
    format!("hi {}", name)
}

struct MyStruct {
    field: i32,
}
`
	if err := os.WriteFile(filepath.Join(workspace, "main.rs"), []byte(src), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewTreeSitterIndexer()
	if err := idx.EnsureBuilt(context.Background(), workspace, workspace, readFile); err != nil {
		t.Fatalf("EnsureBuilt: %v", err)
	}

	hits := idx.Search("hello", 10)
	if len(hits) == 0 {
		t.Fatal("expected to find 'hello'")
	}
	if hits[0].Kind != "function" {
		t.Fatalf("expected function kind, got %q", hits[0].Kind)
	}

	hits = idx.Search("MyStruct", 10)
	if len(hits) == 0 {
		t.Fatal("expected to find 'MyStruct'")
	}
}

func TestTreeSitterIndexerSkipsGoFiles(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	src := `package main
func GoOnlyFunction() {}
`
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewTreeSitterIndexer()
	if err := idx.EnsureBuilt(context.Background(), workspace, workspace, readFile); err != nil {
		t.Fatalf("EnsureBuilt: %v", err)
	}

	// Go files should be skipped by Tree-sitter indexer
	hits := idx.Search("GoOnlyFunction", 10)
	if len(hits) > 0 {
		t.Fatalf("expected no hits for Go function in Tree-sitter index, got %d", len(hits))
	}
}

func TestTreeSitterIndexerNoResults(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "main.py"), []byte("x = 1\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewTreeSitterIndexer()
	if err := idx.EnsureBuilt(context.Background(), workspace, workspace, readFile); err != nil {
		t.Fatalf("EnsureBuilt: %v", err)
	}

	hits := idx.Search("NonExistentSymbol", 10)
	if len(hits) != 0 {
		t.Fatalf("expected 0 hits, got %d", len(hits))
	}
}

func TestTreeSitterIndexerEmptyWorkspace(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	idx := NewTreeSitterIndexer()
	if err := idx.EnsureBuilt(context.Background(), workspace, workspace, readFile); err != nil {
		t.Fatalf("EnsureBuilt: %v", err)
	}

	hits := idx.Search("anything", 10)
	if len(hits) != 0 {
		t.Fatalf("expected 0 hits in empty workspace, got %d", len(hits))
	}
}

func TestTreeSitterIndexerRefreshDetectsNewFile(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	idx := NewTreeSitterIndexer()
	if err := idx.EnsureBuilt(context.Background(), workspace, workspace, readFile); err != nil {
		t.Fatalf("EnsureBuilt: %v", err)
	}

	// Initially empty
	hits := idx.Search("hello", 10)
	if len(hits) != 0 {
		t.Fatalf("expected 0 hits initially, got %d", len(hits))
	}

	// Add a file
	if err := os.WriteFile(filepath.Join(workspace, "main.py"), []byte("def hello():\n    pass\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := idx.Refresh(context.Background(), workspace, workspace, readFile); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	hits = idx.Search("hello", 10)
	if len(hits) == 0 {
		t.Fatal("expected to find 'hello' after refresh")
	}
}

func TestTreeSitterIndexerPrefixSearch(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	src := `def helloWorld(): pass
def helloYou(): pass
def goodbye(): pass
`
	if err := os.WriteFile(filepath.Join(workspace, "main.py"), []byte(src), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	idx := NewTreeSitterIndexer()
	if err := idx.EnsureBuilt(context.Background(), workspace, workspace, readFile); err != nil {
		t.Fatalf("EnsureBuilt: %v", err)
	}

	// Exact search for "helloWorld"
	hits := idx.Search("helloWorld", 10)
	if len(hits) == 0 {
		t.Fatal("expected to find 'helloWorld'")
	}

	// Prefix search "hello" should match both "helloWorld" and "helloYou"
	hits = idx.Search("hello", 10)
	if len(hits) < 2 {
		t.Fatalf("expected at least 2 prefix matches for 'hello', got %d", len(hits))
	}
}
