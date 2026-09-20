package confluence

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	atlassian "github.com/ctreminiom/go-atlassian/v2/confluence/v2"
	"github.com/gkoos/confluence2md/internal/config"
)

// httpStatusError preserves an HTTP response status while retaining the
// original API error in the unwrap chain.
type httpStatusError struct {
	statusCode int
	err        error
}

func (e *httpStatusError) Error() string {
	return e.err.Error()
}

func (e *httpStatusError) Unwrap() error {
	return e.err
}

// IsNotFound reports whether err was caused by an HTTP 404 response.
// It recognizes status errors through any number of wrapping layers.
func IsNotFound(err error) bool {
	var statusErr *httpStatusError
	return errors.As(err, &statusErr) && statusErr.statusCode == http.StatusNotFound
}

// Client wraps the Confluence API client used by the crawler.
//
// It deliberately tracks two distinct base URLs (research R5): siteURL is
// the tenant domain and is used only for values that land in emitted
// artifacts (webui links) and never varies with credential style;
// apiBaseURL is where requests actually go, and is the tenant domain in
// classic mode or the Atlassian gateway in scoped mode.
type Client struct {
	api          *atlassian.Client
	httpClient   *http.Client
	siteURL      string
	apiBaseURL   string
	allowedHosts []string
	username     string
	token        string
	mode         string
	userNames    userNameCache // per-client display-name cache (see user_client.go)
}

// NewClient creates an authenticated Confluence Cloud client.
//
// siteURL is the tenant domain (e.g. https://org.atlassian.net/wiki); it is
// used only for artifact-facing values and is never sent an API request
// unless apiBaseURL equals it (classic mode). apiBaseURL is where every API
// request actually goes — the same value as siteURL in classic mode, or the
// Atlassian gateway base (https://api.atlassian.com/ex/confluence/<cloudid>)
// in scoped mode. See contracts/api-endpoints.md.
func NewClient(siteURL, apiBaseURL, username, token string, retry config.RetryConfig, rateLimitRPM int, concurrency int) (*Client, error) {
	// siteURL is normalized to its root (scheme+host, no /wiki) because
	// every hand-built endpoint and go-atlassian's own relative paths
	// already include a "wiki/..." segment; stripping it here and letting
	// call sites re-add it is what research R4 documents.
	siteURL = strings.TrimSuffix(siteURL, "/")
	siteURL = strings.TrimSuffix(siteURL, "/wiki")

	// apiBaseURL gets the same /wiki-suffix trim. In classic mode
	// apiBaseURL is the site URL itself (".../wiki") and needs exactly the
	// same normalization the site URL gets, for the same reason — endpoint
	// paths add "wiki/..." themselves, so leaving the suffix on here would
	// double it (contracts/api-endpoints.md's own tables never carry a
	// duplicated /wiki segment). In scoped mode apiBaseURL is the gateway
	// base, which never ends in "/wiki" (cloud IDs are UUIDs), so this trim
	// is a safe no-op there — the gateway base never carries the suffix to
	// begin with (research R4).
	apiBaseURL = strings.TrimSuffix(apiBaseURL, "/")
	apiBaseURL = strings.TrimSuffix(apiBaseURL, "/wiki")

	apiHost := ""
	if parsed, err := url.Parse(apiBaseURL); err == nil {
		apiHost = parsed.Host
	}
	if concurrency < 1 {
		concurrency = 1
	}

	// Rate limiting is scoped to the host that actually receives the
	// request volume: the API base, not the (possibly different) site host.
	rateLimitedTransport := newRateLimitTransport(http.DefaultTransport, rateLimitRPM, concurrency, apiHost)
	transport := newRetryTransport(rateLimitedTransport, retry.MaxAttempts, retry.InitialBackoffMS)
	httpClient := &http.Client{
		Timeout:   20 * time.Second,
		Transport: transport,
	}

	api, err := atlassian.New(httpClient, apiBaseURL)
	if err != nil {
		return nil, fmt.Errorf("initialize go-atlassian client: %w", err)
	}

	api.Auth.SetBasicAuth(username, token)

	return &Client{
		api:          api,
		httpClient:   httpClient,
		siteURL:      siteURL,
		apiBaseURL:   apiBaseURL,
		allowedHosts: credentialHostAllowlist(siteURL, apiBaseURL),
		username:     username,
		token:        token,
	}, nil
}

// Mode reports the resolved credential style ("classic" or "scoped") once
// set by the auth-mode resolver (see authmode.go). Empty until resolved.
func (c *Client) Mode() string {
	return c.mode
}

