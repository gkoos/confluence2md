package confluence

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func (c *Client) newAuthedRequest(ctx context.Context, method, endpoint string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.token)
	return req, nil
}

func readLimitedBody(body io.Reader, limit int64) string {
	data, _ := io.ReadAll(io.LimitReader(body, limit))
	return strings.TrimSpace(string(data))
}

func resolveNextEndpoint(baseURL, next string) string {
	next = strings.TrimSpace(next)
	if next == "" {
		return ""
	}
	if strings.HasPrefix(next, "http://") || strings.HasPrefix(next, "https://") {
		return next
	}
	return fmt.Sprintf("%s%s", baseURL, next)
}
