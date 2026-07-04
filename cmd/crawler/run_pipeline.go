package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gkoos/confluence2md/internal/config"
	confluenceclient "github.com/gkoos/confluence2md/internal/confluence"
	"github.com/gkoos/confluence2md/internal/convert"
	"github.com/gkoos/confluence2md/internal/crawl"
	"github.com/gkoos/confluence2md/internal/links"
	"github.com/gkoos/confluence2md/internal/store"
)

type runContext struct {
	mode                string
	dryRun              bool
	cfg                 *config.Config
	client              *confluenceclient.Client
	writer              *store.Writer
	crawler             *crawl.CrawlSession
	seedPageIDs         []int64
	spaceKey            string
	crawlResults        map[int64]*crawl.CrawledPage
	previousCheckpoint  store.CheckpointSnapshot
	previousPages       map[string]store.PageRecord
	oldManagedArtifacts map[string]struct{}
}

type runMetrics struct {
	successCount          int
	errorCount            int
	pagesWithComments     int
	totalCommentsFetched  int
	commentFetchFailures  int
	reusedCount           int
	rerenderedCount       int
	fileAddedCount        int
	fileUpdatedCount      int
	attachmentsDownloaded int
	attachmentsReused     int
}

type runFinalizeResult struct {
	rewriteStats       links.RewriteStats
	reconcileStats     artifactReconcileStats
	checkpointAdvanced bool
}

func bootstrapRun(mode, cfgFile string, dryRun bool) (*runContext, error) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("config error: %w", err)
	}

	if mode != "full" && mode != "updates" {
		return nil, fmt.Errorf("--mode must be 'full' or 'updates'")
	}

	printConfigSummary(cfg)

	client, err := newConfluenceClient(cfg)
	if err != nil {
		return nil, err
	}

	if err := verifyConfluenceAccess(client); err != nil {
		return nil, err
	}

	if shouldPrepareOutputDirectory(mode, dryRun) {
		if err := clearDirectoryContents(cfg.Output.Dir); err != nil {
			return nil, fmt.Errorf("prepare output directory for full crawl: %w", err)
		}
	}

	writer, err := store.NewWriter(cfg.Output.Dir)
	if err != nil {
		return nil, fmt.Errorf("initialize output writer: %w", err)
	}

	previousPages := snapshotPageRecords(writer.GetPages())
	rc := &runContext{
		mode:                mode,
		dryRun:              dryRun,
		cfg:                 cfg,
		client:              client,
		writer:              writer,
		previousCheckpoint:  writer.LastSuccessfulCheckpoint(),
		previousPages:       previousPages,
		oldManagedArtifacts: managedArtifactSet(previousPages),
	}

	rc.seedPageIDs, err = extractSeedPageIDs(client, cfg.Crawl.Seeds)
	if err != nil {
		return nil, fmt.Errorf("extract seed page IDs: %w", err)
	}
	rc.writer.SetSeedPageIDs(int64SliceToStringIDs(rc.seedPageIDs))

	fmt.Printf("\nStarting BFS crawl: %d seed(s), max depth %d, concurrency %d, rate %d rpm\n",
		len(rc.seedPageIDs), cfg.Crawl.MaxDepth, cfg.Crawl.Concurrency, cfg.Crawl.RateLimitRPM)
	fmt.Printf("  [Dx] fetched/visited  page-id — title  (+N links, queue:M)\n\n")

	for _, seed := range cfg.Crawl.Seeds {
		if s := extractSpaceKeyFromSeed(seed); s != "" {
			rc.spaceKey = s
			break
		}
	}

	rc.crawler = crawl.NewCrawlSession(client, cfg, rc.spaceKey)
	rc.crawler.SetDryRun(dryRun)
	if rc.mode == "updates" {
		rc.crawler.EnableUpdatesMode(rc.previousPages)
	}

	return rc, nil
}

func shouldPrepareOutputDirectory(mode string, dryRun bool) bool {
	return mode == "full" && !dryRun
}

func executeTraversal(ctx context.Context, rc *runContext) error {
	crawlResults, err := rc.crawler.Run(ctx, rc.seedPageIDs)
	if err != nil {
		return fmt.Errorf("crawl failed: %w", err)
	}
	rc.crawlResults = crawlResults

	if rc.dryRun {
		fmt.Printf("\nProcessing %d crawled pages (dry-run, no writes)...\n", len(crawlResults))
	} else {
		fmt.Printf("\nProcessing and writing %d crawled pages...\n", len(crawlResults))
	}
	return nil
}

