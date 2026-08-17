package confluence

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSameHost(t *testing.T) {
	cases := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{
			name: "identical hosts",
			a:    "https://example.atlassian.net/wiki/rest/api",
			b:    "https://example.atlassian.net/wiki/api/v2/attachments/123/download",
			want: true,
		},
		{
			name: "mixed-case hosts still match",
			a:    "https://Example.Atlassian.NET/wiki",
			b:    "https://example.atlassian.net/wiki/other",
			want: true,
		},
		{
			name: "cross-host does not match",
			a:    "https://example.atlassian.net/wiki",
			b:    "https://media.example-cdn.com/files/123",
			want: false,
		},
		{
			name: "parse failure on first URL",
			a:    "://not-a-valid-url",
			b:    "https://example.atlassian.net/wiki",
			want: false,
		},
		{
			name: "parse failure on second URL",
			a:    "https://example.atlassian.net/wiki",
			b:    "://not-a-valid-url",
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sameHost(tc.a, tc.b)
			if got != tc.want {
				t.Fatalf("sameHost(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestNewConditionallyAuthedRequest(t *testing.T) {
	client := &Client{
		baseURL:  "https://example.atlassian.net/wiki",
		username: "user@example.com",
		token:    "secret-token",
	}

	t.Run("same host attaches credentials", func(t *testing.T) {
		req, err := client.newConditionallyAuthedRequest(context.Background(), http.MethodGet, "https://example.atlassian.net/wiki/api/v2/pages/1", nil)
		if err != nil {
			t.Fatalf("newConditionallyAuthedRequest: %v", err)
		}
		if _, _, ok := req.BasicAuth(); !ok {
			t.Fatal("expected Authorization header to be set for same-host endpoint")
		}
	})

	t.Run("cross host omits credentials", func(t *testing.T) {
		req, err := client.newConditionallyAuthedRequest(context.Background(), http.MethodGet, "https://media.example-cdn.com/files/123", nil)
		if err != nil {
			t.Fatalf("newConditionallyAuthedRequest: %v", err)
		}
		if _, _, ok := req.BasicAuth(); ok {
			t.Fatal("expected no Authorization header for cross-host endpoint")
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
