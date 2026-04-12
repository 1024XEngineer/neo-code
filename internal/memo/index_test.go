package memo

import (
	"strings"
	"testing"
)

func TestRenderIndexEmpty(t *testing.T) {
	got := RenderIndex(&Index{})
	if got != "" {
		t.Errorf("RenderIndex(empty) = %q, want empty", got)
	}
	got = RenderIndex(nil)
	if got != "" {
		t.Errorf("RenderIndex(nil) = %q, want empty", got)
	}
}

func TestRenderIndexSingleEntry(t *testing.T) {
	index := &Index{
		Entries: []Entry{
			{Type: TypeUser, Title: "偏好 tab 缩进", TopicFile: "user_profile.md"},
		},
	}
	got := RenderIndex(index)
	if !strings.Contains(got, "## User") {
		t.Errorf("RenderIndex missing ## User header: %q", got)
	}
	if !strings.Contains(got, "[user] 偏好 tab 缩进 (user_profile.md)") {
		t.Errorf("RenderIndex missing entry line: %q", got)
	}
}

func TestRenderIndexMultipleTypes(t *testing.T) {
	index := &Index{
		Entries: []Entry{
			{Type: TypeUser, Title: "偏好 tab", TopicFile: "user.md"},
			{Type: TypeFeedback, Title: "不要 mock 数据库", TopicFile: "feedback.md"},
			{Type: TypeProject, Title: "使用 Bubble Tea", TopicFile: "project.md"},
			{Type: TypeReference, Title: "Claude Code 设计", TopicFile: "ref.md"},
		},
	}
	got := RenderIndex(index)
	for _, header := range []string{"## User", "## Feedback", "## Project", "## Reference"} {
		if !strings.Contains(got, header) {
			t.Errorf("RenderIndex missing header %q in: %q", header, got)
		}
	}
}

func TestRenderIndexNoTopicFile(t *testing.T) {
	index := &Index{
		Entries: []Entry{
			{Type: TypeUser, Title: "无文件"},
		},
	}
	got := RenderIndex(index)
	if strings.Contains(got, "()") {
		t.Errorf("RenderIndex should not have empty parens: %q", got)
	}
	if !strings.Contains(got, "[user] 无文件") {
		t.Errorf("RenderIndex missing entry: %q", got)
	}
}

func TestParseIndexEmpty(t *testing.T) {
	idx, err := ParseIndex("")
	if err != nil {
		t.Fatalf("ParseIndex(\"\") error: %v", err)
	}
	if len(idx.Entries) != 0 {
		t.Errorf("ParseIndex(\"\") entries = %d, want 0", len(idx.Entries))
	}

	idx, err = ParseIndex("  \n  \n")
	if err != nil {
		t.Fatalf("ParseIndex(whitespace) error: %v", err)
	}
	if len(idx.Entries) != 0 {
		t.Errorf("ParseIndex(whitespace) entries = %d, want 0", len(idx.Entries))
	}
}

func TestParseIndexSingleEntry(t *testing.T) {
	content := "## User\n- [user] 偏好 tab 缩进 (user_profile.md)\n"
	idx, err := ParseIndex(content)
	if err != nil {
		t.Fatalf("ParseIndex error: %v", err)
	}
	if len(idx.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(idx.Entries))
	}
	e := idx.Entries[0]
	if e.Type != TypeUser {
		t.Errorf("Type = %q, want %q", e.Type, TypeUser)
	}
	if e.Title != "偏好 tab 缩进" {
		t.Errorf("Title = %q, want %q", e.Title, "偏好 tab 缩进")
	}
	if e.TopicFile != "user_profile.md" {
		t.Errorf("TopicFile = %q, want %q", e.TopicFile, "user_profile.md")
	}
}

func TestParseIndexRoundTrip(t *testing.T) {
	original := &Index{
		Entries: []Entry{
			{Type: TypeUser, Title: "偏好 tab 缩进", TopicFile: "user.md"},
			{Type: TypeFeedback, Title: "不要 mock DB", TopicFile: "feedback.md"},
			{Type: TypeProject, Title: "使用 Bubble Tea", TopicFile: "project.md"},
		},
	}
	rendered := RenderIndex(original)
	parsed, err := ParseIndex(rendered)
	if err != nil {
		t.Fatalf("ParseIndex round-trip error: %v", err)
	}
	if len(parsed.Entries) != len(original.Entries) {
		t.Fatalf("round-trip entries: got %d, want %d", len(parsed.Entries), len(original.Entries))
	}
	for i, orig := range original.Entries {
		got := parsed.Entries[i]
		if got.Type != orig.Type {
			t.Errorf("entry[%d].Type = %q, want %q", i, got.Type, orig.Type)
		}
		if got.Title != orig.Title {
			t.Errorf("entry[%d].Title = %q, want %q", i, got.Title, orig.Title)
		}
		if got.TopicFile != orig.TopicFile {
			t.Errorf("entry[%d].TopicFile = %q, want %q", i, got.TopicFile, orig.TopicFile)
		}
	}
}

