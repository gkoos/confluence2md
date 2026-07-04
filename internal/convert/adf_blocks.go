package convert

import (
	"fmt"
	"strings"
)

func registerBlocks() {
	Registry["paragraph"] = renderParagraph
	Registry["heading"] = renderHeading
	Registry["bulletList"] = renderBulletList
	Registry["orderedList"] = renderOrderedList
	Registry["listItem"] = renderListItem
	Registry["taskList"] = renderTaskList
	Registry["taskItem"] = renderTaskItem
	Registry["decisionList"] = renderDecisionList
	Registry["decisionItem"] = renderDecisionItem
	Registry["caption"] = renderCaption
	Registry["codeBlock"] = renderCodeBlock
	Registry["blockquote"] = renderBlockquote
	Registry["rule"] = renderRule
	Registry["expand"] = renderExpand
	Registry["nestedExpand"] = renderExpand
	Registry["panel"] = renderPanel
	Registry["layoutSection"] = renderPassthrough
	Registry["layoutColumn"] = renderPassthrough
	Registry["mediaSingle"] = renderMediaSingle
	Registry["mediaGroup"] = renderMediaGroup
	Registry["bodiedExtension"] = renderBodiedExtension
}

// ensureBlankLine ensures the buffer ends with at least one blank line (two newlines)
// before a block-level element is written. This prevents inline content (e.g. images
// without trailing newlines) from running into the following block.
func ensureBlankLine(buf *strings.Builder) {
	s := buf.String()
	if s == "" {
		return
	}
	if !strings.HasSuffix(s, "\n\n") {
		if strings.HasSuffix(s, "\n") {
			buf.WriteString("\n")
		} else {
			buf.WriteString("\n\n")
		}
	}
}

// renderMediaSingle wraps a single media item as a block (ensures blank lines around it).
func renderMediaSingle(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	ensureBlankLine(buf)
	walkChildren(node, ctx, buf)
	ensureBlankLine(buf)
}

// renderMediaGroup wraps a group of media items as a block (ensures blank lines around them).
func renderMediaGroup(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	ensureBlankLine(buf)
	walkChildren(node, ctx, buf)
	ensureBlankLine(buf)
}

func renderParagraph(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	var inner strings.Builder
	walkChildren(node, ctx, &inner)
	text := inner.String()
	if strings.TrimSpace(text) == "" {
		return
	}
	ensureBlankLine(buf)
	buf.WriteString(text)
	buf.WriteString("\n\n")
}

func renderHeading(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	level := max(attrInt(node, "level", 1), 1)
	if level > 6 {
		level = 6
	}
	ensureBlankLine(buf)
	buf.WriteString(strings.Repeat("#", level))
	buf.WriteString(" ")
	walkChildren(node, ctx, buf)
	buf.WriteString("\n\n")
}

func renderBulletList(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	prev := ctx.ListType
	prevDepth := ctx.ListDepth
	ctx.ListType = "bullet"
	ctx.ListDepth++
	walkChildren(node, ctx, buf)
	buf.WriteString("\n")
	ctx.ListType = prev
	ctx.ListDepth = prevDepth
}

func renderOrderedList(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	prev := ctx.ListType
	prevDepth := ctx.ListDepth
	prevIdx := ctx.OrderedIdx
	ctx.ListType = "ordered"
	ctx.ListDepth++
	ctx.OrderedIdx = 0
	walkChildren(node, ctx, buf)
	buf.WriteString("\n")
	ctx.ListType = prev
	ctx.ListDepth = prevDepth
	ctx.OrderedIdx = prevIdx
}

func renderListItem(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	indent := strings.Repeat("  ", ctx.ListDepth-1)
	var prefix string
	if ctx.ListType == "ordered" {
		ctx.OrderedIdx++
		prefix = fmt.Sprintf("%d. ", ctx.OrderedIdx)
	} else {
		prefix = "- "
	}

	var inner strings.Builder
	walkChildren(node, ctx, &inner)
	text := strings.TrimRight(inner.String(), "\n")

	// Indent continuation lines
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i == 0 {
			buf.WriteString(indent + prefix + line + "\n")
		} else if strings.TrimSpace(line) != "" {
			buf.WriteString(indent + strings.Repeat(" ", len(prefix)) + line + "\n")
		} else {
			buf.WriteString("\n")
		}
	}
}

func renderTaskList(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	prev := ctx.ListType
	prevDepth := ctx.ListDepth
	ctx.ListType = "bullet"
	ctx.ListDepth++
	walkChildren(node, ctx, buf)
	buf.WriteString("\n")
	ctx.ListType = prev
	ctx.ListDepth = prevDepth
}

func renderTaskItem(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	depth := max(ctx.ListDepth-1, 0)
	indent := strings.Repeat("  ", depth)
	var prefix string
	if attrString(node, "state", "") == "DONE" {
		prefix = "- [x] "
	} else {
		prefix = "- [ ] "
	}
	var inner strings.Builder
	walkChildren(node, ctx, &inner)
	buf.WriteString(indent + prefix + strings.TrimRight(inner.String(), "\n") + "\n")
}

func renderDecisionList(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	prev := ctx.ListType
	prevDepth := ctx.ListDepth
	ctx.ListType = "bullet"
	ctx.ListDepth++
	walkChildren(node, ctx, buf)
	buf.WriteString("\n")
	ctx.ListType = prev
	ctx.ListDepth = prevDepth
}

func renderDecisionItem(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	depth := max(ctx.ListDepth-1, 0)
	indent := strings.Repeat("  ", depth)
	var prefix string
	if attrString(node, "state", "") == "DECIDED" {
		prefix = "- [x] "
	} else {
		prefix = "- [ ] "
	}
	var inner strings.Builder
	walkChildren(node, ctx, &inner)
	buf.WriteString(indent + prefix + strings.TrimRight(inner.String(), "\n") + "\n")
}

func renderCaption(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	var inner strings.Builder
	walkChildren(node, ctx, &inner)
	text := strings.TrimSpace(inner.String())
	if text != "" {
		buf.WriteString("*" + text + "*\n")
	}
}

func renderCodeBlock(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	lang := attrString(node, "language", "")
	buf.WriteString("```" + lang + "\n")
	for _, child := range node.Content {
		buf.WriteString(child.Text)
	}
	buf.WriteString("\n```\n\n")
}

func renderBlockquote(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	var inner strings.Builder
	walkChildren(node, ctx, &inner)
	text := strings.TrimRight(inner.String(), "\n")
	for line := range strings.SplitSeq(text, "\n") {
		buf.WriteString("> " + line + "\n")
	}
	buf.WriteString("\n")
}

func renderRule(_ ADFNode, _ *RenderContext, buf *strings.Builder) {
	buf.WriteString("---\n\n")
}

func renderExpand(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	title := attrString(node, "title", "")
	if title != "" {
		buf.WriteString("**" + title + "**\n\n")
	}
	walkChildren(node, ctx, buf)
}

func renderPanel(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	panelType := attrString(node, "panelType", "")
	var inner strings.Builder
	walkChildren(node, ctx, &inner)
	text := strings.TrimRight(inner.String(), "\n")
	prefix := "> "
	if panelType != "" {
		prefix = fmt.Sprintf("> **%s:** ", strings.ToUpper(panelType[:1])+panelType[1:])
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i == 0 {
			buf.WriteString(prefix + line + "\n")
		} else {
			buf.WriteString("> " + line + "\n")
		}
	}
	buf.WriteString("\n")
}

func renderPassthrough(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	walkChildren(node, ctx, buf)
}

func renderBodiedExtension(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	renderExtensionNode(node, ctx, buf)
}
