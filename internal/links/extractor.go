package links

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/gkoos/confluence2md/internal/store"
)

// ExtractPageIDsFromADFWithStats extracts host-scoped Confluence page references
// from ADF JSON by scanning link mark href attributes and inlineCard/blockCard url
// attributes. sourceHost is the host of the page the ADF came from (used for
// relative links); allowedHosts is the set of hosts considered in scope. Returns
// how many absolute links were skipped because their host was out of scope.
func ExtractPageIDsFromADFWithStats(adfJSON, sourceHost string, allowedHosts []string) ([]store.PageRef, int) {
	hrefRegex := regexp.MustCompile(`"href"\s*:\s*"([^"]+)"`)
	urlRegex := regexp.MustCompile(`"url"\s*:\s*"([^"]+)"`)

	seen := make(map[string]bool)
	var refs []store.PageRef
	externalSkipped := 0

	allowed := make(map[string]bool, len(allowedHosts))
	for _, h := range allowedHosts {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			allowed[h] = true
		}
	}
	sourceHost = strings.ToLower(strings.TrimSpace(sourceHost))

	for _, re := range []*regexp.Regexp{hrefRegex, urlRegex} {
		for _, match := range re.FindAllStringSubmatch(adfJSON, -1) {
			if len(match) < 2 {
				continue
			}
			target := strings.TrimSpace(match[1])
			ref, external, inScope := crawlTargetRef(target, sourceHost, allowed)
			if !inScope {
				if external {
					externalSkipped++
				}
				continue
			}
			if ref.ID <= 0 {
				continue
			}
			key := ref.Key()
			if !seen[key] {
				seen[key] = true
				refs = append(refs, ref)
			}
		}
	}

	return refs, externalSkipped
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

// DedupPageRefs removes duplicate page references while preserving order.
func DedupPageRefs(refs []store.PageRef) []store.PageRef {
	seen := make(map[string]bool)
	var result []store.PageRef
	for _, r := range refs {
		k := r.Key()
		if !seen[k] {
			seen[k] = true
			result = append(result, r)
		}
	}
	return result
}

// ExtractPageRefFromURL extracts the host-scoped page reference from a
// Confluence URL. Relative links (no host) resolve to sourceHost.
func ExtractPageRefFromURL(urlStr, sourceHost string) store.PageRef {
	id := ExtractPageIDFromURL(urlStr)
	if id <= 0 {
		return store.PageRef{}
	}
	u, err := url.Parse(urlStr)
	if err != nil || u.Host == "" {
		return store.PageRef{Host: sourceHost, ID: id}
	}
	return store.PageRef{Host: strings.ToLower(u.Host), ID: id}
}

// crawlTargetRef classifies a link target. It returns the host-scoped page
// reference, whether the target is external (host not in the allowed set), and
// whether the target is in crawl scope.
func crawlTargetRef(target, sourceHost string, allowed map[string]bool) (store.PageRef, bool, bool) {
	u, err := url.Parse(target)
	if err != nil {
		// Unparseable: treat slash-prefixed values as in-scope relative links.
		if strings.HasPrefix(target, "/") {
			return store.PageRef{Host: sourceHost, ID: ExtractPageIDFromURL(target)}, false, true
		}
		return store.PageRef{}, false, false
	}

	id := ExtractPageIDFromURL(target)

	// Relative links are in-scope and belong to the source host.
	if u.Host == "" {
		return store.PageRef{Host: sourceHost, ID: id}, false, true
	}

	host := strings.ToLower(u.Host)
	if allowed[host] {
		return store.PageRef{Host: host, ID: id}, false, true
	}
	return store.PageRef{}, true, false
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
	Type    string         `json:"type"`
	Attrs   map[string]any `json:"attrs"`
	Content []adfNode      `json:"content"`
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
func cqlFromMacroParams(attrs map[string]any) string {
	params, _ := attrs["parameters"].(map[string]any)
	if params == nil {
		return ""
	}
	macroParams, _ := params["macroParams"].(map[string]any)
	if macroParams == nil {
		return ""
	}
	cqlEntry, _ := macroParams["cql"].(map[string]any)
	if cqlEntry == nil {
		return ""
	}
	v, _ := cqlEntry["value"].(string)
	return v
}
