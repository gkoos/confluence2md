package crawl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gkoos/confluence2md/internal/config"
	"github.com/gkoos/confluence2md/internal/confluence"
	"github.com/gkoos/confluence2md/internal/store"
)

func TestSetNodeHandlerRejectsNil(t *testing.T) {
	cfg := &config.Config{Crawl: config.CrawlConfig{MaxDepth: 1, Concurrency: 1, RateLimitRPM: 60000, QueueSize: 10000}}
	cs := NewCrawlSession(nil, cfg, "")

	if err := cs.SetNodeHandler(nil); err == nil {
		t.Fatalf("expected error when setting nil node handler")
	}
}

func TestSetDryRun(t *testing.T) {
	cfg := &config.Config{Crawl: config.CrawlConfig{MaxDepth: 1, Concurrency: 1, RateLimitRPM: 60000, QueueSize: 10000}}
	cs := NewCrawlSession(nil, cfg, "")

	if cs.dryRun {
		t.Fatalf("expected dryRun to default to false")
	}

	cs.SetDryRun(true)
	if !cs.dryRun {
		t.Fatalf("expected dryRun to be true after SetDryRun(true)")
	}

	cs.SetDryRun(false)
	if cs.dryRun {
		t.Fatalf("expected dryRun to be false after SetDryRun(false)")
	}
}

func TestRunUsesSharedTraversalWithCustomNodeHandler(t *testing.T) {
	cfg := &config.Config{
		Crawl: config.CrawlConfig{
			MaxDepth:     2,
			Concurrency:  1,
			RateLimitRPM: 60000,
			QueueSize:    10000,
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
			QueueSize:    10000,
		},
	}
	cs := NewCrawlSession(nil, cfg, "")

	graph := map[int64][]int64{
		1: {2, 3},
		2: {4}, // shortest path to 4 => depth 2
		3: {5},
		5: {4}, // longer path to 4 => depth 3
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

func TestRunStoresDeletedNodeWithoutEnqueuingChildren(t *testing.T) {
	cfg := &config.Config{
		Crawl: config.CrawlConfig{
			MaxDepth:     2,
			Concurrency:  1,
			RateLimitRPM: 60000,
			QueueSize:    100,
		},
	}
	cs := NewCrawlSession(nil, cfg, "")

	visited := make(map[int64]int)
	err := cs.SetNodeHandler(func(ctx context.Context, pageID int64, depth int) *NodeHandlerResult {
		visited[pageID]++
		switch pageID {
		case 1:
			page := &CrawledPage{ID: pageID, Depth: depth}
			return &NodeHandlerResult{Page: page, OutgoingLinks: []int64{2}}
		case 2:
			// Include an outgoing link deliberately: deletion must take precedence.
			return &NodeHandlerResult{Deleted: true, OutgoingLinks: []int64{3}, Title: "Gone"}
		default:
			return &NodeHandlerResult{Page: &CrawledPage{ID: pageID, Depth: depth}}
		}
	})
	if err != nil {
		t.Fatalf("SetNodeHandler returned error: %v", err)
	}

	results, runErr := cs.Run(context.Background(), []int64{1})
	if runErr != nil {
		t.Fatalf("Run returned error: %v", runErr)
	}
	if visited[2] != 1 {
		t.Fatalf("expected deleted node 2 to be visited once, got %d", visited[2])
	}
	if visited[3] != 0 {
		t.Fatalf("expected child of deleted node not to be visited, got %d visits", visited[3])
	}
	deletedPage, ok := results[2]
	if !ok || deletedPage == nil || !deletedPage.Deleted {
		t.Fatalf("expected deleted node in crawl results, got %#v", deletedPage)
	}
	if deletedPage.ID != 2 || deletedPage.Depth != 1 {
		t.Fatalf("unexpected synthesized deleted page: %#v", deletedPage)
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

func TestProcessUpdatesNodeTreatsNotFoundAsDeleted(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, `{"message":"page not found"}`, http.StatusNotFound)
	}))
	defer server.Close()

	cfg := &config.Config{
		Confluence: config.ConfluenceConfig{Username: "user", Token: "token"},
		Crawl: config.CrawlConfig{
			Seeds:        []string{server.URL + "/wiki/spaces/SPACE/pages/42/Page"},
			Concurrency:  1,
			RateLimitRPM: 60000,
			QueueSize:    100,
		},
		Retry: config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1},
	}
	client, err := confluence.NewClient(cfg.BaseURL(), cfg.Confluence.Username, cfg.Confluence.Token, cfg.Retry, cfg.Crawl.RateLimitRPM, cfg.Crawl.Concurrency)
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	cs := NewCrawlSession(client, cfg, "SPACE")
	cs.EnableUpdatesMode(map[string]store.PageRecord{
		"42": {ID: "42", Title: "Deleted page"},
	})

	result := cs.processUpdatesNode(context.Background(), 42, 3)
	if result == nil || !result.Deleted {
		t.Fatalf("expected deleted node result, got %#v", result)
	}
	if result.FetchError != "" {
		t.Fatalf("expected deletion not to be a fetch error, got %q", result.FetchError)
	}
	if result.Page == nil || !result.Page.Deleted {
		t.Fatalf("expected deleted page payload, got %#v", result.Page)
	}
	if result.Page.ID != 42 || result.Page.Depth != 3 || result.Page.Title != "Deleted page" {
		t.Fatalf("unexpected deleted page payload: %#v", result.Page)
	}
	if requestCount != 1 {
		t.Fatalf("expected only the state request and no full-fetch fallback, got %d requests", requestCount)
	}
}

