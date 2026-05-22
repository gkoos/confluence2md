package confluence

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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

	client, err := NewClient(ts.URL, "u", "t")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	authorIDs := make(map[string]bool)
	endpoint := ts.URL + "/wiki/api/v2/pages/123/footer-comments?limit=100&body-format=storage"
	comments, err := client.fetchV2CommentsFromEndpoint(context.Background(), endpoint, authorIDs)
	if err != nil {
		t.Fatalf("fetch comments: %v", err)
	}

	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
	if comments[0].ID != "c1" || comments[1].ID != "c2" {
		t.Fatalf("unexpected IDs: %#v", comments)
	}
	if !authorIDs["a1"] || !authorIDs["a2"] {
		t.Fatalf("expected both author IDs, got %#v", authorIDs)
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

	router.HandleFunc("/wiki/api/v2/users-bulk", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		var payload map[string][]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode users payload: %v", err)
		}
		if len(payload["accountIds"]) != 2 {
			t.Fatalf("expected 2 account IDs, got %#v", payload)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [
				{"accountId": "acc-1", "displayName": "Simon Dunn"},
				{"accountId": "acc-2", "displayName": "Natacha Tomkinson"}
			]
		}`))
	})

	ts := httptest.NewServer(router)
	defer ts.Close()

	client, err := NewClient(ts.URL, "u", "t")
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
