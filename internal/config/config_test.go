package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minimalValidYAML returns a config.yaml body that satisfies Validate() for
// every field other than confluence.username and confluence.token, which are
// supplied as parameters so tests can exercise the YAML/env precedence rules.
func minimalValidYAML(username, token string) string {
	return fmt.Sprintf(`
confluence:
  username: %q
  token: %q
crawl:
  seeds:
    - https://example.atlassian.net/wiki/spaces/ABC/pages/123/Example
  max_depth: 1
  concurrency: 2
  rate_limit_rpm: 250
  queue_size: 10000
output:
  dir: ./output
retry:
  max_attempts: 3
  initial_backoff_ms: 1000
`, username, token)
}

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestLoad_YAMLOnlyCredentials(t *testing.T) {
	path := writeTempConfig(t, minimalValidYAML("yaml-user@example.com", "yaml-token"))

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
	if cfg.Confluence.Username != "yaml-user@example.com" {
		t.Fatalf("expected YAML username to be used, got: %s", cfg.Confluence.Username)
	}
	if cfg.Confluence.Token != "yaml-token" {
		t.Fatalf("expected YAML token to be used, got: %s", cfg.Confluence.Token)
	}
}

func TestLoad_EnvOnlyCredentials(t *testing.T) {
	path := writeTempConfig(t, minimalValidYAML("", ""))
	t.Setenv("CONFLUENCE_USERNAME", "env-user@example.com")
	t.Setenv("CONFLUENCE_TOKEN", "env-token")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
	if cfg.Confluence.Username != "env-user@example.com" {
		t.Fatalf("expected env username to be used, got: %s", cfg.Confluence.Username)
	}
	if cfg.Confluence.Token != "env-token" {
		t.Fatalf("expected env token to be used, got: %s", cfg.Confluence.Token)
	}
}

func TestLoad_EnvOverridesYAMLCredentials(t *testing.T) {
	path := writeTempConfig(t, minimalValidYAML("yaml-user@example.com", "yaml-token"))
	t.Setenv("CONFLUENCE_USERNAME", "env-user@example.com")
	t.Setenv("CONFLUENCE_TOKEN", "env-token")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
	if cfg.Confluence.Username != "env-user@example.com" {
		t.Fatalf("expected env username to override YAML, got: %s", cfg.Confluence.Username)
	}
	if cfg.Confluence.Token != "env-token" {
		t.Fatalf("expected env token to override YAML, got: %s", cfg.Confluence.Token)
	}
}

func TestLoad_MissingTokenFailsValidation(t *testing.T) {
	path := writeTempConfig(t, minimalValidYAML("yaml-user@example.com", ""))

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for missing token")
	}
	if !strings.Contains(err.Error(), "confluence.token is required") {
		t.Fatalf("expected confluence.token validation error, got: %v", err)
	}
}

func TestLoad_MissingUsernameFailsValidation(t *testing.T) {
	path := writeTempConfig(t, minimalValidYAML("", "yaml-token"))

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for missing username")
	}
	if !strings.Contains(err.Error(), "confluence.username is required") {
		t.Fatalf("expected confluence.username validation error, got: %v", err)
	}
}

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
		Output:        OutputConfig{Dir: "./output"},
		PostCrawlHook: PostCrawlHookConfig{Command: []string{"./scripts/reindex.sh", "--db", "./output/confluence2md-index.db"}},
		Retry: RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMS: 1000,
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
}

func TestValidate_RejectsHookWithWhitespaceOnlyCommand(t *testing.T) {
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
		Output:        OutputConfig{Dir: "./output"},
		PostCrawlHook: PostCrawlHookConfig{Command: []string{"   ", "\t"}},
		Retry: RetryConfig{
			MaxAttempts:      3,
			InitialBackoffMS: 1000,
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "post_crawl_hook.command must include a non-empty executable") {
		t.Fatalf("expected post-crawl hook validation error, got: %s", err.Error())
	}
}