func processTraversalResults(ctx context.Context, rc *runContext, metrics *runMetrics) error {
	for pageID, crawledPage := range rc.crawlResults {
		if rc.mode == "updates" && crawledPage.Reused {
			if err := processReusedPage(rc, metrics, pageID, crawledPage); err != nil {
				metrics.errorCount++
				continue
			}
			continue
		}

		if err := processRerenderedPage(ctx, rc, metrics, pageID, crawledPage); err != nil {
			metrics.errorCount++
		}
	}

	return nil
}

func processReusedPage(rc *runContext, metrics *runMetrics, pageID int64, crawledPage *crawl.CrawledPage) error {
	pageIDStr := strconv.FormatInt(pageID, 10)
	previous, ok := rc.previousPages[pageIDStr]
	if !ok {
		logPageWithLevel("ERR", pageID, "reused page missing from previous metadata")
		return fmt.Errorf("reused page missing from previous metadata")
	}
	if rc.dryRun {
		if reusedPageArtifactMissing(rc.cfg.Output.Dir, previous) {
			metrics.fileAddedCount++
		}
	} else {
		materializedMarkdown := store.ComposeMarkdownWithFrontMatter(pageIDStr, previous, rc.writer.GetSeedPageIDs(), crawledPage.Markdown)
		materialized, materializeErr := ensureLocalPageArtifact(rc.cfg.Output.Dir, previous, materializedMarkdown)
		if materializeErr != nil {
			logPageWithLevel("ERR", pageID, "reused page artifact check failed: %v", materializeErr)
			return materializeErr
		}
		if materialized {
			metrics.fileAddedCount++
		}
	}

	record := previous
	record.Depth = crawledPage.Depth
	record.OutgoingLinks = int64SliceToStringIDs(crawledPage.OutgoingLinks)
	record.IncomingLinks = []string{}
	if strings.TrimSpace(crawledPage.AttachmentSignature) != "" {
		record.AttachmentSignature = crawledPage.AttachmentSignature
	}

	rc.writer.AddPageMetadata(pageIDStr, record)
	if rc.dryRun {
		logPageWithLevel("OK", pageID, "%s (reused, dry-run)", record.Title)
	} else {
		logPageWithLevel("OK", pageID, "%s (reused)", record.Title)
	}
	metrics.successCount++
	metrics.reusedCount++
	for _, filename := range record.Attachments {
		if strings.TrimSpace(filename) == "" {
			continue
		}
		attachmentPath := filepath.Join(rc.cfg.Output.Dir, "attachments", filename)
		if _, statErr := os.Stat(attachmentPath); statErr == nil {
			metrics.attachmentsReused++
		}
	}

	return nil
}

