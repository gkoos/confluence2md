package store

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gkoos/confluence2md/internal/config"
	"github.com/gkoos/confluence2md/internal/confluence"
)

func TestDownloadPageAttachments_PropagatesFileID(t *testing.T) {
	router := http.NewServeMux()
	router.HandleFunc("/wiki/rest/api/content/p1/child/attachment/a1/download", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("filedata"))
	})
	ts := httptest.NewServer(router)
	defer ts.Close()

	client, err := confluence.NewClient(ts.URL, "u", "t", config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1}, 60000, 1)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	attachments := []confluence.AttachmentData{
		{
			ID:            "a1",
			PageID:        "p1",
			Filename:      "diagram.png",
			MediaType:     "image/png",
			FileSizeBytes: 8,
			FileID:        "test-uuid-abc-123",
		},
	}

	dir := t.TempDir()
	results := DownloadPageAttachments(t.Context(), dir, "p1", attachments, 0, client)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
	if r.FileID != "test-uuid-abc-123" {
		t.Fatalf("expected FileID to be propagated, got %q", r.FileID)
	}
	if r.OriginalName != "diagram.png" {
		t.Fatalf("expected OriginalName %q, got %q", "diagram.png", r.OriginalName)
	}
}
