package confluence

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gkoos/confluence2md/internal/config"
)

// routingRecorder captures every request path this server received, so
// tests can assert the exact request URL contracts/api-endpoints.md
// specifies for each operation, per mode.
type routingRecorder struct {
	paths []string
}

func newRoutingServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, rec *routingRecorder)) (*httptest.Server, *routingRecorder) {
	t.Helper()
	rec := &routingRecorder{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.paths = append(rec.paths, r.URL.Path)
		handler(w, r, rec)
	}))
	t.Cleanup(ts.Close)
	return ts, rec
}

func mustClient(t *testing.T, siteURL, apiBaseURL string) *Client {
	t.Helper()
	client, err := NewClient(siteURL, apiBaseURL, "user@example.com", "token", config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1}, 60000, 1)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

// TestRequestRouting_PerMode exercises every hand-built and go-atlassian
// backed operation this client uses, for both classic mode (API base ==
// site) and scoped mode (API base == a distinct gateway host), asserting
// requests always land on the configured API base and never leak to the
// other host (contracts/api-endpoints.md "Resulting request URLs").
func TestRequestRouting_PerMode(t *testing.T) {
	for _, mode := range []string{"classic", "scoped"} {
		t.Run(mode, func(t *testing.T) {
			apiServer, apiRec := newRoutingServer(t, func(w http.ResponseWriter, r *http.Request, rec *routingRecorder) {
				switch {
				case r.URL.Path == "/wiki/api/v2/pages/123":
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"id":"123","title":"Page","status":"current"}`))
				case r.URL.Path == "/wiki/api/v2/pages/123/children":
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"results":[]}`))
				case r.URL.Path == "/wiki/api/v2/pages/123/attachments":
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"results":[]}`))
				case r.URL.Path == "/wiki/api/v2/pages/123/footer-comments":
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"results":[]}`))
				case r.URL.Path == "/wiki/rest/api/search":
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"results":[],"totalSize":0}`))
				case r.URL.Path == "/wiki/rest/api/user":
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"accountId":"u1","displayName":"User One"}`))
				case r.URL.Path == "/wiki/rest/api/content/123/child/attachment/a1/download":
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("filedata"))
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			})

			var siteURL string
			var otherRec *routingRecorder
			if mode == "classic" {
				siteURL = apiServer.URL
			} else {
				siteServer, srec := newRoutingServer(t, func(w http.ResponseWriter, r *http.Request, rec *routingRecorder) {
					w.WriteHeader(http.StatusNotFound)
				})
				siteURL = siteServer.URL
				otherRec = srec
			}

			client := mustClient(t, siteURL, apiServer.URL)
			ctx := context.Background()

			if _, err := client.GetPageByID(ctx, 123, "ABC"); err != nil {
				t.Fatalf("GetPageByID: %v", err)
			}
			if _, err := client.GetPageChildIDs(ctx, 123); err != nil {
				t.Fatalf("GetPageChildIDs: %v", err)
			}
			if _, err := client.GetPageAttachments(ctx, 123); err != nil {
				t.Fatalf("GetPageAttachments: %v", err)
			}
			if _, err := client.GetPageComments(ctx, 123); err != nil {
				t.Fatalf("GetPageComments: %v", err)
			}
			if _, err := client.SearchPagesByCQL(ctx, "space=ABC"); err != nil {
				t.Fatalf("SearchPagesByCQL: %v", err)
			}
			_ = client.GetUserDisplayName(ctx, "u1-"+mode) // cache key varies per subtest to force a real request

			var buf discardWriter
			_ = client.DownloadAttachment(ctx, AttachmentData{ID: "a1", PageID: "123"}, 0, buf)

			wantPaths := map[string]bool{
				"/wiki/api/v2/pages/123":                                  true,
				"/wiki/api/v2/pages/123/children":                         true,
				"/wiki/api/v2/pages/123/attachments":                      true,
				"/wiki/api/v2/pages/123/footer-comments":                  true,
				"/wiki/rest/api/search":                                   true,
				"/wiki/rest/api/user":                                     true,
				"/wiki/rest/api/content/123/child/attachment/a1/download": true,
			}
			seen := make(map[string]bool)
			for _, p := range apiRec.paths {
				seen[p] = true
			}
			for want := range wantPaths {
				if !seen[want] {
					t.Errorf("expected API base to receive a request for %s, got paths: %v", want, apiRec.paths)
				}
			}

			if otherRec != nil && len(otherRec.paths) != 0 {
				t.Errorf("expected zero requests to the site host in scoped mode, got: %v", otherRec.paths)
			}
		})
	}
}

