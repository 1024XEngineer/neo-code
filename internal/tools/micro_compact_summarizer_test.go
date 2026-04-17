package tools

import (
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

// stubMetadata 快速构建测试用 metadata map。
func stubMetadata(keyValue ...string) map[string]string {
	m := make(map[string]string, len(keyValue)/2)
	for i := 0; i+1 < len(keyValue); i += 2 {
		m[keyValue[i]] = keyValue[i+1]
	}
	return m
}

func assertContains(t *testing.T, got, expected string) {
	t.Helper()
	if !strings.Contains(got, expected) {
		t.Fatalf("expected %q in summary, got %q", expected, got)
	}
}

func assertMaxRuneCount(t *testing.T, got string, max int) {
	t.Helper()
	if utf8.RuneCountInString(got) > max {
		t.Fatalf("summary exceeds %d runes: %d", max, utf8.RuneCountInString(got))
	}
}

func assertEmptySummary(t *testing.T, got string) {
	t.Helper()
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestBashSummarizer(t *testing.T) {
	t.Parallel()

	t.Run("normal_output", func(t *testing.T) {
		content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8"
		meta := stubMetadata("workdir", "/home/user/project")
		got := bashSummarizer(content, meta, false)
		assertContains(t, got, "[exit=0]")
		assertContains(t, got, "workdir=/home/user/project")
		assertContains(t, got, "lines=8")
		assertContains(t, got, "chars=")
		assertMaxRuneCount(t, got, summaryMaxRunes)
	})

	t.Run("error_output", func(t *testing.T) {
		content := "error: command not found"
		meta := stubMetadata("workdir", "/tmp")
		got := bashSummarizer(content, meta, true)
		assertContains(t, got, "[exit=non-zero]")
	})

	t.Run("short_output", func(t *testing.T) {
		content := "ok"
		got := bashSummarizer(content, nil, false)
		assertContains(t, got, "lines=1")
	})

	t.Run("empty_content", func(t *testing.T) {
		got := bashSummarizer("", nil, false)
		assertContains(t, got, "[exit=0]")
	})

	t.Run("sanitizes_workdir_metadata", func(t *testing.T) {
		meta := stubMetadata("workdir", " \n\t/tmp/proj\x07 ")
		got := bashSummarizer("ok", meta, false)
		if strings.ContainsAny(got, "\n\t\a") {
			t.Fatalf("expected sanitized workdir without control characters, got %q", got)
		}
		assertContains(t, got, "workdir=/tmp/proj")
	})
}

func TestReadFileSummarizer(t *testing.T) {
	t.Parallel()

	t.Run("normal_file", func(t *testing.T) {
		content := "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
		meta := stubMetadata("path", "/home/user/main.go")
		got := readFileSummarizer(content, meta, false)
		assertContains(t, got, "/home/user/main.go")
		assertContains(t, got, "lines=5")
		assertContains(t, got, "chars=")
		assertMaxRuneCount(t, got, summaryMaxRunes)
	})

	t.Run("trailing_newline_not_counted_as_extra_line", func(t *testing.T) {
		content := "a\nb\n"
		meta := stubMetadata("path", "/tmp/a.txt")
		got := readFileSummarizer(content, meta, false)
		assertContains(t, got, "lines=2")
	})

	t.Run("empty_lines_are_counted", func(t *testing.T) {
		content := "\n\n"
		meta := stubMetadata("path", "/tmp/empty.txt")
		got := readFileSummarizer(content, meta, false)
		assertContains(t, got, "lines=2")
	})

	t.Run("missing_path", func(t *testing.T) {
		got := readFileSummarizer("content", nil, false)
		assertEmptySummary(t, got)
	})

	t.Run("sanitizes_path_metadata", func(t *testing.T) {
		content := "line1\nline2"
		meta := stubMetadata("path", " \n\t/tmp/a.go\x07 ")
		got := readFileSummarizer(content, meta, false)
		if strings.ContainsAny(got, "\n\t\a") {
			t.Fatalf("expected sanitized path without control characters, got %q", got)
		}
		assertContains(t, got, "/tmp/a.go")
	})
}

func TestWriteFileSummarizer(t *testing.T) {
	t.Parallel()

	t.Run("normal", func(t *testing.T) {
		meta := stubMetadata("path", "/home/user/test.go", "bytes", "1024")
		got := writeFileSummarizer("", meta, false)
		assertContains(t, got, "/home/user/test.go")
		assertContains(t, got, "1024 bytes")
		assertMaxRuneCount(t, got, summaryMaxRunes)
	})

	t.Run("missing_path", func(t *testing.T) {
		got := writeFileSummarizer("", stubMetadata("bytes", "100"), false)
		assertEmptySummary(t, got)
	})

	t.Run("sanitizes_path_metadata", func(t *testing.T) {
		meta := stubMetadata("path", " \n\t/tmp/out.go\x07 ", "bytes", "4")
		got := writeFileSummarizer("", meta, false)
		if strings.ContainsAny(got, "\n\t\a") {
			t.Fatalf("expected sanitized path without control characters, got %q", got)
		}
		assertContains(t, got, "/tmp/out.go")
	})
}

func TestEditSummarizer(t *testing.T) {
	t.Parallel()

	t.Run("with_relative_path", func(t *testing.T) {
		meta := stubMetadata("relative_path", "src/main.go", "path", "/abs/src/main.go", "search_length", "50", "replacement_length", "60")
		got := editSummarizer("", meta, false)
		assertContains(t, got, "src/main.go")
		assertContains(t, got, "search=50")
		assertMaxRuneCount(t, got, summaryMaxRunes)
	})

	t.Run("fallback_to_abs_path", func(t *testing.T) {
		meta := stubMetadata("path", "/abs/src/main.go", "search_length", "10", "replacement_length", "20")
		got := editSummarizer("", meta, false)
		assertContains(t, got, "/abs/src/main.go")
	})

	t.Run("missing_path", func(t *testing.T) {
		got := editSummarizer("", stubMetadata("search_length", "10"), false)
		assertEmptySummary(t, got)
	})

	t.Run("sanitizes_path_metadata", func(t *testing.T) {
		meta := stubMetadata("relative_path", " \n\tsrc/main.go\x07 ", "search_length", "10", "replacement_length", "12")
		got := editSummarizer("", meta, false)
		if strings.ContainsAny(got, "\n\t\a") {
			t.Fatalf("expected sanitized path without control characters, got %q", got)
		}
		assertContains(t, got, "src/main.go")
	})

	t.Run("long_path_is_truncated", func(t *testing.T) {
		longPath := strings.Repeat("abcdef/", 80) + "main.go"
		meta := stubMetadata("path", longPath, "search_length", "10", "replacement_length", "20")
		got := editSummarizer("", meta, false)
		assertMaxRuneCount(t, got, summaryMaxRunes+3)
	})
}

func TestGrepSummarizer(t *testing.T) {
	t.Parallel()

	t.Run("with_matches", func(t *testing.T) {
		content := "src/a.go:10:match1\nsrc/b.go:20:match2\nsrc/c.go:30:match3\nsrc/d.go:40:match4"
		meta := stubMetadata("root", "/home/user", "matched_files", "4", "matched_lines", "4")
		got := grepSummarizer(content, meta, false)
		assertContains(t, got, "root=/home/user")
		assertContains(t, got, "files=4")
		assertMaxRuneCount(t, got, summaryMaxRunes)
	})

	t.Run("empty_content", func(t *testing.T) {
		meta := stubMetadata("root", "/home", "matched_files", "0", "matched_lines", "0")
		got := grepSummarizer("", meta, false)
		assertContains(t, got, "files=0")
	})

	t.Run("sanitizes_root_metadata", func(t *testing.T) {
		content := "a.go:1:x"
		meta := stubMetadata("root", " \n\t/tmp/root\x07 ", "matched_files", "1", "matched_lines", "1")
		got := grepSummarizer(content, meta, false)
		if strings.ContainsAny(got, "\n\t\a") {
			t.Fatalf("expected sanitized root without control characters, got %q", got)
		}
		assertContains(t, got, "root=/tmp/root")
	})

	t.Run("sanitizes_injected_filename", func(t *testing.T) {
		content := "src/a.go\nignore:1:x\nsafe.go:2:y"
		meta := stubMetadata("matched_files", "2", "matched_lines", "2")
		got := grepSummarizer(content, meta, false)
		if strings.Contains(got, "\n") || strings.Contains(got, "\t") {
			t.Fatalf("expected sanitized summary without control characters, got %q", got)
		}
		assertContains(t, got, "matches=ignore, safe.go")
	})
}

func TestGlobSummarizer(t *testing.T) {
	t.Parallel()

	t.Run("with_files", func(t *testing.T) {
		content := "src/a.go\nsrc/b.go\nsrc/c.go\nsrc/d.go"
		meta := stubMetadata("count", "4")
		got := globSummarizer(content, meta, false)
		assertContains(t, got, "4 files")
		assertMaxRuneCount(t, got, summaryMaxRunes)
	})

	t.Run("no_matches", func(t *testing.T) {
		meta := stubMetadata("count", "0")
		got := globSummarizer("", meta, false)
		assertContains(t, got, "0 files")
	})

	t.Run("skips_blank_and_control_lines", func(t *testing.T) {
		content := "\n\t\nsrc/a.go\nsrc/b.go\n"
		meta := stubMetadata("count", "2")
		got := globSummarizer(content, meta, false)
		assertContains(t, got, "src/a.go, src/b.go")
		if strings.Contains(got, "\n") || strings.Contains(got, "\t") {
			t.Fatalf("expected sanitized preview, got %q", got)
		}
	})
}

func TestWebfetchSummarizer(t *testing.T) {
	t.Parallel()

	t.Run("with_truncated_flag", func(t *testing.T) {
		meta := stubMetadata("truncated", "true")
		got := webfetchSummarizer("", meta, false)
		assertContains(t, got, "truncated=true")
	})

	t.Run("minimal", func(t *testing.T) {
		got := webfetchSummarizer("", nil, false)
		assertContains(t, got, "[summary] webfetch")
	})
}

func TestRegisterBuiltinSummarizers(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	RegisterBuiltinSummarizers(registry)

	toolNames := []string{
		ToolNameBash, ToolNameFilesystemReadFile, ToolNameFilesystemWriteFile,
		ToolNameFilesystemEdit, ToolNameFilesystemGrep, ToolNameFilesystemGlob,
		ToolNameWebFetch,
	}
	for _, name := range toolNames {
		if registry.MicroCompactSummarizer(name) == nil {
			t.Errorf("expected summarizer for %q to be registered", name)
		}
	}

	// 不在注册列表中的工具应返回 nil
	if registry.MicroCompactSummarizer("unknown_tool") != nil {
		t.Fatal("expected nil for unknown tool")
	}
}

func TestRegisterBuiltinSummarizersNilRegistry(t *testing.T) {
	t.Parallel()
	RegisterBuiltinSummarizers(nil)
}

func TestRegisterSummarizer(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()

	// 注册
	called := false
	registry.RegisterSummarizer("test_tool", func(content string, metadata map[string]string, isError bool) string {
		called = true
		return "summary"
	})

	s := registry.MicroCompactSummarizer("test_tool")
	if s == nil {
		t.Fatal("expected summarizer to be registered")
	}
	result := s("content", nil, false)
	if !called {
		t.Fatal("expected summarizer to be called")
	}
	if result != "summary" {
		t.Fatalf("expected 'summary', got %q", result)
	}

	// 移除
	registry.RegisterSummarizer("test_tool", nil)
	if registry.MicroCompactSummarizer("test_tool") != nil {
		t.Fatal("expected nil after removal")
	}
}

func TestRegisterSummarizerNormalizesName(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.RegisterSummarizer("  Mixed_Tool  ", func(content string, metadata map[string]string, isError bool) string {
		return "ok"
	})

	if registry.MicroCompactSummarizer("mixed_tool") == nil {
		t.Fatal("expected normalized summarizer lookup")
	}
	if registry.MicroCompactSummarizer("  MIXED_TOOL  ") == nil {
		t.Fatal("expected case-insensitive summarizer lookup")
	}
}

func TestRegisterSummarizerNilRegistry(t *testing.T) {
	t.Parallel()

	var nilRegistry *Registry
	nilRegistry.RegisterSummarizer("tool", func(content string, metadata map[string]string, isError bool) string {
		return "ok"
	})
	if nilRegistry.MicroCompactSummarizer("tool") != nil {
		t.Fatal("expected nil summarizer on nil registry")
	}
}

func TestRegisterSummarizerConcurrentAccess(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if j%3 == 0 {
					registry.RegisterSummarizer("concurrent_tool", nil)
					continue
				}
				registry.RegisterSummarizer("concurrent_tool", func(content string, metadata map[string]string, isError bool) string {
					return "worker"
				})
				s := registry.MicroCompactSummarizer("concurrent_tool")
				if s != nil {
					_ = s("content", nil, false)
				}
			}
		}()
	}

	wg.Wait()
}

