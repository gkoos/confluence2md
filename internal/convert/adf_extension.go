package convert

import "strings"

// renderExtensionNode handles both bodiedExtension and inlineExtension/extension nodes.
// Only the "jira" extensionKey is special-cased; all others are silently skipped
// (content is walked so nested text nodes are still emitted for bodiedExtensions).
func renderExtensionNode(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	key := attrString(node, "extensionKey", "")

	switch key {
	case "jira":
		params := macroParams(node)
		issueKey := params["key"]
		if issueKey == "" {
			issueKey = params["issueKey"]
		}
		if issueKey != "" {
			buf.WriteString("[" + issueKey + "](https://jira.atlassian.net/browse/" + issueKey + ")")
		}
	default:
		// Unknown macro — walk children so any nested text content is preserved
		// for bodiedExtensions; inlineExtensions typically have no content.
		walkChildren(node, ctx, buf)
	}
}

// macroParams extracts the macroParams object from an extension node's attrs.
// ADF stores macro parameters as a nested object at attrs.macroParams or attrs.parameters.
func macroParams(node ADFNode) map[string]string {
	out := make(map[string]string)
	if node.Attrs == nil {
		return out
	}

	// Try attrs.macroParams first, then attrs.parameters
	for _, key := range []string{"macroParams", "parameters"} {
		raw, ok := node.Attrs[key]
		if !ok {
			continue
		}
		params, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for k, v := range params {
			switch val := v.(type) {
			case string:
				out[k] = val
			case map[string]any:
				// ADF wraps each param as { "value": "..." }
				if s, ok := val["value"].(string); ok {
					out[k] = s
				}
			}
		}
		return out
	}
	return out
}