func TestProcessUpdatesNodeTreatsTrashedStatusAsDeleted(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"42","status":"trashed","title":"Page To Be Deleted","version":{"number":1}}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		Confluence: config.ConfluenceConfig{Username: "user", Token: "token"},
		Crawl: config.CrawlConfig{
			Seeds:        []string{server.URL + "/wiki/spaces/SPACE/pages/42/Page"},
			Concurrency:  1,
			RateLimitRPM: 60000,
			QueueSize:    100,
		},
		Retry: config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1},
	}
	client, err := confluence.NewClient(cfg.BaseURL(), cfg.Confluence.Username, cfg.Confluence.Token, cfg.Retry, cfg.Crawl.RateLimitRPM, cfg.Crawl.Concurrency)
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	cs := NewCrawlSession(client, cfg, "SPACE")
	cs.EnableUpdatesMode(map[string]store.PageRecord{
		"42": {ID: "42", Title: "Page To Be Deleted"},
	})

	result := cs.processUpdatesNode(context.Background(), 42, 1)
	if result == nil || !result.Deleted {
		t.Fatalf("expected trashed page to produce a deleted result, got %#v", result)
	}
	if result.FetchError != "" {
		t.Fatalf("expected trashed page not to be a fetch error, got %q", result.FetchError)
	}
	if result.Page == nil || !result.Page.Deleted || result.Page.Title != "Page To Be Deleted" {
		t.Fatalf("unexpected deleted page payload: %#v", result.Page)
	}
	if requestCount != 1 {
		t.Fatalf("expected only the state request and no full-fetch fallback, got %d requests", requestCount)
	}
}

func TestProcessUpdatesNodeFallsBackToFullFetchForTransientError(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, `{"message":"temporary failure"}`, http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := &config.Config{
		Confluence: config.ConfluenceConfig{Username: "user", Token: "token"},
		Crawl: config.CrawlConfig{
			Seeds:        []string{server.URL + "/wiki/spaces/SPACE/pages/42/Page"},
			Concurrency:  1,
			RateLimitRPM: 60000,
			QueueSize:    100,
		},
		Retry: config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1},
	}
	client, err := confluence.NewClient(cfg.BaseURL(), cfg.Confluence.Username, cfg.Confluence.Token, cfg.Retry, cfg.Crawl.RateLimitRPM, cfg.Crawl.Concurrency)
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	cs := NewCrawlSession(client, cfg, "SPACE")
	cs.EnableUpdatesMode(map[string]store.PageRecord{
		"42": {ID: "42", Title: "Existing page"},
	})

	result := cs.processUpdatesNode(context.Background(), 42, 1)
	if result == nil || result.Deleted {
		t.Fatalf("expected non-deleted fallback result, got %#v", result)
	}
	if result.FetchError == "" || result.Page == nil || result.Page.FetchError == "" {
		t.Fatalf("expected the failed full-fetch fallback to be reported, got %#v", result)
	}
	if requestCount != 2 {
		t.Fatalf("expected state request followed by full-fetch fallback, got %d requests", requestCount)
	}
}

func TestProcessFullNodeTreatsTrashedStatusAsDeleted(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"42","status":"trashed","title":"Deleted page","version":{"number":1}}`))
	}))
	defer server.Close()

	cs := newTestCrawlSession(t, server.URL)
	result := cs.processFullNode(context.Background(), 42, 2)

	assertDeletedNodeResult(t, result, 42, 2, "Deleted page")
	if requestCount != 1 {
		t.Fatalf("expected only the full page request, got %d requests", requestCount)
	}
}

func TestProcessFullNodeTreatsNotFoundAsDeleted(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, `{"message":"page not found"}`, http.StatusNotFound)
	}))
	defer server.Close()

	cs := newTestCrawlSession(t, server.URL)
	result := cs.processFullNode(context.Background(), 42, 2)

	assertDeletedNodeResult(t, result, 42, 2, "")
	if requestCount != 1 {
		t.Fatalf("expected only the full page request, got %d requests", requestCount)
	}
}

func TestProcessFullNodeKeepsTransientFailureAsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"temporary failure"}`, http.StatusInternalServerError)
	}))
	defer server.Close()

	cs := newTestCrawlSession(t, server.URL)
	result := cs.processFullNode(context.Background(), 42, 2)

	if result == nil || result.Deleted {
		t.Fatalf("expected non-deleted error result, got %#v", result)
	}
	if result.FetchError == "" || result.Page == nil || result.Page.FetchError == "" {
		t.Fatalf("expected transient failure to remain a fetch error, got %#v", result)
	}
}

