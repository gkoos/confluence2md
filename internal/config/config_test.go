package config

import (
	"strings"
	"testing"
)

func TestValidate_RejectsNonPositiveConcurrencyAndRateLimit(t *testing.T) {
	cfg := &Config{
		Confluence: ConfluenceConfig{
			Username: "user@example.com",
			Token:    "token",
		},
		Crawl: CrawlConfig{
			Seeds:        []string{"https://example.atlassian.net/wiki/spaces/ABC/pages/123/Example"},
			MaxDepth:     1,
			Concurrency:  0,
			RateLimitRPM: 0,
			QueueSize:    0,
		},
		Output: OutputConfig{Dir: "./output"},
		Retry: RetryConfig{
			MaxAttempts:      0,
			InitialBackoffMS: 0,
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected validation error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "crawl.concurrency must be > 0") {
		t.Fatalf("expected concurrency validation error, got: %s", msg)
	}
	if !strings.Contains(msg, "crawl.rate_limit_rpm must be > 0") {
		t.Fatalf("expected rate_limit_rpm validation error, got: %s", msg)
	}
	if !strings.Contains(msg, "crawl.queue_size must be > 0") {
		t.Fatalf("expected queue_size validation error, got: %s", msg)
	}
	if !strings.Contains(msg, "retry.max_attempts must be >= 1") {
		t.Fatalf("expected retry.max_attempts validation error, got: %s", msg)
	}
	if !strings.Contains(msg, "retry.initial_backoff_ms must be >= 1") {
		t.Fatalf("expected retry.initial_backoff_ms validation error, got: %s", msg)
	}
}

func TestValidate_AcceptsPositiveConcurrencyAndRateLimit(t *testing.T) {
	cfg := &Config{
		Confluence: ConfluenceConfig{
			Username: "user@example.com",
			Token:    "token",
		},
		Crawl: CrawlConfig{
			Seeds:        []string{"https://example.atlassian.net/wiki/spaces/ABC/pages/123/Example"},
			MaxDepth:     1,
			Concurrency:  2,
			RateLimitRPM: 250,
			QueueSize:    10000,
		},
		Output: OutputConfig{Dir: "./output"},
		Retry: RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMS: 1000,
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
}