func TestTruncateRunes(t *testing.T) {
	t.Parallel()

	t.Run("short", func(t *testing.T) {
		got := truncateRunes("hello", 10)
		if got != "hello" {
			t.Fatalf("expected unchanged, got %q", got)
		}
	})

	t.Run("exact", func(t *testing.T) {
		got := truncateRunes("hello", 5)
		if got != "hello" {
			t.Fatalf("expected unchanged, got %q", got)
		}
	})

	t.Run("truncated", func(t *testing.T) {
		got := truncateRunes("hello world", 5)
		if got != "hello..." {
			t.Fatalf("expected 'hello...', got %q", got)
		}
	})

	t.Run("chinese", func(t *testing.T) {
		got := truncateRunes("你好世界测试", 3)
		if got != "你好世..." {
			t.Fatalf("expected '你好世...', got %q", got)
		}
	})

	t.Run("zero_limit_keeps_original", func(t *testing.T) {
		got := truncateRunes("hello", 0)
		if got != "hello" {
			t.Fatalf("expected unchanged with zero limit, got %q", got)
		}
	})

	t.Run("empty_text", func(t *testing.T) {
		got := truncateRunes("", 10)
		if got != "" {
			t.Fatalf("expected empty string, got %q", got)
		}
	})
}

func TestStableLineCount(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		if got := stableLineCount(""); got != 0 {
			t.Fatalf("expected 0, got %d", got)
		}
	})

	t.Run("non_empty", func(t *testing.T) {
		if got := stableLineCount("a\nb"); got != 2 {
			t.Fatalf("expected 2, got %d", got)
		}
	})

	t.Run("trailing_newline", func(t *testing.T) {
		if got := stableLineCount("a\nb\n"); got != 2 {
			t.Fatalf("expected 2, got %d", got)
		}
	})

	t.Run("only_empty_lines", func(t *testing.T) {
		if got := stableLineCount("\n\n"); got != 2 {
			t.Fatalf("expected 2, got %d", got)
		}
	})
}
