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
// Confluence Basic Auth only when endpoint's host is on the client's
// credential host allowlist (c.allowedHosts). Use this for any URL that
// originated from an API response (redirect Location header, pagination
// _links.next, etc.) rather than being built directly from c.apiBaseURL,
// since such URLs may point cross-host (e.g. a media CDN, or — in scoped
// mode — a signed redirect) and must not receive our credentials.
//
// This is the mechanism behind FR-021: widening which hosts may receive
// credentials requires a deliberate change to credentialHostAllowlist, never
// a fallback or a permissive default here.
func (c *Client) newConditionallyAuthedRequest(ctx context.Context, method, endpoint string, body io.Reader) (*http.Request, error) {
	if c.hostAllowed(endpoint) {
		return c.newAuthedRequest(ctx, method, endpoint, body)
	}
	return http.NewRequestWithContext(ctx, method, endpoint, body)
}

func readLimitedBody(body io.Reader, limit int64) string {
	data, _ := io.ReadAll(io.LimitReader(body, limit))
	return strings.TrimSpace(string(data))
}

// credentialHostAllowlist computes the set of hosts a client may attach
// Basic Auth credentials to (data-model.md "Entity: CredentialHostAllowlist",
// FR-021): the API base host, always, plus the site host when it differs
// (i.e. in scoped mode). It deliberately derives membership only from these
// two known, statically-determined bases — never from a response-supplied
// host — so a later refactor cannot silently widen the set.
func credentialHostAllowlist(siteURL, apiBaseURL string) []string {
	hosts := make([]string, 0, 2)
	seen := make(map[string]bool, 2)
	add := func(raw string) {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return
		}
		h := strings.ToLower(u.Host)
		if !seen[h] {
			seen[h] = true
			hosts = append(hosts, h)
		}
	}
	add(apiBaseURL)
	add(siteURL)
	return hosts
}

// hostAllowed reports whether endpoint's host is on c.allowedHosts
// (case-insensitive). A parse failure, or a host that cannot be determined,
// is treated as "not allowed" — credentials are withheld whenever the
// destination is uncertain (FR-021, constitution Principle IV). This
// fail-closed property must be preserved by any future change here: on
// doubt, withhold, never widen.
func (c *Client) hostAllowed(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Host)
	for _, h := range c.allowedHosts {
		if h == host {
			return true
		}
	}
	return false
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
