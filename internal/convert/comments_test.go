package convert

import (
	"strings"
	"testing"
	"time"

	"github.com/gkoos/confluence2md/internal/confluence"
)

func TestCommentsToMarkdown_Empty(t *testing.T) {
	if got := CommentsToMarkdown(nil); got != "" {
		t.Fatalf("expected empty comments markdown, got: %q", got)
	}
}

func TestCommentsToMarkdown_RendersSection(t *testing.T) {
	comments := []confluence.CommentData{
		{
			ID:        "c1",
			Author:    "Simon Dunn",
			CreatedAt: time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC),
			Body:      "<p>Hello <strong>world</strong></p>",
		},
		{
			ID:        "c2",
			ParentID:  "c1",
			Author:    "Natacha Tomkinson",
			CreatedAt: time.Date(2026, 5, 22, 10, 1, 0, 0, time.UTC),
			Body:      "<p>Reply</p>",
		},
	}

	got := CommentsToMarkdown(comments)

	checks := []string{
		"## Comments",
		"Simon Dunn",
		"22 May 2026",
		"Hello world",
		"Natacha Tomkinson",
		"Reply",
	}
	for _, c := range checks {
		if !strings.Contains(got, c) {
			t.Fatalf("expected comments markdown to contain %q, got:\n%s", c, got)
		}
	}

	if strings.Index(got, "Hello world") > strings.Index(got, "Reply") {
		t.Fatalf("expected parent comment to appear before reply, got:\n%s", got)
	}
}

func TestCommentsToMarkdown_UsesAuthorIDFallback(t *testing.T) {
	comments := []confluence.CommentData{
		{
			ID:        "c1",
			AuthorID:  "acc-123",
			CreatedAt: time.Date(2026, 2, 13, 0, 0, 0, 0, time.UTC),
			Body:      "<p>Fallback author</p>",
		},
	}

	got := CommentsToMarkdown(comments)
	if !strings.Contains(got, "acc-123") {
		t.Fatalf("expected authorID fallback to be rendered, got:\n%s", got)
	}
}

func TestCommentsToMarkdown_PreservesParagraphBoundaries(t *testing.T) {
	comments := []confluence.CommentData{
		{
			ID:        "c1",
			Author:    "Simon Dunn",
			CreatedAt: time.Date(2026, 2, 13, 0, 0, 0, 0, time.UTC),
			Body:      `<p data-node="x">Re the above</p><ac:structured-macro ac:name="note"><ac:rich-text-body><p>RPS have a business rule of ten files per upload.</p></ac:rich-text-body></ac:structured-macro>`,
		},
	}

	got := CommentsToMarkdown(comments)
	if !strings.Contains(got, "Re the above\nRPS have a business rule of ten files per upload.") {
		t.Fatalf("expected paragraph boundary between lines, got:\n%s", got)
	}
}
