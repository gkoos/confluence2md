package links

import "testing"

// TestExtractPageIDsFromADFWithStats_UsesSiteHostNotGatewayHost guards
// against the regression research.md R5 predicts: link-scope host matching
// must always be performed against the tenant's site host, never against
// the scoped-token gateway host (api.atlassian.com). If a call site were
// ever changed to pass the gateway host instead of the site host in scoped
// mode, in-scope links would silently stop matching and link traversal
// would discover nothing.
func TestExtractPageIDsFromADFWithStats_UsesSiteHostNotGatewayHost(t *testing.T) {
	const siteHost = "example.atlassian.net"
	const gatewayHost = "api.atlassian.com"

	adfJSON := `{"type":"doc","content":[{"type":"text","marks":[{"type":"link","attrs":{"href":"https://example.atlassian.net/wiki/spaces/ABC/pages/123/Some+Page"}}]}]}`

	t.Run("matches when host-matched against the site host", func(t *testing.T) {
		refs, externalSkipped := ExtractPageIDsFromADFWithStats(adfJSON, siteHost, []string{siteHost})
		if len(refs) != 1 || refs[0].ID != 123 || refs[0].Host != siteHost {
			t.Fatalf("expected page ref 123 on %s, got refs=%v externalSkipped=%d", siteHost, refs, externalSkipped)
		}
		if externalSkipped != 0 {
			t.Fatalf("expected no external links skipped, got %d", externalSkipped)
		}
	})

	t.Run("fails to match when host-matched against the gateway host", func(t *testing.T) {
		// This is the regression itself, made explicit: a link to the
		// tenant's own site is treated as external/out-of-scope if the
		// caller mistakenly passes the gateway host instead of the site
		// host, because the hosts never match in scoped mode.
		refs, externalSkipped := ExtractPageIDsFromADFWithStats(adfJSON, gatewayHost, []string{gatewayHost})
		if len(refs) != 0 {
			t.Fatalf("expected no page refs discovered when host-matched against the gateway host, got refs=%v", refs)
		}
		if externalSkipped != 1 {
			t.Fatalf("expected the site link to be counted as external-skipped, got %d", externalSkipped)
		}
	})
}

// TestExtractPageIDsFromADFWithStats_MultiHostCrossHostLinkIsInScope verifies
// that a link from one seed host to another seed host is discovered with the
// target's host attached, so the crawl can fetch it from the correct tenant.
func TestExtractPageIDsFromADFWithStats_MultiHostCrossHostLinkIsInScope(t *testing.T) {
	const sourceHost = "company1.atlassian.net"
	const targetHost = "company2.atlassian.net"

	adfJSON := `{"type":"doc","content":[{"type":"text","marks":[{"type":"link","attrs":{"href":"https://company2.atlassian.net/wiki/spaces/B/pages/456/Other"}}]}]}`

	refs, externalSkipped := ExtractPageIDsFromADFWithStats(adfJSON, sourceHost, []string{sourceHost, targetHost})
	if len(refs) != 1 || refs[0].ID != 456 || refs[0].Host != targetHost {
		t.Fatalf("expected cross-host ref 456 on %s, got refs=%v", targetHost, refs)
	}
	if externalSkipped != 0 {
		t.Fatalf("expected cross-host link to be in scope, got externalSkipped=%d", externalSkipped)
	}
}

// TestExtractPageIDFromURL covers every Confluence URL shape the pass-2 rewrite
// relies on, including the editor's /pages/edit-v2/{id} form that Confluence
// emits for links to headings on the same page.
func TestExtractPageIDFromURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want int64
	}{
		{
			name: "space-scoped path",
			url:  "https://example.atlassian.net/wiki/spaces/SFD/pages/123/Some+Page",
			want: 123,
		},
		{
			name: "numeric space key with draftShareId query",
			url:  "https://example.atlassian.net/wiki/spaces/4930699280/pages/456/?draftShareId=3e8da1ea",
			want: 456,
		},
		{
			name: "pageId query parameter",
			url:  "https://example.atlassian.net/wiki/pages/viewpage.action?pageId=789",
			want: 789,
		},
		{
			name: "edit-v2 link to a heading on the same page",
			url:  "https://example.atlassian.net/wiki/spaces/FALSSFDO/pages/edit-v2/321#Cognito",
			want: 321,
		},
		{
			name: "edit-v2 link without a space segment",
			url:  "https://example.atlassian.net/wiki/pages/edit-v2/654",
			want: 654,
		},
		{
			name: "bare page path without a space segment",
			url:  "https://example.atlassian.net/wiki/pages/987",
			want: 987,
		},
		{
			name: "relative path",
			url:  "/wiki/spaces/SFD/pages/111",
			want: 111,
		},
		{
			name: "jira issue link has no page id",
			url:  "https://example.atlassian.net/browse/SFD2-258",
			want: 0,
		},
		{
			name: "attachment download link has no page id",
			url:  "https://example.atlassian.net/wiki/download/attachments/123/diagram.png",
			want: 0,
		},
		{
			name: "empty url",
			url:  "",
			want: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ExtractPageIDFromURL(test.url); got != test.want {
				t.Fatalf("ExtractPageIDFromURL(%q) = %d, want %d", test.url, got, test.want)
			}
		})
	}
}
