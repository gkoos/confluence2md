package confluence

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gkoos/confluence2md/internal/config"
)

// TestDownloadAttachment_BelowLimit downloads a file below the configured limit.
func TestDownloadAttachment_BelowLimit(t *testing.T) {
	fileContent := bytes.Repeat([]byte("x"), 50*1024*1024) // 50 MiB

	router := http.NewServeMux()
	router.HandleFunc("/wiki/rest/api/content/p1/child/attachment/a1/download", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/file.bin", http.StatusFound)
	})
	router.HandleFunc("/file.bin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(fileContent)))
		_, _ = w.Write(fileContent)
	})

	ts := httptest.NewServer(router)
	defer ts.Close()

	client, err := NewClient(ts.URL, "u", "t", config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1}, 60000, 1)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	attachment := AttachmentData{
		ID:       "a1",
		PageID:   "p1",
		Filename: "test.bin",
	}

	maxBytes := int64(100 * 1024 * 1024) // 100 MiB limit
	data, err := client.DownloadAttachment(context.Background(), attachment, maxBytes)
	if err != nil {
		t.Fatalf("download attachment: %v", err)
	}

	if len(data) != len(fileContent) {
		t.Fatalf("expected %d bytes, got %d", len(fileContent), len(data))
	}
}

// TestDownloadAttachment_AtLimit downloads a file exactly at the configured limit.
func TestDownloadAttachment_AtLimit(t *testing.T) {
	fileContent := bytes.Repeat([]byte("x"), 100*1024*1024) // 100 MiB

	router := http.NewServeMux()
	router.HandleFunc("/wiki/rest/api/content/p1/child/attachment/a1/download", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/file.bin", http.StatusFound)
	})
	router.HandleFunc("/file.bin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(fileContent)))
		_, _ = w.Write(fileContent)
	})

	ts := httptest.NewServer(router)
	defer ts.Close()

	client, err := NewClient(ts.URL, "u", "t", config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1}, 60000, 1)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	attachment := AttachmentData{
		ID:       "a1",
		PageID:   "p1",
		Filename: "test.bin",
	}

	maxBytes := int64(100 * 1024 * 1024) // 100 MiB limit
	data, err := client.DownloadAttachment(context.Background(), attachment, maxBytes)
	if err != nil {
		t.Fatalf("download attachment: %v", err)
	}

	if len(data) != len(fileContent) {
		t.Fatalf("expected %d bytes, got %d", len(fileContent), len(data))
	}
}

// TestDownloadAttachment_AboveLimit tries to download a file above the configured limit.
// Should error with truncation message.
func TestDownloadAttachment_AboveLimit(t *testing.T) {
	fileContent := bytes.Repeat([]byte("x"), 110*1024*1024) // 110 MiB

	router := http.NewServeMux()
	router.HandleFunc("/wiki/rest/api/content/p1/child/attachment/a1/download", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/file.bin", http.StatusFound)
	})
	router.HandleFunc("/file.bin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(fileContent)))
		_, _ = w.Write(fileContent)
	})

	ts := httptest.NewServer(router)
	defer ts.Close()

	client, err := NewClient(ts.URL, "u", "t", config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1}, 60000, 1)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	attachment := AttachmentData{
		ID:       "a1",
		PageID:   "p1",
		Filename: "test.bin",
	}

	maxBytes := int64(100 * 1024 * 1024) // 100 MiB limit
	_, err = client.DownloadAttachment(context.Background(), attachment, maxBytes)
	if err == nil {
		t.Fatal("expected error for file above limit, got nil")
	}

	if !strings.Contains(err.Error(), "truncated") && !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("expected truncation error, got: %v", err)
	}
}

