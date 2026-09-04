package confluence

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gkoos/confluence2md/internal/config"
)

func authTestConfig(seedServerURL string, mode string) *config.Config {
	return &config.Config{
		Confluence: config.ConfluenceConfig{
			Username: "user@example.com",
			Token:    "secret-token",
			AuthMode: mode,
		},
		Crawl: config.CrawlConfig{
			Seeds:        []string{seedServerURL + "/wiki/spaces/ABC/pages/123/Example"},
			Concurrency:  1,
			RateLimitRPM: 60000,
			QueueSize:    100,
		},
		Retry: config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1},
	}
}

// pageOKHandler responds to a v2 page-fetch request the way a successful
// Ping probe expects.
func pageOKHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"id":"123","title":"Example","status":"current"}`))
}

func TestResolveAuth_ClassicModeSucceedsWithoutTouchingGateway(t *testing.T) {
	gatewayCalled := false
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gatewayCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer gateway.Close()

	site := httptest.NewServer(http.HandlerFunc(pageOKHandler))
	defer site.Close()

	cfg := authTestConfig(site.URL, "") // absent -> auto
	auth, err := resolveAuth(t.Context(), cfg, gateway.URL)
	if err != nil {
		t.Fatalf("resolveAuth: %v", err)
	}
	if auth.Mode != config.AuthModeClassic {
		t.Fatalf("expected classic mode, got %q", auth.Mode)
	}
	if gatewayCalled {
		t.Fatal("classic-mode success must not touch the gateway at all")
	}
}

func TestResolveAuth_AutoProbe_SiteFailsThenGatewaySucceeds_ResolvesScoped(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_edge/tenant_info" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"cloudId":"11111111-1111-1111-1111-111111111111"}`))
			return
		}
		http.Error(w, `{"message":"insufficient permissions"}`, http.StatusUnauthorized)
	}))
	defer site.Close()

	var gatewayRequests int
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gatewayRequests++
		pageOKHandler(w, r)
	}))
	defer gateway.Close()

	cfg := authTestConfig(site.URL, "") // auto
	auth, err := resolveAuth(t.Context(), cfg, gateway.URL)
	if err != nil {
		t.Fatalf("resolveAuth: %v", err)
	}
	if auth.Mode != config.AuthModeScoped {
		t.Fatalf("expected scoped mode after site 401 + gateway success, got %q", auth.Mode)
	}
	if auth.CloudID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("expected resolved cloud ID, got %q", auth.CloudID)
	}
	if gatewayRequests == 0 {
		t.Fatal("expected the gateway to be probed after the site probe failed with 401")
	}

	// Mode never re-resolves mid-run: the returned client is fixed to the
	// gateway base for every subsequent request.
	if auth.Client.Mode() != config.AuthModeScoped {
		t.Fatalf("expected client to be fixed to scoped mode, got %q", auth.Client.Mode())
	}
}

func TestResolveAuth_AutoProbe_SiteSucceeds_NeverTouchesGateway(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(pageOKHandler))
	defer site.Close()

	gatewayCalled := false
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gatewayCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer gateway.Close()

	cfg := authTestConfig(site.URL, "auto")
	auth, err := resolveAuth(t.Context(), cfg, gateway.URL)
	if err != nil {
		t.Fatalf("resolveAuth: %v", err)
	}
	if auth.Mode != config.AuthModeClassic {
		t.Fatalf("expected classic mode when the site probe succeeds, got %q", auth.Mode)
	}
	if gatewayCalled {
		t.Fatal("site-probe success must resolve to classic without probing the gateway")
	}
}

