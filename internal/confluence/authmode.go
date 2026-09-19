package confluence

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gkoos/confluence2md/internal/config"
)

// AuthOutcome is the classification the tool reports for an authorization
// failure, replacing a single opaque failure (data-model.md "Entity:
// AuthorizationOutcome", FR-009, FR-010, FR-020).
type AuthOutcome string

const (
	// AuthOutcomeAuthorized means the probe succeeded.
	AuthOutcomeAuthorized AuthOutcome = "authorized"
	// AuthOutcomeCredentialsRejected means the credentials themselves are
	// the problem: invalid, revoked, or expired (classic-mode 401/403, or
	// both auto attempts failing).
	AuthOutcomeCredentialsRejected AuthOutcome = "credentials_rejected"
	// AuthOutcomePermissionMissing means the credentials authenticated but
	// lack a permission the request needed (scoped-mode 401/403).
	AuthOutcomePermissionMissing AuthOutcome = "permission_missing"
)

// ClassifyAuthFailure classifies a non-2xx status observed while operating
// in the given resolved mode ("classic" or "scoped").
//
// Per research R8, Atlassian returns 401 for both a bad/expired credential
// and an insufficient scope, so status code alone cannot discriminate; the
// only reliable signal available to a Basic Auth caller is which host
// answered. Classic mode is authenticated straight against the tenant, so
// any 401/403 there means the credential itself was rejected. Scoped mode is
// only reachable at all with a credential the gateway accepted at some
// level, so a 401/403 there is attributed to a missing per-endpoint scope
// (data-model.md "Entity: AuthorizationOutcome").
//
// Returns AuthOutcomeAuthorized for any status outside 401/403 — callers
// should not treat that as a decisive non-auth outcome by itself, only as
// "this function has nothing to add."
func ClassifyAuthFailure(mode string, statusCode int) AuthOutcome {
	if statusCode != http.StatusUnauthorized && statusCode != http.StatusForbidden {
		return AuthOutcomeAuthorized
	}
	if strings.EqualFold(mode, config.AuthModeScoped) {
		return AuthOutcomePermissionMissing
	}
	return AuthOutcomeCredentialsRejected
}

// DescribeAuthFailure builds an operator-facing message for an authorization
// failure against the given content kind (e.g. "page 123", "the validation
// page", "attachment discovery"). It never includes the token or an
// Authorization header value (FR-012) — only the bounded diagnostic body
// already captured on apiError/httpStatusError-wrapped errors.
func DescribeAuthFailure(mode, contentKind string, err error) string {
	code, ok := statusCodeFromError(err)
	if !ok {
		return err.Error()
	}
	switch ClassifyAuthFailure(mode, code) {
	case AuthOutcomePermissionMissing:
		return fmt.Sprintf(
			"permission denied reading %s: credentials authenticated but lack a required permission (status %d, %s mode). See README.md for the documented required-scope list. Upstream: %v",
			contentKind, code, mode, err,
		)
	case AuthOutcomeCredentialsRejected:
		return fmt.Sprintf(
			"credentials rejected while reading %s: invalid, revoked, or expired (status %d, %s mode). Upstream: %v",
			contentKind, code, mode, err,
		)
	default:
		return err.Error()
	}
}

// statusCodeFromError extracts an HTTP status code from err if it wraps an
// *apiError or *httpStatusError anywhere in its chain.
func statusCodeFromError(err error) (int, bool) {
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		return apiErr.Status, true
	}
	var statusErr *httpStatusError
	if errors.As(err, &statusErr) {
		return statusErr.statusCode, true
	}
	return 0, false
}

// isAuthFailure reports whether err carries a 401 or 403 status.
func isAuthFailure(err error) bool {
	code, ok := statusCodeFromError(err)
	return ok && (code == http.StatusUnauthorized || code == http.StatusForbidden)
}

// AuthAttempt records the outcome of one credential-style probe attempt, for
// reporting when every attempt fails.
type AuthAttempt struct {
	Mode string
	Err  error
}

