package confluence

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gkoos/confluence2md/internal/config"
)

// newTestUserClient builds a Client pointed at ts, using minimal retry
// config so tests run fast and don't retry on error responses.
func newTestUserClient(t *testing.T, ts *httptest.Server) *Client {
	t.Helper()
	client, err := NewClient(ts.URL, ts.URL, "u", "t", config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1}, 60000, 1)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client
}

func TestGetUserDisplayName_CacheHit(t *testing.T) {
	accountID := "cache-hit-account-id"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("expected no HTTP call for a cached account ID")
	}))
	defer ts.Close()

	client := newTestUserClient(t, ts)

	// Seed this client's own cache: names are cached per client, so there is no
	// process-wide map to prime (which is what this test used to rely on).
	client.userNames.store(accountID, "Cached Name")

	name := client.GetUserDisplayName(context.Background(), accountID)
	if name != "Cached Name" {
		t.Fatalf("expected cached name, got %q", name)
	}
}

func TestGetUserDisplayName_FetchesAndCaches(t *testing.T) {
	accountID := "fetch-and-cache-account-id"
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accountId":"` + accountID + `","displayName":"Jane Doe","publicName":"jdoe"}`))
	}))
	defer ts.Close()

	client := newTestUserClient(t, ts)
	name := client.GetUserDisplayName(context.Background(), accountID)
	if name != "Jane Doe" {
		t.Fatalf("expected %q, got %q", "Jane Doe", name)
	}

	// Second call should be served from cache, not hit the server again.
	name = client.GetUserDisplayName(context.Background(), accountID)
	if name != "Jane Doe" {
		t.Fatalf("expected cached %q, got %q", "Jane Doe", name)
	}
	if callCount != 1 {
		t.Fatalf("expected exactly 1 HTTP call, got %d", callCount)
	}
}

func TestGetUserDisplayName_FallsBackToPublicName(t *testing.T) {
	accountID := "fallback-public-name-account-id"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accountId":"` + accountID + `","displayName":"","publicName":"jdoe"}`))
	}))
	defer ts.Close()

	client := newTestUserClient(t, ts)
	name := client.GetUserDisplayName(context.Background(), accountID)
	if name != "jdoe" {
		t.Fatalf("expected fallback to publicName %q, got %q", "jdoe", name)
	}
}

func TestGetUserDisplayName_NonSuccessStatusReturnsEmpty(t *testing.T) {
	accountID := "non-success-status-account-id"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"not found"}`))
	}))
	defer ts.Close()

	client := newTestUserClient(t, ts)
	name := client.GetUserDisplayName(context.Background(), accountID)
	if name != "" {
		t.Fatalf("expected empty name on non-success status, got %q", name)
	}
}

func TestGetUserDisplayName_MalformedJSONReturnsEmpty(t *testing.T) {
	accountID := "malformed-json-account-id"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer ts.Close()

	client := newTestUserClient(t, ts)
	name := client.GetUserDisplayName(context.Background(), accountID)
	if name != "" {
		t.Fatalf("expected empty name on malformed JSON, got %q", name)
	}
}

func TestGetUserDisplayName_EmptyAccountIDReturnsEmpty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("expected no HTTP call for an empty account ID")
	}))
	defer ts.Close()

	client := newTestUserClient(t, ts)
	name := client.GetUserDisplayName(context.Background(), "   ")
	if name != "" {
		t.Fatalf("expected empty name for empty/whitespace account ID, got %q", name)
	}
}

// countingUserServer serves the v1 user endpoint with a fixed displayName and
// counts the requests it received, so tests can assert cache behaviour.
func countingUserServer(t *testing.T, accountID, displayName string) (*Client, *int32) {
	t.Helper()

	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accountId":"` + accountID + `","displayName":"` + displayName + `"}`))
	}))
	t.Cleanup(ts.Close)

	return newTestUserClient(t, ts), &calls
}

