package confluence

import (
	"errors"
	"io"
	"net"
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
		if !shouldRetry(attemptReq, resp, err) || attempt == t.maxAttempts {
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

// shouldRetry reports whether the attempt should be repeated.
//
// Status-based transients (429 and the 5xx set below) are retried as before.
// Transport errors are classified rather than blanket-retried: only failures that
// can plausibly be transient (client timeouts, connection-level errors, EOF,
// non-NXDOMAIN resolver failures) are retried, so permanent failures such as TLS
// verification errors, malformed requests or an unknown host are returned to the
// caller immediately instead of costing maxAttempts round trips.
func shouldRetry(req *http.Request, resp *http.Response, err error) bool {
	// A finished caller context cannot be retried and retrying would only delay
	// cancellation/deadline propagation.
	if req != nil && req.Context().Err() != nil {
		return false
	}

	if err != nil {
		return isRetryableTransportError(err)
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

// isRetryableTransportError classifies a RoundTrip error. Anything not recognised
// as transient is treated as permanent, which keeps the retry budget for failures
// that might actually succeed on a second attempt.
func isRetryableTransportError(err error) bool {
	// The connection closed before a response was received.
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	// Client timeout, dial timeout, read/write deadline. A caller-driven deadline
	// never reaches this point: shouldRetry rejects a finished request context.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// DNS: NXDOMAIN is permanent, other resolver failures are usually transient.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return !dnsErr.IsNotFound
	}

	// Connection-level failures (reset, refused, aborted, broken pipe).
	var opErr *net.OpError
	return errors.As(err, &opErr)
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