// TestDownloadAttachment_UnlimitedDownload downloads a large file with no limit (maxBytes=0).
func TestDownloadAttachment_UnlimitedDownload(t *testing.T) {
	fileContent := bytes.Repeat([]byte("x"), 150*1024*1024) // 150 MiB

	router := http.NewServeMux()
	router.HandleFunc("/wiki/rest/api/content/p1/child/attachment/a1/download", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/file.bin", http.StatusFound)
	})
	router.HandleFunc("/file.bin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(fileContent)))
		_, _ = w.Write(fileContent)
	})

	ts := httptest.NewServer(router)
	defer ts.Close()

	client, err := NewClient(ts.URL, "u", "t", config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1}, 60000, 1)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	attachment := AttachmentData{
		ID:       "a1",
		PageID:   "p1",
		Filename: "test.bin",
	}

	maxBytes := int64(0) // No limit
	data, err := client.DownloadAttachment(context.Background(), attachment, maxBytes)
	if err != nil {
		t.Fatalf("download attachment: %v", err)
	}

	if len(data) != len(fileContent) {
		t.Fatalf("expected %d bytes, got %d", len(fileContent), len(data))
	}
}

// TestDownloadAttachment_ContentLengthExceedsLimit checks pre-validation against Content-Length header.
func TestDownloadAttachment_ContentLengthExceedsLimit(t *testing.T) {
	fileContent := bytes.Repeat([]byte("x"), 50*1024*1024) // 50 MiB actual

	router := http.NewServeMux()
	router.HandleFunc("/wiki/rest/api/content/p1/child/attachment/a1/download", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/file.bin", http.StatusFound)
	})
	router.HandleFunc("/file.bin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		// Lie about content length: claim 110 MiB when we only have 50 MiB
		w.Header().Set("Content-Length", fmt.Sprintf("%d", 110*1024*1024))
		_, _ = w.Write(fileContent)
	})

	ts := httptest.NewServer(router)
	defer ts.Close()

	client, err := NewClient(ts.URL, "u", "t", config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1}, 60000, 1)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	attachment := AttachmentData{
		ID:       "a1",
		PageID:   "p1",
		Filename: "test.bin",
	}

	maxBytes := int64(100 * 1024 * 1024) // 100 MiB limit
	_, err = client.DownloadAttachment(context.Background(), attachment, maxBytes)
	if err == nil {
		t.Fatal("expected error for Content-Length exceeding limit, got nil")
	}

	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("expected Content-Length validation error, got: %v", err)
	}
}

// TestReadWithLimit_NoLimitReadsAll verifies that maxBytes=0 reads all data.
func TestReadWithLimit_NoLimitReadsAll(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 1024*1024)
	resp := &http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: int64(len(data)),
		Body:          io.NopCloser(bytes.NewReader(data)),
	}

	result, err := readWithLimit(resp, 0)
	if err != nil {
		t.Fatalf("readWithLimit: %v", err)
	}

	if len(result) != len(data) {
		t.Fatalf("expected %d bytes, got %d", len(data), len(result))
	}
}

// TestReadWithLimit_ExactLimitSucceeds verifies that reading exactly to the limit succeeds.
func TestReadWithLimit_ExactLimitSucceeds(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 100)
	resp := &http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: int64(len(data)),
		Body:          io.NopCloser(bytes.NewReader(data)),
	}

	result, err := readWithLimit(resp, int64(len(data)))
	if err != nil {
		t.Fatalf("readWithLimit: %v", err)
	}

	if len(result) != len(data) {
		t.Fatalf("expected %d bytes, got %d", len(data), len(result))
	}
}

// TestReadWithLimit_DetectsTruncation verifies that exceeding the limit is detected.
func TestReadWithLimit_DetectsTruncation(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 110)
	resp := &http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: int64(len(data)),
		Body:          io.NopCloser(bytes.NewReader(data)),
	}

	_, err := readWithLimit(resp, int64(100))
	if err == nil {
		t.Fatal("expected error for truncation, got nil")
	}

	if !strings.Contains(err.Error(), "truncated") && !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("expected truncation or exceeds-limit error, got: %v", err)
	}
}