// AuthResolutionError is returned when no credential style could be
// validated. It preserves every attempt made so the operator can see which
// styles were tried and why each failed (spec Edge Cases, research R8).
type AuthResolutionError struct {
	Attempts []AuthAttempt
}

func (e *AuthResolutionError) Error() string {
	parts := make([]string, 0, len(e.Attempts))
	for _, a := range e.Attempts {
		parts = append(parts, fmt.Sprintf("%s mode: %v", a.Mode, a.Err))
	}
	return "credential validation failed for every attempted credential style: " + strings.Join(parts, "; ")
}

// AuthResolution is the outcome of resolving which credential style a run
// uses, fixed for the remainder of the run (data-model.md "Entity: AuthMode"
// state transitions: auto resolves exactly once, at startup).
type AuthResolution struct {
	Mode       string // "classic" or "scoped" — never "auto"
	APIBaseURL string
	CloudID    string // empty in classic mode
	Client     *Client
}

// AuthResolutions is the outcome of resolving credential style for every
// distinct host in a crawl. Each host is validated independently and gets its
// own Client, so cross-host links are fetched from the correct tenant.
type AuthResolutions struct {
	ByHost map[string]*AuthResolution
	Order  []string
}

// Clients returns a ClientSet built from the resolved per-host clients.
func (r *AuthResolutions) Clients() *ClientSet {
	cs := NewClientSet()
	for _, host := range r.Order {
		cs.Add(r.ByHost[host].Client)
	}
	return cs
}

// ForHost returns the resolution for the given lower-cased host.
func (r *AuthResolutions) ForHost(host string) *AuthResolution {
	return r.ByHost[host]
}

// ResolveAuth determines the effective credential style for every distinct
// seed host and returns a per-host client, each validated by fetching a
// representative seed page (research R7, FR-008). See resolveSingleHost for
// the per-host classic/scoped/auto behaviour.
func ResolveAuth(ctx context.Context, cfg *config.Config) (*AuthResolutions, error) {
	return resolveAuthForHosts(ctx, cfg, gatewayBaseURL)
}

// resolveAuthForHosts is ResolveAuth's implementation, parameterized on the
// gateway host so tests can substitute a local httptest server and never make
// a live request to api.atlassian.com (constitution Principle III).
func resolveAuthForHosts(ctx context.Context, cfg *config.Config, gatewayBase string) (*AuthResolutions, error) {
	if len(cfg.Crawl.Seeds) == 0 {
		return nil, fmt.Errorf("resolve auth mode: crawl.seeds must contain at least one URL")
	}

	res := &AuthResolutions{ByHost: make(map[string]*AuthResolution)}
	for _, site := range cfg.SiteURLs() {
		host := hostOfURL(site)
		if host == "" {
			continue
		}
		seed := firstSeedForHost(cfg.Crawl.Seeds, host)
		if seed == "" {
			continue
		}
		r, err := resolveSingleHost(ctx, cfg, gatewayBase, site, seed)
		if err != nil {
			return nil, err
		}
		res.ByHost[host] = r
		res.Order = append(res.Order, host)
	}
	if len(res.Order) == 0 {
		return nil, fmt.Errorf("resolve auth mode: no resolvable seed hosts")
	}
	return res, nil
}

func hostOfURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

func firstSeedForHost(seeds []string, host string) string {
	for _, seed := range seeds {
		if hostOfURL(seed) == host {
			return seed
		}
	}
	return ""
}

