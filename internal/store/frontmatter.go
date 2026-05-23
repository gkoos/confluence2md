package store

import (
	"sort"
	"strconv"
	"strings"
)

// ComposeMarkdownWithFrontMatter returns markdown with a deterministic YAML front matter preamble.
func ComposeMarkdownWithFrontMatter(pageID string, pageRecord PageRecord, seedPageIDs []string, markdown string) string {
	body := stripExistingFrontMatter(markdown)
	frontMatter := renderFrontMatter(pageID, pageRecord, seedPageIDs)
	body = strings.TrimLeft(body, "\n")
	if body == "" {
		return frontMatter + "\n"
	}
	return frontMatter + "\n" + body
}

func renderFrontMatter(pageID string, pageRecord PageRecord, seedPageIDs []string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("page_id: ")
	b.WriteString(strconv.Quote(strings.TrimSpace(pageID)))
	b.WriteString("\n")
	b.WriteString("title: ")
	b.WriteString(strconv.Quote(strings.TrimSpace(pageRecord.Title)))
	b.WriteString("\n")
	b.WriteString("source_url: ")
	b.WriteString(strconv.Quote(strings.TrimSpace(pageRecord.SourceURL)))
	b.WriteString("\n")
	b.WriteString("canonical_url: ")
	b.WriteString(strconv.Quote(strings.TrimSpace(pageRecord.CanonicalURL)))
	b.WriteString("\n")
	b.WriteString("space_key: ")
	b.WriteString(strconv.Quote(strings.TrimSpace(pageRecord.SpaceKey)))
	b.WriteString("\n")
	b.WriteString("is_seed: ")
	b.WriteString(strconv.FormatBool(isSeedPage(pageID, seedPageIDs)))
	b.WriteString("\n")
	b.WriteString("crawled_at: ")
	b.WriteString(strconv.Quote(pageRecord.CrawledAt.UTC().Format("2006-01-02T15:04:05Z")))
	b.WriteString("\n")

	if pageRecord.CommentCount > 0 {
		b.WriteString("comment_count: ")
		b.WriteString(strconv.Itoa(pageRecord.CommentCount))
		b.WriteString("\n")
	}

	if errMsg := strings.TrimSpace(pageRecord.CommentsFetchError); errMsg != "" {
		b.WriteString("comments_fetch_error: ")
		b.WriteString(strconv.Quote(strings.ReplaceAll(errMsg, "\n", " ")))
		b.WriteString("\n")
	}

	if len(pageRecord.Attachments) > 0 {
		attachments := make([]string, 0, len(pageRecord.Attachments))
		for _, attachment := range pageRecord.Attachments {
			attachment = strings.TrimSpace(attachment)
			if attachment != "" {
				attachments = append(attachments, attachment)
			}
		}
		if len(attachments) > 0 {
			sort.Strings(attachments)
			b.WriteString("attachments:\n")
			for _, attachment := range attachments {
				b.WriteString("  - ")
				b.WriteString(strconv.Quote(attachment))
				b.WriteString("\n")
			}
		}
	}

	b.WriteString("---\n")
	return b.String()
}

func isSeedPage(pageID string, seedPageIDs []string) bool {
	pageID = strings.TrimSpace(pageID)
	if pageID == "" {
		return false
	}
	for _, seedID := range seedPageIDs {
		if pageID == strings.TrimSpace(seedID) {
			return true
		}
	}
	return false
}

func stripExistingFrontMatter(markdown string) string {
	trimmed := strings.TrimPrefix(markdown, "\ufeff")
	if !strings.HasPrefix(trimmed, "---\n") {
		return markdown
	}

	end := strings.Index(trimmed[4:], "\n---\n")
	if end < 0 {
		return markdown
	}
	end += 4
	header := trimmed[:end+5]
	if !strings.Contains(header, "page_id:") || !strings.Contains(header, "crawled_at:") {
		return markdown
	}

	body := trimmed[end+5:]
	body = strings.TrimLeft(body, "\n")
	return body
}