// SetMode overrides the resolved credential style. Exposed for tests that
// need to exercise mode-dependent classification (e.g.
// confluence.DescribeAuthFailure) without going through the full
// ResolveAuth probe sequence. Production code should never need this: mode
// is set once, by ResolveAuth, at startup.
func (c *Client) SetMode(mode string) {
	c.mode = mode
}

// SiteURL returns the tenant domain this client was constructed with.
func (c *Client) SiteURL() string {
	return c.siteURL
}

// Host returns the lower-cased tenant host this client is bound to.
func (c *Client) Host() string {
	u, err := url.Parse(c.siteURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

// APIBaseURL returns the base URL every request is actually sent to.
func (c *Client) APIBaseURL() string {
	return c.apiBaseURL
}

// Ping validates credentials by fetching the first configured seed page
// through the client's selected API base — the same endpoint, host, and
// scope the crawl's very first real request uses (research R7, FR-008), so a
// passing Ping reliably predicts the crawl can begin (SC-002). It
// deliberately no longer calls the space-listing endpoint: that required a
// scope (space read) the crawl never otherwise needs.
func (c *Client) Ping(ctx context.Context, seed string) error {
	if _, err := c.GetPageBySeed(ctx, seed); err != nil {
		return fmt.Errorf("confluence auth check failed: %w", err)
	}
	return nil
}

// GetPageBySeed resolves a seed URL or numeric page ID and returns full page data with body.
func (c *Client) GetPageBySeed(ctx context.Context, seed string) (*PageData, error) {
	pageID, err := parseSeedPageID(seed)
	if err != nil {
		return nil, err
	}

	page, response, err := c.api.Page.Get(ctx, pageID, "atlas_doc_format", false, 0)
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf("failed to fetch page %d (status %d): %w", pageID, response.Code, &httpStatusError{statusCode: response.Code, err: err})
		}
		return nil, fmt.Errorf("failed to fetch page %d: %w", pageID, err)
	}

	data := &PageData{
		ID:    page.ID,
		Title: page.Title,
		Seed:  seed,
	}
	if page.Version != nil {
		data.Version = page.Version.Number
	}
	if page.Body != nil && page.Body.AtlasDocFormat != nil {
		data.StorageFormat = page.Body.AtlasDocFormat.Value
	}

	return data, nil
}

// GetPageByID fetches a page by numeric ID with full content and returns structured page data.
// spaceKey should be the alphanumeric space key (e.g., "SFD", "DS") for proper metadata and CQL lookups.
func (c *Client) GetPageByID(ctx context.Context, pageID int64, spaceKey string) (*FullPageData, error) {
	page, response, err := c.api.Page.Get(ctx, int(pageID), "atlas_doc_format", false, 0)
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf("failed to fetch page %d (status %d): %w", pageID, response.Code, &httpStatusError{statusCode: response.Code, err: err})
		}
		return nil, fmt.Errorf("failed to fetch page %d: %w", pageID, err)
	}

	data := &FullPageData{
		ID:        pageID,
		Title:     page.Title,
		Status:    strings.TrimSpace(page.Status),
		CreatedAt: page.CreatedAt,
		AuthorID:  page.AuthorID,
		ParentID:  page.ParentID,
	}

	if page.Version != nil {
		data.Version.Number = page.Version.Number
		data.Version.CreatedAt = page.Version.CreatedAt
		data.Version.AuthorID = page.Version.AuthorID
	}

	// Store the alphanumeric space key (not the numeric ID from API)
	if spaceKey != "" {
		data.Space.Key = spaceKey
	}

	if page.Body != nil && page.Body.AtlasDocFormat != nil {
		data.Body.ADF.Value = page.Body.AtlasDocFormat.Value
	}

	// Construct canonical URL - Confluence Cloud defaults to viewpage.action format
	// The API doesn't return direct links in PageScheme, so we construct it.
	//
	// This MUST use the site URL, never the API base: it is artifact-facing
	// (page front matter, metadata.json) and must stay identical regardless
	// of credential style (research R5, FR-013, constitution Principle I).
	data.Links.Webui = fmt.Sprintf("%s/wiki/pages/viewpage.action?pageId=%d", c.siteURL, pageID)

	return data, nil
}

