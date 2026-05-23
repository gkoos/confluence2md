package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMarkSuccessfulCheckpoint_Validation(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter returned error: %v", err)
	}

	start := time.Now().UTC()
	end := start.Add(1 * time.Minute)

	if err := w.MarkSuccessfulCheckpoint("invalid", start, end); err == nil {
		t.Fatalf("expected error for invalid mode")
	}
	if err := w.MarkSuccessfulCheckpoint("full", time.Time{}, end); err == nil {
		t.Fatalf("expected error for zero start")
	}
	if err := w.MarkSuccessfulCheckpoint("full", start, time.Time{}); err == nil {
		t.Fatalf("expected error for zero completion")
	}
	if err := w.MarkSuccessfulCheckpoint("full", end, start); err == nil {
		t.Fatalf("expected error when completion is before start")
	}
}

func TestMarkCompletedCheckpoint_Validation(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter returned error: %v", err)
	}

	start := time.Now().UTC()
	end := start.Add(1 * time.Minute)

	if err := w.MarkCompletedCheckpoint("invalid", start, end); err == nil {
		t.Fatalf("expected error for invalid mode")
	}
	if err := w.MarkCompletedCheckpoint("full", time.Time{}, end); err == nil {
		t.Fatalf("expected error for zero start")
	}
	if err := w.MarkCompletedCheckpoint("full", start, time.Time{}); err == nil {
		t.Fatalf("expected error for zero completion")
	}
	if err := w.MarkCompletedCheckpoint("full", end, start); err == nil {
		t.Fatalf("expected error when completion is before start")
	}
}

