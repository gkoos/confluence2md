package store

import (
	"strings"
	"testing"
	"time"
)

func TestComposeMarkdownWithFrontMatter_RequiredFieldsAndSeedFlag(t *testing.T) {
	record := PageRecord{
		ID:           "123",
		Title:        "Decision Records",
		SourceURL:    "https://example/wiki/pages/viewpage.action?pageId=123",
		CanonicalURL: "https://example/wiki/pages/viewpage.action?pageId=123",
		SpaceKey:     "SFD",
		CrawledAt:    time.Date(2026, 5, 23, 12, 30, 0, 0, time.FixedZone("BST", 3600)),
		StorageFormat: "# Decision Records\n\nBody",
	}

	out := ComposeMarkdownWithFrontMatter("123", record, []string{"123", "999"}, record.StorageFormat)

	wants := []string{
		"---\n",
		"page_id: \"123\"\n",
		"title: \"Decision Records\"\n",
		"source_url: \"https://example/wiki/pages/viewpage.action?pageId=123\"\n",
		"canonical_url: \"https://example/wiki/pages/viewpage.action?pageId=123\"\n",
		"space_key: \"SFD\"\n",
		"is_seed: true\n",
		"crawled_at: \"2026-05-23T11:30:00Z\"\n",
		"---\n\n# Decision Records\n\nBody",
	}

	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestComposeMarkdownWithFrontMatter_OmitsOptionalFieldsWhenEmpty(t *testing.T) {
	record := PageRecord{
		ID:            "321",
		Title:         "Empty Optional",
		SourceURL:     "https://example/wiki/pages/viewpage.action?pageId=321",
		CanonicalURL:  "https://example/wiki/pages/viewpage.action?pageId=321",
		SpaceKey:      "SFD",
		CrawledAt:     time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC),
		CommentCount:  0,
		Attachments:   nil,
		StorageFormat: "# Empty Optional",
	}

	out := ComposeMarkdownWithFrontMatter("321", record, []string{"123"}, record.StorageFormat)

	mustNotContain := []string{
		"comment_count:",
		"comments_fetch_error:",
		"attachments:",
	}

	for _, notExpected := range mustNotContain {
		if strings.Contains(out, notExpected) {
			t.Fatalf("did not expect output to contain %q, got:\n%s", notExpected, out)
		}
	}

	if !strings.Contains(out, "is_seed: false\n") {
		t.Fatalf("expected is_seed false in output, got:\n%s", out)
	}
}

func TestComposeMarkdownWithFrontMatter_ReplacesExistingManagedFrontMatter(t *testing.T) {
	record := PageRecord{
		ID:            "777",
		Title:         "Replacement",
		SourceURL:     "https://example/wiki/pages/viewpage.action?pageId=777",
		CanonicalURL:  "https://example/wiki/pages/viewpage.action?pageId=777",
		SpaceKey:      "SFD",
		CrawledAt:     time.Date(2026, 5, 23, 13, 0, 0, 0, time.UTC),
		CommentCount:  2,
		Attachments:   []string{"b.png", "a.png"},
		StorageFormat: "# Replacement",
	}

	in := "---\npage_id: \"old\"\ncrawled_at: \"2020-01-01T00:00:00Z\"\n---\n# Replacement"
	out := ComposeMarkdownWithFrontMatter("777", record, []string{"777"}, in)

	if strings.Contains(out, "page_id: \"old\"") {
		t.Fatalf("expected old front matter to be replaced, got:\n%s", out)
	}
	if strings.Count(out, "---\n") != 2 {
		t.Fatalf("expected a single front matter block, got:\n%s", out)
	}
	if !strings.Contains(out, "attachments:\n  - \"a.png\"\n  - \"b.png\"\n") {
		t.Fatalf("expected sorted attachments in front matter, got:\n%s", out)
	}
}
