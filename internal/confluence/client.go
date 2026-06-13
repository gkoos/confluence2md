package confluence

import (
	"context"
	"encoding/json"
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

// Client wraps the Confluence API client used by the crawler.
type Client struct {
	api        *atlassian.Client
	httpClient *http.Client
	baseURL    string
	username   string
	token      string
}

// NewClient creates an authenticated Confluence Cloud client.
func NewClient(baseURL, username, token string, retry config.RetryConfig, rateLimitRPM int, concurrency int) (*Client, error) {
	baseURL = strings.TrimSuffix(baseURL, "/")
	baseURL = strings.TrimSuffix(baseURL, "/wiki")

	baseHost := ""
	if parsed, err := url.Parse(baseURL); err == nil {
		baseHost = parsed.Host
	}
	if concurrency < 1 {
		concurrency = 1
	}

	rateLimitedTransport := newRateLimitTransport(http.DefaultTransport, rateLimitRPM, concurrency, baseHost)
	transport := newRetryTransport(rateLimitedTransport, retry.MaxAttempts, retry.InitialBackoffMS)
	httpClient := &http.Client{
		Timeout:   20 * time.Second,
		Transport: transport,
	}

	api, err := atlassian.New(httpClient, baseURL)
	if err != nil {
		return nil, fmt.Errorf("initialize go-atlassian client: %w", err)
	}

	api.Auth.SetBasicAuth(username, token)

	return &Client{
		api:        api,
		httpClient: httpClient,
		baseURL:    baseURL,
		username:   username,
		token:      token,
	}, nil
}

// Ping performs a lightweight authenticated call to verify credentials.
func (c *Client) Ping(ctx context.Context) error {
	_, response, err := c.api.Space.Bulk(ctx, nil, "", 1)
	if err != nil {
		if response != nil {
			return fmt.Errorf("confluence auth check failed (status %d): %w", response.Code, err)
		}
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
			return nil, fmt.Errorf("failed to fetch page %d (status %d): %w", pageID, response.Code, err)
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
			return nil, fmt.Errorf("failed to fetch page %d (status %d): %w", pageID, response.Code, err)
		}
		return nil, fmt.Errorf("failed to fetch page %d: %w", pageID, err)
	}

	data := &FullPageData{
		ID:        pageID,
		Title:     page.Title,
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
	// The API doesn't return direct links in PageScheme, so we construct it
	data.Links.Webui = fmt.Sprintf("%s/wiki/pages/viewpage.action?pageId=%d", c.baseURL, pageID)

	return data, nil
}

// GetPageState fetches lightweight page metadata for dirty/clean classification.
// If includeAttachments is true, attachment metadata is fetched and folded into a stable signature.
func (c *Client) GetPageState(ctx context.Context, pageID int64, includeAttachments bool) (*PageStateData, error) {
	page, response, err := c.api.Page.Get(ctx, int(pageID), "", false, 0)
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf("failed to fetch page state %d (status %d): %w", pageID, response.Code, err)
		}
		return nil, fmt.Errorf("failed to fetch page state %d: %w", pageID, err)
	}

	state := &PageStateData{
		ID:    pageID,
		Title: strings.TrimSpace(page.Title),
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

		endpoint := fmt.Sprintf("%s/wiki/rest/api/search?%s", c.baseURL, params.Encode())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("build CQL search request: %w", err)
		}
		req.SetBasicAuth(c.username, c.token)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("CQL search request: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("CQL search returned HTTP %d", resp.StatusCode)
		}

		var page searchPage
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode CQL search response: %w", err)
		}
		resp.Body.Close()

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


