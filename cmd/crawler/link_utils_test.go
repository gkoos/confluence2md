package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gkoos/confluence2md/internal/config"
	confluenceclient "github.com/gkoos/confluence2md/internal/confluence"
	"github.com/gkoos/confluence2md/internal/store"
)

func TestRewriteAttachmentLinks_RewritesByOriginalName(t *testing.T) {
	results := []store.AttachmentResult{
		{Filename: "123_diagram.png", OriginalName: "diagram.png"},
	}
	input := "![diagram](attachment://diagram.png)"
	got := rewriteAttachmentLinks(input, results)

	if strings.Contains(got, "attachment://diagram.png") {
		t.Fatalf("expected original name to be rewritten, got:\n%s", got)
	}
	if !strings.Contains(got, "attachments/123_diagram.png") {
		t.Fatalf("expected local path in output, got:\n%s", got)
	}
}

func TestRewriteAttachmentLinks_RewritesByFileID(t *testing.T) {
	results := []store.AttachmentResult{
		{Filename: "123_diagram.png", OriginalName: "diagram.png", FileID: "abc-def-uuid"},
	}
	input := "![media](attachment://abc-def-uuid)"
	got := rewriteAttachmentLinks(input, results)

	if strings.Contains(got, "attachment://abc-def-uuid") {
		t.Fatalf("expected UUID to be rewritten, got:\n%s", got)
	}
	if !strings.Contains(got, "attachments/123_diagram.png") {
		t.Fatalf("expected local path in output, got:\n%s", got)
	}
}

func TestRewriteAttachmentLinks_UnknownKeyUnchanged(t *testing.T) {
	results := []store.AttachmentResult{
		{Filename: "123_diagram.png", OriginalName: "diagram.png", FileID: "abc-def-uuid"},
	}
	input := "![other](attachment://unknown-key)"
	got := rewriteAttachmentLinks(input, results)

	if got != input {
		t.Fatalf("expected unchanged output for unknown key, got:\n%s", got)
	}
}

func TestRewriteAttachmentLinks_EmptyResultsNoop(t *testing.T) {
	input := "![media](attachment://some-uuid)"
	got := rewriteAttachmentLinks(input, nil)
	if got != input {
		t.Fatalf("expected no change for empty results, got:\n%s", got)
	}
}

// newTitleLookupServer serves the v2 page endpoint GetPageTitleByID uses and
// counts the requests it received.
func newTitleLookupServer(t *testing.T, titles map[string]string) (*confluenceclient.Client, *int32) {
	t.Helper()

	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)

		id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		title, known := titles[id]
		w.Header().Set("Content-Type", "application/json")
		if !known {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"` + id + `","title":"` + title + `","status":"current","spaceId":"S","version":{"number":1}}`))
	}))
	t.Cleanup(ts.Close)

	client, err := confluenceclient.NewClient(ts.URL, ts.URL, "u", "t", config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1}, 60000, 1)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client, &calls
}

func TestEnrichURLOnlyLinkLabels_HonorsCallerContext(t *testing.T) {
	client, calls := newTitleLookupServer(t, map[string]string{"456": "Linked Page"})

	const link = "https://example.atlassian.net/wiki/spaces/S/pages/456"
	input := "[" + link + "](" + link + ")"

	cases := []struct {
		name   string
		ctxFor func(t *testing.T) context.Context
	}{
		{
			name: "cancelled context",
			ctxFor: func(t *testing.T) context.Context {
				t.Helper()

				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
		},
		{
			name: "expired deadline",
			ctxFor: func(t *testing.T) context.Context {
				t.Helper()

				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				t.Cleanup(cancel)
				return ctx
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := enrichURLOnlyLinkLabels(tc.ctxFor(t), input, client)
			if err == nil {
				t.Fatal("expected an error when the caller's context is already done")
			}
			if got != input {
				t.Fatalf("expected markdown to be left untouched, got:\n%s", got)
			}
			if n := atomic.LoadInt32(calls); n != 0 {
				t.Fatalf("expected no HTTP requests with a done context, got %d", n)
			}
		})
	}
}

func TestEnrichURLOnlyLinkLabels_ReplacesLabelsAndCachesPerTarget(t *testing.T) {
	client, calls := newTitleLookupServer(t, map[string]string{
		"456": "Linked Page",
		"789": "Other Page",
	})

	const first = "https://example.atlassian.net/wiki/spaces/S/pages/456"
	const second = "https://example.atlassian.net/wiki/spaces/S/pages/789"
	input := "[" + first + "](" + first + ")\n[" + first + "](" + first + ")\n[" + second + "](" + second + ")\n"

	got, err := enrichURLOnlyLinkLabels(context.Background(), input, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, want := range []string{"[Linked Page](" + first + ")", "[Other Page](" + second + ")"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in output, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "["+first+"](") {
		t.Fatalf("expected the URL-only label to be replaced, got:\n%s", got)
	}
	// One lookup per distinct target: the repeated link is served by the
	// per-call title cache.
	if n := atomic.LoadInt32(calls); n != 2 {
		t.Fatalf("expected 2 lookups (one per distinct target), got %d", n)
	}
}