func processRerenderedPage(ctx context.Context, rc *runContext, metrics *runMetrics, pageID int64, crawledPage *crawl.CrawledPage) error {
	if crawledPage.FetchError != "" {
		logPageWithLevel("ERR", pageID, "%s", crawledPage.FetchError)
		return fmt.Errorf("fetch error")
	}

	pageIDStr := strconv.FormatInt(pageID, 10)
	markdown := crawledPage.Markdown

	if !rc.dryRun {
		var err error
		markdown, err = absolutizeConfluenceLinks(markdown, rc.cfg.BaseURL())
		if err != nil {
			logPageWithLevel("ERR", pageID, "absolutize links failed: %v", err)
			return err
		}

		markdown, err = enrichURLOnlyLinkLabels(markdown, rc.client)
		if err != nil {
			logPageWithLevel("WARN", pageID, "enrich links failed: %v", err)
		}
	}

	if crawledPage.CommentFetchError != "" {
		logPageWithLevel("WARN", pageID, "%s", crawledPage.CommentFetchError)
		metrics.commentFetchFailures++
	}

	var savedAttachments []string
	if rc.cfg.Attachments.Download && len(crawledPage.Attachments) > 0 {
		if crawledPage.AttachmentFetchError != "" {
			logPageWithLevel("WARN", pageID, "%s", crawledPage.AttachmentFetchError)
		}
		var results []store.AttachmentResult
		if rc.dryRun {
			results = previewPageAttachments(pageIDStr, crawledPage.Attachments, rc.cfg.Attachments.MaxSizeMB)
		} else {
			results = store.DownloadPageAttachments(ctx, rc.cfg.Output.Dir, pageIDStr, crawledPage.Attachments, rc.cfg.Attachments.MaxSizeMB, rc.client)
		}
		for _, r := range results {
			if r.Error != nil {
				logPageAttachmentWarning(pageID, r.Error)
			} else if r.Filename != "" {
				savedAttachments = append(savedAttachments, r.Filename)
				metrics.attachmentsDownloaded++
				artifactPath := filepath.ToSlash(filepath.Join("attachments", r.Filename))
				if _, existed := rc.oldManagedArtifacts[artifactPath]; existed {
					metrics.fileUpdatedCount++
				} else {
					metrics.fileAddedCount++
				}
			}
		}
		if !rc.dryRun {
			markdown = rewriteAttachmentLinks(markdown, results)
		}
	}

	if crawledPage.CommentCount > 0 {
		metrics.pagesWithComments++
		metrics.totalCommentsFetched += crawledPage.CommentCount
	}

	if !rc.dryRun {
		commentsMD := convert.CommentsToMarkdown(crawledPage.Comments)
		if commentsMD != "" {
			markdown = strings.TrimRight(markdown, "\n") + "\n\n" + commentsMD + "\n"
		}
	}

	record := store.PageRecord{
		ID:                  pageIDStr,
		Title:               crawledPage.Title,
		Version:             crawledPage.Version,
		CrawledAt:           crawledPage.CrawledAt,
		CommentCount:        crawledPage.CommentCount,
		CommentsLastFetched: crawledPage.CrawledAt,
		CommentsFetchError:  crawledPage.CommentFetchError,
		SourceURL:           crawledPage.SourceURL,
		CanonicalURL:        crawledPage.CanonicalURL,
		SpaceKey:            crawledPage.SpaceKey,
		Depth:               crawledPage.Depth,
		OutgoingLinks:       int64SliceToStringIDs(crawledPage.OutgoingLinks),
		IncomingLinks:       []string{},
		Attachments:         savedAttachments,
		AttachmentSignature: crawledPage.AttachmentSignature,
		StorageFormat:       markdown,
		CreatedByID:         crawledPage.CreatedByID,
		CreatedByName:       crawledPage.CreatedByName,
		LastModifiedByID:    crawledPage.LastModifiedByID,
		LastModifiedByName:  crawledPage.LastModifiedByName,
	}

	// Parse temporal metadata
	if crawledPage.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339, crawledPage.CreatedAt); err == nil {
			record.CreatedAt = t
		}
	}
	if crawledPage.LastModifiedAt != "" {
		if t, err := time.Parse(time.RFC3339, crawledPage.LastModifiedAt); err == nil {
			record.LastModifiedAt = t
		}
	}
	if crawledPage.ParentID != nil {
		record.ConfluenceParentID = crawledPage.ParentID
	}

	if rc.dryRun {
		rc.writer.AddPageMetadata(pageIDStr, record)
	} else {
		if err := rc.writer.AddPage(pageIDStr, record); err != nil {
			logPageWithLevel("ERR", pageID, "write failed: %v", err)
			return err
		}
	}

	storedRecord, ok := rc.writer.GetPages()[pageIDStr]
	if ok {
		artifactPath := normalizeManagedPath(storedRecord.LocalPath)
		if _, existed := rc.oldManagedArtifacts[artifactPath]; existed {
			metrics.fileUpdatedCount++
		} else {
			metrics.fileAddedCount++
		}
	}
	metrics.rerenderedCount++

	if rc.dryRun {
		logPageWithLevel("OK", pageID, "%s (dry-run)", crawledPage.Title)
	} else {
		logPageWithLevel("OK", pageID, "%s", crawledPage.Title)
	}
	metrics.successCount++
	return nil
}

func reusedPageArtifactMissing(outputDir string, record store.PageRecord) bool {
	localPath := strings.TrimSpace(record.LocalPath)
	if localPath == "" {
		return true
	}

	absPath := filepath.Join(outputDir, filepath.FromSlash(localPath))
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return true
	}

	return false
}

