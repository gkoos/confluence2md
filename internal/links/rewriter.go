package links

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gkoos/confluence2md/internal/store"
)

var markdownLinkTargetPattern = regexp.MustCompile(`\[[^\]]+\]\(([^)\s]+)\)`)

// RewriteStats captures pass-2 local-link rewrite metrics.
type RewriteStats struct {
	PagesProcessed int
	PagesUpdated   int
	LinksSeen      int
	LinksRewritten int
}

// RewriteCrawledPageLinks rewrites links to crawled Confluence pages into relative
// local Markdown paths, leaving unresolved/external links unchanged.
func RewriteCrawledPageLinks(outputDir string, pages map[string]store.PageRecord) (RewriteStats, error) {
	stats := RewriteStats{}

	idToLocal := make(map[string]string, len(pages))
	for pageID, record := range pages {
		if strings.TrimSpace(record.LocalPath) != "" {
			idToLocal[pageID] = record.LocalPath
		}
	}

	for pageID, record := range pages {
		if strings.TrimSpace(record.LocalPath) == "" {
			continue
		}

		srcPath := filepath.Join(outputDir, record.LocalPath)
		srcDir := filepath.Dir(srcPath)

		contentBytes, err := os.ReadFile(srcPath)
		if err != nil {
			return stats, fmt.Errorf("read page %s: %w", srcPath, err)
		}
		content := string(contentBytes)

		stats.PagesProcessed++
		matches := markdownLinkTargetPattern.FindAllStringSubmatch(content, -1)
		stats.LinksSeen += len(matches)

		pageRewrites := 0
		rewritten := markdownLinkTargetPattern.ReplaceAllStringFunc(content, func(match string) string {
			sub := markdownLinkTargetPattern.FindStringSubmatch(match)
			if len(sub) < 2 {
				return match
			}
			target := strings.TrimSpace(sub[1])
			targetID := ExtractPageIDFromURL(target)
			if targetID <= 0 {
				return match
			}

			targetLocal, ok := idToLocal[strconv.FormatInt(targetID, 10)]
			if !ok {
				return match
			}

			targetPath := filepath.Join(outputDir, targetLocal)
			relPath, err := filepath.Rel(srcDir, targetPath)
			if err != nil {
				return match
			}

			relPath = filepath.ToSlash(relPath)
			if fragment := extractFragment(target); fragment != "" {
				relPath += "#" + fragment
			}

			if relPath == "" || target == relPath {
				return match
			}

			pageRewrites++
			stats.LinksRewritten++
			return strings.Replace(match, "("+target+")", "("+relPath+")", 1)
		})

		if pageRewrites == 0 {
			continue
		}

		if err := os.WriteFile(srcPath, []byte(rewritten), 0644); err != nil {
			return stats, fmt.Errorf("write rewritten page %s: %w", srcPath, err)
		}

		record.StorageFormat = rewritten
		pages[pageID] = record
		stats.PagesUpdated++
	}

	return stats, nil
}

func extractFragment(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return u.Fragment
}
