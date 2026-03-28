package webfetch

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

var skippedHTMLTags = map[string]struct{}{
	"canvas":   {},
	"head":     {},
	"noscript": {},
	"script":   {},
	"style":    {},
	"svg":      {},
	"template": {},
}

func extractHTMLContent(body []byte) (string, string, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("parse document: %w", err)
	}

	root := findElement(doc, "body")
	if root == nil {
		root = doc
	}

	text := normalizeWhitespace(collectVisibleText(root))
	title := normalizeWhitespace(textContent(findElement(doc, "title")))
	return text, title, nil
}

func findElement(node *html.Node, tag string) *html.Node {
	if node == nil {
		return nil
	}
	if node.Type == html.ElementNode && strings.EqualFold(node.Data, tag) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElement(child, tag); found != nil {
			return found
		}
	}
	return nil
}

func collectVisibleText(node *html.Node) string {
	var builder strings.Builder
	appendVisibleText(&builder, node)
	return builder.String()
}

func appendVisibleText(builder *strings.Builder, node *html.Node) {
	if node == nil {
		return
	}
	if shouldSkipNode(node) {
		return
	}

	if node.Type == html.TextNode {
		text := strings.TrimSpace(node.Data)
		if text != "" {
			builder.WriteString(text)
			builder.WriteByte('\n')
		}
	}

	for child := node.FirstChild; child != nil; child = child.NextSibling {
		appendVisibleText(builder, child)
	}
}

func shouldSkipNode(node *html.Node) bool {
	if node == nil || node.Type != html.ElementNode {
		return false
	}
	_, skip := skippedHTMLTags[strings.ToLower(node.Data)]
	return skip
}

func textContent(node *html.Node) string {
	if node == nil {
		return ""
	}

	var builder strings.Builder
	var visit func(*html.Node)
	visit = func(current *html.Node) {
		if current == nil {
			return
		}
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
			builder.WriteByte(' ')
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(node)
	return builder.String()
}

func normalizeWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