func TestGetUserDisplayName_ClientCachesAreIsolated(t *testing.T) {
	const accountID = "isolation-account-id"

	first, firstCalls := countingUserServer(t, accountID, "Name On First Host")
	second, secondCalls := countingUserServer(t, accountID, "Name On Second Host")

	if got := first.GetUserDisplayName(context.Background(), accountID); got != "Name On First Host" {
		t.Fatalf("first client returned %q, want %q", got, "Name On First Host")
	}
	if got := second.GetUserDisplayName(context.Background(), accountID); got != "Name On Second Host" {
		t.Fatalf("second client returned %q, want %q (caches must not be shared between clients)", got, "Name On Second Host")
	}
	if got := atomic.LoadInt32(secondCalls); got != 1 {
		t.Fatalf("expected the second client to issue its own request, got %d calls", got)
	}
	if got := atomic.LoadInt32(firstCalls); got != 1 {
		t.Fatalf("expected the first client to issue exactly one request, got %d calls", got)
	}
}

func TestGetUserDisplayName_FailureDoesNotAffectOtherClients(t *testing.T) {
	const accountID = "failure-isolation-account-id"

	var failingCalls int32
	failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&failingCalls, 1)
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	}))
	defer failingServer.Close()
	failingClient := newTestUserClient(t, failingServer)

	healthyClient, healthyCalls := countingUserServer(t, accountID, "Healthy Name")

	if got := failingClient.GetUserDisplayName(context.Background(), accountID); got != "" {
		t.Fatalf("expected an empty name from the failing client, got %q", got)
	}
	if got := healthyClient.GetUserDisplayName(context.Background(), accountID); got != "Healthy Name" {
		t.Fatalf("expected the healthy client to resolve the name, got %q", got)
	}
	if got := atomic.LoadInt32(healthyCalls); got != 1 {
		t.Fatalf("expected the healthy client to issue its own request, got %d calls", got)
	}
}

func TestGetUserDisplayName_FailureIsNotCachedWithinClient(t *testing.T) {
	const accountID = "recovery-account-id"

	var healthy atomic.Bool
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if !healthy.Load() {
			http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accountId":"` + accountID + `","displayName":"Recovered Name"}`))
	}))
	defer ts.Close()

	client := newTestUserClient(t, ts)

	if got := client.GetUserDisplayName(context.Background(), accountID); got != "" {
		t.Fatalf("expected an empty name while the lookup fails, got %q", got)
	}

	healthy.Store(true)
	if got := client.GetUserDisplayName(context.Background(), accountID); got != "Recovered Name" {
		t.Fatalf("expected the same client to retry and recover the name, got %q", got)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 requests (failure then retry), got %d", got)
	}

	// The recovered value is cached, so the client stops asking. This also proves
	// the earlier failure was not stored.
	healthy.Store(false)
	if got := client.GetUserDisplayName(context.Background(), accountID); got != "Recovered Name" {
		t.Fatalf("expected the recovered name to be cached, got %q", got)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected no further requests after recovery, got %d", got)
	}
}

func TestGetUserDisplayName_ConcurrentLookupsShareOneCachedResult(t *testing.T) {
	const accountID = "concurrent-account-id"
	const goroutines = 8

	client, calls := countingUserServer(t, accountID, "Concurrent Name")

	var wg sync.WaitGroup
	results := make(chan string, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- client.GetUserDisplayName(context.Background(), accountID)
		}()
	}
	wg.Wait()
	close(results)

	for got := range results {
		if got != "Concurrent Name" {
			t.Fatalf("concurrent lookup returned %q, want %q", got, "Concurrent Name")
		}
	}

	// Duplicate in-flight lookups are permitted (no singleflight), so the request
	// count is bounded by the number of callers rather than pinned to one.
	if got := atomic.LoadInt32(calls); got < 1 || got > goroutines {
		t.Fatalf("expected between 1 and %d requests, got %d", goroutines, got)
	}

	before := atomic.LoadInt32(calls)
	if got := client.GetUserDisplayName(context.Background(), accountID); got != "Concurrent Name" {
		t.Fatalf("cached value after the race = %q, want %q", got, "Concurrent Name")
	}
	if after := atomic.LoadInt32(calls); after != before {
		t.Fatalf("expected the raced result to be cached, but requests grew from %d to %d", before, after)
	}
}