func previewPageAttachments(pageID string, attachments []confluenceclient.AttachmentData, maxSizeMB int) []store.AttachmentResult {
	if len(attachments) == 0 {
		return nil
	}

	maxBytes := int64(maxSizeMB) * 1024 * 1024
	results := make([]store.AttachmentResult, 0, len(attachments))
	for _, a := range attachments {
		result := store.AttachmentResult{OriginalName: a.Filename}
		if maxBytes > 0 && a.FileSizeBytes > maxBytes {
			result.Skipped = true
			result.Error = fmt.Errorf("attachment %q skipped: size %d bytes exceeds limit of %d bytes", a.Filename, a.FileSizeBytes, maxBytes)
			results = append(results, result)
			continue
		}
		if strings.TrimSpace(a.ID) == "" {
			result.Error = fmt.Errorf("attachment %q has no attachment ID", a.Filename)
			results = append(results, result)
			continue
		}

		result.Filename = store.PageAttachmentFilename(pageID, a.Filename)
		result.FileID = a.FileID
		results = append(results, result)
	}

	return results
}

func logPageWithLevel(level string, pageID int64, messageFormat string, args ...any) {
	params := append([]any{level, pageID}, args...)
	fmt.Printf("  [%s] Page %d: "+messageFormat+"\n", params...)
}

func logPageAttachmentWarning(pageID int64, err error) {
	fmt.Printf("  [WARN] Page %d attachment: %v\n", pageID, err)
}

func finalizeRun(rc *runContext, metrics *runMetrics) (*runFinalizeResult, error) {
	if rc.dryRun {
		pagesPreview := snapshotPageRecords(rc.writer.GetPages())
		pruneMetadataToCrawledSet(pagesPreview, rc.crawlResults)
		rebuildIncomingLinks(pagesPreview)

		reconcileStats := previewManagedArtifactReconcile(rc.previousPages, pagesPreview)
		return &runFinalizeResult{
			rewriteStats:       links.RewriteStats{},
			reconcileStats:     reconcileStats,
			checkpointAdvanced: false,
		}, nil
	}

	pruneMetadataToCrawledSet(rc.writer.GetPages(), rc.crawlResults)

	rewriteStats, err := finalizeTraversalOutput(rc.cfg.Output.Dir, rc.writer)
	if err != nil {
		return nil, fmt.Errorf("finalize traversal output: %w", err)
	}

	reconcileStats, err := reconcileManagedArtifacts(rc.cfg.Output.Dir, rc.previousPages, rc.writer.GetPages())
	if err != nil {
		return nil, fmt.Errorf("reconcile managed artifacts: %w", err)
	}

	checkpointCompletedAt := time.Now().UTC()
	if err := rc.writer.MarkCompletedCheckpoint(rc.mode, rc.writer.CrawlStartedAt(), checkpointCompletedAt); err != nil {
		return nil, fmt.Errorf("set completed crawl checkpoint: %w", err)
	}

	successfulAdvanced := false
	if metrics.errorCount == 0 {
		if err := rc.writer.MarkSuccessfulCheckpoint(rc.mode, rc.writer.CrawlStartedAt(), checkpointCompletedAt); err != nil {
			return nil, fmt.Errorf("set successful crawl checkpoint: %w", err)
		}
		successfulAdvanced = !rc.previousCheckpoint.Present ||
			rc.previousCheckpoint.Mode != rc.mode ||
			!rc.previousCheckpoint.StartedAt.Equal(rc.writer.CrawlStartedAt()) ||
			!rc.previousCheckpoint.CompletedAt.Equal(checkpointCompletedAt)
	}

	if err := rc.writer.SaveMetadata(); err != nil {
		return nil, fmt.Errorf("save metadata: %w", err)
	}

	if err := writeStartIndex(rc.cfg.Output.Dir, rc.writer); err != nil {
		return nil, fmt.Errorf("write start index: %w", err)
	}

	return &runFinalizeResult{
		rewriteStats:       rewriteStats,
		reconcileStats:     reconcileStats,
		checkpointAdvanced: successfulAdvanced,
	}, nil
}

