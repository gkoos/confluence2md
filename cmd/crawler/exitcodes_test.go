package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gkoos/confluence2md/internal/crawl"
)

func TestProcessTraversalResultsStopsWhenCancelled(t *testing.T) {
	rc := &runContext{crawlResults: map[string]*crawl.CrawledPage{
		"example.test/1": {ID: 1, Host: "example.test", Deleted: true},
	}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	metrics := &runMetrics{}
	err := processTraversalResults(ctx, rc, metrics)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if metrics.deletedCount != 0 {
		t.Fatalf("no page may be processed after cancellation, got %d", metrics.deletedCount)
	}

	metrics = &runMetrics{}
	if err := processTraversalResults(context.Background(), rc, metrics); err != nil {
		t.Fatalf("a live context must process normally: %v", err)
	}
	if metrics.deletedCount != 1 {
		t.Fatalf("deletedCount = %d, want 1", metrics.deletedCount)
	}
}

func TestExitCodeFor(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, exitOK},
		{"uncoded", errors.New("boom"), exitFailure},
		{"config", withExitCode(exitConfig, errors.New("bad yaml")), exitConfig},
		{"auth wrapped further", fmt.Errorf("run: %w", withExitCode(exitAuth, errors.New("401"))), exitAuth},
		{"partial", partialCrawlError(3), exitPartial},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCodeFor(tc.err); got != tc.want {
				t.Fatalf("exitCodeFor = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestWithExitCodeKeepsNil(t *testing.T) {
	if withExitCode(exitAuth, nil) != nil {
		t.Fatal("a nil error must stay nil")
	}
}

func TestBootstrapRunRejectsBadModeWithConfigExitCode(t *testing.T) {
	_, err := bootstrapRun(context.Background(), "sideways", "no-such-config.yaml", false)
	if got := exitCodeFor(err); got != exitConfig {
		t.Fatalf("exit code = %d, want %d (err: %v)", got, exitConfig, err)
	}
}

func TestInterruptedOr(t *testing.T) {
	cause := errors.New("crawl failed: context canceled")

	live := context.Background()
	if got := interruptedOr(live, cause); got != cause {
		t.Fatalf("without a signal the error must pass through, got %v", got)
	}

	signalled, cancel := context.WithCancel(context.Background())
	cancel()
	got := interruptedOr(signalled, cause)
	if exitCodeFor(got) != exitInterrupted {
		t.Fatalf("exit code = %d, want %d", exitCodeFor(got), exitInterrupted)
	}
	if !errors.Is(got, cause) {
		t.Fatal("the original error must stay reachable")
	}
}
