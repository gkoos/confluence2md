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

// newConditionallyAuthedRequest builds a request for endpoint, attaching
// Confluence Basic Auth only when endpoint targets the same host as
// c.baseURL. Use this for any URL that originated from an API response
// (redirect Location header, pagination _links.next, etc.) rather than
// being built directly from c.baseURL, since such URLs may point cross-host
// (e.g. a media CDN) and must not receive our credentials.
func (c *Client) newConditionallyAuthedRequest(ctx context.Context, method, endpoint string, body io.Reader) (*http.Request, error) {
	if sameHost(c.baseURL, endpoint) {
		return c.newAuthedRequest(ctx, method, endpoint, body)
	}
	return http.NewRequestWithContext(ctx, method, endpoint, body)
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
