package convert

import (
	"strings"
	"time"
)

func registerInlines() {
	Registry["text"] = renderText
	Registry["hardBreak"] = renderHardBreak
	Registry["mention"] = renderMention
	Registry["emoji"] = renderEmoji
	Registry["date"] = renderDate
	Registry["inlineCard"] = renderInlineCard
	Registry["blockCard"] = renderBlockCard
	Registry["status"] = renderStatus
	Registry["media"] = renderMedia
	Registry["mediaInline"] = renderMedia
	Registry["extension"] = renderInlineExtension
	Registry["inlineExtension"] = renderInlineExtension
}

func renderText(node ADFNode, _ *RenderContext, buf *strings.Builder) {
	buf.WriteString(ApplyMarks(node.Text, node.Marks))
}

func renderHardBreak(_ ADFNode, _ *RenderContext, buf *strings.Builder) {
	buf.WriteString("\n")
}

func renderMention(node ADFNode, _ *RenderContext, buf *strings.Builder) {
	buf.WriteString(attrString(node, "text", ""))
}

func renderEmoji(node ADFNode, _ *RenderContext, buf *strings.Builder) {
	text := attrString(node, "text", "")
	if text == "" {
		text = attrString(node, "shortName", "")
	}
	buf.WriteString(text)
}

func renderDate(node ADFNode, _ *RenderContext, buf *strings.Builder) {
	ts := attrString(node, "timestamp", "")
	if ts == "" {
		return
	}
	// ADF timestamp is milliseconds since epoch as a string
	var ms int64
	for _, c := range ts {
		if c < '0' || c > '9' {
			break
		}
		ms = ms*10 + int64(c-'0')
	}
	if ms > 0 {
		t := time.UnixMilli(ms).UTC()
		buf.WriteString(t.Format("2 January 2006"))
	} else {
		buf.WriteString(ts)
	}
}

func renderInlineCard(node ADFNode, _ *RenderContext, buf *strings.Builder) {
	u := attrString(node, "url", "")
	if u == "" {
		return
	}
	buf.WriteString("[" + u + "](" + u + ")")
}

func renderBlockCard(node ADFNode, _ *RenderContext, buf *strings.Builder) {
	u := attrString(node, "url", "")
	if u == "" {
		return
	}
	buf.WriteString("\n[" + u + "](" + u + ")\n\n")
}

func renderStatus(node ADFNode, _ *RenderContext, buf *strings.Builder) {
	text := attrString(node, "text", "")
	if text != "" {
		buf.WriteString("**" + text + "**")
	}
}

// renderMedia emits attachment://uuid where uuid is ADF media.attrs.id — the
// Confluence Media Services UUID (fileId from the v2 Attachment API).
// rewriteAttachmentLinks resolves this to a local path after attachments are downloaded.
func renderMedia(node ADFNode, _ *RenderContext, buf *strings.Builder) {
	id := attrString(node, "id", "")
	if id == "" {
		return
	}
	alt := attrString(node, "alt", id)
	buf.WriteString("![" + alt + "](attachment://" + id + ")")
}

func renderInlineExtension(node ADFNode, ctx *RenderContext, buf *strings.Builder) {
	renderExtensionNode(node, ctx, buf)
}


