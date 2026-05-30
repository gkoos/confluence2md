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

	comments, err := c.fetchV2CommentsFromEndpoint(ctx, rootEndpoint)
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
		children, err := c.fetchV2CommentsFromEndpoint(ctx, childrenEndpoint)
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

	// Populate author display names using cached lookups.
	for i := range comments {
		if comments[i].AuthorID != "" {
			comments[i].Author = c.GetUserDisplayName(ctx, comments[i].AuthorID)
			// If GetUserDisplayName returns empty, keep the authorID as fallback
			if comments[i].Author == "" {
				comments[i].Author = comments[i].AuthorID
			}
		}
	}

	return comments, nil
}

func (c *Client) fetchV2CommentsFromEndpoint(ctx context.Context, endpoint string) ([]CommentData, error) {
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
			_ = resp.Body.Close()
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
			_ = resp.Body.Close()
			return nil, fmt.Errorf("unmarshal v2 comments response: %w", err)
		}
		_ = resp.Body.Close()

		for _, item := range payload.Results {
			authorID := strings.TrimSpace(item.Version.AuthorID)

			created := parseConfluenceTime(item.Version.CreatedAt)
			comments = append(comments, CommentData{
				ID:        strings.TrimSpace(item.ID),
				ParentID:  strings.TrimSpace(item.ParentCommentID),
				AuthorID:  authorID,
				Author:    "", // Will be populated by GetUserDisplayName later
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
