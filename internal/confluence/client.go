package confluence

import (
	"encoding/json"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	atlassian "github.com/ctreminiom/go-atlassian/v2/confluence/v2"
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
func NewClient(baseURL, username, token string) (*Client, error) {
	baseURL = strings.TrimSuffix(baseURL, "/")
	baseURL = strings.TrimSuffix(baseURL, "/wiki")

	httpClient := &http.Client{Timeout: 20 * time.Second}

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

	page, response, err := c.api.Page.Get(ctx, pageID, "storage", false, 0)
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
	if page.Body != nil && page.Body.Storage != nil {
		data.StorageFormat = page.Body.Storage.Value
	}

	return data, nil
}

// GetPageByID fetches a page by numeric ID with full content and returns structured page data.
func (c *Client) GetPageByID(ctx context.Context, pageID int64) (*FullPageData, error) {
	page, response, err := c.api.Page.Get(ctx, int(pageID), "storage", false, 0)
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf("failed to fetch page %d (status %d): %w", pageID, response.Code, err)
		}
		return nil, fmt.Errorf("failed to fetch page %d: %w", pageID, err)
	}

	data := &FullPageData{
		ID:    pageID,
		Title: page.Title,
	}

	if page.Version != nil {
		data.Version.Number = page.Version.Number
	}

	// SpaceID is available but not the key; we'll need to look it up separately or construct URL
	if page.SpaceID != "" {
		// Note: PageScheme doesn't include space key, only ID
		// For now, we construct the URL with just the page ID
		data.Space.Key = page.SpaceID
	}

	if page.Body != nil && page.Body.Storage != nil {
		data.Body.Storage.Value = page.Body.Storage.Value
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

// ResolvePageURLByTitle resolves a Confluence page title to a canonical page URL.
func (c *Client) ResolvePageURLByTitle(ctx context.Context, title, spaceKey string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", fmt.Errorf("title is empty")
	}

	escapedTitle := strings.ReplaceAll(title, `"`, `\"`)
	cql := fmt.Sprintf(`type="page" and title="%s"`, escapedTitle)
	if strings.TrimSpace(spaceKey) != "" {
		cql += fmt.Sprintf(` and space="%s"`, strings.TrimSpace(spaceKey))
	}

	endpoint := fmt.Sprintf("%s/wiki/rest/api/content/search?cql=%s&limit=1", c.baseURL, url.QueryEscape(cql))
	req, err := c.newAuthedRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("build search request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("search page by title: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body := readLimitedBody(resp.Body, 1024)
		return "", fmt.Errorf("search page by title failed: status=%d body=%s", resp.StatusCode, body)
	}

	var payload struct {
		Results []struct {
			ID    string `json:"id"`
			Links struct {
				WebUI string `json:"webui"`
			} `json:"_links"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode search response: %w", err)
	}

	if len(payload.Results) == 0 {
		return "", fmt.Errorf("no page found for title %q", title)
	}

	result := payload.Results[0]
	if result.Links.WebUI != "" {
		return c.baseURL + "/wiki" + result.Links.WebUI, nil
	}
	if result.ID != "" {
		return c.baseURL + "/wiki/spaces/" + strings.TrimSpace(spaceKey) + "/pages/" + result.ID, nil
	}

	return "", fmt.Errorf("page found for title %q but no resolvable URL", title)
}