func printRunSummary(rc *runContext, metrics *runMetrics, finalizeResult *runFinalizeResult, elapsed time.Duration) {
	stats := rc.crawler.Stats()
	fmt.Printf("\n=== Crawl Complete ===\n")
	if rc.dryRun {
		fmt.Printf("Mode: %s (dry-run)\n", rc.mode)
		fmt.Printf("Dry-run: no output artifacts, metadata, or checkpoints were written\n")
	} else {
		fmt.Printf("Mode: %s\n", rc.mode)
	}
	fmt.Printf("Total pages crawled: %d\n", stats["total_pages"])
	if depthDist, ok := stats["depth_distribution"].(map[int]int); ok {
		for depth := 0; depth <= rc.cfg.Crawl.MaxDepth; depth++ {
			if count, exists := depthDist[depth]; exists && count > 0 {
				fmt.Printf("  Depth %d: %d pages\n", depth, count)
			}
		}
	}
	if rc.dryRun {
		fmt.Printf("Pages processed successfully (predicted): %d\n", metrics.successCount)
	} else {
		fmt.Printf("Pages written successfully: %d\n", metrics.successCount)
	}
	fmt.Printf("Pages with errors: %d\n", metrics.errorCount)
	fmt.Printf("Internal crawl links discovered (edge count): %d\n", stats["total_links"])
	fmt.Printf("Unique internal target pages linked: %d\n", stats["unique_internal_targets"])
	fmt.Printf("External links skipped (host filter): %d\n", stats["external_links_skipped"])
	if rc.dryRun {
		fmt.Printf("Link rewrite pass: skipped (dry-run)\n")
	} else {
		fmt.Printf("Pages with rewritten links: %d/%d\n", finalizeResult.rewriteStats.PagesUpdated, finalizeResult.rewriteStats.PagesProcessed)
		fmt.Printf("Markdown links rewritten to local paths: %d/%d\n", finalizeResult.rewriteStats.LinksRewritten, finalizeResult.rewriteStats.LinksSeen)
	}
	fmt.Printf("Pages with comments appended: %d\n", metrics.pagesWithComments)
	fmt.Printf("Total comments fetched: %d\n", metrics.totalCommentsFetched)
	fmt.Printf("Pages with comment fetch warnings: %d\n", metrics.commentFetchFailures)
	if rc.mode == "updates" {
		reachablePages := len(rc.crawlResults)
		renderCandidates := metrics.rerenderedCount + metrics.reusedCount
		rerenderSavedCount := metrics.reusedCount
		rerenderSavedPercent := 0.0
		if renderCandidates > 0 {
			rerenderSavedPercent = (float64(rerenderSavedCount) / float64(renderCandidates)) * 100
		}

		fmt.Printf("Reachable pages: %d\n", reachablePages)
		fmt.Printf("Pages re-rendered: %d\n", metrics.rerenderedCount)
		fmt.Printf("Pages reused without full re-processing: %d\n", metrics.reusedCount)
		fmt.Printf("Re-render saves: %d (%.1f%%)\n", rerenderSavedCount, rerenderSavedPercent)
		if rc.dryRun {
			fmt.Printf("Managed files that would be added/updated/deleted: %d/%d/%d\n", metrics.fileAddedCount, metrics.fileUpdatedCount, finalizeResult.reconcileStats.Deleted)
		} else {
			fmt.Printf("Managed files added/updated/deleted: %d/%d/%d\n", metrics.fileAddedCount, metrics.fileUpdatedCount, finalizeResult.reconcileStats.Deleted)
		}
		fmt.Printf("Attachments downloaded/reused: %d/%d\n", metrics.attachmentsDownloaded, metrics.attachmentsReused)
		if rc.dryRun {
			fmt.Printf("Output commit status: dry-run (no writes)\n")
			fmt.Printf("Checkpoint advanced: no (dry-run suppresses checkpoint writes)\n")
		} else {
			fmt.Printf("Output commit status: direct-write (non-transactional)\n")
			if finalizeResult.checkpointAdvanced {
				fmt.Printf("Checkpoint advanced: yes\n")
			} else {
				fmt.Printf("Checkpoint advanced: no\n")
			}
		}
	}
	if rc.dryRun {
		fmt.Printf("Managed artifacts that would be deleted as stale: %d\n", finalizeResult.reconcileStats.Deleted)
	} else {
		fmt.Printf("Managed artifacts deleted as stale: %d\n", finalizeResult.reconcileStats.Deleted)
	}
	fmt.Printf("Output directory: %s\n", rc.cfg.Output.Dir)
	fmt.Printf("Total time: %s\n", elapsed.Round(time.Second))
}
