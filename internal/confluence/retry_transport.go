package confluence

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type retryTransport struct {
	base             http.RoundTripper
	maxAttempts      int
	initialBackoffMS int
}

func newRetryTransport(base http.RoundTripper, maxAttempts, initialBackoffMS int) *retryTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if initialBackoffMS < 1 {
		initialBackoffMS = 1
	}

	return &retryTransport{
		base:             base,
		maxAttempts:      maxAttempts,
		initialBackoffMS: initialBackoffMS,
	}
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("retry transport: nil request")
	}

	var lastResp *http.Response
	var lastErr error

	for attempt := 1; attempt <= t.maxAttempts; attempt++ {
		attemptReq, err := cloneRequestForAttempt(req, attempt)
		if err != nil {
			return nil, err
		}

		resp, err := t.base.RoundTrip(attemptReq)
		if !shouldRetry(resp, err) || attempt == t.maxAttempts {
			return resp, err
		}

		// Close response body before retrying to avoid leaking connections.
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}

		lastResp = resp
		lastErr = err

		wait := retryDelay(resp, attempt, t.initialBackoffMS)
		if wait <= 0 {
			continue
		}

		timer := time.NewTimer(wait)
		select {
		case <-attemptReq.Context().Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, attemptReq.Context().Err()
		case <-timer.C:
		}
	}

	return lastResp, lastErr
}

func cloneRequestForAttempt(req *http.Request, attempt int) (*http.Request, error) {
	if attempt == 1 {
		return req.Clone(req.Context()), nil
	}

	cloned := req.Clone(req.Context())
	if req.Body == nil {
		return cloned, nil
	}
	if req.GetBody == nil {
		return nil, errors.New("retry transport: cannot retry request with non-replayable body")
	}

	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	cloned.Body = body
	return cloned, nil
}

func shouldRetry(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}
	if resp == nil {
		return false
	}

	switch resp.StatusCode {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func retryDelay(resp *http.Response, attempt, initialBackoffMS int) time.Duration {
	if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
		if d := parseRetryAfter(resp.Header.Get("Retry-After")); d > 0 {
			return d
		}
	}

	base := time.Duration(initialBackoffMS) * time.Millisecond
	shift := min(attempt-1, 10)
	delay := base * time.Duration(1<<shift)

	// Add bounded jitter (+/-10%) to reduce synchronized retries.
	window := delay / 10
	if window <= 0 {
		return delay
	}

	span := int64(2*window) + 1
	jitter := time.Duration((time.Now().UnixNano() % span) - int64(window))
	return delay + jitter
}

func parseRetryAfter(value string) time.Duration {
	v := strings.TrimSpace(value)
	if v == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(v); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}

	if ts, err := http.ParseTime(v); err == nil {
		d := time.Until(ts)
		if d > 0 {
			return d
		}
	}

	return 0
}
