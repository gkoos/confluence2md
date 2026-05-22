package crawl

import (
	"context"
	"sync"
	"testing"

	"github.com/gkoos/confluence2md/internal/confluence"
	"github.com/gkoos/confluence2md/internal/config"
	"github.com/gkoos/confluence2md/internal/store"
)

func TestSetNodeHandlerRejectsNil(t *testing.T) {
	cfg := &config.Config{Crawl: config.CrawlConfig{MaxDepth: 1, Concurrency: 1, RateLimitRPM: 60000}}
	cs := NewCrawlSession(nil, cfg, "")

	if err := cs.SetNodeHandler(nil); err == nil {
		t.Fatalf("expected error when setting nil node handler")
	}
}

func TestRunUsesSharedTraversalWithCustomNodeHandler(t *testing.T) {
	cfg := &config.Config{
		Crawl: config.CrawlConfig{
			MaxDepth:     2,
			Concurrency:  1,
			RateLimitRPM: 60000,
		},
	}
	cs := NewCrawlSession(nil, cfg, "")

	graph := map[int64][]int64{
		1: {2, 3},
		2: {4},
		3: {4},
		4: {},
	}

	visitedByHandler := make(map[int64]int)
	var mu sync.Mutex

	err := cs.SetNodeHandler(func(ctx context.Context, pageID int64, depth int) *NodeHandlerResult {
		mu.Lock()
		visitedByHandler[pageID]++
		mu.Unlock()

		return &NodeHandlerResult{
			Title:         "test",
			OutgoingLinks: graph[pageID],
		}
	})
	if err != nil {
		t.Fatalf("SetNodeHandler returned error: %v", err)
	}

	results, runErr := cs.Run(context.Background(), []int64{1})
	if runErr != nil {
		t.Fatalf("Run returned error: %v", runErr)
	}

	// Custom handler didn't emit page payloads; traversal still runs and deduplicates visits.
	if len(results) != 0 {
		t.Fatalf("expected no page results from custom handler, got %d", len(results))
	}

	expected := []int64{1, 2, 3, 4}
	for _, id := range expected {
		if visitedByHandler[id] != 1 {
			t.Fatalf("expected page %d to be visited once, got %d", id, visitedByHandler[id])
		}
	}
}

func TestTraversalUsesMinimalDepthAcrossBranches(t *testing.T) {
	cfg := &config.Config{
		Crawl: config.CrawlConfig{
			MaxDepth:     4,
			Concurrency:  1,
			RateLimitRPM: 60000,
		},
	}
	cs := NewCrawlSession(nil, cfg, "")

	graph := map[int64][]int64{
		1: {2, 3},
		2: {4},    // shortest path to 4 => depth 2
		3: {5},
		5: {4},    // longer path to 4 => depth 3
		4: {},
	}

	depthByNode := make(map[int64]int)
	var mu sync.Mutex

	err := cs.SetNodeHandler(func(ctx context.Context, pageID int64, depth int) *NodeHandlerResult {
		mu.Lock()
		depthByNode[pageID] = depth
		mu.Unlock()
		return &NodeHandlerResult{Title: "test", OutgoingLinks: graph[pageID]}
	})
	if err != nil {
		t.Fatalf("SetNodeHandler returned error: %v", err)
	}

	if _, runErr := cs.Run(context.Background(), []int64{1}); runErr != nil {
		t.Fatalf("Run returned error: %v", runErr)
	}

	if got := depthByNode[4]; got != 2 {
		t.Fatalf("expected node 4 at minimal depth 2, got %d", got)
	}
}

func TestIsDirtyComparedToPrevious(t *testing.T) {
	prev := store.PageRecord{
		Title:               "Page A",
		Version:             7,
		AttachmentSignature: "sig123",
	}
	state := &confluence.PageStateData{Title: "Page A", Version: 7, AttachmentSignature: "sig123"}

	if isDirtyComparedToPrevious(prev, state, true) {
		t.Fatalf("expected clean when fingerprint matches")
	}

	state.Version = 8
	if !isDirtyComparedToPrevious(prev, state, true) {
		t.Fatalf("expected dirty on version change")
	}

	state.Version = 7
	state.Title = "Page B"
	if !isDirtyComparedToPrevious(prev, state, true) {
		t.Fatalf("expected dirty on title change")
	}

	state.Title = "Page A"
	state.AttachmentSignature = "other"
	if !isDirtyComparedToPrevious(prev, state, true) {
		t.Fatalf("expected dirty on attachment signature change")
	}

	state.AttachmentSignature = "sig123"
	prev.AttachmentSignature = ""
	if !isDirtyComparedToPrevious(prev, state, true) {
		t.Fatalf("expected conservative dirty when previous attachment signature missing")
	}

	prev.AttachmentSignature = ""
	state.AttachmentSignature = "different"
	if isDirtyComparedToPrevious(prev, state, false) {
		t.Fatalf("expected clean when only attachment signature differs and attachment checks are disabled")
	}
}

func TestParseOutgoingLinkIDs(t *testing.T) {
	ids := parseOutgoingLinkIDs([]string{"123", "456", "123", "abc", ""})
	if len(ids) != 2 {
		t.Fatalf("expected 2 parsed IDs, got %d", len(ids))
	}
	if ids[0] != 123 || ids[1] != 456 {
		t.Fatalf("unexpected parsed IDs: %#v", ids)
	}
}
