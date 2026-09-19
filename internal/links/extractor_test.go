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
