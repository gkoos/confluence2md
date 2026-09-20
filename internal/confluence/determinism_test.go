package confluence

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gkoos/confluence2md/internal/config"
)

// pageResponseFixture is a stubbed v2 page response shared between the
// classic and scoped clients in TestFullPageData_IdenticalAcrossModes.
const pageResponseFixture = `{
	"id": "123",
	"title": "Example",
	"status": "current",
	"createdAt": "2024-01-01T00:00:00.000Z",
	"authorId": "author-1",
	"parentId": "1",
	"version": {"number": 3, "createdAt": "2024-02-01T00:00:00.000Z", "authorId": "editor-1"},
	"body": {"atlas_doc_format": {"value": "{\"type\":\"doc\"}"}}
}`

func pageFixtureHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(pageResponseFixture))
}

// TestFullPageData_IdenticalAcrossModes asserts the invariant behind
// FR-013/SC-006: every field emitted onto FullPageData — most importantly
// Links.Webui, which is what ends up in front matter and metadata.json —
// must be identical whether the client is constructed in classic mode
// (mode=="classic") or scoped mode (mode=="scoped") for the same fixture and
// the same site URL. The credential style itself must not be observable in
// any emitted value.
func TestFullPageData_IdenticalAcrossModes(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(pageFixtureHandler))
	defer apiServer.Close()

	const siteURL = "https://example.atlassian.net"
	retry := config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1}

	// Both clients point their API base at the same fixture server, so the
	// only thing that differs between them is the resolved mode label —
	// exactly isolating whether that label leaks into emitted data.
	classic, err := NewClient(siteURL, apiServer.URL, "user@example.com", "token", retry, 60000, 1)
	if err != nil {
		t.Fatalf("NewClient (classic): %v", err)
	}
	classic.mode = config.AuthModeClassic

	scoped, err := NewClient(siteURL, apiServer.URL, "user@example.com", "token", retry, 60000, 1)
	if err != nil {
		t.Fatalf("NewClient (scoped): %v", err)
	}
	scoped.mode = config.AuthModeScoped

	classicPage, err := classic.GetPageByID(context.Background(), 123, "ABC")
	if err != nil {
		t.Fatalf("GetPageByID (classic): %v", err)
	}
	scopedPage, err := scoped.GetPageByID(context.Background(), 123, "ABC")
	if err != nil {
		t.Fatalf("GetPageByID (scoped): %v", err)
	}

	if classicPage.Links.Webui != scopedPage.Links.Webui {
		t.Fatalf("Links.Webui differs by mode: classic=%q scoped=%q (violates FR-013/SC-006)", classicPage.Links.Webui, scopedPage.Links.Webui)
	}
	wantWebui := siteURL + "/wiki/pages/viewpage.action?pageId=123"
	if classicPage.Links.Webui != wantWebui {
		t.Fatalf("Links.Webui = %q, want %q (must use the site URL, never the API base)", classicPage.Links.Webui, wantWebui)
	}
	if classicPage.Title != scopedPage.Title || classicPage.Status != scopedPage.Status || classicPage.Version.Number != scopedPage.Version.Number {
		t.Fatalf("page data differs by mode: classic=%+v scoped=%+v", classicPage, scopedPage)
	}
	if classicPage.CreatedAt != scopedPage.CreatedAt || classicPage.AuthorID != scopedPage.AuthorID || classicPage.ParentID != scopedPage.ParentID {
		t.Fatalf("provenance fields differ by mode: classic=%+v scoped=%+v", classicPage, scopedPage)
	}
}

// TestNoTokenLeak_InErrorsAndAuthFailureMessages asserts FR-012/SC-007: the
// configured token must never appear in any error message, auth-failure
// classification string, or the client's own state as exposed to callers,
// for either credential style.
func TestNoTokenLeak_InErrorsAndAuthFailureMessages(t *testing.T) {
	const secretToken = "super-secret-token-value-should-never-leak"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"insufficient permissions"}`, http.StatusForbidden)
	}))
	defer ts.Close()

	for _, mode := range []string{config.AuthModeClassic, config.AuthModeScoped} {
		t.Run(mode, func(t *testing.T) {
			client, err := NewClient(ts.URL, ts.URL, "user@example.com", secretToken, config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1}, 60000, 1)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			client.mode = mode

			_, err = client.GetPageByID(context.Background(), 1, "ABC")
			if err == nil {
				t.Fatal("expected an error from the 403 fixture")
			}
			if containsToken(err.Error(), secretToken) {
				t.Fatalf("token leaked into error message: %v", err)
			}

			described := DescribeAuthFailure(mode, "page 1", err)
			if containsToken(described, secretToken) {
				t.Fatalf("token leaked into DescribeAuthFailure output: %s", described)
			}

			resolutionErr := &AuthResolutionError{Attempts: []AuthAttempt{{Mode: mode, Err: err}}}
			if containsToken(resolutionErr.Error(), secretToken) {
				t.Fatalf("token leaked into AuthResolutionError output: %s", resolutionErr.Error())
			}
		})
	}
}

func containsToken(s, token string) bool {
	return token != "" && strings.Contains(s, token)
}

// TestComputeAttachmentSignature_StableAndOrderIndependent pins the shared
// signature helper: it drives updates-mode dirty detection, so it must be stable,
// order-independent and sensitive to real attachment changes.
func TestComputeAttachmentSignature_StableAndOrderIndependent(t *testing.T) {
	if got := ComputeAttachmentSignature(nil); got != "none" {
		t.Fatalf("empty attachments signature = %q, want %q", got, "none")
	}

	first := AttachmentData{ID: "a1", Filename: "diagram.png", MediaType: "image/png", FileSizeBytes: 1024}
	second := AttachmentData{ID: "a2", Filename: "notes.txt", MediaType: "text/plain", FileSizeBytes: 20}

	forward := ComputeAttachmentSignature([]AttachmentData{first, second})
	if want := "a1|diagram.png|image/png|1024;a2|notes.txt|text/plain|20"; forward != want {
		t.Fatalf("signature = %q, want %q", forward, want)
	}

	if reversed := ComputeAttachmentSignature([]AttachmentData{second, first}); reversed != forward {
		t.Fatalf("signature is order-dependent: %q vs %q", forward, reversed)
	}

	spaced := first
	spaced.Filename = "  diagram.png  "
	spaced.MediaType = " image/png "
	if got := ComputeAttachmentSignature([]AttachmentData{spaced, second}); got != forward {
		t.Fatalf("signature changed with surrounding whitespace: %q vs %q", got, forward)
	}

	resized := first
	resized.FileSizeBytes = 2048
	if got := ComputeAttachmentSignature([]AttachmentData{resized, second}); got == forward {
		t.Fatalf("signature did not change when a file size changed: %q", got)
	}
}
