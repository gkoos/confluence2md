package confluence

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestIsNotFound(t *testing.T) {
	notFound := &httpStatusError{
		statusCode: http.StatusNotFound,
		err:        errors.New("page does not exist"),
	}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "direct 404", err: notFound, want: true},
		{name: "wrapped 404", err: fmt.Errorf("fetch page: %w", notFound), want: true},
		{name: "server error", err: &httpStatusError{statusCode: http.StatusInternalServerError, err: errors.New("server error")}},
		{name: "untyped error containing status", err: errors.New("request failed with status 404")},
		{name: "nil", err: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNotFound(tt.err); got != tt.want {
				t.Fatalf("IsNotFound() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClassifyAuthFailure(t *testing.T) {
	cases := []struct {
		name       string
		mode       string
		statusCode int
		want       AuthOutcome
	}{
		{
			name:       "classic mode 401 -> credentials rejected",
			mode:       "classic",
			statusCode: http.StatusUnauthorized,
			want:       AuthOutcomeCredentialsRejected,
		},
		{
			name:       "classic mode 403 -> credentials rejected",
			mode:       "classic",
			statusCode: http.StatusForbidden,
			want:       AuthOutcomeCredentialsRejected,
		},
		{
			name:       "scoped mode 401 after a successful probe -> missing scope",
			mode:       "scoped",
			statusCode: http.StatusUnauthorized,
			want:       AuthOutcomePermissionMissing,
		},
		{
			name:       "scoped mode 403 -> missing scope",
			mode:       "scoped",
			statusCode: http.StatusForbidden,
			want:       AuthOutcomePermissionMissing,
		},
		{
			name:       "non-auth status is not classified as a failure outcome",
			mode:       "classic",
			statusCode: http.StatusNotFound,
			want:       AuthOutcomeAuthorized,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyAuthFailure(tc.mode, tc.statusCode); got != tc.want {
				t.Fatalf("ClassifyAuthFailure(%q, %d) = %q, want %q", tc.mode, tc.statusCode, got, tc.want)
			}
		})
	}
}

// TestClassifyAuthFailure_ExpiredCredentialIsRejectedNotMissingScope covers
// FR-020: an expired credential must be classified as a rejected credential,
// distinctly from a missing-scope failure — even though Atlassian returns
// the same 401 status for both (research R8, R12). In classic mode, this
// falls out of the mode-based classification rule directly: classic-mode
// failures are always "credentials_rejected", never "permission_missing".
func TestClassifyAuthFailure_ExpiredCredentialIsRejectedNotMissingScope(t *testing.T) {
	expiredCredentialErr := &apiError{Status: http.StatusUnauthorized, Body: `{"message":"The token used has expired"}`}

	got := ClassifyAuthFailure("classic", expiredCredentialErr.Status)
	if got != AuthOutcomeCredentialsRejected {
		t.Fatalf("expired-credential 401 in classic mode = %q, want %q (FR-020)", got, AuthOutcomeCredentialsRejected)
	}
	if got == AuthOutcomePermissionMissing {
		t.Fatal("expired credential must never be classified as a missing permission")
	}
}

func TestDescribeAuthFailure_NamesContentKindAndPointsAtScopeDocs(t *testing.T) {
	err := fmt.Errorf("fetch failed: %w", &apiError{Status: http.StatusForbidden, Body: "insufficient permissions"})

	msg := DescribeAuthFailure("scoped", "attachment discovery for page 42", err)
	if !strings.Contains(msg, "attachment discovery for page 42") {
		t.Fatalf("expected message to name the content kind, got: %s", msg)
	}
	if !strings.Contains(msg, "README.md") {
		t.Fatalf("expected message to point at the documented scope list, got: %s", msg)
	}
	if !strings.Contains(msg, "authenticated but lack a required permission") {
		t.Fatalf("expected message to distinguish 'missing permission' from 'rejected credential', got: %s", msg)
	}
}

func TestDescribeAuthFailure_ClassicModeNamesRejectedCredential(t *testing.T) {
	err := fmt.Errorf("fetch failed: %w", &apiError{Status: http.StatusUnauthorized, Body: "unauthorized"})

	msg := DescribeAuthFailure("classic", "the validation page", err)
	if !strings.Contains(msg, "credentials rejected") {
		t.Fatalf("expected classic-mode 401 to be described as a rejected credential, got: %s", msg)
	}
	if strings.Contains(msg, "lack a required permission") {
		t.Fatalf("classic-mode failure must not be described as a missing permission, got: %s", msg)
	}
}

func TestHTTPStatusErrorPreservesCause(t *testing.T) {
	cause := errors.New("api error")
	err := fmt.Errorf("failed to fetch page (status 404): %w", &httpStatusError{
		statusCode: http.StatusNotFound,
		err:        cause,
	})

	if !errors.Is(err, cause) {
		t.Fatal("expected the original API error to remain in the unwrap chain")
	}
}