// GetPageState fetches lightweight page metadata for dirty/clean classification.
// If includeAttachments is true, attachment metadata is fetched and folded into a stable signature.
func (c *Client) GetPageState(ctx context.Context, pageID int64, includeAttachments bool) (*PageStateData, error) {
	page, response, err := c.api.Page.Get(ctx, int(pageID), "", false, 0)
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf("failed to fetch page state %d (status %d): %w", pageID, response.Code, &httpStatusError{statusCode: response.Code, err: err})
		}
		return nil, fmt.Errorf("failed to fetch page state %d: %w", pageID, err)
	}

	state := &PageStateData{
		ID:     pageID,
		Title:  strings.TrimSpace(page.Title),
		Status: strings.TrimSpace(page.Status),
	}
	if page.Version != nil {
		state.Version = page.Version.Number
	}

	if includeAttachments {
		attachments, err := c.GetPageAttachments(ctx, pageID)
		if err != nil {
			return nil, fmt.Errorf("fetch attachment state for page %d: %w", pageID, err)
		}
		state.AttachmentSignature = computeAttachmentSignature(attachments)
	}

	return state, nil
}

func computeAttachmentSignature(attachments []AttachmentData) string {
	if len(attachments) == 0 {
		return "none"
	}

	parts := make([]string, 0, len(attachments))
	for _, a := range attachments {
		parts = append(parts, strings.Join([]string{
			strings.TrimSpace(a.ID),
			strings.TrimSpace(a.Filename),
			strings.TrimSpace(a.MediaType),
			strconv.FormatInt(a.FileSizeBytes, 10),
		}, "|"))
	}

	sort.Strings(parts)
	return strings.Join(parts, ";")
}

// GetPageTitleByID fetches a page title by page ID.
func (c *Client) GetPageTitleByID(ctx context.Context, pageID int) (string, error) {
	page, response, err := c.api.Page.Get(ctx, pageID, "", false, 0)
	if err != nil {
		if response != nil {
			return "", fmt.Errorf("failed to fetch page title for %d (status %d): %w", pageID, response.Code, err)
		}
		return "", fmt.Errorf("failed to fetch page title for %d: %w", pageID, err)
	}

	return page.Title, nil
}

// SearchPagesByCQL executes a CQL search against the Confluence REST v1 search API
// and returns the page IDs of all matching content entries. Results are paginated
// automatically using start/limit offsets.
func (c *Client) SearchPagesByCQL(ctx context.Context, cql string) ([]int64, error) {
	type contentEntry struct {
		ID string `json:"id"`
	}
	type resultEntry struct {
		Content contentEntry `json:"content"`
	}
	type searchPage struct {
		Results   []resultEntry `json:"results"`
		TotalSize int           `json:"totalSize"`
	}

	var ids []int64
	seen := make(map[int64]bool)
	const limit = 100
	start := 0

	for {
		params := url.Values{}
		params.Set("cql", cql)
		params.Set("limit", strconv.Itoa(limit))
		params.Set("start", strconv.Itoa(start))

		endpoint := fmt.Sprintf("%s/wiki/rest/api/search?%s", c.apiBaseURL, params.Encode())
		req, err := c.newAuthedRequest(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("build CQL search request: %w", err)
		}
		req.Header.Set("Accept", "application/json")

		var page searchPage
		if err := c.doJSONRequest(req, &page); err != nil {
			return nil, fmt.Errorf("CQL search: %w", err)
		}

		for _, r := range page.Results {
			id, err := strconv.ParseInt(r.Content.ID, 10, 64)
			if err == nil && id > 0 && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}

		start += len(page.Results)
		if start >= page.TotalSize || len(page.Results) == 0 {
			break
		}
	}

	return ids, nil
}

// GetPageChildIDs fetches all direct child page IDs for a given page, following pagination.
func (c *Client) GetPageChildIDs(ctx context.Context, pageID int64) ([]int64, error) {
	var ids []int64
	cursor := ""
	const limit = 100

	for {
		chunk, _, err := c.api.Page.GetsByParent(ctx, int(pageID), cursor, limit)
		if err != nil {
			return nil, fmt.Errorf("fetch children of page %d: %w", pageID, err)
		}
		if chunk == nil {
			break
		}
		for _, child := range chunk.Results {
			id, err := strconv.ParseInt(child.ID, 10, 64)
			if err == nil && id > 0 {
				ids = append(ids, id)
			}
		}
		if chunk.Links == nil || chunk.Links.Next == "" {
			break
		}
		// Extract cursor from the next link query string
		if u, err := url.Parse(chunk.Links.Next); err == nil {
			cursor = u.Query().Get("cursor")
		}
		if cursor == "" {
			break
		}
	}

	return ids, nil
}
