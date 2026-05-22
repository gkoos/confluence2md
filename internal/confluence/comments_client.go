package confluence

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// GetPageComments fetches comments and looks up author display names.
func (c *Client) GetPageComments(ctx context.Context, pageID int64) ([]CommentData, error) {
	rootEndpoint := fmt.Sprintf("%s/wiki/api/v2/pages/%d/footer-comments?limit=100&body-format=storage", c.baseURL, pageID)
	authorIDs := make(map[string]bool)

	comments, err := c.fetchV2CommentsFromEndpoint(ctx, rootEndpoint, authorIDs)
	if err != nil {
		return nil, err
	}

	// Fetch nested replies recursively via /footer-comments/{id}/children.
	queue := make([]string, 0, len(comments))
	visited := make(map[string]bool)
	for _, comment := range comments {
		if id := strings.TrimSpace(comment.ID); id != "" {
			queue = append(queue, id)
		}
	}

	for len(queue) > 0 {
		commentID := queue[0]
		queue = queue[1:]

		if commentID == "" || visited[commentID] {
			continue
		}
		visited[commentID] = true

		childrenEndpoint := fmt.Sprintf("%s/wiki/api/v2/footer-comments/%s/children?limit=100&body-format=storage", c.baseURL, url.PathEscape(commentID))
		children, err := c.fetchV2CommentsFromEndpoint(ctx, childrenEndpoint, authorIDs)
		if err != nil {
			return nil, err
		}

		for _, child := range children {
			comments = append(comments, child)
			if childID := strings.TrimSpace(child.ID); childID != "" {
				queue = append(queue, childID)
			}
		}
	}

	// Populate author display names.
	if len(authorIDs) > 0 {
		displayNames, err := c.getUserDisplayNames(ctx, authorIDs)
		if err == nil {
			for i := range comments {
				if displayName, ok := displayNames[comments[i].AuthorID]; ok {
					comments[i].Author = displayName
				}
			}
		}
	}

	return comments, nil
}

func (c *Client) fetchV2CommentsFromEndpoint(ctx context.Context, endpoint string, authorIDs map[string]bool) ([]CommentData, error) {
	comments := make([]CommentData, 0)

	for {
		req, err := c.newAuthedRequest(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("build v2 comments request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request v2 comments: %w", err)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body := readLimitedBody(resp.Body, 2048)
			resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound {
				return comments, nil
			}
			return nil, fmt.Errorf("request v2 comments failed: status=%d body=%s", resp.StatusCode, body)
		}

		var payload struct {
			Results []struct {
				ID              string `json:"id"`
				ParentCommentID string `json:"parentCommentId"`
				Version         struct {
					CreatedAt string `json:"createdAt"`
					AuthorID  string `json:"authorId"`
				} `json:"version"`
				Body struct {
					Storage struct {
						Value string `json:"value"`
					} `json:"storage"`
				} `json:"body"`
			} `json:"results"`
			Links struct {
				Next string `json:"next"`
			} `json:"_links"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("unmarshal v2 comments response: %w", err)
		}
		resp.Body.Close()

		for _, item := range payload.Results {
			authorID := strings.TrimSpace(item.Version.AuthorID)
			if authorID != "" {
				authorIDs[authorID] = true
			}

			created := parseConfluenceTime(item.Version.CreatedAt)
			comments = append(comments, CommentData{
				ID:        strings.TrimSpace(item.ID),
				ParentID:  strings.TrimSpace(item.ParentCommentID),
				AuthorID:  authorID,
				Author:    authorID,
				CreatedAt: created,
				UpdatedAt: created,
				Body:      strings.TrimSpace(item.Body.Storage.Value),
			})
		}

		next := resolveNextEndpoint(c.baseURL, payload.Links.Next)
		if next == "" {
			break
		}
		endpoint = next
	}

	return comments, nil
}

// getUserDisplayNames fetches display names for the given account IDs using the bulk Users API.
// Returns a map of accountId -> displayName.
func (c *Client) getUserDisplayNames(ctx context.Context, authorIDs map[string]bool) (map[string]string, error) {
	if len(authorIDs) == 0 {
		return make(map[string]string), nil
	}

	accountIDs := make([]string, 0, len(authorIDs))
	for id := range authorIDs {
		accountIDs = append(accountIDs, id)
	}

	requestBody := map[string][]string{"accountIds": accountIDs}
	bodyBytes, _ := json.Marshal(requestBody)

	endpoint := fmt.Sprintf("%s/wiki/api/v2/users-bulk", c.baseURL)
	req, err := c.newAuthedRequest(ctx, http.MethodPost, endpoint, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("build users lookup request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request users lookup: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody := readLimitedBody(resp.Body, 2048)
		return nil, fmt.Errorf("users lookup failed: status=%d body=%s", resp.StatusCode, respBody)
	}

	var payload struct {
		Results []struct {
			AccountID   string `json:"accountId"`
			DisplayName string `json:"displayName"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("unmarshal users lookup response: %w", err)
	}

	result := make(map[string]string)
	for _, user := range payload.Results {
		if user.DisplayName != "" {
			result[user.AccountID] = user.DisplayName
		}
	}

	return result, nil
}
