package confluence

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

// sameHost reports whether two URLs target the same host (scheme-insensitive,
// case-insensitive). A parse failure on either side is treated as "not same"
// so credentials are withheld whenever the destination is uncertain.
func sameHost(a, b string) bool {
	ua, err := url.Parse(a)
	if err != nil {
		return false
	}
	ub, err := url.Parse(b)
	if err != nil {
		return false
	}
	return ua.Host != "" && strings.EqualFold(ua.Host, ub.Host)
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
