package confluence

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// userNameCache is an in-memory cache for account ID to display name lookups
type userNameCache struct {
	mu    sync.RWMutex
	names map[string]string
}

var globalUserCache = &userNameCache{
	names: make(map[string]string),
}

// userResponse represents the Confluence User API response
type userResponse struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
	PublicName  string `json:"publicName"`
}

// GetUserDisplayName resolves a Confluence account ID to a display name.
// Returns empty string on error (best-effort, won't fail the crawl).
// Results are cached in memory to avoid duplicate API calls.
func (c *Client) GetUserDisplayName(ctx context.Context, accountID string) string {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return ""
	}

	// Check cache first
	globalUserCache.mu.RLock()
	if name, ok := globalUserCache.names[accountID]; ok {
		globalUserCache.mu.RUnlock()
		return name
	}
	globalUserCache.mu.RUnlock()

	// Fetch from API using direct HTTP call (consistent with comments_client.go pattern)
	// Confluence v1 REST API: GET /wiki/rest/api/user?accountId={accountId}
	// Note: v2 API does not have a user lookup endpoint, use v1 instead
	endpoint := fmt.Sprintf("%s/wiki/rest/api/user?accountId=%s", c.baseURL, url.QueryEscape(accountID))
	req, err := c.newAuthedRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		fmt.Printf("Warning: failed to create user request for %s: %v\n", accountID, err)
		globalUserCache.mu.Lock()
		globalUserCache.names[accountID] = ""
		globalUserCache.mu.Unlock()
		return ""
	}

	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		fmt.Printf("Warning: failed to fetch user %s: %v\n", accountID, err)
		globalUserCache.mu.Lock()
		globalUserCache.names[accountID] = ""
		globalUserCache.mu.Unlock()
		return ""
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != 200 {
		fmt.Printf("Warning: failed to fetch user %s (status %d)\n", accountID, resp.StatusCode)
		globalUserCache.mu.Lock()
		globalUserCache.names[accountID] = ""
		globalUserCache.mu.Unlock()
		return ""
	}

	var user userResponse
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		fmt.Printf("Warning: failed to decode user response for %s: %v\n", accountID, err)
		globalUserCache.mu.Lock()
		globalUserCache.names[accountID] = ""
		globalUserCache.mu.Unlock()
		return ""
	}

	displayName := strings.TrimSpace(user.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(user.PublicName)
	}

	// Cache the result
	globalUserCache.mu.Lock()
	globalUserCache.names[accountID] = displayName
	globalUserCache.mu.Unlock()

	return displayName
}
