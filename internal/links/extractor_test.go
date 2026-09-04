package links

import "testing"

// TestExtractPageIDsFromADFWithStats_UsesSiteHostNotGatewayHost guards
// against the regression research.md R5 predicts: link-scope host matching
// must always be performed against the tenant's site host, never against
// the scoped-token gateway host (api.atlassian.com). If a call site were
// ever changed to pass the API base URL instead of the site URL in scoped
// mode, in-scope links would silently stop matching and link traversal
// would discover nothing.
func TestExtractPageIDsFromADFWithStats_UsesSiteHostNotGatewayHost(t *testing.T) {
	const siteURL = "https://example.atlassian.net/wiki"
	const gatewayURL = "https://api.atlassian.com/ex/confluence/11111111-1111-1111-1111-111111111111"

	adfJSON := `{"type":"doc","content":[{"type":"text","marks":[{"type":"link","attrs":{"href":"https://example.atlassian.net/wiki/spaces/ABC/pages/123/Some+Page"}}]}]}`

	t.Run("matches when host-matched against the site URL", func(t *testing.T) {
		ids, externalSkipped := ExtractPageIDsFromADFWithStats(adfJSON, siteURL)
		if len(ids) != 1 || ids[0] != 123 {
			t.Fatalf("expected page ID 123 to be discovered against the site URL, got ids=%v externalSkipped=%d", ids, externalSkipped)
		}
		if externalSkipped != 0 {
			t.Fatalf("expected no external links skipped, got %d", externalSkipped)
		}
	})

	t.Run("fails to match when host-matched against the gateway URL", func(t *testing.T) {
		// This is the regression itself, made explicit: a link to the
		// tenant's own site is treated as external/out-of-scope if the
		// caller mistakenly passes the gateway base instead of the site
		// URL, because the hosts never match in scoped mode.
		ids, externalSkipped := ExtractPageIDsFromADFWithStats(adfJSON, gatewayURL)
		if len(ids) != 0 {
			t.Fatalf("expected no page IDs discovered when host-matched against the gateway URL (this is the R5 regression), got ids=%v", ids)
		}
		if externalSkipped != 1 {
			t.Fatalf("expected the site link to be counted as external-skipped when matched against the gateway host, got %d", externalSkipped)
		}
	})
}
