package convert

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToMarkdown_HRDoesNotPromoteParagraphToHeading(t *testing.T) {
	input := `<h2>Summary</h2><p>The service currently has no deduplication enforcement.</p><hr/>`

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
		"\n\n---\n",
	}
	for _, want := range wantContains {
		if !strings.Contains(got, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestToMarkdown_TableHasBlockSeparation(t *testing.T) {
	input := `<h3>Option 1</h3><p>Intro text.</p><table><tr><th>Pros</th><th>Cons</th></tr><tr><td>Fast</td><td>Risky</td></tr></table><p>After table.</p>`

	got, err := ToMarkdown(input)
	if err != nil {
		t.Fatalf("ToMarkdown returned error: %v", err)
	}

	wantContains := []string{
		"### Option 1",
		"Intro text.",
		"\n\n| Pros | Cons |",
		"| --- | --- |",
		"| Fast | Risky |\n\nAfter table.",
	}
	for _, want := range wantContains {
		if !strings.Contains(got, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestToMarkdown_CodeMacroRendersFenceWithoutLayoutNoise(t *testing.T) {
	input := `<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">js</ac:parameter><ac:parameter ac:name="layout">wide</ac:parameter><ac:plain-text-body><![CDATA[const a = 1;]]></ac:plain-text-body></ac:structured-macro>`

	got, err := ToMarkdown(input)
	if err != nil {
		t.Fatalf("ToMarkdown returned error: %v", err)
	}

	if strings.Contains(got, "wide") {
		t.Fatalf("unexpected macro parameter leakage in output:\n%s", got)
	}

	want := "```js\nconst a = 1;\n```"
	if !strings.Contains(got, want) {
		t.Fatalf("expected fenced code block %q, got:\n%s", want, got)
	}
}

func TestToMarkdown_JiraMacroRendersLink(t *testing.T) {
	input := `<ac:structured-macro ac:name="jira"><ac:parameter ac:name="key">FLS1-20</ac:parameter></ac:structured-macro>`

	got, err := ToMarkdown(input)
	if err != nil {
		t.Fatalf("ToMarkdown returned error: %v", err)
	}

	want := "[FLS1-20](/browse/FLS1-20)"
	if !strings.Contains(got, want) {
		t.Fatalf("expected jira link %q, got:\n%s", want, got)
	}
}

func TestToMarkdown_GoldenFixtures(t *testing.T) {
	t.Parallel()

	fixtures, err := filepath.Glob("testdata/golden/*.input.xml")
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}
	if len(fixtures) == 0 {
		t.Fatalf("no golden fixtures found")
	}

	for _, inputPath := range fixtures {
		inputPath := inputPath
		name := strings.TrimSuffix(filepath.Base(inputPath), ".input.xml")

		t.Run(name, func(t *testing.T) {
			inputBytes, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatalf("read input fixture: %v", err)
			}

			expectedPath := strings.TrimSuffix(inputPath, ".input.xml") + ".expected.md"
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
