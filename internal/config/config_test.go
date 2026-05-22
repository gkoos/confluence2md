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
		},
		Output: OutputConfig{Dir: "./output"},
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
		},
		Output: OutputConfig{Dir: "./output"},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
}