func TestNewWriter_LoadMetadataRejectsInvalidRecordID(t *testing.T) {
	dir := t.TempDir()
	invalidMeta := `{
  "crawl_started_at": "2026-01-01T00:00:00Z",
  "pages": {
    "123": {
      "id": "999",
      "title": "Bad",
      "local_path": "bad_123.md",
      "version": 1,
      "crawled_at": "2026-01-01T00:00:00Z",
      "source_url": "https://example/wiki/pages/123",
      "canonical_url": "https://example/wiki/spaces/X/pages/123",
      "space_key": "X",
      "depth": 0,
      "outgoing_links": [],
      "incoming_links": []
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(invalidMeta), 0644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}

	_, err := NewWriter(dir)
	if err == nil {
		t.Fatalf("expected NewWriter to fail for invalid metadata")
	}
	if !strings.Contains(err.Error(), "validate metadata") {
		t.Fatalf("expected validation error, got: %v", err)
	}
}

func TestSaveAndLoad_PersistsSuccessfulCheckpoint(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter returned error: %v", err)
	}

	record := PageRecord{
		ID:            "123",
		Title:         "Page",
		Version:       2,
		CrawledAt:     time.Now().UTC(),
		SourceURL:     "https://example/wiki/pages/123",
		CanonicalURL:  "https://example/wiki/spaces/X/pages/123",
		SpaceKey:      "X",
		Depth:         0,
		OutgoingLinks: []string{},
		IncomingLinks: []string{},
		StorageFormat: "# Page",
	}
	if err := w.AddPage("123", record); err != nil {
		t.Fatalf("AddPage returned error: %v", err)
	}

	started := w.CrawlStartedAt()
	completed := started.Add(2 * time.Minute)
	if err := w.MarkSuccessfulCheckpoint("full", started, completed); err != nil {
		t.Fatalf("MarkSuccessfulCheckpoint returned error: %v", err)
	}
	if err := w.SaveMetadata(); err != nil {
		t.Fatalf("SaveMetadata returned error: %v", err)
	}

	w2, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter reload returned error: %v", err)
	}

	pages := w2.GetPages()
	if _, ok := pages["123"]; !ok {
		t.Fatalf("expected page 123 to be loaded")
	}

	metaPath := filepath.Join(dir, "metadata.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read metadata.json: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `"last_successful_crawl_mode": "full"`) {
		t.Fatalf("expected successful crawl mode in metadata.json")
	}
	if !strings.Contains(content, `"last_successful_crawl_started_at"`) {
		t.Fatalf("expected successful crawl start timestamp in metadata.json")
	}
	if !strings.Contains(content, `"last_successful_crawl_completed_at"`) {
		t.Fatalf("expected successful crawl completion timestamp in metadata.json")
	}
}

func TestSaveAndLoad_PersistsCompletedCheckpoint(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter returned error: %v", err)
	}

	started := w.CrawlStartedAt()
	completed := started.Add(2 * time.Minute)
	if err := w.MarkCompletedCheckpoint("updates", started, completed); err != nil {
		t.Fatalf("MarkCompletedCheckpoint returned error: %v", err)
	}
	if err := w.SaveMetadata(); err != nil {
		t.Fatalf("SaveMetadata returned error: %v", err)
	}

	w2, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter reload returned error: %v", err)
	}

	cp := w2.LastCompletedCheckpoint()
	if !cp.Present {
		t.Fatalf("expected completed checkpoint to be present")
	}
	if cp.Mode != "updates" {
		t.Fatalf("expected mode updates, got %q", cp.Mode)
	}

	metaPath := filepath.Join(dir, "metadata.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read metadata.json: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `"last_completed_crawl_mode": "updates"`) {
		t.Fatalf("expected completed crawl mode in metadata.json")
	}
	if !strings.Contains(content, `"last_completed_crawl_started_at"`) {
		t.Fatalf("expected completed crawl start timestamp in metadata.json")
	}
	if !strings.Contains(content, `"last_completed_crawl_completed_at"`) {
		t.Fatalf("expected completed crawl completion timestamp in metadata.json")
	}
}

func TestAddPageMetadata_UpsertsWithoutWritingFile(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter returned error: %v", err)
	}

	record := PageRecord{
		ID:            "123",
		Title:         "Existing Page",
		LocalPath:     "existing-page_123.md",
		Version:       4,
		CrawledAt:     time.Now().UTC(),
		SourceURL:     "https://example/wiki/pages/123",
		CanonicalURL:  "https://example/wiki/spaces/X/pages/123",
		SpaceKey:      "X",
		Depth:         1,
		OutgoingLinks: []string{"456"},
		IncomingLinks: []string{},
		StorageFormat: "# Existing",
	}

	w.AddPageMetadata("123", record)

	if _, statErr := os.Stat(filepath.Join(dir, record.LocalPath)); !os.IsNotExist(statErr) {
		t.Fatalf("expected no markdown file write, stat error=%v", statErr)
	}

	pages := w.GetPages()
	got, ok := pages["123"]
	if !ok {
		t.Fatalf("expected page 123 in metadata map")
	}
	if got.Title != record.Title {
		t.Fatalf("unexpected title, got=%q want=%q", got.Title, record.Title)
	}
	if got.LocalPath != record.LocalPath {
		t.Fatalf("unexpected local path, got=%q want=%q", got.LocalPath, record.LocalPath)
	}
}

func TestMarkSuccessfulCheckpoint_FailureDoesNotMutateExistingCheckpoint(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter returned error: %v", err)
	}

	start := w.CrawlStartedAt()
	end := start.Add(1 * time.Minute)
	if err := w.MarkSuccessfulCheckpoint("full", start, end); err != nil {
		t.Fatalf("MarkSuccessfulCheckpoint initial set returned error: %v", err)
	}
	if err := w.SaveMetadata(); err != nil {
		t.Fatalf("SaveMetadata returned error: %v", err)
	}

	w2, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter reload returned error: %v", err)
	}

	// Invalid mode should fail and must not overwrite the previous successful checkpoint.
	badStart := w2.CrawlStartedAt().Add(2 * time.Minute)
	badEnd := badStart.Add(1 * time.Minute)
	if err := w2.MarkSuccessfulCheckpoint("bad-mode", badStart, badEnd); err == nil {
		t.Fatalf("expected error for invalid mode")
	}
	if err := w2.SaveMetadata(); err != nil {
		t.Fatalf("SaveMetadata after failed mark returned error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		t.Fatalf("read metadata.json: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `"last_successful_crawl_mode": "full"`) {
		t.Fatalf("expected prior successful checkpoint mode to remain full")
	}
}

func TestLastSuccessfulCheckpoint_EmptyWhenUnset(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter returned error: %v", err)
	}

	cp := w.LastSuccessfulCheckpoint()
	if cp.Present {
		t.Fatalf("expected checkpoint to be absent")
	}
}

func TestLastSuccessfulCheckpoint_ReturnsPersistedTuple(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter returned error: %v", err)
	}

	start := w.CrawlStartedAt()
	completed := start.Add(30 * time.Second)
	if err := w.MarkSuccessfulCheckpoint("updates", start, completed); err != nil {
		t.Fatalf("MarkSuccessfulCheckpoint returned error: %v", err)
	}

	cp := w.LastSuccessfulCheckpoint()
	if !cp.Present {
		t.Fatalf("expected checkpoint to be present")
	}
	if cp.Mode != "updates" {
		t.Fatalf("expected mode updates, got %q", cp.Mode)
	}
	if !cp.StartedAt.Equal(start) {
		t.Fatalf("unexpected started_at: got %s want %s", cp.StartedAt, start)
	}
	if !cp.CompletedAt.Equal(completed) {
		t.Fatalf("unexpected completed_at: got %s want %s", cp.CompletedAt, completed)
	}
}

func TestLastCompletedCheckpoint_EmptyWhenUnset(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter returned error: %v", err)
	}

	cp := w.LastCompletedCheckpoint()
	if cp.Present {
		t.Fatalf("expected completed checkpoint to be absent")
	}
}

func TestLastCompletedCheckpoint_ReturnsPersistedTuple(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter returned error: %v", err)
	}

	start := w.CrawlStartedAt()
	completed := start.Add(30 * time.Second)
	if err := w.MarkCompletedCheckpoint("full", start, completed); err != nil {
		t.Fatalf("MarkCompletedCheckpoint returned error: %v", err)
	}

	cp := w.LastCompletedCheckpoint()
	if !cp.Present {
		t.Fatalf("expected completed checkpoint to be present")
	}
	if cp.Mode != "full" {
		t.Fatalf("expected mode full, got %q", cp.Mode)
	}
	if !cp.StartedAt.Equal(start) {
		t.Fatalf("unexpected started_at: got %s want %s", cp.StartedAt, start)
	}
	if !cp.CompletedAt.Equal(completed) {
		t.Fatalf("unexpected completed_at: got %s want %s", cp.CompletedAt, completed)
	}
}

func TestNewWriter_LoadMetadataRejectsPartialCompletedCheckpointTuple(t *testing.T) {
	dir := t.TempDir()
	invalidMeta := `{
  "crawl_started_at": "2026-01-01T00:00:00Z",
  "last_completed_crawl_mode": "full",
  "pages": {}
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(invalidMeta), 0644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}

	_, err := NewWriter(dir)
	if err == nil {
		t.Fatalf("expected NewWriter to fail for partial completed checkpoint tuple")
	}
	if !strings.Contains(err.Error(), "last completed checkpoint fields must be all set or all unset") {
		t.Fatalf("expected completed tuple validation error, got: %v", err)
	}
}

