package links

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// ExtractPageIDsFromADFWithStats extracts Confluence page IDs from ADF JSON by scanning
// link mark href attributes and inlineCard/blockCard url attributes. Returns how many
// absolute links were skipped because their host didn't match the configured Confluence
// base URL host.
func ExtractPageIDsFromADFWithStats(adfJSON, baseURL string) ([]int64, int) {
	hrefRegex := regexp.MustCompile(`"href"\s*:\s*"([^"]+)"`)
	urlRegex := regexp.MustCompile(`"url"\s*:\s*"([^"]+)"`)

	seen := make(map[int64]bool)
	var pageIDs []int64
	externalSkipped := 0

	allowedHost := hostFromBaseURL(baseURL)

	for _, re := range []*regexp.Regexp{hrefRegex, urlRegex} {
		for _, match := range re.FindAllStringSubmatch(adfJSON, -1) {
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
	}

	return pageIDs, externalSkipped
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

// HasChildrenMacro reports whether the ADF JSON contains a Confluence "children"
// extension node (the built-in "Child pages" macro). When present the crawler
// should fetch child pages via the API and add them to the outgoing link set.
func HasChildrenMacro(adfJSON string) bool {
	return strings.Contains(adfJSON, `"extensionKey":"children"`) ||
		strings.Contains(adfJSON, `"extensionKey": "children"`)
}

// adfNode is a minimal ADF node used only for CQL extraction.
type adfNode struct {
	Type    string                 `json:"type"`
	Attrs   map[string]interface{} `json:"attrs"`
	Content []adfNode              `json:"content"`
}

// ExtractContentByLabelCQLs returns the CQL query strings from all
// "contentbylabel" extension nodes in the ADF document. Each such
// node stores its CQL under attrs.parameters.macroParams.cql.value.
func ExtractContentByLabelCQLs(adfJSON string) []string {
	// Fast pre-check — avoid full JSON parse when macro is absent.
	if !strings.Contains(adfJSON, `"contentbylabel"`) {
		return nil
	}

	var doc adfNode
	if err := json.Unmarshal([]byte(adfJSON), &doc); err != nil {
		return nil
	}

	var results []string
	var walk func(adfNode)
	walk = func(n adfNode) {
		if n.Type == "extension" {
			if ek, _ := n.Attrs["extensionKey"].(string); ek == "contentbylabel" {
				if cql := cqlFromMacroParams(n.Attrs); cql != "" {
					results = append(results, cql)
				}
			}
		}
		for _, child := range n.Content {
			walk(child)
		}
	}
	walk(doc)
	return results
}

// cqlFromMacroParams drills into the nested ADF macro params structure:
//
//	attrs.parameters.macroParams.cql.value
func cqlFromMacroParams(attrs map[string]interface{}) string {
	params, _ := attrs["parameters"].(map[string]interface{})
	if params == nil {
		return ""
	}
	macroParams, _ := params["macroParams"].(map[string]interface{})
	if macroParams == nil {
		return ""
	}
	cqlEntry, _ := macroParams["cql"].(map[string]interface{})
	if cqlEntry == nil {
		return ""
	}
	v, _ := cqlEntry["value"].(string)
	return v
}
