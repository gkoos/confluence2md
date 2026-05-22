package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gkoos/confluence2md/internal/crawl"
	"github.com/gkoos/confluence2md/internal/links"
	"github.com/gkoos/confluence2md/internal/store"
)

func rebuildIncomingLinks(pagesByID map[string]store.PageRecord) {
	for pageID, record := range pagesByID {
		record.IncomingLinks = record.IncomingLinks[:0]
		pagesByID[pageID] = record
	}

	pageIDs := make([]string, 0, len(pagesByID))
	for pageID := range pagesByID {
		pageIDs = append(pageIDs, pageID)
	}
	sort.Strings(pageIDs)

	// Build reverse index: for each page, add it to incoming_links of pages it's referenced from
	for _, pageID := range pageIDs {
		record := pagesByID[pageID]
		outgoing := append([]string(nil), record.OutgoingLinks...)
		sort.Strings(outgoing)
		for _, outgoingLink := range outgoing {
			if targetRecord, exists := pagesByID[outgoingLink]; exists {
				// Add pageID to targetRecord's incoming links
				targetRecord.IncomingLinks = append(targetRecord.IncomingLinks, pageID)
				pagesByID[outgoingLink] = targetRecord
			}
		}
	}
}

func finalizeTraversalOutput(outputDir string, w *store.Writer) (links.RewriteStats, error) {
	pagesByID := w.GetPages()
	rebuildIncomingLinks(pagesByID)

	stats, err := links.RewriteCrawledPageLinks(outputDir, pagesByID)
	if err != nil {
		return stats, err
	}

	return stats, nil
}

type artifactReconcileStats struct {
	Deleted int
}

func pruneMetadataToCrawledSet(pages map[string]store.PageRecord, crawlResults map[int64]*crawl.CrawledPage) {
	if len(pages) == 0 {
		return
	}
	reachable := make(map[string]struct{}, len(crawlResults))
	for pageID := range crawlResults {
		reachable[strconv.FormatInt(pageID, 10)] = struct{}{}
	}
	for pageID := range pages {
		if _, ok := reachable[pageID]; !ok {
			delete(pages, pageID)
		}
	}
}

func reconcileManagedArtifacts(outputDir string, oldPages, newPages map[string]store.PageRecord) (artifactReconcileStats, error) {
	stats := artifactReconcileStats{}
	oldSet := managedArtifactSet(oldPages)
	newSet := managedArtifactSet(newPages)

	for relPath := range oldSet {
		if _, keep := newSet[relPath]; keep {
			continue
		}
		absPath := filepath.Join(outputDir, filepath.FromSlash(relPath))
		if err := os.Remove(absPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return stats, fmt.Errorf("delete stale artifact %s: %w", absPath, err)
		}
		stats.Deleted++
	}

	return stats, nil
}

func managedArtifactSet(pages map[string]store.PageRecord) map[string]struct{} {
	artifacts := make(map[string]struct{})
	for _, record := range pages {
		if localPath := normalizeManagedPath(record.LocalPath); localPath != "" {
			artifacts[localPath] = struct{}{}
		}
		for _, attachment := range record.Attachments {
			if filename := strings.TrimSpace(attachment); filename != "" {
				artifacts[filepath.ToSlash(filepath.Join("attachments", filename))] = struct{}{}
			}
		}
	}
	return artifacts
}

func normalizeManagedPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(path))
}