func TestNewWriter_LoadLegacyMetadataWithoutCompletedCheckpoint(t *testing.T) {
	dir := t.TempDir()
	legacyMeta := `{
  "crawl_started_at": "2026-01-01T00:00:00Z",
  "last_successful_crawl_started_at": "2026-01-01T00:00:00Z",
  "last_successful_crawl_completed_at": "2026-01-01T00:05:00Z",
  "last_successful_crawl_mode": "full",
  "pages": {}
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(legacyMeta), 0644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}

	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("expected legacy metadata to load, got error: %v", err)
	}

	completed := w.LastCompletedCheckpoint()
	if completed.Present {
		t.Fatalf("expected completed checkpoint to be absent for legacy metadata")
	}

	successful := w.LastSuccessfulCheckpoint()
	if !successful.Present {
		t.Fatalf("expected successful checkpoint to remain present for legacy metadata")
	}
}

func TestSetSeedPageIDs_NormalizesAndPersists(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter returned error: %v", err)
	}

	w.SetSeedPageIDs([]string{" 123 ", "", "123", "456", " 456 ", "789"})

	got := w.GetSeedPageIDs()
	if len(got) != 3 {
		t.Fatalf("expected 3 normalized seed IDs, got %d: %#v", len(got), got)
	}
	if got[0] != "123" || got[1] != "456" || got[2] != "789" {
		t.Fatalf("unexpected normalized seed IDs: %#v", got)
	}

	if err := w.SaveMetadata(); err != nil {
		t.Fatalf("SaveMetadata returned error: %v", err)
	}

	w2, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter reload returned error: %v", err)
	}

	reloaded := w2.GetSeedPageIDs()
	if len(reloaded) != 3 {
		t.Fatalf("expected 3 reloaded seed IDs, got %d: %#v", len(reloaded), reloaded)
	}
	if reloaded[0] != "123" || reloaded[1] != "456" || reloaded[2] != "789" {
		t.Fatalf("unexpected reloaded seed IDs: %#v", reloaded)
	}
}

func TestNewWriter_LoadMetadataRejectsEmptySeedPageID(t *testing.T) {
	dir := t.TempDir()
	invalidMeta := `{
  "crawl_started_at": "2026-01-01T00:00:00Z",
  "seed_page_ids": ["123", ""],
  "pages": {}
}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(invalidMeta), 0644); err != nil {
		t.Fatalf("write metadata.json: %v", err)
	}

	_, err := NewWriter(dir)
	if err == nil {
		t.Fatalf("expected NewWriter to fail for empty seed page id")
	}
	if !strings.Contains(err.Error(), "seed_page_ids[1] is empty") {
		t.Fatalf("expected seed_page_ids validation error, got: %v", err)
	}
}
