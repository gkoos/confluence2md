package links

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gkoos/confluence2md/internal/store"
)

const testHost = "example.atlassian.net"

// testPage is one fixture page written into a temporary output directory.
type testPage struct {
	key       string
	localPath string
	content   string
}

// writeCrawl writes the fixture page files and returns the output directory plus
// the page map RewriteCrawledPageLinks operates on.
func writeCrawl(t *testing.T, pages []testPage) (string, map[string]store.PageRecord) {
	t.Helper()

	dir := t.TempDir()
	records := make(map[string]store.PageRecord, len(pages))
	for _, page := range pages {
		if err := os.WriteFile(filepath.Join(dir, page.localPath), []byte(page.content), 0644); err != nil {
			t.Fatalf("write %s: %v", page.localPath, err)
		}
		records[page.key] = store.PageRecord{
			ID:        page.key,
			Host:      testHost,
			Title:     page.localPath,
			LocalPath: page.localPath,
		}
	}

	return dir, records
}

func readPage(t *testing.T, dir, localPath string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(dir, localPath))
	if err != nil {
		t.Fatalf("read %s: %v", localPath, err)
	}
	return string(content)
}

// rewriteCase describes one seed-page fixture and the expected rewrite result.
type rewriteCase struct {
	name      string
	content   string
	want      string
	seen      int
	rewritten int
}

// runRewriteCases runs each case against a two-page crawl: the seed page being
// rewritten and one crawled target page.
func runRewriteCases(t *testing.T, cases []rewriteCase) {
	t.Helper()

	const seedFile = "seed.md"

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			dir, pages := writeCrawl(t, []testPage{
				{key: "example.atlassian.net/100", localPath: seedFile, content: test.content},
				{key: "example.atlassian.net/200", localPath: "target.md", content: ""},
			})

			stats, err := RewriteCrawledPageLinks(dir, pages)
			if err != nil {
				t.Fatalf("RewriteCrawledPageLinks returned error: %v", err)
			}

			if got := readPage(t, dir, seedFile); got != test.want {
				t.Fatalf("rewritten content mismatch\n got: %q\nwant: %q", got, test.want)
			}
			if stats.LinksSeen != test.seen {
				t.Fatalf("LinksSeen = %d, want %d", stats.LinksSeen, test.seen)
			}
			if stats.LinksRewritten != test.rewritten {
				t.Fatalf("LinksRewritten = %d, want %d", stats.LinksRewritten, test.rewritten)
			}
		})
	}
}

func TestRewriteCrawledPageLinks_Targets(t *testing.T) {
	const target = "https://example.atlassian.net/wiki/spaces/SFD/pages/200"

	runRewriteCases(t, []rewriteCase{
		{
			name:      "plain link to a crawled page becomes relative",
			content:   "See [Target](" + target + ") here.\n",
			want:      "See [Target](target.md) here.\n",
			seen:      1,
			rewritten: 1,
		},
		{
			// Regression: a label carrying its own bracketed tag ("[[scope] title]")
			// is invisible to a "no closing bracket in the label" regex.
			name:      "bracketed link label is rewritten",
			content:   "| 20 August 2026 | [[fcp-sfd-object-processor] 10MB per-file upload limit](" + target + ") |\n",
			want:      "| 20 August 2026 | [[fcp-sfd-object-processor] 10MB per-file upload limit](target.md) |\n",
			seen:      1,
			rewritten: 1,
		},
		{
			name:      "several bracketed segments in the label are rewritten",
			content:   "| [[a] [b] title](" + target + ") |\n",
			want:      "| [[a] [b] title](target.md) |\n",
			seen:      1,
			rewritten: 1,
		},
		{
			name:      "numeric space key and draftShareId query are rewritten",
			content:   "[x](https://example.atlassian.net/wiki/spaces/4930699280/pages/200/?draftShareId=3e8da1ea) |\n",
			want:      "[x](target.md) |\n",
			seen:      1,
			rewritten: 1,
		},
		{
			name:      "fragment is preserved",
			content:   "[x](" + target + "#Heading)\n",
			want:      "[x](target.md#Heading)\n",
			seen:      1,
			rewritten: 1,
		},
		{
			name:      "link into the same page keeps only the fragment",
			content:   "[Cognito](https://example.atlassian.net/wiki/spaces/FALSSFDO/pages/edit-v2/100#Cognito)\n",
			want:      "[Cognito](#Cognito)\n",
			seen:      1,
			rewritten: 1,
		},
		{
			name:      "external host is left alone",
			content:   "[x](https://other.example.com/wiki/spaces/SFD/pages/200)\n",
			want:      "[x](https://other.example.com/wiki/spaces/SFD/pages/200)\n",
			seen:      1,
			rewritten: 0,
		},
		{
			name:      "page outside the crawled set is left alone",
			content:   "[x](https://example.atlassian.net/wiki/spaces/SFD/pages/999)\n",
			want:      "[x](https://example.atlassian.net/wiki/spaces/SFD/pages/999)\n",
			seen:      1,
			rewritten: 0,
		},
		{
			name:      "jira link is left alone",
			content:   "[SFD2-258](https://example.atlassian.net/browse/SFD2-258)\n",
			want:      "[SFD2-258](https://example.atlassian.net/browse/SFD2-258)\n",
			seen:      1,
			rewritten: 0,
		},
	})
}

