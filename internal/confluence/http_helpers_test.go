package confluence

import (
	"context"
	"net/http"
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
