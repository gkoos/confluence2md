package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gkoos/confluence2md/internal/config"
	"github.com/gkoos/confluence2md/internal/confluence"
	"github.com/gkoos/confluence2md/internal/crawl"
	"github.com/gkoos/confluence2md/internal/store"
)

func TestClearDirectoryContents_RejectsUnsafePath(t *testing.T) {
	err := clearDirectoryContents(".")
	if err == nil {
		t.Fatalf("expected error for unsafe directory path")
	}
}

func TestClearDirectoryContents_CreatesMissingDirectory(t *testing.T) {
	root := t.TempDir()
	outDir := filepath.Join(root, "output")

	if err := clearDirectoryContents(outDir); err != nil {
		t.Fatalf("clearDirectoryContents returned error: %v", err)
	}

	info, err := os.Stat(outDir)
	if err != nil {
		t.Fatalf("expected output dir to exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected output path to be a directory")
	}
}

func TestClearDirectoryContents_RemovesExistingContents(t *testing.T) {
	outDir := t.TempDir()
	childDir := filepath.Join(outDir, "nested")
	childFile := filepath.Join(outDir, "page.md")
	childNestedFile := filepath.Join(childDir, "attachment.bin")

	if err := os.MkdirAll(childDir, 0755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(childFile, []byte("content"), 0644); err != nil {
		t.Fatalf("write page file: %v", err)
	}
	if err := os.WriteFile(childNestedFile, []byte("attachment"), 0644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	if err := clearDirectoryContents(outDir); err != nil {
		t.Fatalf("clearDirectoryContents returned error: %v", err)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected output dir to be empty, found %d entries", len(entries))
	}
}

func TestRebuildIncomingLinks_ResetsAndRecomputesDeterministically(t *testing.T) {
	pages := map[string]store.PageRecord{
		"1": {
			ID:            "1",
			IncomingLinks: []string{"stale"},
			OutgoingLinks: []string{"3", "2"},
		},
		"2": {
			ID:            "2",
			IncomingLinks: []string{"stale"},
			OutgoingLinks: []string{"3"},
		},
		"3": {
			ID:            "3",
			IncomingLinks: []string{"stale"},
			OutgoingLinks: []string{},
		},
	}

	rebuildIncomingLinks(pages)

	if !reflect.DeepEqual(pages["1"].IncomingLinks, []string{}) {
		t.Fatalf("expected page 1 incoming links to be empty, got %#v", pages["1"].IncomingLinks)
	}
	if !reflect.DeepEqual(pages["2"].IncomingLinks, []string{"1"}) {
		t.Fatalf("expected page 2 incoming links [1], got %#v", pages["2"].IncomingLinks)
	}
	if !reflect.DeepEqual(pages["3"].IncomingLinks, []string{"1", "2"}) {
		t.Fatalf("expected page 3 incoming links [1 2], got %#v", pages["3"].IncomingLinks)
	}
}

func TestPruneMetadataToCrawledSet_RemovesUnreachableRecords(t *testing.T) {
	pages := map[string]store.PageRecord{
		"1": {ID: "1"},
		"2": {ID: "2"},
	}
	results := map[int64]*crawl.CrawledPage{
		1: {ID: 1},
	}

	pruneMetadataToCrawledSet(pages, results)

	if len(pages) != 1 {
		t.Fatalf("expected 1 page after prune, got %d", len(pages))
	}
	if _, ok := pages["1"]; !ok {
		t.Fatalf("expected page 1 to remain")
	}
	if _, ok := pages["2"]; ok {
		t.Fatalf("expected page 2 to be removed")
	}
}

func TestReconcileManagedArtifacts_DeletesOldMinusNew(t *testing.T) {
	outDir := t.TempDir()

	oldPage := filepath.Join(outDir, "old_1.md")
	oldAttachment := filepath.Join(outDir, "attachments", "1_old.bin")
	keepPage := filepath.Join(outDir, "keep_2.md")
	keepAttachment := filepath.Join(outDir, "attachments", "2_keep.bin")

	if err := os.MkdirAll(filepath.Dir(oldAttachment), 0755); err != nil {
		t.Fatalf("mkdir attachments: %v", err)
	}
	for _, p := range []string{oldPage, oldAttachment, keepPage, keepAttachment} {
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	oldPages := map[string]store.PageRecord{
		"1": {ID: "1", LocalPath: "old_1.md", Attachments: []string{"1_old.bin"}},
		"2": {ID: "2", LocalPath: "keep_2.md", Attachments: []string{"2_keep.bin"}},
	}
	newPages := map[string]store.PageRecord{
		"2": {ID: "2", LocalPath: "keep_2.md", Attachments: []string{"2_keep.bin"}},
	}

	stats, err := reconcileManagedArtifacts(outDir, oldPages, newPages)
	if err != nil {
		t.Fatalf("reconcileManagedArtifacts returned error: %v", err)
	}
	if stats.Deleted != 2 {
		t.Fatalf("expected 2 deleted artifacts, got %d", stats.Deleted)
	}

	if _, err := os.Stat(oldPage); !os.IsNotExist(err) {
		t.Fatalf("expected old page file removed, stat err=%v", err)
	}
	if _, err := os.Stat(oldAttachment); !os.IsNotExist(err) {
		t.Fatalf("expected old attachment removed, stat err=%v", err)
	}
	if _, err := os.Stat(keepPage); err != nil {
		t.Fatalf("expected kept page to exist, err=%v", err)
	}
	if _, err := os.Stat(keepAttachment); err != nil {
		t.Fatalf("expected kept attachment to exist, err=%v", err)
	}
}

func TestReconcileManagedArtifacts_DeletesOldFilenameOnRenameSamePageID(t *testing.T) {
	outDir := t.TempDir()

	oldPath := filepath.Join(outDir, "old-title_123.md")
	newPath := filepath.Join(outDir, "new-title_123.md")
	if err := os.WriteFile(oldPath, []byte("old"), 0644); err != nil {
		t.Fatalf("write old page file: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("new"), 0644); err != nil {
		t.Fatalf("write new page file: %v", err)
	}

	oldPages := map[string]store.PageRecord{
		"123": {ID: "123", LocalPath: "old-title_123.md"},
	}
	newPages := map[string]store.PageRecord{
		"123": {ID: "123", LocalPath: "new-title_123.md"},
	}

	stats, err := reconcileManagedArtifacts(outDir, oldPages, newPages)
	if err != nil {
		t.Fatalf("reconcileManagedArtifacts returned error: %v", err)
	}
	if stats.Deleted != 1 {
		t.Fatalf("expected 1 deleted artifact, got %d", stats.Deleted)
	}

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected old file removed, stat err=%v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected new file kept, err=%v", err)
	}
}

func TestNormalizeManagedPath_RejectsEmpty(t *testing.T) {
	if got := normalizeManagedPath("   "); got != "" {
		t.Fatalf("expected empty normalized path for whitespace input, got %q", got)
	}
}

func TestEnsureLocalPageArtifact_CreatesMissingFile(t *testing.T) {
	outDir := t.TempDir()
	record := store.PageRecord{ID: "123", LocalPath: "page_123.md"}

	created, err := ensureLocalPageArtifact(outDir, record, "# Page")
	if err != nil {
		t.Fatalf("ensureLocalPageArtifact returned error: %v", err)
	}
	if !created {
		t.Fatalf("expected helper to create missing artifact")
	}

	absPath := filepath.Join(outDir, record.LocalPath)
	data, readErr := os.ReadFile(absPath)
	if readErr != nil {
		t.Fatalf("expected created artifact to be readable: %v", readErr)
	}
	if string(data) != "# Page" {
		t.Fatalf("unexpected artifact contents: %q", string(data))
	}
}

func TestEnsureLocalPageArtifact_NoOpWhenFileExists(t *testing.T) {
	outDir := t.TempDir()
	record := store.PageRecord{ID: "123", LocalPath: "page_123.md"}
	absPath := filepath.Join(outDir, record.LocalPath)
	if err := os.WriteFile(absPath, []byte("old"), 0644); err != nil {
		t.Fatalf("seed existing file: %v", err)
	}

	created, err := ensureLocalPageArtifact(outDir, record, "new")
	if err != nil {
		t.Fatalf("ensureLocalPageArtifact returned error: %v", err)
	}
	if created {
		t.Fatalf("expected helper not to recreate existing artifact")
	}

	data, readErr := os.ReadFile(absPath)
	if readErr != nil {
		t.Fatalf("read existing file: %v", readErr)
	}
	if string(data) != "old" {
		t.Fatalf("expected existing artifact to be unchanged, got %q", string(data))
	}
}

func TestEnsureLocalPageArtifact_RejectsMissingLocalPath(t *testing.T) {
	outDir := t.TempDir()
	record := store.PageRecord{ID: "123", LocalPath: "   "}

	_, err := ensureLocalPageArtifact(outDir, record, "# Page")
	if err == nil {
		t.Fatalf("expected error for missing local path")
	}
}

func TestFinalizeRun_PartialErrorsAdvanceCompletedOnly(t *testing.T) {
	outDir := t.TempDir()
	w, err := store.NewWriter(outDir)
	if err != nil {
		t.Fatalf("NewWriter returned error: %v", err)
	}

	oldStart := time.Now().UTC().Add(-2 * time.Hour)
	oldEnd := oldStart.Add(1 * time.Minute)
	if err := w.MarkSuccessfulCheckpoint("full", oldStart, oldEnd); err != nil {
		t.Fatalf("MarkSuccessfulCheckpoint returned error: %v", err)
	}

	rc := &runContext{
		mode:               "updates",
		cfg:                &config.Config{Output: config.OutputConfig{Dir: outDir}},
		writer:             w,
		crawlResults:       map[int64]*crawl.CrawledPage{},
		previousCheckpoint: w.LastSuccessfulCheckpoint(),
		previousPages:      snapshotPageRecords(w.GetPages()),
	}
	metrics := &runMetrics{errorCount: 1}

	result, err := finalizeRun(rc, metrics)
	if err != nil {
		t.Fatalf("finalizeRun returned error: %v", err)
	}
	if result.checkpointAdvanced {
		t.Fatalf("expected successful checkpoint not to advance on partial errors")
	}

	completed := w.LastCompletedCheckpoint()
	if !completed.Present {
		t.Fatalf("expected completed checkpoint to be present")
	}
	if completed.Mode != "updates" {
		t.Fatalf("expected completed checkpoint mode updates, got %q", completed.Mode)
	}

	successful := w.LastSuccessfulCheckpoint()
	if !successful.Present {
		t.Fatalf("expected successful checkpoint to remain present")
	}
	if successful.Mode != "full" {
		t.Fatalf("expected successful checkpoint mode to remain full, got %q", successful.Mode)
	}
	if !successful.StartedAt.Equal(oldStart) || !successful.CompletedAt.Equal(oldEnd) {
		t.Fatalf("expected successful checkpoint tuple unchanged")
	}
}

func TestFinalizeRun_ZeroErrorsAdvanceCompletedAndSuccessful(t *testing.T) {
	outDir := t.TempDir()
	w, err := store.NewWriter(outDir)
	if err != nil {
		t.Fatalf("NewWriter returned error: %v", err)
	}

	rc := &runContext{
		mode:               "full",
		cfg:                &config.Config{Output: config.OutputConfig{Dir: outDir}},
		writer:             w,
		crawlResults:       map[int64]*crawl.CrawledPage{},
		previousCheckpoint: w.LastSuccessfulCheckpoint(),
		previousPages:      snapshotPageRecords(w.GetPages()),
	}
	metrics := &runMetrics{errorCount: 0}

	result, err := finalizeRun(rc, metrics)
	if err != nil {
		t.Fatalf("finalizeRun returned error: %v", err)
	}
	if !result.checkpointAdvanced {
		t.Fatalf("expected successful checkpoint to advance on zero-error run")
	}

	completed := w.LastCompletedCheckpoint()
	if !completed.Present {
		t.Fatalf("expected completed checkpoint to be present")
	}
	if completed.Mode != "full" {
		t.Fatalf("expected completed checkpoint mode full, got %q", completed.Mode)
	}

	successful := w.LastSuccessfulCheckpoint()
	if !successful.Present {
		t.Fatalf("expected successful checkpoint to be present")
	}
	if successful.Mode != "full" {
		t.Fatalf("expected successful checkpoint mode full, got %q", successful.Mode)
	}

	if _, statErr := os.Stat(filepath.Join(outDir, "index.md")); statErr != nil {
		t.Fatalf("expected index.md to be generated, stat err=%v", statErr)
	}
}

func TestFinalizeRun_DryRunSkipsWritesAndCheckpoints(t *testing.T) {
	outDir := t.TempDir()
	w, err := store.NewWriter(outDir)
	if err != nil {
		t.Fatalf("NewWriter returned error: %v", err)
	}

	oldStart := time.Now().UTC().Add(-2 * time.Hour)
	oldEnd := oldStart.Add(1 * time.Minute)
	if err := w.MarkSuccessfulCheckpoint("full", oldStart, oldEnd); err != nil {
		t.Fatalf("MarkSuccessfulCheckpoint returned error: %v", err)
	}

	rc := &runContext{
		mode:               "updates",
		dryRun:             true,
		cfg:                &config.Config{Output: config.OutputConfig{Dir: outDir}},
		writer:             w,
		crawlResults:       map[int64]*crawl.CrawledPage{},
		previousCheckpoint: w.LastSuccessfulCheckpoint(),
		previousPages: map[string]store.PageRecord{
			"123": {ID: "123", LocalPath: "page_123.md"},
		},
	}
	metrics := &runMetrics{errorCount: 0}

	result, err := finalizeRun(rc, metrics)
	if err != nil {
		t.Fatalf("finalizeRun returned error: %v", err)
	}
	if result.checkpointAdvanced {
		t.Fatalf("expected dry-run not to advance successful checkpoint")
	}
	if result.reconcileStats.Deleted != 1 {
		t.Fatalf("expected dry-run to preview 1 stale artifact delete, got %d", result.reconcileStats.Deleted)
	}

	completed := w.LastCompletedCheckpoint()
	if completed.Present {
		t.Fatalf("expected dry-run not to write completed checkpoint")
	}

	successful := w.LastSuccessfulCheckpoint()
	if !successful.Present {
		t.Fatalf("expected successful checkpoint to remain present")
	}
	if successful.Mode != "full" {
		t.Fatalf("expected successful checkpoint mode to remain full, got %q", successful.Mode)
	}
	if !successful.StartedAt.Equal(oldStart) || !successful.CompletedAt.Equal(oldEnd) {
		t.Fatalf("expected successful checkpoint tuple unchanged")
	}

	if _, statErr := os.Stat(filepath.Join(outDir, "index.md")); !os.IsNotExist(statErr) {
		t.Fatalf("expected dry-run not to generate index.md, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(outDir, "metadata.json")); !os.IsNotExist(statErr) {
		t.Fatalf("expected dry-run not to save metadata.json, stat err=%v", statErr)
	}
}

func TestWriteStartIndex_IncludesSummaryAndSeedLinks(t *testing.T) {
	outDir := t.TempDir()
	w, err := store.NewWriter(outDir)
	if err != nil {
		t.Fatalf("NewWriter returned error: %v", err)
	}

	start := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Minute)
	if err := w.MarkCompletedCheckpoint("updates", start, end); err != nil {
		t.Fatalf("MarkCompletedCheckpoint returned error: %v", err)
	}
	if err := w.MarkSuccessfulCheckpoint("updates", start, end); err != nil {
		t.Fatalf("MarkSuccessfulCheckpoint returned error: %v", err)
	}

	w.SetSeedPageIDs([]string{"100", "999"})
	w.AddPageMetadata("100", store.PageRecord{
		ID:           "100",
		Title:        "Decision Records",
		LocalPath:    "decision-records_100.md",
		SourceURL:    "https://example/wiki/pages/viewpage.action?pageId=100",
		CanonicalURL: "https://example/wiki/pages/viewpage.action?pageId=100",
		SpaceKey:     "SFD",
		CrawledAt:    start,
	})

	if err := writeStartIndex(outDir, w); err != nil {
		t.Fatalf("writeStartIndex returned error: %v", err)
	}

	indexBytes, err := os.ReadFile(filepath.Join(outDir, "index.md"))
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	index := string(indexBytes)

	wants := []string{
		"# Start Here",
		"## Crawl Summary",
		"mode=updates, started=2026-05-23T12:00:00Z, completed=2026-05-23T12:02:00Z",
		"## Seed Pages",
		"- [Decision Records](decision-records_100.md) - source: <https://example/wiki/pages/viewpage.action?pageId=100>",
		"- Page 999 (not present in current crawl output)",
		"## Metadata",
		"[metadata.json](metadata.json)",
	}

	for _, want := range wants {
		if !strings.Contains(index, want) {
			t.Fatalf("expected index to contain %q, got:\n%s", want, index)
		}
	}
}

func TestRootCommand_HasDryRunFlagWithDefaultFalse(t *testing.T) {
	flag := rootCmd.Flags().Lookup("dry-run")
	if flag == nil {
		t.Fatalf("expected dry-run flag to be registered")
	}

	if flag.DefValue != "false" {
		t.Fatalf("expected dry-run default to be false, got %q", flag.DefValue)
	}
}

func TestProcessReusedPage_DryRunSkipsArtifactMaterialization(t *testing.T) {
	outDir := t.TempDir()
	w, err := store.NewWriter(outDir)
	if err != nil {
		t.Fatalf("NewWriter returned error: %v", err)
	}
	w.SetSeedPageIDs([]string{"123"})

	previous := store.PageRecord{
		ID:            "123",
		Title:         "Decision Records",
		LocalPath:     "decision-records_123.md",
		OutgoingLinks: []string{"555"},
	}
	previousPages := map[string]store.PageRecord{"123": previous}

	rc := &runContext{
		dryRun:              true,
		cfg:                 &config.Config{Output: config.OutputConfig{Dir: outDir}},
		writer:              w,
		previousPages:       previousPages,
		oldManagedArtifacts: managedArtifactSet(previousPages),
	}
	metrics := &runMetrics{}
	crawledPage := &crawl.CrawledPage{
		ID:           123,
		Title:        "Decision Records",
		Depth:        1,
		OutgoingLinks: []int64{555},
	}

	if err := processReusedPage(rc, metrics, 123, crawledPage); err != nil {
		t.Fatalf("processReusedPage returned error: %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(outDir, previous.LocalPath)); !os.IsNotExist(statErr) {
		t.Fatalf("expected dry-run to skip reused-page artifact write, stat err=%v", statErr)
	}
	if got := metrics.fileAddedCount; got != 1 {
		t.Fatalf("expected dry-run to predict one added reused artifact, got %d", got)
	}
	if got := metrics.reusedCount; got != 1 {
		t.Fatalf("expected reused count 1, got %d", got)
	}
	if got := metrics.successCount; got != 1 {
		t.Fatalf("expected success count 1, got %d", got)
	}
}

func TestProcessRerenderedPage_DryRunSkipsPageAndAttachmentWrites(t *testing.T) {
	outDir := t.TempDir()
	w, err := store.NewWriter(outDir)
	if err != nil {
		t.Fatalf("NewWriter returned error: %v", err)
	}

	rc := &runContext{
		dryRun: true,
		cfg: &config.Config{
			Confluence: config.ConfluenceConfig{Username: "user", Token: "token"},
			Crawl:      config.CrawlConfig{Seeds: []string{"https://example.atlassian.net/wiki/spaces/SPACE/pages/123/Title"}},
			Output:     config.OutputConfig{Dir: outDir},
			Attachments: config.AttachmentsConfig{
				Download:  true,
				MaxSizeMB: 10,
			},
			Retry: config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1000},
		},
		writer:              w,
		oldManagedArtifacts: map[string]struct{}{},
	}
	metrics := &runMetrics{}
	crawledPage := &crawl.CrawledPage{
		ID:               123,
		Title:            "Rendering Candidate",
		Version:          2,
		SourceURL:        "https://example.atlassian.net/wiki/pages/viewpage.action?pageId=123",
		CanonicalURL:     "https://example.atlassian.net/wiki/pages/viewpage.action?pageId=123",
		SpaceKey:         "SPACE",
		Depth:            0,
		CrawledAt:        time.Now().UTC(),
		OutgoingLinks:    []int64{456},
		CommentCount:     1,
		Comments:         []confluence.CommentData{{ID: "c1", Body: "{\"type\":\"doc\",\"content\":[]}"}},
		Attachments:      []confluence.AttachmentData{{ID: "a1", Filename: "diagram.png", FileID: "fid-1", FileSizeBytes: 1024}},
		CreatedByID:      "u1",
		LastModifiedByID: "u2",
	}

	if err := processRerenderedPage(context.Background(), rc, metrics, 123, crawledPage); err != nil {
		t.Fatalf("processRerenderedPage returned error: %v", err)
	}

	entries, readErr := os.ReadDir(outDir)
	if readErr != nil {
		t.Fatalf("ReadDir returned error: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected dry-run to avoid filesystem writes, found %d entries", len(entries))
	}

	if got := metrics.successCount; got != 1 {
		t.Fatalf("expected success count 1, got %d", got)
	}
	if got := metrics.rerenderedCount; got != 1 {
		t.Fatalf("expected rerendered count 1, got %d", got)
	}
	if got := metrics.attachmentsDownloaded; got != 1 {
		t.Fatalf("expected one predicted attachment download, got %d", got)
	}
	if got := metrics.fileAddedCount; got != 2 {
		t.Fatalf("expected predicted added files count 2 (page + attachment), got %d", got)
	}
}

func TestShouldPrepareOutputDirectory(t *testing.T) {
	if !shouldPrepareOutputDirectory("full", false) {
		t.Fatalf("expected full non-dry-run to prepare output directory")
	}
	if shouldPrepareOutputDirectory("full", true) {
		t.Fatalf("expected full dry-run not to prepare output directory")
	}
	if shouldPrepareOutputDirectory("updates", false) {
		t.Fatalf("expected updates mode not to prepare output directory")
	}
}
