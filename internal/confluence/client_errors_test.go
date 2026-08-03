package confluence

import (
	"errors"
	"fmt"
	"net/http"
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
