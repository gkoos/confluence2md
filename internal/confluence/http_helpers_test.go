package confluence

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCredentialHostAllowlist(t *testing.T) {
	cases := []struct {
		name       string
		siteURL    string
		apiBaseURL string
		want       []string
	}{
		{
			name:       "classic mode: single host",
			siteURL:    "https://example.atlassian.net",
			apiBaseURL: "https://example.atlassian.net",
			want:       []string{"example.atlassian.net"},
		},
		{
			name:       "scoped mode: both hosts allowed",
			siteURL:    "https://example.atlassian.net",
			apiBaseURL: "https://api.atlassian.com/ex/confluence/11111111-1111-1111-1111-111111111111",
			want:       []string{"api.atlassian.com", "example.atlassian.net"},
		},
		{
			name:       "case-insensitive host comparison",
			siteURL:    "https://Example.Atlassian.NET",
			apiBaseURL: "https://Example.Atlassian.NET",
			want:       []string{"example.atlassian.net"},
		},
		{
			name:       "unparseable site URL contributes nothing",
			siteURL:    "://not-a-valid-url",
			apiBaseURL: "https://api.atlassian.com/ex/confluence/abc",
			want:       []string{"api.atlassian.com"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := credentialHostAllowlist(tc.siteURL, tc.apiBaseURL)
			if len(got) != len(tc.want) {
				t.Fatalf("credentialHostAllowlist(%q, %q) = %v, want %v", tc.siteURL, tc.apiBaseURL, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("credentialHostAllowlist(%q, %q) = %v, want %v", tc.siteURL, tc.apiBaseURL, got, tc.want)
				}
			}
		})
	}
}

func TestNewConditionallyAuthedRequest(t *testing.T) {
	// Scoped-mode allowlist: both the gateway host and the site host are
	// allowed, exercising FR-021's "site host in scoped mode" member.
	client := &Client{
		allowedHosts: credentialHostAllowlist("https://example.atlassian.net", "https://api.atlassian.com/ex/confluence/11111111-1111-1111-1111-111111111111"),
		username:     "user@example.com",
		token:        "secret-token",
	}

	t.Run("API base host attaches credentials", func(t *testing.T) {
		req, err := client.newConditionallyAuthedRequest(context.Background(), http.MethodGet, "https://api.atlassian.com/ex/confluence/11111111-1111-1111-1111-111111111111/wiki/api/v2/pages/1", nil)
		if err != nil {
			t.Fatalf("newConditionallyAuthedRequest: %v", err)
		}
		if _, _, ok := req.BasicAuth(); !ok {
			t.Fatal("expected Authorization header to be set for the API base host")
		}
	})

	t.Run("site host attaches credentials in scoped mode", func(t *testing.T) {
		req, err := client.newConditionallyAuthedRequest(context.Background(), http.MethodGet, "https://example.atlassian.net/wiki/rest/api/content/1/child/attachment/2/download", nil)
		if err != nil {
			t.Fatalf("newConditionallyAuthedRequest: %v", err)
		}
		if _, _, ok := req.BasicAuth(); !ok {
			t.Fatal("expected Authorization header to be set for the allowlisted site host")
		}
	})

	t.Run("foreign redirect target omits credentials", func(t *testing.T) {
		req, err := client.newConditionallyAuthedRequest(context.Background(), http.MethodGet, "https://media.example-cdn.com/files/123", nil)
		if err != nil {
			t.Fatalf("newConditionallyAuthedRequest: %v", err)
		}
		if _, _, ok := req.BasicAuth(); ok {
			t.Fatal("expected no Authorization header for a foreign redirect target")
		}
	})

	t.Run("host cannot be determined: never treated as allowed", func(t *testing.T) {
		// hostAllowed must fail closed for a destination whose host can't be
		// parsed (FR-021) — verified directly, since http.NewRequestWithContext
		// itself also rejects a malformed URL before credential attachment
		// would even be considered.
		if client.hostAllowed("://not-a-valid-url") {
			t.Fatal("expected hostAllowed to reject an unparseable destination")
		}
		if client.hostAllowed("relative/no-host") {
			t.Fatal("expected hostAllowed to reject a destination with no host")
		}
	})
}

func TestAPIError_Error(t *testing.T) {
	err := &apiError{Status: 404, Body: "not found"}
	want := "status=404 body=not found"
	if got := err.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestReadAPIError(t *testing.T) {
	longBody := strings.Repeat("x", diagnosticBodyLimit+500)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(longBody))
	}))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}

	got := readAPIError(resp)
	var apiErr *apiError
	if !errors.As(got, &apiErr) {
		t.Fatalf("expected *apiError, got: %v", got)
	}
	if apiErr.Status != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, apiErr.Status)
	}
	if len(apiErr.Body) != diagnosticBodyLimit {
		t.Fatalf("expected body truncated to %d bytes, got %d", diagnosticBodyLimit, len(apiErr.Body))
	}
}

func TestDoJSONRequest_DecodesOnSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"widget"}`))
	}))
	defer ts.Close()

	client := &Client{httpClient: http.DefaultClient}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	var out struct {
		Name string `json:"name"`
	}
	if err := client.doJSONRequest(req, &out); err != nil {
		t.Fatalf("doJSONRequest: %v", err)
	}
	if out.Name != "widget" {
		t.Fatalf("expected decoded name %q, got %q", "widget", out.Name)
	}
}

func TestDoJSONRequest_ReturnsAPIErrorOnNon2xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"missing"}`))
	}))
	defer ts.Close()

	client := &Client{httpClient: http.DefaultClient}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	var out struct{}
	err = client.doJSONRequest(req, &out)

	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *apiError, got: %v", err)
	}
	if apiErr.Status != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, apiErr.Status)
	}
}

func TestDoJSONRequest_WrapsDecodeError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer ts.Close()

	client := &Client{httpClient: http.DefaultClient}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	var out struct{}
	if err := client.doJSONRequest(req, &out); err == nil {
		t.Fatal("expected decode error, got nil")
	}
}

func TestDoJSONRequest_NilOutSkipsDecode(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`whatever, not valid json`))
	}))
	defer ts.Close()

	client := &Client{httpClient: http.DefaultClient}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	if err := client.doJSONRequest(req, nil); err != nil {
		t.Fatalf("doJSONRequest with nil out: %v", err)
	}
}
