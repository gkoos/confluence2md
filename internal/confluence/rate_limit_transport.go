package confluence

import (
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/time/rate"
)

type rateLimitTransport struct {
	base        http.RoundTripper
	limiter     *rate.Limiter
	allowedHost string
}

func newRateLimitTransport(base http.RoundTripper, rpm, burst int, allowedHost string) *rateLimitTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	if rpm < 1 {
		rpm = 1
	}
	if burst < 1 {
		burst = 1
	}
	if burst > rpm {
		burst = rpm
	}

	requestsPerSecond := float64(rpm) / 60.0

	return &rateLimitTransport{
		base:        base,
		limiter:     rate.NewLimiter(rate.Limit(requestsPerSecond), burst),
		allowedHost: strings.ToLower(strings.TrimSpace(allowedHost)),
	}
}

func (t *rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("rate limit transport: nil request")
	}

	if t.allowedHost != "" && !strings.EqualFold(req.URL.Host, t.allowedHost) {
		return t.base.RoundTrip(req)
	}

	if err := t.limiter.Wait(req.Context()); err != nil {
		return nil, fmt.Errorf("rate limit wait: %w", err)
	}

	return t.base.RoundTrip(req)
}
