package convert

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToMarkdown_HRDoesNotPromoteParagraphToHeading(t *testing.T) {
	input := `{"version":1,"type":"doc","content":[{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Summary"}]},{"type":"paragraph","content":[{"type":"text","text":"The service currently has no deduplication enforcement."}]},{"type":"rule"}]}`

	got, err := ToMarkdown(input)
	if err != nil {
		t.Fatalf("ToMarkdown returned error: %v", err)
	}

	if strings.Contains(got, "The service currently has no deduplication enforcement.\n---") {
		t.Fatalf("paragraph is directly followed by hr, can be parsed as setext heading:\n%s", got)
	}

	wantContains := []string{
		"## Summary",
		"The service currently has no deduplication enforcement.",
		"\n\n---",
	}
	for _, want := range wantContains {
		if !strings.Contains(got, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestToMarkdown_TableHasBlockSeparation(t *testing.T) {
	input := `{"version":1,"type":"doc","content":[{"type":"heading","attrs":{"level":3},"content":[{"type":"text","text":"Option 1"}]},{"type":"paragraph","content":[{"type":"text","text":"Intro text."}]},{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Pros"}]}]},{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Cons"}]}]}]},{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"Fast"}]}]},{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"Risky"}]}]}]}]},{"type":"paragraph","content":[{"type":"text","text":"After table."}]}]}`

	got, err := ToMarkdown(input)
	if err != nil {
		t.Fatalf("ToMarkdown returned error: %v", err)
	}

	wantContains := []string{
		"### Option 1",
		"Intro text.",
		"\n\n| Pros | Cons |",
		"| --- | --- |",
		"| Fast | Risky |",
		"After table.",
	}
	for _, want := range wantContains {
		if !strings.Contains(got, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestToMarkdown_CodeBlockRendersFenced(t *testing.T) {
	input := `{"version":1,"type":"doc","content":[{"type":"codeBlock","attrs":{"language":"js"},"content":[{"type":"text","text":"const a = 1;"}]}]}`

	got, err := ToMarkdown(input)
	if err != nil {
		t.Fatalf("ToMarkdown returned error: %v", err)
	}

	want := "```js\nconst a = 1;\n```"
	if !strings.Contains(got, want) {
		t.Fatalf("expected fenced code block %q, got:\n%s", want, got)
	}
}

func TestToMarkdown_JiraMacroRendersLink(t *testing.T) {
	input := `{"version":1,"type":"doc","content":[{"type":"paragraph","content":[{"type":"inlineExtension","attrs":{"extensionType":"com.atlassian.confluence.macro.core","extensionKey":"jira","macroParams":{"key":{"value":"FLS1-20"}}}}]}]}`

	got, err := ToMarkdown(input)
	if err != nil {
		t.Fatalf("ToMarkdown returned error: %v", err)
	}

	want := "[FLS1-20](https://jira.atlassian.net/browse/FLS1-20)"
	if !strings.Contains(got, want) {
		t.Fatalf("expected jira link %q, got:\n%s", want, got)
	}
}

func TestToMarkdown_GoldenFixtures(t *testing.T) {
	t.Parallel()

	fixtures, err := filepath.Glob("testdata/golden/*.input.json")
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}
	if len(fixtures) == 0 {
		t.Fatalf("no golden fixtures found")
	}

	for _, inputPath := range fixtures {
		inputPath := inputPath
		name := strings.TrimSuffix(filepath.Base(inputPath), ".input.json")

		t.Run(name, func(t *testing.T) {
			inputBytes, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatalf("read input fixture: %v", err)
			}

			expectedPath := strings.TrimSuffix(inputPath, ".input.json") + ".expected.md"
			expectedBytes, err := os.ReadFile(expectedPath)
			if err != nil {
				t.Fatalf("read expected fixture: %v", err)
			}

			got, err := ToMarkdown(string(inputBytes))
			if err != nil {
				t.Fatalf("ToMarkdown returned error: %v", err)
			}

			want := normalizeNewlines(strings.TrimSpace(string(expectedBytes)))
			got = normalizeNewlines(strings.TrimSpace(got))

			if got != want {
				t.Fatalf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
			}
		})
	}
}

func normalizeNewlines(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}
