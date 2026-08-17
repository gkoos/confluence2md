package confluence

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// diagnosticBodyLimit bounds how much of a non-2xx response body is read and
// embedded into an error message. Confluence error payloads are small JSON
// objects, so this is generous for diagnostics while avoiding unbounded
// reads of an unexpectedly large or malicious response.
const diagnosticBodyLimit = 1024

// apiError represents a non-2xx response from the Confluence API. Callers
// that need to special-case a status code (e.g. treating 404 as "no results"
// rather than a hard failure) can use errors.As to inspect Status.
type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("status=%d body=%s", e.Status, e.Body)
}

// readAPIError reads a bounded diagnostic body from resp, closes it, and
// returns an *apiError describing the failure. Use this for call sites that
// don't decode a JSON success body (e.g. binary downloads, redirect
// handling) but still need consistent non-2xx error reporting.
func readAPIError(resp *http.Response) error {
	body := readLimitedBody(resp.Body, diagnosticBodyLimit)
	_ = resp.Body.Close()
	return &apiError{Status: resp.StatusCode, Body: body}
}

// doJSONRequest executes req and, on a 2xx response, decodes the JSON body
// into out (skipped if out is nil). On a non-2xx response it returns an
// *apiError with a bounded diagnostic body. It does not handle pagination or
// authenticated-request construction; callers remain responsible for those,
// keeping this helper narrowly scoped to "send a request, get JSON back".
func (c *Client) doJSONRequest(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return readAPIError(resp)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

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
