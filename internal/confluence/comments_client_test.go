package confluence

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gkoos/confluence2md/internal/config"
)

func TestFetchV2CommentsFromEndpoint_Paginates(t *testing.T) {
	// Use a dispatcher to handle same path with/without cursor.
	router := http.NewServeMux()
	router.HandleFunc("/wiki/api/v2/pages/123/footer-comments", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "abc" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"results": [{
					"id": "c2",
					"parentCommentId": "",
					"version": {"createdAt": "2026-02-13T10:01:00Z", "authorId": "a2"},
					"body": {"storage": {"value": "<p>second</p>"}}
				}],
				"_links": {}
			}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [{
				"id": "c1",
				"parentCommentId": "",
				"version": {"createdAt": "2026-02-13T10:00:00Z", "authorId": "a1"},
				"body": {"storage": {"value": "<p>first</p>"}}
			}],
			"_links": {"next": "/wiki/api/v2/pages/123/footer-comments?cursor=abc"}
		}`))
	})

	ts := httptest.NewServer(router)
	defer ts.Close()

	client, err := NewClient(ts.URL, "u", "t", config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1}, 60000, 1)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	endpoint := ts.URL + "/wiki/api/v2/pages/123/footer-comments?limit=100&body-format=storage"
	comments, err := client.fetchV2CommentsFromEndpoint(context.Background(), endpoint)
	if err != nil {
		t.Fatalf("fetch comments: %v", err)
	}

	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
	if comments[0].ID != "c1" || comments[1].ID != "c2" {
		t.Fatalf("unexpected IDs: %#v", comments)
	}
}

func TestGetPageCommentsV2_FetchesChildrenAndDisplayNames(t *testing.T) {
	router := http.NewServeMux()
	router.HandleFunc("/wiki/api/v2/pages/644/footer-comments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [{
				"id": "p1",
				"parentCommentId": "",
				"version": {"createdAt": "2026-02-13T10:00:00Z", "authorId": "acc-1"},
				"body": {"storage": {"value": "<p>parent</p>"}}
			}],
			"_links": {}
		}`))
	})

	router.HandleFunc("/wiki/api/v2/footer-comments/p1/children", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [{
				"id": "c1",
				"parentCommentId": "p1",
				"version": {"createdAt": "2026-02-17T10:00:00Z", "authorId": "acc-2"},
				"body": {"storage": {"value": "<p>child</p>"}}
			}],
			"_links": {}
		}`))
	})

	router.HandleFunc("/wiki/api/v2/footer-comments/c1/children", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results": [], "_links": {}}`))
	})

	// Mock user lookup endpoints (v1 REST API)
	router.HandleFunc("/wiki/rest/api/user", func(w http.ResponseWriter, r *http.Request) {
		accountID := r.URL.Query().Get("accountId")
		w.Header().Set("Content-Type", "application/json")
		switch accountID {
		case "acc-1":
			_, _ = w.Write([]byte(`{"accountId": "acc-1", "displayName": "Simon Dunn"}`))
		case "acc-2":
			_, _ = w.Write([]byte(`{"accountId": "acc-2", "displayName": "Natacha Tomkinson"}`))
		default:
			http.Error(w, "not found", 404)
		}
	})

	ts := httptest.NewServer(router)
	defer ts.Close()

	client, err := NewClient(ts.URL, "u", "t", config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1}, 60000, 1)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	comments, err := client.GetPageComments(context.Background(), 644)
	if err != nil {
		t.Fatalf("GetPageComments: %v", err)
	}

	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}

	var sawParent, sawChild bool
	for _, c := range comments {
		if c.ID == "p1" {
			sawParent = true
			if c.Author != "Simon Dunn" {
				t.Fatalf("expected parent author to be enriched, got %q", c.Author)
			}
		}
		if c.ID == "c1" {
			sawChild = true
			if c.ParentID != "p1" {
				t.Fatalf("expected child parentID p1, got %q", c.ParentID)
			}
			if c.Author != "Natacha Tomkinson" {
				t.Fatalf("expected child author to be enriched, got %q", c.Author)
			}
		}
	}

	if !sawParent || !sawChild {
		t.Fatalf("expected both parent and child comment, got %#v", comments)
	}
}
