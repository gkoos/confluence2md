package confluence

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gkoos/confluence2md/internal/config"
)

// newTestUserClient builds a Client pointed at ts, using minimal retry
// config so tests run fast and don't retry on error responses.
func newTestUserClient(t *testing.T, ts *httptest.Server) *Client {
	t.Helper()
	client, err := NewClient(ts.URL, "u", "t", config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1}, 60000, 1)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client
}

func TestGetUserDisplayName_CacheHit(t *testing.T) {
	accountID := "cache-hit-account-id"
	globalUserCache.mu.Lock()
	globalUserCache.names[accountID] = "Cached Name"
	globalUserCache.mu.Unlock()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("expected no HTTP call for a cached account ID")
	}))
	defer ts.Close()

	client := newTestUserClient(t, ts)
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
