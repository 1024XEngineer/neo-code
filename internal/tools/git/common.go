package git

import (
	"path/filepath"
	"strings"
)

func effectiveRoot(defaultRoot string, workdir string) string {
	base := strings.TrimSpace(workdir)
	if base == "" {
		base = defaultRoot
	}
	return base
}

func formatFileList(files []fileEntry, totalCount int, truncated bool) string {
	var b strings.Builder
	b.WriteString("returned_count: ")
	b.WriteString(itoa(len(files)))
	b.WriteString("\ntotal_count: ")
	b.WriteString(itoa(totalCount))
	b.WriteString("\ntruncated: ")
	b.WriteString(boolToString(truncated))
	if len(files) > 0 {
		b.WriteString("\n")
	}
	for _, f := range files {
		b.WriteString("\n- status: ")
		b.WriteString(f.Status)
		b.WriteString("\n  path: ")
		b.WriteString(filepath.ToSlash(f.Path))
		if f.OldPath != "" {
			b.WriteString("\n  old_path: ")
			b.WriteString(filepath.ToSlash(f.OldPath))
		}
		if f.Snippet != "" {
			b.WriteString("\n  snippet: |\n")
			for _, line := range strings.Split(f.Snippet, "\n") {
				b.WriteString("    ")
				b.WriteString(line)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

type fileEntry struct {
	Status  string
	Path    string
	OldPath string
	Snippet string
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	negative := i < 0
	if negative {
		i = -i
	}
	var buf [20]byte
	bp := len(buf)
	for i > 0 {
		bp--
		buf[bp] = byte('0' + i%10)
		i /= 10
	}
	if negative {
		bp--
		buf[bp] = '-'
	}
	return string(buf[bp:])
}

func boolToString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