func TestRewriteCrawledPageLinks_MarkdownSyntax(t *testing.T) {
	const target = "https://example.atlassian.net/wiki/spaces/SFD/pages/200"

	runRewriteCases(t, []rewriteCase{
		{
			name:      "image destination to a crawled page is rewritten",
			content:   "![diagram](" + target + ")\n",
			want:      "![diagram](target.md)\n",
			seen:      1,
			rewritten: 1,
		},
		{
			name:      "downloaded attachment image is left alone",
			content:   "![diagram](attachments/diagram.png)\n",
			want:      "![diagram](attachments/diagram.png)\n",
			seen:      1,
			rewritten: 0,
		},
		{
			name:      "multiple links on one line only rewrite in-crawl targets",
			content:   "| [[t] b](" + target + ") and [c](https://other.example.com/x) |\n",
			want:      "| [[t] b](target.md) and [c](https://other.example.com/x) |\n",
			seen:      2,
			rewritten: 1,
		},
		{
			name:      "angle-bracketed destination is rewritten",
			content:   "[x](<" + target + ">)\n",
			want:      "[x](<target.md>)\n",
			seen:      1,
			rewritten: 1,
		},
		{
			name:      "link title after the destination is preserved",
			content:   "[x](" + target + " \"Title\")\n",
			want:      "[x](target.md \"Title\")\n",
			seen:      1,
			rewritten: 1,
		},
		{
			name:      "destinations inside a backtick fence are untouched",
			content:   "Before [l](" + target + ").\n\n```md\n[l](" + target + ")\n```\n\nAfter [l](" + target + ").\n",
			want:      "Before [l](target.md).\n\n```md\n[l](" + target + ")\n```\n\nAfter [l](target.md).\n",
			seen:      2,
			rewritten: 2,
		},
		{
			name:      "destinations inside a tilde fence are untouched",
			content:   "~~~\n[l](" + target + ")\n~~~\n",
			want:      "~~~\n[l](" + target + ")\n~~~\n",
			seen:      0,
			rewritten: 0,
		},
		{
			name:      "destination inside an inline code span is untouched",
			content:   "Use `[l](" + target + ")` syntax.\n",
			want:      "Use `[l](" + target + ")` syntax.\n",
			seen:      0,
			rewritten: 0,
		},
		{
			name:      "CRLF line endings are preserved",
			content:   "[x](" + target + ")\r\n",
			want:      "[x](target.md)\r\n",
			seen:      1,
			rewritten: 1,
		},
	})
}

func TestRewriteCrawledPageLinks_StatsAndIdempotency(t *testing.T) {
	const target = "https://example.atlassian.net/wiki/spaces/SFD/pages/200"

	content := strings.Join([]string{
		"[internal](" + target + ")",
		"[[tag] internal](" + target + ")",
		"[external](https://other.example.com/page)",
		"[uncrawled](https://example.atlassian.net/wiki/spaces/SFD/pages/999)",
		"",
	}, "\n")

	dir, pages := writeCrawl(t, []testPage{
		{key: "example.atlassian.net/100", localPath: "seed.md", content: content},
		{key: "example.atlassian.net/200", localPath: "target.md", content: ""},
	})

	stats, err := RewriteCrawledPageLinks(dir, pages)
	if err != nil {
		t.Fatalf("RewriteCrawledPageLinks returned error: %v", err)
	}
	if stats.PagesProcessed != 2 {
		t.Fatalf("PagesProcessed = %d, want 2", stats.PagesProcessed)
	}
	if stats.PagesUpdated != 1 {
		t.Fatalf("PagesUpdated = %d, want 1", stats.PagesUpdated)
	}
	if stats.LinksSeen != 4 {
		t.Fatalf("LinksSeen = %d, want 4", stats.LinksSeen)
	}
	if stats.LinksRewritten != 2 {
		t.Fatalf("LinksRewritten = %d, want 2", stats.LinksRewritten)
	}

	firstPass := readPage(t, dir, "seed.md")

	second, err := RewriteCrawledPageLinks(dir, pages)
	if err != nil {
		t.Fatalf("second RewriteCrawledPageLinks returned error: %v", err)
	}
	if second.LinksRewritten != 0 || second.PagesUpdated != 0 {
		t.Fatalf("second pass rewrote %d links across %d pages, want 0/0", second.LinksRewritten, second.PagesUpdated)
	}
	if got := readPage(t, dir, "seed.md"); got != firstPass {
		t.Fatalf("second pass changed the file\n got: %q\nwant: %q", got, firstPass)
	}
}

func TestRewriteCrawledPageLinks_MissingPageFileIsReported(t *testing.T) {
	dir := t.TempDir()
	pages := map[string]store.PageRecord{
		"example.atlassian.net/100": {
			ID:        "example.atlassian.net/100",
			Host:      testHost,
			LocalPath: "missing.md",
		},
	}

	if _, err := RewriteCrawledPageLinks(dir, pages); err == nil {
		t.Fatal("expected an error for a page whose local file is missing")
	}
}

func TestRewriteCrawledPageLinks_SkipsPagesWithoutLocalPath(t *testing.T) {
	dir := t.TempDir()
	pages := map[string]store.PageRecord{
		"example.atlassian.net/100": {
			ID:   "example.atlassian.net/100",
			Host: testHost,
		},
	}

	stats, err := RewriteCrawledPageLinks(dir, pages)
	if err != nil {
		t.Fatalf("RewriteCrawledPageLinks returned error: %v", err)
	}
	if stats.PagesProcessed != 0 {
		t.Fatalf("PagesProcessed = %d, want 0 for a record without a local path", stats.PagesProcessed)
	}
}
