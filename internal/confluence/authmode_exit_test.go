package confluence

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/gkoos/confluence2md/internal/config"
)

func TestIsAuthFailure(t *testing.T) {
	rejected := &apiError{Status: http.StatusUnauthorized, Body: "unauthorized"}
	serverDown := &apiError{Status: http.StatusBadGateway, Body: "bad gateway"}

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("dial tcp: no route"), false},
		{"401", fmt.Errorf("fetch: %w", rejected), true},
		{"5xx", fmt.Errorf("fetch: %w", serverDown), false},
		{
			"every auto attempt rejected",
			&AuthResolutionError{Attempts: []AuthAttempt{
				{Mode: config.AuthModeClassic, Err: describeProbeFailure(config.AuthModeClassic, rejected)},
				{Mode: config.AuthModeScoped, Err: describeProbeFailure(config.AuthModeScoped, rejected)},
			}},
			true,
		},
		{
			"explicit mode failed on the network",
			&AuthResolutionError{Attempts: []AuthAttempt{
				{Mode: config.AuthModeScoped, Err: describeProbeFailure(config.AuthModeScoped, errors.New("cloud id lookup timed out"))},
			}},
			false,
		},
		{
			"wrapped resolution error",
			fmt.Errorf("credential validation failed: %w", &AuthResolutionError{Attempts: []AuthAttempt{
				{Mode: config.AuthModeClassic, Err: describeProbeFailure(config.AuthModeClassic, rejected)},
			}}),
			true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsAuthFailure(tc.err); got != tc.want {
				t.Fatalf("IsAuthFailure = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDescribeProbeFailureKeepsOperatorMessage(t *testing.T) {
	cause := &apiError{Status: http.StatusUnauthorized, Body: "unauthorized"}

	got := describeProbeFailure(config.AuthModeClassic, cause)

	if want := DescribeAuthFailure(config.AuthModeClassic, "the validation page", cause); got.Error() != want {
		t.Fatalf("message = %q, want %q", got.Error(), want)
	}
}
