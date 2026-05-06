package repository

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// searchSymbolsWithTreeSitter 使用 Tree-sitter 在非 Go 文件中搜索符号定义。
// 它遍历工作区，对支持的文件类型执行 tags.scm query，匹配 name capture。
func searchSymbolsWithTreeSitter(
	ctx context.Context,
	root string,
	scope string,
	symbol string,
	readFile FileReader,
	effectiveLimit int,
) ([]SymbolSearchHit, error) {
	hits := make([]SymbolSearchHit, 0, effectiveLimit)

	err := walkWorkspaceFiles(ctx, root, scope, func(path string) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if len(hits) >= effectiveLimit {
			return errRetrievalLimitReached
		}
		// Skip Go files — handled by Go AST path.
		if filepath.Ext(path) == ".go" {
			return nil
		}

		entry := grammars.DetectLanguage(filepath.Base(path))
		if entry == nil {
			return nil
		}

		content, ok := readRetrievalTextWithReader(root, path, readFile)
		if !ok {
			return nil
		}

		// Fast path: skip files that do not contain the symbol text at all.
		if !strings.Contains(content, symbol) {
			return nil
		}

		lang := entry.Language()
		tagsQuery := grammars.ResolveTagsQuery(*entry)
		if tagsQuery == "" {
			return nil
		}

		parser := gotreesitter.NewParser(lang)
		tree, parseErr := parser.Parse([]byte(content))
		if parseErr != nil {
			return nil
		}
		rootNode := tree.RootNode()

		query, queryErr := gotreesitter.NewQuery(tagsQuery, lang)
		if queryErr != nil {
			return nil
		}

		cursor := query.Exec(rootNode, lang, []byte(content))
		src := []byte(content)

		for {
			match, ok := cursor.NextMatch()
			if !ok {
				break
			}
			if len(hits) >= effectiveLimit {
				return errRetrievalLimitReached
			}

			var defNode *gotreesitter.Node
			var defKind string
			var nameText string
			var nameLine int

			for _, cap := range match.Captures {
				capName := cap.Name
				if capName == "name" {
					nameText = strings.TrimSpace(cap.Text(src))
					nameLine = int(cap.Node.StartPoint().Row) + 1
				}
				if strings.HasPrefix(capName, "definition.") {
					defNode = cap.Node
					defKind = captureNameToKind(capName)
				}
			}

			if nameText != symbol {
				continue
			}

			sig := ""
			if defNode != nil {
				sig = extractTreeSitterSignature(src, defNode)
			}
			if sig == "" && nameLine > 0 {
				sig = extractLineSignature(content, nameLine)
			}

			rel, _ := filepath.Rel(root, path)
			hits = append(hits, SymbolSearchHit{
				Path:      filepath.Clean(rel),
				LineHint:  nameLine,
				Kind:      defKind,
				Signature: sig,
			})
		}
		return nil
	})

	if err != nil {
		if errors.Is(err, errRetrievalLimitReached) {
			err = nil
		}
	}
	if err != nil {
		return nil, err
	}
	return hits, nil
}

// captureNameToKind 将 tags.scm capture 名称映射到统一的符号类别。
func captureNameToKind(capName string) string {
	switch capName {
	case "definition.function":
		return "function"
	case "definition.method":
		return "method"
	case "definition.class":
		return "class"
	case "definition.type":
		return "type"
	case "definition.variable":
		return "variable"
	case "definition.interface":
		return "interface"
	case "definition.constant":
		return "constant"
	}
	return "unknown"
}

// extractTreeSitterSignature 从定义节点提取签名，限制在 maxSignatureLength 内。
func extractTreeSitterSignature(src []byte, node *gotreesitter.Node) string {
	text := node.Text(src)
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}
	// Take the first line and trim trailing brace body start.
	sig := strings.TrimSpace(lines[0])
	if idx := strings.Index(sig, "{"); idx >= 0 {
		sig = strings.TrimSpace(sig[:idx])
	}
	if len(sig) > maxSignatureLength {
		sig = sig[:maxSignatureLength]
	}
	return sig
}

// extractLineSignature 从文件内容的指定行提取原始文本作为签名。
func extractLineSignature(content string, lineNumber int) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if lineNumber <= 0 || lineNumber > len(lines) {
		return ""
	}
	sig := strings.TrimSpace(lines[lineNumber-1])
	if len(sig) > maxSignatureLength {
		sig = sig[:maxSignatureLength]
	}
	return sig
}

// readRetrievalTextWithReader 使用给定的 reader 读取检索候选文件。
func readRetrievalTextWithReader(root string, path string, readFile FileReader) (string, bool) {
	target, _, allowed, err := resolveRepositorySnippetFileFromRoot(root, path)
	if err != nil || !allowed {
		return "", false
	}
	content, err := readFile(target)
	if err != nil || isBinaryContent(content) {
		return "", false
	}
	return string(content), true
}
