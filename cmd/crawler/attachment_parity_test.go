package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gkoos/confluence2md/internal/config"
	confluenceclient "github.com/gkoos/confluence2md/internal/confluence"
	"github.com/gkoos/confluence2md/internal/store"
)

// TestAttachmentPreviewMatchesDownloadClassification pins the invariant behind the
// shared store.ClassifyAttachment helper: the dry-run preview and the real
// download must classify an attachment identically (same skip decision, same
// error text, same saved filename), and neither may reach the network for an
// attachment that failed validation.
func TestAttachmentPreviewMatchesDownloadClassification(t *testing.T) {
	const pageKey = "company1.atlassian.net/123"
	const maxSizeMB = 10

	var requests int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("filedata"))
	}))
	t.Cleanup(ts.Close)

	client, err := confluenceclient.NewClient(ts.URL, ts.URL, "u", "t", config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1}, 60000, 1)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	attachments := []confluenceclient.AttachmentData{
		{
			ID:            "a-oversize",
			PageID:        "123",
			Filename:      "huge.bin",
			MediaType:     "application/octet-stream",
			FileSizeBytes: int64(maxSizeMB)*1024*1024 + 1,
		},
		{ID: "   ", PageID: "123", Filename: "no-id.bin", MediaType: "text/plain", FileSizeBytes: 5},
		{ID: "a-valid", PageID: "123", Filename: "diagram.png", MediaType: "image/png", FileSizeBytes: 8, FileID: "fid-valid"},
	}

	preview := previewPageAttachments(pageKey, attachments, maxSizeMB)
	if len(preview) != len(attachments) {
		t.Fatalf("expected %d previewed attachments, got %d", len(attachments), len(preview))
	}

	real := store.DownloadPageAttachments(context.Background(), t.TempDir(), pageKey, attachments, maxSizeMB, client)
	if len(real) != len(attachments) {
		t.Fatalf("expected %d download results, got %d", len(attachments), len(real))
	}

	for i := range attachments {
		p, d := preview[i], real[i]

		if p.Skipped != d.Skipped {
			t.Fatalf("attachment %d: Skipped differs (preview=%v download=%v)", i, p.Skipped, d.Skipped)
		}
		if (p.Error == nil) != (d.Error == nil) {
			t.Fatalf("attachment %d: error presence differs (preview=%v download=%v)", i, p.Error, d.Error)
		}
		if p.Error != nil && d.Error != nil && p.Error.Error() != d.Error.Error() {
			t.Fatalf("attachment %d: error text differs:\npreview:  %v\ndownload: %v", i, p.Error, d.Error)
		}
		if p.Filename != d.Filename {
			t.Fatalf("attachment %d: saved filename differs (preview=%q download=%q)", i, p.Filename, d.Filename)
		}
		if p.OriginalName != d.OriginalName {
			t.Fatalf("attachment %d: original name differs (preview=%q download=%q)", i, p.OriginalName, d.OriginalName)
		}
		if p.FileID != d.FileID {
			t.Fatalf("attachment %d: file ID differs (preview=%q download=%q)", i, p.FileID, d.FileID)
		}
	}

	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("expected exactly 1 request (the valid attachment only), got %d", got)
	}
}
