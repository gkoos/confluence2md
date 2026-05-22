package links

import (
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// ExtractPageIDsFromStorageXML extracts Confluence page IDs from <a href> links
// in the raw storage format XML. These are explicit URL-based links with numeric IDs
// in the path (e.g. /wiki/spaces/SFD/pages/6511563548/...).
func ExtractPageIDsFromStorageXML(storageXML, baseURL string) []int64 {
	pageIDs, _ := ExtractPageIDsFromStorageXMLWithStats(storageXML, baseURL)
	return pageIDs
}

// ExtractPageIDsFromStorageXMLWithStats behaves like ExtractPageIDsFromStorageXML and
// also returns how many absolute links were skipped because their host didn't match
// the configured Confluence base URL host.
func ExtractPageIDsFromStorageXMLWithStats(storageXML, baseURL string) ([]int64, int) {
	hrefRegex := regexp.MustCompile(`href="([^"]+)"`)
	matches := hrefRegex.FindAllStringSubmatch(storageXML, -1)

	seen := make(map[int64]bool)
	var pageIDs []int64
	externalSkipped := 0

	allowedHost := hostFromBaseURL(baseURL)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		target := strings.TrimSpace(match[1])
		if allowed, isExternal := isAllowedCrawlTarget(target, allowedHost); !allowed {
			if isExternal {
				externalSkipped++
			}
			continue
		}

		id := ExtractPageIDFromURL(target)
		if id > 0 && !seen[id] {
			seen[id] = true
			pageIDs = append(pageIDs, id)
		}
	}

	return pageIDs, externalSkipped
}

// ExtractLinkedTitlesFromStorageXML extracts page titles from <ri:page ri:content-title="...">
// elements. These are structured ac:link refs that have no numeric ID in the XML —
// the title must be resolved to an ID separately via the API.
func ExtractLinkedTitlesFromStorageXML(storageXML string) []string {
	titleRegex := regexp.MustCompile(`ri:content-title="([^"]+)"`)
	matches := titleRegex.FindAllStringSubmatch(storageXML, -1)

	seen := make(map[string]bool)
	var titles []string

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		title := html.UnescapeString(match[1])
		if title != "" && !seen[title] {
			seen[title] = true
			titles = append(titles, title)
		}
	}

	return titles
}

// ExtractPageIDsFromMarkdown finds all Confluence page IDs from Markdown link targets
// Handles multiple Confluence URL formats and returns deduplicated, validated IDs
func ExtractPageIDsFromMarkdown(markdown string) []int64 {
	// Match Markdown links: [text](url)
	// Regex: \[[^\]]*\]\(([^)]+)\)
	markdownLinkRegex := regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
	matches := markdownLinkRegex.FindAllStringSubmatch(markdown, -1)

	seen := make(map[int64]bool)
	var pageIDs []int64

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		linkTarget := match[1]

		// Extract page ID from the URL
		pageID := ExtractPageIDFromURL(linkTarget)
		if pageID > 0 && !seen[pageID] {
			seen[pageID] = true
			pageIDs = append(pageIDs, pageID)
		}
	}

	return pageIDs
}

// ExtractPageIDFromURL extracts the numeric page ID from various Confluence URL formats
// Handles:
// - /spaces/{space}/pages/{id}/{title}
// - /wiki/pages/viewpage.action?pageId={id}
// - /wiki/spaces/{spaceId}/pages/{id}/?draftShareId=...
// - Full URLs with host
// Returns 0 if no valid ID found
func ExtractPageIDFromURL(urlStr string) int64 {
	if urlStr == "" {
		return 0
	}

	// Parse as URL to extract components
	u, err := url.Parse(urlStr)
	if err != nil {
		// Try as path-only string
		return extractIDFromPath(urlStr)
	}

	// Try query parameters first (pageId=...)
	if pageIDStr := u.Query().Get("pageId"); pageIDStr != "" {
		if id, err := strconv.ParseInt(pageIDStr, 10, 64); err == nil {
			return id
		}
	}

	// Try path extraction
	return extractIDFromPath(u.Path)
}

// extractIDFromPath extracts page ID from URL path component
// Handles patterns:
// - /spaces/SFD/pages/6511563548/...
// - /wiki/spaces/4930699280/pages/6511563548/...
// - /wiki/pages/viewpage.action?pageId=...
func extractIDFromPath(path string) int64 {
	// Pattern 1: /spaces/{KEY}/pages/{ID}/ or /wiki/spaces/{KEY}/pages/{ID}/
	// This catches both /spaces/SFD/pages/6511563548 and /wiki/spaces/SFD/pages/6511563548
	spacesPageRegex := regexp.MustCompile(`/(?:wiki/)?spaces/[^/]+/pages/(\d+)`)
	if match := spacesPageRegex.FindStringSubmatch(path); len(match) > 1 {
		if id, err := strconv.ParseInt(match[1], 10, 64); err == nil {
			return id
		}
	}

	// Pattern 2: /wiki/spaces/{NUMERIC_ID}/pages/{ID}/
	// Catches /wiki/spaces/4930699280/pages/6511563548/
	wikiSpacesPagesRegex := regexp.MustCompile(`/wiki/spaces/\d+/pages/(\d+)`)
	if match := wikiSpacesPagesRegex.FindStringSubmatch(path); len(match) > 1 {
		if id, err := strconv.ParseInt(match[1], 10, 64); err == nil {
			return id
		}
	}

	return 0
}

// ValidatePageID checks if a page ID is valid (positive integer)
func ValidatePageID(pageID int64) bool {
	return pageID > 0
}

// DedupPageIDs removes duplicate page IDs while preserving order
func DedupPageIDs(pageIDs []int64) []int64 {
	seen := make(map[int64]bool)
	var result []int64
	for _, id := range pageIDs {
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result
}

func hostFromBaseURL(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

func isAllowedCrawlTarget(target, allowedHost string) (bool, bool) {
	u, err := url.Parse(target)
	if err != nil {
		// If parsing fails, treat slash-prefixed values as in-scope relative links.
		return strings.HasPrefix(target, "/"), false
	}

	// Relative links are in-scope.
	if u.Host == "" {
		return true, false
	}

	if allowedHost == "" {
		return false, true
	}

	isAllowed := strings.EqualFold(u.Host, allowedHost)
	return isAllowed, !isAllowed
}