// TestClassicMode_NoDuplicatedWikiSegment is a regression test for a bug
// caught only by manually driving the built binary against a stub server:
// in classic mode, NewClient is called with apiBaseURL == siteURL, both
// carrying the /wiki suffix (config.Config.SiteURL()'s actual output, e.g.
// "http://host/wiki"). NewClient originally trimmed that suffix from the
// siteURL parameter only, leaving apiBaseURL untouched; go-atlassian's own
// relative-path joining then produced a duplicated "/wiki/wiki/..." request
// path, and every hand-built endpoint would have done the same. Both
// parameters must receive the same normalization.
func TestClassicMode_NoDuplicatedWikiSegment(t *testing.T) {
	apiServer, apiRec := newRoutingServer(t, func(w http.ResponseWriter, r *http.Request, rec *routingRecorder) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"123","title":"Page","status":"current"}`))
	})

	// Simulate config.Config.SiteURL()'s real shape: scheme+host+"/wiki",
	// used verbatim as both the site URL and (in classic mode) the API base
	// — exactly how ResolveAuth's probeClassic constructs a classic client.
	siteAndAPIBase := apiServer.URL + "/wiki"
	client := mustClient(t, siteAndAPIBase, siteAndAPIBase)

	if _, err := client.GetPageByID(context.Background(), 123, "ABC"); err != nil {
		t.Fatalf("GetPageByID: %v", err)
	}

	wantPath := "/wiki/api/v2/pages/123"
	found := false
	for _, p := range apiRec.paths {
		if p == wantPath {
			found = true
		}
		if p == "/wiki/wiki/api/v2/pages/123" {
			t.Fatalf("duplicated /wiki segment in classic-mode request path: %s", p)
		}
	}
	if !found {
		t.Fatalf("expected request path %s, got: %v", wantPath, apiRec.paths)
	}
}

// TestGatewayBase_NoDuplicatedOrMissingWikiSegment confirms that in scoped
// mode the API base carries no /wiki suffix on construction, and every
// hand-built endpoint still produces exactly one /wiki segment in the final
// request path (contracts/api-endpoints.md: "The scoped base carries no
// /wiki suffix — the /wiki segment is part of each endpoint path").
func TestGatewayBase_NoDuplicatedOrMissingWikiSegment(t *testing.T) {
	apiServer, apiRec := newRoutingServer(t, func(w http.ResponseWriter, r *http.Request, rec *routingRecorder) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"123","title":"Page","status":"current"}`))
	})

	gatewayBase := apiServer.URL + "/ex/confluence/11111111-1111-1111-1111-111111111111"
	client := mustClient(t, "https://example.atlassian.net", gatewayBase)

	if _, err := client.GetPageByID(context.Background(), 123, "ABC"); err != nil {
		t.Fatalf("GetPageByID: %v", err)
	}

	wantPath := "/ex/confluence/11111111-1111-1111-1111-111111111111/wiki/api/v2/pages/123"
	found := false
	for _, p := range apiRec.paths {
		if p == wantPath {
			found = true
		}
		if p == "/wiki/wiki/api/v2/pages/123" || p == "/ex/confluence/11111111-1111-1111-1111-111111111111/wiki/wiki/api/v2/pages/123" {
			t.Fatalf("duplicated /wiki segment in request path: %s", p)
		}
	}
	if !found {
		t.Fatalf("expected request path %s, got: %v", wantPath, apiRec.paths)
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

var _ io.Writer = discardWriter{}