func TestProcessUpdatesNodeHandlesDeletionBetweenStateAndFullFetch(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		if requestCount == 1 {
			_, _ = w.Write([]byte(`{"id":"42","status":"current","title":"Page","version":{"number":2}}`))
			return
		}
		http.Error(w, `{"message":"page not found"}`, http.StatusNotFound)
	}))
	defer server.Close()

	cs := newTestCrawlSession(t, server.URL)
	cs.EnableUpdatesMode(map[string]store.PageRecord{
		"42": {ID: "42", Title: "Page", Version: 1},
	})
	result := cs.processUpdatesNode(context.Background(), 42, 1)

	assertDeletedNodeResult(t, result, 42, 1, "")
	if requestCount != 2 {
		t.Fatalf("expected state request followed by full page request, got %d requests", requestCount)
	}
}

func newTestCrawlSession(t *testing.T, serverURL string) *CrawlSession {
	t.Helper()
	cfg := &config.Config{
		Confluence: config.ConfluenceConfig{Username: "user", Token: "token"},
		Crawl: config.CrawlConfig{
			Seeds:        []string{serverURL + "/wiki/spaces/SPACE/pages/42/Page"},
			Concurrency:  1,
			RateLimitRPM: 60000,
			QueueSize:    100,
		},
		Retry: config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1},
	}
	client, err := confluence.NewClient(cfg.BaseURL(), cfg.Confluence.Username, cfg.Confluence.Token, cfg.Retry, cfg.Crawl.RateLimitRPM, cfg.Crawl.Concurrency)
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	return NewCrawlSession(client, cfg, "SPACE")
}

func assertDeletedNodeResult(t *testing.T, result *NodeHandlerResult, pageID int64, depth int, title string) {
	t.Helper()
	if result == nil || !result.Deleted || result.FetchError != "" {
		t.Fatalf("expected deletion without fetch error, got %#v", result)
	}
	if result.Page == nil || !result.Page.Deleted {
		t.Fatalf("expected deleted page payload, got %#v", result.Page)
	}
	if result.Page.ID != pageID || result.Page.Depth != depth || result.Page.Title != title {
		t.Fatalf("unexpected deleted page payload: %#v", result.Page)
	}
	if result.Page.CrawledAt.IsZero() {
		t.Fatal("expected deleted page crawl timestamp")
	}
}

func TestRun_FailsLoudlyWhenQueueSaturates(t *testing.T) {
	cfg := &config.Config{
		Crawl: config.CrawlConfig{
			MaxDepth:     1,
			Concurrency:  1,
			RateLimitRPM: 60000,
			QueueSize:    1,
		},
	}
	cs := NewCrawlSession(nil, cfg, "")

	graph := map[int64][]int64{
		1: {2, 3, 4, 5}, // queue can only hold one child while worker is enqueuing
		2: {}, 3: {}, 4: {}, 5: {},
	}

	err := cs.SetNodeHandler(func(ctx context.Context, pageID int64, depth int) *NodeHandlerResult {
		return &NodeHandlerResult{Title: "test", OutgoingLinks: graph[pageID]}
	})
	if err != nil {
		t.Fatalf("SetNodeHandler returned error: %v", err)
	}

	_, runErr := cs.Run(context.Background(), []int64{1})
	if runErr == nil {
		t.Fatal("expected queue saturation error, got nil")
	}
	if !strings.Contains(runErr.Error(), "crawl queue saturated") {
		t.Fatalf("expected queue saturation error message, got: %v", runErr)
	}
	if cs.enqueueDrops == 0 {
		t.Fatal("expected enqueueDrops > 0 on saturation")
	}
}

func TestRun_DoesNotFailWhenQueueHasCapacity(t *testing.T) {
	cfg := &config.Config{
		Crawl: config.CrawlConfig{
			MaxDepth:     1,
			Concurrency:  1,
			RateLimitRPM: 60000,
			QueueSize:    16,
		},
	}
	cs := NewCrawlSession(nil, cfg, "")

	graph := map[int64][]int64{
		1: {2, 3, 4, 5},
		2: {}, 3: {}, 4: {}, 5: {},
	}

	err := cs.SetNodeHandler(func(ctx context.Context, pageID int64, depth int) *NodeHandlerResult {
		return &NodeHandlerResult{Title: "test", OutgoingLinks: graph[pageID]}
	})
	if err != nil {
		t.Fatalf("SetNodeHandler returned error: %v", err)
	}

	_, runErr := cs.Run(context.Background(), []int64{1})
	if runErr != nil {
		t.Fatalf("expected no queue saturation error, got: %v", runErr)
	}
	if cs.enqueueDrops != 0 {
		t.Fatalf("expected zero enqueue drops, got %d", cs.enqueueDrops)
	}
}
