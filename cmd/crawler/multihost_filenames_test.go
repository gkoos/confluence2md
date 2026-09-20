package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gkoos/confluence2md/internal/config"
	confluenceclient "github.com/gkoos/confluence2md/internal/confluence"
	"github.com/gkoos/confluence2md/internal/crawl"
	"github.com/gkoos/confluence2md/internal/store"
)

func TestPreviewPageAttachments_MatchesDownloadNamingForQualifiedKey(t *testing.T) {
	const pageKey = "company1.atlassian.net/123"
	attachments := []confluenceclient.AttachmentData{
		{ID: "a1", Filename: "diagram.png", MediaType: "image/png", FileSizeBytes: 8, FileID: "fid-1"},
	}

	results := previewPageAttachments(pageKey, attachments, 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 previewed attachment, got %d", len(results))
	}

	want := store.PageAttachmentFilename(pageKey, "diagram.png")
	if results[0].Filename != want {
		t.Fatalf("preview filename = %q, want %q (dry-run and real runs must agree)", results[0].Filename, want)
	}
	if strings.ContainsAny(results[0].Filename, `/\:`) {
		t.Fatalf("preview filename %q is not a flat path segment", results[0].Filename)
	}
}

func TestProcessRerenderedPage_MultiHostWritesFlatPageAndAttachment(t *testing.T) {
	type hostFixture struct {
		host    string
		payload string
		client  *confluenceclient.Client
	}

	newHost := func(t *testing.T, payload string) hostFixture {
		t.Helper()

		router := http.NewServeMux()
		router.HandleFunc("/wiki/rest/api/content/123/child/attachment/a1/download", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(payload))
		})
		ts := httptest.NewServer(router)
		t.Cleanup(ts.Close)

		client, err := confluenceclient.NewClient(ts.URL, ts.URL, "u", "t", config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1}, 60000, 1)
		if err != nil {
			t.Fatalf("new client: %v", err)
		}

		// httptest hosts carry a port, so these keys also exercise the ":"
		// escaping required for Windows filenames.
		return hostFixture{host: strings.TrimPrefix(ts.URL, "http://"), payload: payload, client: client}
	}

	hosts := []hostFixture{
		newHost(t, "payload-from-company1"),
		newHost(t, "payload-from-company2"),
	}

	clients := confluenceclient.NewClientSet()
	seeds := make([]string, 0, len(hosts))
	for _, h := range hosts {
		clients.Add(h.client)
		seeds = append(seeds, "https://"+h.host+"/wiki/spaces/SPACE/pages/123/Title")
	}

	outDir := t.TempDir()
	w, err := store.NewWriter(outDir)
	if err != nil {
		t.Fatalf("NewWriter returned error: %v", err)
	}

	rc := &runContext{
		mode: "full",
		cfg: &config.Config{
			Confluence:  config.ConfluenceConfig{Username: "user", Token: "token"},
			Crawl:       config.CrawlConfig{Seeds: seeds},
			Output:      config.OutputConfig{Dir: outDir},
			Attachments: config.AttachmentsConfig{Download: true, MaxSizeMB: 10},
			Retry:       config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1},
		},
		clients:             clients,
		writer:              w,
		previousPages:       map[string]store.PageRecord{},
		oldManagedArtifacts: map[string]struct{}{},
	}

	for _, h := range hosts {
		crawledPage := &crawl.CrawledPage{
			ID:        123,
			Host:      h.host,
			Title:     "Rendering Candidate",
			Version:   2,
			CrawledAt: time.Now().UTC(),
			Markdown:  "![diagram](attachment://diagram.png)\n",
			Attachments: []confluenceclient.AttachmentData{
				{ID: "a1", PageID: "123", Filename: "diagram.png", FileID: "fid-1", FileSizeBytes: int64(len(h.payload))},
			},
			CreatedByID:      "u1",
			LastModifiedByID: "u2",
		}

		metrics := &runMetrics{}
		if err := processRerenderedPage(context.Background(), rc, metrics, 123, crawledPage); err != nil {
			t.Fatalf("host %q: processRerenderedPage returned error: %v", h.host, err)
		}
		if metrics.errorCount != 0 {
			t.Fatalf("host %q: expected no page errors, got %d", h.host, metrics.errorCount)
		}
		if metrics.attachmentsDownloaded != 1 {
			t.Fatalf("host %q: expected 1 downloaded attachment, got %d", h.host, metrics.attachmentsDownloaded)
		}
	}

	savedPageFiles := make(map[string]string, len(hosts))
	for _, h := range hosts {
		pageKey := h.host + "/123"

		record, ok := w.GetPages()[pageKey]
		if !ok {
			t.Fatalf("expected metadata to keep the canonical host-qualified key %q", pageKey)
		}
		if strings.ContainsAny(record.LocalPath, `/\`) {
			t.Fatalf("host %q: local path %q is not flat", h.host, record.LocalPath)
		}

		wantLocal := "rendering-candidate_" + store.FlattenPageKey(pageKey) + ".md"
		if record.LocalPath != wantLocal {
			t.Fatalf("host %q: local path = %q, want %q", h.host, record.LocalPath, wantLocal)
		}

		pageContent, readErr := os.ReadFile(filepath.Join(outDir, record.LocalPath))
		if readErr != nil {
			t.Fatalf("host %q: read page file: %v", h.host, readErr)
		}

		savedName := store.PageAttachmentFilename(pageKey, "diagram.png")
		if strings.ContainsAny(savedName, `/\:`) {
			t.Fatalf("host %q: saved attachment name %q is not flat", h.host, savedName)
		}
		if !strings.Contains(string(pageContent), "attachments/"+savedName) {
			t.Fatalf("host %q: page markdown does not link its own flat attachment %q:\n%s", h.host, savedName, string(pageContent))
		}

		savedAttachment, readErr := os.ReadFile(filepath.Join(outDir, "attachments", savedName))
		if readErr != nil {
			t.Fatalf("host %q: read saved attachment: %v", h.host, readErr)
		}
		if string(savedAttachment) != h.payload {
			t.Fatalf("host %q: attachment contents = %q, want %q", h.host, string(savedAttachment), h.payload)
		}

		savedPageFiles[record.LocalPath] = h.host
	}

	if len(savedPageFiles) != len(hosts) {
		t.Fatalf("expected %d distinct page files, got %d", len(hosts), len(savedPageFiles))
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "attachments" {
			t.Fatalf("unexpected nested directory %q in output dir", entry.Name())
		}
	}
}
