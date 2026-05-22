package main

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	confluenceclient "github.com/gkoos/confluence2md/internal/confluence"
	"github.com/gkoos/confluence2md/internal/store"
)

var (
	urlOnlyMarkdownLinkPattern = regexp.MustCompile(`\[(https?://[^\]\s]+)\]\((https?://[^)\s]+)\)`)
	pageIDFromURLPattern       = regexp.MustCompile(`/pages/(\d+)`)
	relativeRootLinkPattern    = regexp.MustCompile(`\]\((/[^)\s]+)\)`)
	searchLinkPattern          = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^)\s]+/wiki/search\?text=[^)\s]+)\)`)
	attachmentLinkPattern      = regexp.MustCompile(`\]\(attachment://([^)]+)\)`)
)

func enrichURLOnlyLinkLabels(markdown string, client *confluenceclient.Client) (string, error) {
	titleCache := make(map[string]string)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	out := urlOnlyMarkdownLinkPattern.ReplaceAllStringFunc(markdown, func(match string) string {
		submatches := urlOnlyMarkdownLinkPattern.FindStringSubmatch(match)
		if len(submatches) != 3 {
			return match
		}

		label := submatches[1]
		target := submatches[2]
		if label != target {
			return match
		}

		idMatch := pageIDFromURLPattern.FindStringSubmatch(target)
		if len(idMatch) != 2 {
			return match
		}

		pageIDText := idMatch[1]
		if cached, ok := titleCache[pageIDText]; ok {
			return "[" + cached + "](" + target + ")"
		}

		pageID, err := strconv.Atoi(pageIDText)
		if err != nil {
			return match
		}

		title, err := client.GetPageTitleByID(ctx, pageID)
		if err != nil {
			return match
		}

		title = strings.TrimSpace(title)
		if title == "" {
			return match
		}

		titleCache[pageIDText] = title
		return "[" + title + "](" + target + ")"
	})

	return out, nil
}

func absolutizeConfluenceLinks(markdown, baseURL string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base URL %q: %w", baseURL, err)
	}

	origin := parsed.Scheme + "://" + parsed.Host
	out := relativeRootLinkPattern.ReplaceAllString(markdown, "]("+origin+"$1)")

	return out, nil
}

func resolveSearchLinksToPageURLs(markdown string, client *confluenceclient.Client, spaceKey string) (string, error) {
	resolved := make(map[string]string)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	out := searchLinkPattern.ReplaceAllStringFunc(markdown, func(match string) string {
		submatches := searchLinkPattern.FindStringSubmatch(match)
		if len(submatches) != 3 {
			return match
		}

		label := strings.TrimSpace(submatches[1])
		target := strings.TrimSpace(submatches[2])
		if label == "" || target == "" {
			return match
		}

		cacheKey := spaceKey + "|" + label
		if cached, ok := resolved[cacheKey]; ok {
			return "[" + label + "](" + cached + ")"
		}

		pageURL, err := client.ResolvePageURLByTitle(ctx, label, spaceKey)
		if err != nil {
			return match
		}

		resolved[cacheKey] = pageURL
		return "[" + label + "](" + pageURL + ")"
	})

	return out, nil
}

func rewriteAttachmentLinks(markdown string, results []store.AttachmentResult) string {
	if len(results) == 0 || !strings.Contains(markdown, "attachment://") {
		return markdown
	}

	byOriginal := make(map[string]string, len(results))
	for _, r := range results {
		if r.Error != nil || r.Filename == "" || strings.TrimSpace(r.OriginalName) == "" {
			continue
		}
		byOriginal[r.OriginalName] = store.AttachmentLocalPath(r.Filename)
	}

	if len(byOriginal) == 0 {
		return markdown
	}

	return attachmentLinkPattern.ReplaceAllStringFunc(markdown, func(match string) string {
		submatches := attachmentLinkPattern.FindStringSubmatch(match)
		if len(submatches) != 2 {
			return match
		}

		originalName := strings.TrimSpace(submatches[1])
		localPath, ok := byOriginal[originalName]
		if !ok {
			return match
		}

		return "](" + localPath + ")"
	})
}