// resolveSingleHost resolves the credential style for a single site and
// returns a Client validated against the representative seed page.
func resolveSingleHost(ctx context.Context, cfg *config.Config, gatewayBase, siteURL, seed string) (*AuthResolution, error) {

	probeClassic := func() (*Client, error) {
		client, err := NewClient(siteURL, siteURL, cfg.Confluence.Username, cfg.Confluence.Token, cfg.Retry, cfg.Crawl.RateLimitRPM, cfg.Crawl.Concurrency)
		if err != nil {
			return nil, fmt.Errorf("build classic-mode client: %w", err)
		}
		if err := client.Ping(ctx, seed); err != nil {
			return nil, err
		}
		client.mode = config.AuthModeClassic
		return client, nil
	}

	resolveCloudID := func() (string, error) {
		if cid := strings.TrimSpace(cfg.Confluence.CloudID); cid != "" {
			return cid, nil
		}
		return ResolveCloudID(ctx, siteURL)
	}

	probeScoped := func() (*Client, string, error) {
		cloudID, err := resolveCloudID()
		if err != nil {
			return nil, "", fmt.Errorf("resolve cloud ID for site %s: %w", siteURL, err)
		}
		apiBase := gatewayBaseURLFor(gatewayBase, cloudID)
		client, err := NewClient(siteURL, apiBase, cfg.Confluence.Username, cfg.Confluence.Token, cfg.Retry, cfg.Crawl.RateLimitRPM, cfg.Crawl.Concurrency)
		if err != nil {
			return nil, cloudID, fmt.Errorf("build scoped-mode client: %w", err)
		}
		if err := client.Ping(ctx, seed); err != nil {
			return nil, cloudID, err
		}
		client.mode = config.AuthModeScoped
		return client, cloudID, nil
	}

	switch cfg.EffectiveAuthMode() {
	case config.AuthModeClassic:
		client, err := probeClassic()
		if err != nil {
			return nil, &AuthResolutionError{Attempts: []AuthAttempt{{Mode: config.AuthModeClassic, Err: describeProbeFailure(config.AuthModeClassic, err)}}}
		}
		return &AuthResolution{Mode: config.AuthModeClassic, APIBaseURL: siteURL, Client: client}, nil

	case config.AuthModeScoped:
		client, cloudID, err := probeScoped()
		if err != nil {
			return nil, &AuthResolutionError{Attempts: []AuthAttempt{{Mode: config.AuthModeScoped, Err: describeProbeFailure(config.AuthModeScoped, err)}}}
		}
		return &AuthResolution{Mode: config.AuthModeScoped, APIBaseURL: gatewayBaseURLFor(gatewayBase, cloudID), CloudID: cloudID, Client: client}, nil

	default: // "auto"
		classicClient, classicErr := probeClassic()
		if classicErr == nil {
			return &AuthResolution{Mode: config.AuthModeClassic, APIBaseURL: siteURL, Client: classicClient}, nil
		}
		if !isAuthFailure(classicErr) {
			// A non-auth failure (network error, 5xx, timeout) is not a
			// signal to try the gateway — auto only falls back on an
			// authorization failure (research R2). Report it directly,
			// exactly as classic mode would today.
			return nil, fmt.Errorf("validate credentials against %s: %w", siteURL, classicErr)
		}

		scopedClient, scopedCloudID, scopedErr := probeScoped()
		if scopedErr == nil {
			return &AuthResolution{Mode: config.AuthModeScoped, APIBaseURL: scopedClient.APIBaseURL(), CloudID: scopedCloudID, Client: scopedClient}, nil
		}

		return nil, &AuthResolutionError{Attempts: []AuthAttempt{
			{Mode: config.AuthModeClassic, Err: describeProbeFailure(config.AuthModeClassic, classicErr)},
			{Mode: config.AuthModeScoped, Err: describeProbeFailure(config.AuthModeScoped, scopedErr)},
		}}
	}
}

// describeProbeFailure wraps a probe error with the auth-outcome
// classification when it carries a recognizable HTTP status, otherwise
// returns it unchanged (e.g. network errors, cloud-ID resolution failures).
func describeProbeFailure(mode string, err error) error {
	if !isAuthFailure(err) {
		return err
	}
	return errors.New(DescribeAuthFailure(mode, "the validation page", err))
}
