package convert

import (
	"encoding/json"
	"strings"
)

// Registry maps ADF node types to their renderer functions.
var Registry = map[string]func(ADFNode, *RenderContext, *strings.Builder){}

// RenderContext carries state threaded through the recursive walk.
type RenderContext struct {
	ListDepth  int
	ListType   string // "bullet" or "ordered"
	OrderedIdx int
}

func init() {
	registerBlocks()
	registerInlines()
}

// Walk dispatches node to its registered renderer, falling back to walking children
// for unknown node types so future ADF additions degrade gracefully.
func Walk(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	if fn, ok := Registry[node.Type]; ok {
		fn(node, ctx, buf)
		return
	}
	// Unknown node — silently walk content
	for _, child := range node.Content {
		Walk(child, ctx, buf)
	}
}

// walkChildren walks all children of a node.
func walkChildren(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	for _, child := range node.Content {
		Walk(child, ctx, buf)
	}
}

// adfToMarkdown is the internal conversion entry point used by the new ADF pipeline.
// It unmarshals ADF JSON and renders to Markdown.
func adfToMarkdown(adfJSON string) (string, error) {
	var doc ADFDoc
	if err := json.Unmarshal([]byte(adfJSON), &doc); err != nil {
		return "", err
	}

	var buf strings.Builder
	ctx := &RenderContext{}

	for _, node := range doc.Content {
		Walk(node, ctx, &buf)
	}

	return normalizeMarkdown(buf.String()), nil
}
