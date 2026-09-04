package confluence

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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

// ResolveAuth determines the effective credential style for a run and
// returns a Client already fixed to that style, validated by fetching the
// first configured seed page (research R7, FR-008) — the exact request the
// crawl's own first fetch will make, so a successful resolution reliably
// predicts the crawl can begin (SC-002).
//
// Behaviour by cfg.EffectiveAuthMode():
//
//   - "classic": probe the site domain only. Any failure is reported as a
//     rejected credential (research R8).
//   - "scoped": resolve the cloud ID (config override, or the site's
//     unauthenticated tenant_info endpoint) and probe the gateway only. A
//     cloud-ID resolution failure aborts before any crawl work, naming the
//     site and the resolution step (spec Edge Cases, FR-006).
//   - "auto" (default): probe the site domain first. On success, mode is
//     "classic" — today's only behaviour, so an existing config.yaml with no
//     auth_mode takes today's path unchanged (FR-002, FR-018, SC-003). On an
//     authorization failure (401/403), resolve the cloud ID and probe the
//     gateway. First success fixes the mode for the run. A non-auth failure
//     (network error, 5xx, etc.) on the site probe is NOT treated as a
//     signal to try the gateway — only a genuine authorization failure is.
//     If both attempts fail, both are reported (research R2).
func ResolveAuth(ctx context.Context, cfg *config.Config) (*AuthResolution, error) {
	return resolveAuth(ctx, cfg, gatewayBaseURL)
}

// resolveAuth is ResolveAuth's implementation, parameterized on the gateway
// host so tests can substitute a local httptest server and never make a live
// request to api.atlassian.com (constitution Principle III: no test may
// require live network access). ResolveAuth always passes the real
// gatewayBaseURL constant.
func resolveAuth(ctx context.Context, cfg *config.Config, gatewayBase string) (*AuthResolution, error) {
	if len(cfg.Crawl.Seeds) == 0 {
		return nil, fmt.Errorf("resolve auth mode: crawl.seeds must contain at least one URL")
	}
	seed := cfg.Crawl.Seeds[0]
	siteURL := cfg.SiteURL()

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
