package confluence

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRetryTransport_RetriesOn503ThenSucceeds(t *testing.T) {
	attempts := 0
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("retry"))}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})

	transport := newRetryTransport(base, 3, 1)
	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %#v", resp)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestRetryTransport_DoesNotRetryOn404(t *testing.T) {
	attempts := 0
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("missing"))}, nil
	})

	transport := newRetryTransport(base, 5, 1)
	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %#v", resp)
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
}

func TestRetryDelay_UsesRetryAfterHeaderFor429(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header)}
	resp.Header.Set("Retry-After", "2")

	delay := retryDelay(resp, 1, 10)
	if delay != 2*time.Second {
		t.Fatalf("expected 2s delay from Retry-After, got %s", delay)
	}
}

func TestRetryTransport_StopsOnContextCancellation(t *testing.T) {
	attempts := 0
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("retry"))}, nil
	})

	transport := newRetryTransport(base, 5, 1000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, runErr := transport.RoundTrip(req)
		done <- runErr
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for cancellation")
	}

	if attempts != 1 {
		t.Fatalf("expected exactly 1 attempt before cancellation, got %d", attempts)
	}
}

func TestRetryTransport_RetriesTransientTransportErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{name: "unexpected EOF", err: io.ErrUnexpectedEOF},
		{name: "i/o timeout", err: &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded}},
		{name: "connection reset", err: &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset by peer")}},
		{name: "temporary resolver failure", err: &net.DNSError{Err: "server misbehaving", Name: "example.com"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			attempts := 0
			base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				attempts++
				return nil, tc.err
			})

			transport := newRetryTransport(base, 3, 1)
			req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}

			if _, roundTripErr := transport.RoundTrip(req); !errors.Is(roundTripErr, tc.err) {
				t.Fatalf("expected the underlying error to be returned, got %v", roundTripErr)
			}
			if attempts != 3 {
				t.Fatalf("expected 3 attempts for a transient error, got %d", attempts)
			}
		})
	}
}

func TestRetryTransport_DoesNotRetryPermanentTransportErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{name: "plain permanent error", err: errors.New("http: server gave HTTP response to HTTPS client")},
		{name: "invalid address", err: &net.AddrError{Err: "invalid port", Addr: ":99999"}},
		{name: "unknown host", err: &net.DNSError{Err: "no such host", Name: "nope.invalid", IsNotFound: true}},
		{name: "cancelled caller", err: context.Canceled},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			attempts := 0
			base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				attempts++
				return nil, tc.err
			})

			transport := newRetryTransport(base, 5, 1)
			req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}

			if _, roundTripErr := transport.RoundTrip(req); !errors.Is(roundTripErr, tc.err) {
				t.Fatalf("expected the underlying error to be returned, got %v", roundTripErr)
			}
			if attempts != 1 {
				t.Fatalf("expected a permanent error to be tried once, got %d attempts", attempts)
			}
		})
	}
}

func TestRetryTransport_DoesNotRetryWhenCallerContextIsDone(t *testing.T) {
	attempts := 0
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		return nil, io.ErrUnexpectedEOF // transient on its own, but the caller is gone
	})

	transport := newRetryTransport(base, 5, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	if _, roundTripErr := transport.RoundTrip(req); !errors.Is(roundTripErr, io.ErrUnexpectedEOF) {
		t.Fatalf("expected the underlying error to be returned, got %v", roundTripErr)
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt with a finished context, got %d", attempts)
	}
}

func TestRetryTransport_DoesNotRetryStatusWhenCallerContextIsDone(t *testing.T) {
	attempts := 0
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("retry"))}, nil
	})

	transport := newRetryTransport(base, 5, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected the 503 response to be returned, got %#v", resp)
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt with a finished context, got %d", attempts)
	}
}
