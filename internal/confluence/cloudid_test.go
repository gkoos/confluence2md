package confluence

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveCloudID_Success(t *testing.T) {
	var gotAuth bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_edge/tenant_info" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if _, _, ok := r.BasicAuth(); ok {
			gotAuth = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cloudId":"11111111-1111-1111-1111-111111111111"}`))
	}))
	defer ts.Close()

	cloudID, err := ResolveCloudID(context.Background(), ts.URL+"/wiki")
	if err != nil {
		t.Fatalf("ResolveCloudID: %v", err)
	}
	if cloudID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("unexpected cloud ID: %q", cloudID)
	}
	if gotAuth {
		t.Fatal("tenant_info request must be unauthenticated (research R3), but Basic Auth was present")
	}
}

func TestResolveCloudID_MalformedJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not-json`))
	}))
	defer ts.Close()

	if _, err := ResolveCloudID(context.Background(), ts.URL); err == nil {
		t.Fatal("expected error for malformed JSON response")
	}
}

func TestResolveCloudID_NonUUIDPayloadRejected(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cloudId":"<script>not-a-uuid</script>"}`))
	}))
	defer ts.Close()

	_, err := ResolveCloudID(context.Background(), ts.URL)
	if err == nil {
		t.Fatal("expected error for a cloudId value that is not a valid UUID")
	}
	if !strings.Contains(err.Error(), "not a valid UUID") {
		t.Fatalf("expected UUID-validation error, got: %v", err)
	}
}

func TestResolveCloudID_HTTPFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	}))
	defer ts.Close()

	if _, err := ResolveCloudID(context.Background(), ts.URL); err == nil {
		t.Fatal("expected error for a non-2xx tenant_info response")
	}
}

func TestResolveCloudID_UnparseableSiteURL(t *testing.T) {
	if _, err := ResolveCloudID(context.Background(), "://not-a-valid-url"); err == nil {
		t.Fatal("expected error for an unparseable site URL")
	}
}

func TestGatewayBaseURL_NoWikiSuffix(t *testing.T) {
	got := GatewayBaseURL("11111111-1111-1111-1111-111111111111")
	want := "https://api.atlassian.com/ex/confluence/11111111-1111-1111-1111-111111111111"
	if got != want {
		t.Fatalf("GatewayBaseURL() = %q, want %q", got, want)
	}
	if strings.HasSuffix(got, "/wiki") {
		t.Fatalf("GatewayBaseURL() must not carry a /wiki suffix, got %q", got)
	}
}
