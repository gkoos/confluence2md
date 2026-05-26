package confluence

import (
	"fmt"
	"net/http"

	"golang.org/x/time/rate"
)

type rateLimitTransport struct {
	base    http.RoundTripper
	limiter *rate.Limiter
}

func newRateLimitTransport(base http.RoundTripper, rpm int) *rateLimitTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	if rpm < 1 {
		rpm = 1
	}

	requestsPerSecond := float64(rpm) / 60.0

	return &rateLimitTransport{
		base:    base,
		limiter: rate.NewLimiter(rate.Limit(requestsPerSecond), 1),
	}
}

func (t *rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("rate limit transport: nil request")
	}

	if err := t.limiter.Wait(req.Context()); err != nil {
		return nil, fmt.Errorf("rate limit wait: %w", err)
	}

	return t.base.RoundTrip(req)
}
