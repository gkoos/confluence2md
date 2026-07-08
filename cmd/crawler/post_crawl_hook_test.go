package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gkoos/confluence2md/internal/config"
)

func TestNormalizePostCrawlHookCommand_TrimsAndFiltersEmptyTokens(t *testing.T) {
	got := normalizePostCrawlHookCommand([]string{"  ./scripts/reindex.sh  ", "", "  --db  ", " \t "})
	want := []string{"./scripts/reindex.sh", "--db"}

	if len(got) != len(want) {
		t.Fatalf("expected %d command tokens, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected token %d to be %q, got %q", i, want[i], got[i])
		}
	}
}

func TestRunPostCrawlHookWithWriter_WarnsWhenCommandFailsToStart(t *testing.T) {
	rc := &runContext{
		mode: "full",
		cfg: &config.Config{
			Output:        config.OutputConfig{Dir: "./output"},
			PostCrawlHook: config.PostCrawlHookConfig{Command: []string{"definitely-not-a-real-command-xyz"}},
		},
	}

	var out bytes.Buffer
	runPostCrawlHookWithWriter(rc, &out)

	got := out.String()
	if !strings.Contains(got, "[WARN] post-crawl hook failed to start") {
		t.Fatalf("expected hook start warning, got: %s", got)
	}
}

func TestRunPostCrawlHookWithWriter_SkipsInDryRun(t *testing.T) {
	rc := &runContext{
		mode:   "full",
		dryRun: true,
		cfg: &config.Config{
			Output:        config.OutputConfig{Dir: "./output"},
			PostCrawlHook: config.PostCrawlHookConfig{Command: []string{"echo", "hello"}},
		},
	}

	var out bytes.Buffer
	runPostCrawlHookWithWriter(rc, &out)

	if out.Len() != 0 {
		t.Fatalf("expected no hook output in dry-run, got: %s", out.String())
	}
}
