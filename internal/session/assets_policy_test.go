package session

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDefaultAssetPolicy(t *testing.T) {
	t.Parallel()

	policy := DefaultAssetPolicy()
	if policy.MaxSessionAssetBytes != MaxSessionAssetBytes {
		t.Fatalf("expected default max session asset bytes %d, got %d", MaxSessionAssetBytes, policy.MaxSessionAssetBytes)
	}
}

func TestNormalizeAssetPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   AssetPolicy
		want int64
	}{
		{
			name: "non-positive uses default",
			in:   AssetPolicy{MaxSessionAssetBytes: 0},
			want: MaxSessionAssetBytes,
		},
		{
			name: "caps at hard limit",
			in:   AssetPolicy{MaxSessionAssetBytes: MaxSessionAssetBytes + 1},
			want: MaxSessionAssetBytes,
		},
		{
			name: "keeps valid value",
			in:   AssetPolicy{MaxSessionAssetBytes: 1024},
			want: 1024,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeAssetPolicy(tt.in)
			if got.MaxSessionAssetBytes != tt.want {
				t.Fatalf("NormalizeAssetPolicy(%+v) max=%d, want=%d", tt.in, got.MaxSessionAssetBytes, tt.want)
			}
		})
	}
}

func TestDefaultTextAssetWhitelist(t *testing.T) {
	t.Parallel()

	whitelist := DefaultTextAssetWhitelist()
	cases := []struct {
		ext      string
		fileName string
		mime     string
		want     string
	}{
		{ext: "txt", fileName: "notes.txt", want: "text/plain"},
		{ext: "md", fileName: "README.md", want: "text/markdown"},
		{ext: "json", fileName: "config.json", want: "application/json"},
		{ext: "yaml", fileName: "values.yaml", want: "text/yaml"},
		{ext: "yml", fileName: "values.yml", want: "application/x-yaml"},
		{ext: "csv", fileName: "data.csv", want: "text/csv"},
	}
	for _, tc := range cases {
		if got := whitelist.LookupByExtension(tc.fileName); got != tc.want {
			t.Errorf("LookupByExtension(%q) = %q, want %q", tc.fileName, got, tc.want)
		}
		if !whitelist.LookupByMime(tc.want) {
			t.Errorf("LookupByMime(%q) = false, want true", tc.want)
		}
	}
	// 路径无关性：basename 提取正确。
	if got := whitelist.LookupByExtension("/tmp/sub/notes.txt"); got != "text/plain" {
		t.Errorf("LookupByExtension with dir prefix = %q, want text/plain", got)
	}
	// 命中后白名单应非空。
	if whitelist.IsEmpty() {
		t.Fatalf("default text whitelist should not be empty")
	}
	// 不在白名单的扩展名/MIME 应返回空/否。
	if got := whitelist.LookupByExtension("page.html"); got != "" {
		t.Errorf("LookupByExtension(.html) = %q, want empty", got)
	}
	if whitelist.LookupByMime("text/html") {
		t.Errorf("LookupByMime(text/html) = true, want false")
	}
}

func TestTextAssetWhitelistWithExtensions(t *testing.T) {
	t.Parallel()

	whitelist := DefaultTextAssetWhitelist().WithExtensions(map[string]string{
		"log": "text/plain",
		"tsv": "text/tab-separated-values",
	})
	if got := whitelist.LookupByExtension("debug.log"); got != "text/plain" {
		t.Errorf("LookupByExtension(.log) = %q, want text/plain", got)
	}
	if !whitelist.LookupByMime("text/tab-separated-values") {
		t.Errorf("LookupByMime(text/tab-separated-values) = false, want true")
	}
	// 默认项保留。
	if got := whitelist.LookupByExtension("data.csv"); got != "text/csv" {
		t.Errorf("LookupByExtension(.csv) = %q, want text/csv (default preserved)", got)
	}
}

func TestTextAssetWhitelistEmptyAndInvalidEntries(t *testing.T) {
	t.Parallel()

	whitelist := NewTextAssetWhitelist(map[string]string{
		"":     "text/plain",
		".txt": "",
		".MD":  " TEXT/MARKDOWN ",
	})
	if whitelist.LookupByExtension("README.MD") != "text/markdown" {
		t.Fatalf("unexpected normalized markdown lookup: %q", whitelist.LookupByExtension("README.MD"))
	}
	if whitelist.LookupByExtension("README") != "" {
		t.Fatal("extensionless filename must not match")
	}
	if whitelist.LookupByMime(" ") {
		t.Fatal("empty MIME must not match")
	}

	empty := NewTextAssetWhitelist(nil)
	if got := empty.Extensions(); got != nil {
		t.Fatalf("empty Extensions() = %v, want nil", got)
	}
	if got := empty.String(); got != "empty" {
		t.Fatalf("empty String() = %q, want empty", got)
	}
	extended := empty.WithExtensions(map[string]string{"": "text/plain", "txt": ""})
	if !extended.IsEmpty() {
		t.Fatalf("invalid extensions must be ignored: %s", extended.String())
	}
	if got := whitelist.String(); !strings.Contains(got, "md→text/markdown") {
		t.Fatalf("String() = %q, want markdown mapping", got)
	}
}

func TestNormalizeTextAssetPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		in             TextAssetPolicy
		wantBytes      int64
		wantChars      int
		wantWhitelistN int
	}{
		{
			name:           "zero values use defaults",
			in:             TextAssetPolicy{},
			wantBytes:      DefaultMaxTextAssetBytes,
			wantChars:      DefaultMaxTextAssetChars,
			wantWhitelistN: len(DefaultTextAssetWhitelist().Extensions()),
		},
		{
			name: "byte cap hard limit",
			in: TextAssetPolicy{
				Whitelist:         DefaultTextAssetWhitelist(),
				MaxTextAssetBytes: MaxTextAssetBytesHardLimit + 1024,
				MaxTextAssetChars: DefaultMaxTextAssetChars,
			},
			wantBytes:      MaxTextAssetBytesHardLimit,
			wantChars:      DefaultMaxTextAssetChars,
			wantWhitelistN: len(DefaultTextAssetWhitelist().Extensions()),
		},
		{
			name: "char cap hard limit",
			in: TextAssetPolicy{
				Whitelist:         DefaultTextAssetWhitelist(),
				MaxTextAssetBytes: 1024,
				MaxTextAssetChars: MaxTextAssetCharsHardLimit + 100,
			},
			wantBytes:      1024,
			wantChars:      MaxTextAssetCharsHardLimit,
			wantWhitelistN: len(DefaultTextAssetWhitelist().Extensions()),
		},
		{
			name: "empty whitelist falls back to default",
			in: TextAssetPolicy{
				Whitelist:         NewTextAssetWhitelist(nil),
				MaxTextAssetBytes: 512,
				MaxTextAssetChars: 100,
			},
			wantBytes:      512,
			wantChars:      100,
			wantWhitelistN: len(DefaultTextAssetWhitelist().Extensions()),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeTextAssetPolicy(tt.in)
			if got.MaxTextAssetBytes != tt.wantBytes {
				t.Errorf("MaxTextAssetBytes = %d, want %d", got.MaxTextAssetBytes, tt.wantBytes)
			}
			if got.MaxTextAssetChars != tt.wantChars {
				t.Errorf("MaxTextAssetChars = %d, want %d", got.MaxTextAssetChars, tt.wantChars)
			}
			if n := len(got.Whitelist.Extensions()); n != tt.wantWhitelistN {
				t.Errorf("whitelist size = %d, want %d", n, tt.wantWhitelistN)
			}
		})
	}
}

func TestSanitizeTextAssetFileName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		raw      string
		fallback string
		want     string
	}{
		{name: "plain name kept", raw: "notes.md", want: "notes.md"},
		{name: "path separator collapsed", raw: "../etc/passwd", want: "passwd"},
		{name: "backslash collapsed", raw: `..\..\evil.md`, want: "evil.md"},
		{name: "absolute path", raw: "/var/log/app.log", want: "app.log"},
		{name: "control chars replaced", raw: "name\x00\x01.md", want: "name__.md"},
		{name: "quotes replaced", raw: "evil\"name`.md", want: "evil_name_.md"},
		{name: "empty falls back", raw: "", fallback: "fallback.txt", want: "fallback.txt"},
		{name: "all invalid empty", raw: "////", want: ""},
		{name: "dot dot returns empty", raw: "..", want: ""},
		{name: "trim spaces", raw: "  hello.txt  ", want: "hello.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeTextAssetFileName(tc.raw, tc.fallback)
			if got != tc.want {
				t.Errorf("SanitizeTextAssetFileName(%q, %q) = %q, want %q", tc.raw, tc.fallback, got, tc.want)
			}
		})
	}

	// 超长输入按字节裁剪到上限。
	longName := strings.Repeat("a", MaxTextAssetFileNameBytes+50) + ".md"
	got := SanitizeTextAssetFileName(longName, "")
	if len(got) > MaxTextAssetFileNameBytes {
		t.Errorf("sanitized name length %d exceeds limit %d", len(got), MaxTextAssetFileNameBytes)
	}

	// 多字节 UTF-8（中文）长文件名截断后必须保持合法 UTF-8，不在字符中间切断。
	chineseName := strings.Repeat("设计方案报告", 20) + ".md" // 每个中文字符 3 字节，远超 128 字节上限
	chineseGot := SanitizeTextAssetFileName(chineseName, "")
	if len(chineseGot) > MaxTextAssetFileNameBytes {
		t.Errorf("chinese name length %d exceeds limit %d", len(chineseGot), MaxTextAssetFileNameBytes)
	}
	if !utf8.ValidString(chineseGot) {
		t.Errorf("sanitized chinese name is not valid UTF-8: %q", chineseGot)
	}
}
