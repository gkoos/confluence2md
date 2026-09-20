package confluence

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// userNameCache is an in-memory account ID -> display name cache scoped to a
// single Client.
//
// It is deliberately per-client instead of process-wide: a display name is only
// valid for the context that resolved it (tenant host, credentials, credential
// mode), and Confluence restricts what a caller may see based on the target
// user's profile visibility settings.
//
// Failed lookups are never stored. Leaving the entry absent lets a later call
// retry, instead of permanently blanking that author for every consumer sharing
// the client. Successful responses are cached even when they resolve to an empty
// name, because that is a real answer (displayName and publicName both
// restricted).
type userNameCache struct {
	mu    sync.RWMutex
	names map[string]string
}

func (c *userNameCache) lookup(accountID string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	name, ok := c.names[accountID]
	return name, ok
}

func (c *userNameCache) store(accountID, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.names == nil {
		c.names = make(map[string]string)
	}
	c.names[accountID] = name
}

// userResponse represents the Confluence User API response
type userResponse struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
	PublicName  string `json:"publicName"`
}

// GetUserDisplayName resolves a Confluence account ID to a display name.
// Returns empty string on error (best-effort, won't fail the crawl).
// Results are cached per Client; failures are not cached, so a later call can
// retry instead of inheriting an earlier transient failure.
func (c *Client) GetUserDisplayName(ctx context.Context, accountID string) string {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return ""
	}

	if name, ok := c.userNames.lookup(accountID); ok {
		return name
	}

	// Fetch from API using direct HTTP call (consistent with comments_client.go pattern)
	// Confluence v1 REST API: GET /wiki/rest/api/user?accountId={accountId}
	// Note: v2 API does not have a user lookup endpoint, use v1 instead
	endpoint := fmt.Sprintf("%s/wiki/rest/api/user?accountId=%s", c.apiBaseURL, url.QueryEscape(accountID))
	req, err := c.newAuthedRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		fmt.Printf("Warning: failed to create user request for %s: %v\n", accountID, err)
		return ""
	}

	req.Header.Set("Accept", "application/json")

	var user userResponse
	if err := c.doJSONRequest(req, &user); err != nil {
		fmt.Printf("Warning: failed to fetch user %s: %v\n", accountID, err)
		return ""
	}

	displayName := strings.TrimSpace(user.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(user.PublicName)
	}

	c.userNames.store(accountID, displayName)

	return displayName
}
