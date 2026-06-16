package convert

import (
	"strings"
)

func init() {
	Registry["table"] = renderTable
}

func renderTable(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	if tableHasSpans(node) || tableHasComplexCells(node) {
		renderHTMLTable(node, ctx, buf)
	} else {
		renderGFMTable(node, ctx, buf)
	}
	buf.WriteString("\n")
}

func tableHasSpans(node ADFNode) bool {
	for _, row := range node.Content {
		for _, cell := range row.Content {
			if attrInt(cell, "rowspan", 1) > 1 || attrInt(cell, "colspan", 1) > 1 {
				return true
			}
		}
	}
	return false
}

// complexCellTypes are ADF node types that cannot be faithfully represented
// inside a GFM pipe-table cell and require full HTML output.
var complexCellTypes = map[string]bool{
	"heading":      true,
	"codeBlock":    true,
	"bulletList":   true,
	"orderedList":  true,
	"taskList":     true,
	"decisionList": true,
	"table":        true, // nested table
}

func tableHasComplexCells(node ADFNode) bool {
	for _, row := range node.Content {
		for _, cell := range row.Content {
			if cellHasComplexContent(cell) {
				return true
			}
		}
	}
	return false
}

func cellHasComplexContent(node ADFNode) bool {
	for _, child := range node.Content {
		if complexCellTypes[child.Type] {
			return true
		}
		if cellHasComplexContent(child) {
			return true
		}
	}
	return false
}

func renderGFMTable(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	rows := node.Content
	if len(rows) == 0 {
		return
	}

	// Render all rows, treating the first as the header
	var rendered [][]string
	for _, row := range rows {
		var cells []string
		for _, cell := range row.Content {
			var inner strings.Builder
			walkChildren(cell, ctx, &inner)
			// Collapse newlines inside a cell to spaces
			text := strings.ReplaceAll(strings.TrimSpace(inner.String()), "\n", " ")
			// Escape pipe characters
			text = strings.ReplaceAll(text, "|", "\\|")
			cells = append(cells, text)
		}
		rendered = append(rendered, cells)
	}

	// Normalize column count
	cols := 0
	for _, r := range rendered {
		if len(r) > cols {
			cols = len(r)
		}
	}

	writeRow := func(cells []string) {
		buf.WriteString("|")
		for i := 0; i < cols; i++ {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			buf.WriteString(" " + cell + " |")
		}
		buf.WriteString("\n")
	}

	writeSep := func() {
		buf.WriteString("|")
		for i := 0; i < cols; i++ {
			buf.WriteString(" --- |")
		}
		buf.WriteString("\n")
	}

	writeRow(rendered[0])
	writeSep()
	for _, row := range rendered[1:] {
		writeRow(row)
	}
}

func renderHTMLTable(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	buf.WriteString("<table>\n")
	for _, row := range node.Content {
		buf.WriteString("<tr>")
		for _, cell := range row.Content {
			tag := "td"
			if cell.Type == "tableHeader" {
				tag = "th"
			}
			attrs := ""
			if cs := attrInt(cell, "colspan", 1); cs > 1 {
				attrs += ` colspan="` + itoa(cs) + `"`
			}
			if rs := attrInt(cell, "rowspan", 1); rs > 1 {
				attrs += ` rowspan="` + itoa(rs) + `"`
			}
			buf.WriteString("<" + tag + attrs + ">")
			var inner strings.Builder
			renderCellHTML(cell, ctx, &inner)
			buf.WriteString(strings.TrimSpace(inner.String()))
			buf.WriteString("</" + tag + ">")
		}
		buf.WriteString("</tr>\n")
	}
	buf.WriteString("</table>")
}

// renderCellHTML renders cell content as HTML so that block elements like headings
// are preserved correctly inside <td>/<th> (markdown syntax doesn't work there).
func renderCellHTML(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	for _, child := range node.Content {
		renderNodeHTML(child, ctx, buf)
	}
}

func renderNodeHTML(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	switch node.Type {
	case "heading":
		lvl := attrInt(node, "level", 1)
		if lvl < 1 {
			lvl = 1
		}
		if lvl > 6 {
			lvl = 6
		}
		tag := "h" + itoa(lvl)
		buf.WriteString("<" + tag + ">")
		var inner strings.Builder
		walkChildren(node, ctx, &inner)
		buf.WriteString(strings.TrimSpace(inner.String()))
		buf.WriteString("</" + tag + ">")
	case "paragraph":
		var inner strings.Builder
		walkChildren(node, ctx, &inner)
		text := strings.TrimSpace(inner.String())
		if text != "" {
			buf.WriteString("<p>" + text + "</p>")
		}
	case "bulletList", "orderedList":
		tag := "ul"
		if node.Type == "orderedList" {
			tag = "ol"
		}
		buf.WriteString("<" + tag + ">")
		for _, item := range node.Content {
			buf.WriteString("<li>")
			var inner strings.Builder
			renderCellHTML(item, ctx, &inner)
			buf.WriteString(strings.TrimSpace(inner.String()))
			buf.WriteString("</li>")
		}
		buf.WriteString("</" + tag + ">")
	case "codeBlock":
		buf.WriteString("<pre><code>")
		var inner strings.Builder
		walkChildren(node, ctx, &inner)
		buf.WriteString(strings.TrimSpace(inner.String()))
		buf.WriteString("</code></pre>")
	case "taskList", "decisionList":
		buf.WriteString("<ul>")
		for _, item := range node.Content {
			state := attrString(item, "state", "")
			checkbox := "[ ] "
			if state == "DONE" || state == "DECIDED" {
				checkbox = "[x] "
			}
			buf.WriteString("<li>" + checkbox)
			var inner strings.Builder
			renderCellHTML(item, ctx, &inner)
			buf.WriteString(strings.TrimSpace(inner.String()))
			buf.WriteString("</li>")
		}
		buf.WriteString("</ul>")
	default:
		// For inline nodes and anything else, fall back to the markdown registry
		// (inline text, marks, etc. render fine as markdown inside HTML)
		walkChildren(node, ctx, buf)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 10)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