func TestParseIndexInvalidLines(t *testing.T) {
	content := "## User\nrandom text\n- [invalid_type] foo (bar.md)\n- [user] valid entry (ok.md)\n"
	idx, err := ParseIndex(content)
	if err != nil {
		t.Fatalf("ParseIndex error: %v", err)
	}
	if len(idx.Entries) != 1 {
		t.Fatalf("entries = %d, want 1 (invalid lines should be skipped)", len(idx.Entries))
	}
	if idx.Entries[0].Title != "valid entry" {
		t.Errorf("Title = %q, want %q", idx.Entries[0].Title, "valid entry")
	}
}

func TestRenderTopic(t *testing.T) {
	entry := &Entry{
		Type:     TypeUser,
		Title:    "偏好 tab 缩进",
		Content:  "用户偏好使用 tab 缩进和中文注释",
		Keywords: []string{"tabs", "chinese"},
		Source:   SourceUserManual,
	}
	got := RenderTopic(entry)
	if !strings.Contains(got, "---") {
		t.Error("RenderTopic missing frontmatter delimiter")
	}
	if !strings.Contains(got, "type: user") {
		t.Error("RenderTopic missing type in frontmatter")
	}
	if !strings.Contains(got, "source: user_manual") {
		t.Error("RenderTopic missing source in frontmatter")
	}
	if !strings.Contains(got, "keywords: [tabs, chinese]") {
		t.Error("RenderTopic missing keywords in frontmatter")
	}
	if !strings.Contains(got, "用户偏好使用 tab 缩进和中文注释") {
		t.Error("RenderTopic missing content")
	}
}

func TestRenderTopicNoKeywords(t *testing.T) {
	entry := &Entry{
		Type:    TypeFeedback,
		Title:   "test",
		Content: "content",
		Source:  SourceAutoExtract,
	}
	got := RenderTopic(entry)
	if strings.Contains(got, "keywords:") {
		t.Errorf("RenderTopic should not contain keywords when empty: %q", got)
	}
}

func TestParseIndexLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		line      string
		wantOK    bool
		wantType  Type
		wantTitle string
		wantTopic string
	}{
		{"valid user entry", "- [user] 偏好 tab (user_profile.md)", true, TypeUser, "偏好 tab", "user_profile.md"},
		{"entry without topic file", "- [feedback] 不要 mock", true, TypeFeedback, "不要 mock", ""},
		{"invalid prefix", "random text", false, "", "", ""},
		{"missing close bracket", "- [user foo", false, "", "", ""},
		{"invalid type", "- [invalid] foo (bar.md)", false, "", "", ""},
		{"empty rest after bracket", "- [user]", false, "", "", ""},
		{"project type", "- [project] 使用 Bubble Tea (proj.md)", true, TypeProject, "使用 Bubble Tea", "proj.md"},
		{"reference type", "- [reference] Claude Code 设计", true, TypeReference, "Claude Code 设计", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, ok := parseIndexLine(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if entry.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", entry.Type, tt.wantType)
			}
			if entry.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", entry.Title, tt.wantTitle)
			}
			if entry.TopicFile != tt.wantTopic {
				t.Errorf("TopicFile = %q, want %q", entry.TopicFile, tt.wantTopic)
			}
		})
	}
}

func TestTypeDisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		typ  Type
		want string
	}{
		{TypeUser, "User"},
		{TypeFeedback, "Feedback"},
		{TypeProject, "Project"},
		{TypeReference, "Reference"},
		{Type("unknown"), "unknown"},
	}
	for _, tt := range tests {
		got := typeDisplayName(tt.typ)
		if got != tt.want {
			t.Errorf("typeDisplayName(%q) = %q, want %q", tt.typ, got, tt.want)
		}
	}
}

func TestNormalizeTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"normal text", "hello world", "hello world"},
		{"extra spaces", "  hello   world  ", "hello world"},
		{"newlines", "line1\nline2", "line1 line2"},
		{"parens replaced", "use (default)", "use {default}"},
		{"tabs and spaces", "\thello\t world", "hello world"},
		{"empty", "   ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeTitle(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeTitle(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTopicNameFromEntry(t *testing.T) {
	t.Parallel()

	t.Run("with TopicFile", func(t *testing.T) {
		entry := &Entry{TopicFile: "user_profile.md"}
		got := topicNameFromEntry(entry)
		if got != "user_profile" {
			t.Errorf("topicNameFromEntry = %q, want %q", got, "user_profile")
		}
	})

	t.Run("without TopicFile", func(t *testing.T) {
		entry := &Entry{Type: TypeUser, Source: SourceUserManual}
		got := topicNameFromEntry(entry)
		if got != "user_user_manual" {
			t.Errorf("topicNameFromEntry = %q, want %q", got, "user_user_manual")
		}
	})
}