// TestResolveAuth_AutoProbe_NonAuthFailureDoesNotFallBackToGateway covers
// research R2's explicit decision: auto mode only falls back to the gateway
// on a genuine authorization failure (401/403). A non-auth failure on the
// site probe — a 500, a network error, a timeout — must be reported
// directly, exactly as classic mode would today, and must never cause a
// request to the gateway. Without this, an operator's transient site outage
// would silently start probing the gateway instead of just failing loudly.
func TestResolveAuth_AutoProbe_NonAuthFailureDoesNotFallBackToGateway(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"internal server error"}`, http.StatusInternalServerError)
	}))
	defer site.Close()

	gatewayCalled := false
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gatewayCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer gateway.Close()

	cfg := authTestConfig(site.URL, "auto")
	_, err := resolveAuth(t.Context(), cfg, gateway.URL)
	if err == nil {
		t.Fatal("expected an error when the site probe fails with a non-auth status")
	}
	if gatewayCalled {
		t.Fatal("a non-auth failure (500) must not trigger a gateway fallback probe (research R2)")
	}

	// This must NOT be reported as a multi-attempt AuthResolutionError: only
	// the single site attempt was made.
	var resErr *AuthResolutionError
	if asAuthResolutionError(err, &resErr) {
		t.Fatalf("expected a direct error, not an AuthResolutionError (no gateway attempt was made), got: %+v", resErr)
	}
}

func TestResolveAuth_AutoProbe_BothFail_ReportsBothAttempts(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_edge/tenant_info" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"cloudId":"11111111-1111-1111-1111-111111111111"}`))
			return
		}
		http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer site.Close()

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"forbidden"}`, http.StatusForbidden)
	}))
	defer gateway.Close()

	cfg := authTestConfig(site.URL, "auto")
	_, err := resolveAuth(t.Context(), cfg, gateway.URL)
	if err == nil {
		t.Fatal("expected an error when both credential styles fail")
	}

	var resErr *AuthResolutionError
	if !asAuthResolutionError(err, &resErr) {
		t.Fatalf("expected *AuthResolutionError, got: %T (%v)", err, err)
	}
	if len(resErr.Attempts) != 2 {
		t.Fatalf("expected both attempts reported, got %d: %+v", len(resErr.Attempts), resErr.Attempts)
	}
	if resErr.Attempts[0].Mode != config.AuthModeClassic || resErr.Attempts[1].Mode != config.AuthModeScoped {
		t.Fatalf("expected classic then scoped attempts, got %+v", resErr.Attempts)
	}
}

func TestResolveAuth_ScopedModeExplicit_ResolvesCloudIDAndProbesGatewayOnly(t *testing.T) {
	siteCalled := 0
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		siteCalled++
		if r.URL.Path == "/_edge/tenant_info" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"cloudId":"22222222-2222-2222-2222-222222222222"}`))
			return
		}
		t.Errorf("scoped mode must not fetch content from the site domain, got request to %s", r.URL.Path)
	}))
	defer site.Close()

	gateway := httptest.NewServer(http.HandlerFunc(pageOKHandler))
	defer gateway.Close()

	cfg := authTestConfig(site.URL, "scoped")
	auth, err := resolveAuth(t.Context(), cfg, gateway.URL)
	if err != nil {
		t.Fatalf("resolveAuth: %v", err)
	}
	if auth.Mode != config.AuthModeScoped {
		t.Fatalf("expected scoped mode, got %q", auth.Mode)
	}
	if auth.CloudID != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("expected resolved cloud ID, got %q", auth.CloudID)
	}
	if siteCalled != 1 {
		t.Fatalf("expected exactly one site request (tenant_info), got %d", siteCalled)
	}
}

func TestResolveAuth_ScopedModeExplicit_CloudIDResolutionFailureAbortsBeforeCrawl(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer site.Close()

	gatewayCalled := false
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gatewayCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer gateway.Close()

	cfg := authTestConfig(site.URL, "scoped")
	_, err := resolveAuth(t.Context(), cfg, gateway.URL)
	if err == nil {
		t.Fatal("expected an error when cloud ID resolution fails")
	}
	if gatewayCalled {
		t.Fatal("must abort before any gateway request when cloud ID resolution fails")
	}

	// The error must name both the site and the resolution step that failed
	// (spec Edge Cases: "a message naming the site and the resolution step").
	var resErr *AuthResolutionError
	if !asAuthResolutionError(err, &resErr) {
		t.Fatalf("expected *AuthResolutionError, got: %T (%v)", err, err)
	}
	if len(resErr.Attempts) != 1 {
		t.Fatalf("expected exactly one attempt reported, got %d", len(resErr.Attempts))
	}
	msg := resErr.Attempts[0].Err.Error()
	if !strings.Contains(msg, "resolve cloud ID") {
		t.Fatalf("expected error to name the resolution step, got: %s", msg)
	}
	if !strings.Contains(msg, site.URL) {
		t.Fatalf("expected error to name the site, got: %s", msg)
	}
}

// asAuthResolutionError is a small errors.As helper kept local to this test
// file to avoid importing "errors" solely for one call site.
func asAuthResolutionError(err error, target **AuthResolutionError) bool {
	if e, ok := err.(*AuthResolutionError); ok {
		*target = e
		return true
	}
	return false
}
