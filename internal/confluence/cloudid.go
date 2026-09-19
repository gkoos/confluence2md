package confluence

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// gatewayBaseURL is the fixed host Atlassian serves scoped API tokens from.
// See research.md R1.
const gatewayBaseURL = "https://api.atlassian.com/ex/confluence"

// cloudIDResolveTimeout bounds the single unauthenticated request made to
// resolve a site's cloud ID.
const cloudIDResolveTimeout = 15 * time.Second

// GatewayBaseURL builds the scoped-token API base for the given cloud ID.
// The result carries no /wiki suffix — see contracts/api-endpoints.md.
func GatewayBaseURL(cloudID string) string {
	return gatewayBaseURLFor(gatewayBaseURL, cloudID)
}

// gatewayBaseURLFor builds a gateway-shaped API base against an arbitrary
// gateway host. Production code always uses the real gatewayBaseURL
// constant via GatewayBaseURL; tests substitute a local httptest server here
// so probing "scoped" mode never makes a live request to api.atlassian.com.
func gatewayBaseURLFor(base, cloudID string) string {
	return strings.TrimSuffix(base, "/") + "/" + strings.TrimSpace(cloudID)
}

// ResolveCloudID fetches the Atlassian cloud ID for siteURL's site from its
// unauthenticated tenant-info endpoint (research R3):
//
//	GET https://<site>.atlassian.net/_edge/tenant_info -> {"cloudId": "<uuid>"}
//
// siteURL may or may not carry the /wiki suffix; only the scheme and host are
// used, since /_edge/tenant_info lives at the site root.
//
// The response is unauthenticated and therefore untrusted input: the
// returned value is validated as a UUID before being handed back, so a
// hostile or misconfigured response cannot be used to redirect subsequent
// authenticated requests to an attacker-influenced path.
func ResolveCloudID(ctx context.Context, siteURL string) (string, error) {
	u, err := url.Parse(siteURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("resolve cloud ID: parse site URL %q: %w", siteURL, err)
	}

	endpoint := fmt.Sprintf("%s://%s/_edge/tenant_info", u.Scheme, u.Host)

	reqCtx, cancel := context.WithTimeout(ctx, cloudIDResolveTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("resolve cloud ID: build tenant_info request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("resolve cloud ID: request tenant_info from %s: %w", endpoint, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("resolve cloud ID: tenant_info request to %s failed: %w", endpoint, readAPIError(resp))
	}

	var payload struct {
		CloudID string `json:"cloudId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("resolve cloud ID: decode tenant_info response from %s: %w", endpoint, err)
	}

	cloudID := strings.TrimSpace(payload.CloudID)
	if _, err := uuid.Parse(cloudID); err != nil {
		return "", fmt.Errorf("resolve cloud ID: tenant_info at %s returned a value that is not a valid UUID: %q", endpoint, cloudID)
	}

	return cloudID, nil
}
