package confluence

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type rateLimitRoundTripFunc func(*http.Request) (*http.Response, error)

func (f rateLimitRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRateLimitTransport_AllowsImmediateRequestWhenBudgetAvailable(t *testing.T) {
	called := 0
	base := rateLimitRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		called++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})

	transport := newRateLimitTransport(base, 60)

	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	start := time.Now()
	resp, err := transport.RoundTrip(req)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 response, got %#v", resp)
	}
	if called != 1 {
		t.Fatalf("expected base transport to be called once, got %d", called)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("expected immediate request, elapsed too high: %s", elapsed)
	}
}

func TestRateLimitTransport_WaitsWhenTokenUnavailable(t *testing.T) {
	base := rateLimitRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})

	// 1200 RPM = 20 req/s => ~50ms between tokens with burst=1.
	transport := newRateLimitTransport(base, 1200)

	req1, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("new first request: %v", err)
	}
	if _, err := transport.RoundTrip(req1); err != nil {
		t.Fatalf("first round trip: %v", err)
	}

	req2, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("new second request: %v", err)
	}

	start := time.Now()
	if _, err := transport.RoundTrip(req2); err != nil {
		t.Fatalf("second round trip: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < 35*time.Millisecond {
		t.Fatalf("expected second request to wait for limiter token, elapsed only %s", elapsed)
	}
}

func TestRateLimitTransport_RespectsContextCancellationWhileWaiting(t *testing.T) {
	called := 0
	base := rateLimitRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		called++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})

	// 60 RPM = 1 req/s. First request consumes immediate burst token.
	transport := newRateLimitTransport(base, 60)

	req1, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("new first request: %v", err)
	}
	if _, err := transport.RoundTrip(req1); err != nil {
		t.Fatalf("first round trip: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req2, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("new second request: %v", err)
	}

	_, err = transport.RoundTrip(req2)
	if err == nil {
		t.Fatal("expected context cancellation error while waiting for limiter token")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "would exceed context deadline") {
		t.Fatalf("expected context deadline cancellation behavior, got %v", err)
	}
	if called != 1 {
		t.Fatalf("expected base transport not to be called for canceled request, got %d calls", called)
	}
}
