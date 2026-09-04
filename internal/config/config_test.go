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

func baseValidConfig() *Config {
	return &Config{
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
}

func TestValidate_AuthModeEnum(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		wantErr bool
	}{
		{name: "empty defaults to auto, accepted", mode: "", wantErr: false},
		{name: "auto accepted", mode: "auto", wantErr: false},
		{name: "classic accepted", mode: "classic", wantErr: false},
		{name: "scoped accepted", mode: "scoped", wantErr: false},
		{name: "case-insensitive and trimmed", mode: "  ScOpEd  ", wantErr: false},
		{name: "unrecognized value rejected", mode: "bogus", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseValidConfig()
			cfg.Confluence.AuthMode = tc.mode
			err := cfg.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected validation error for auth_mode %q", tc.mode)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no validation error for auth_mode %q, got: %v", tc.mode, err)
			}
			if tc.wantErr && !strings.Contains(err.Error(), "confluence.auth_mode must be one of") {
				t.Fatalf("expected auth_mode validation message, got: %v", err)
			}
		})
	}
}

func TestValidate_CloudIDMustBeUUID(t *testing.T) {
	cfg := baseValidConfig()
	cfg.Confluence.AuthMode = "scoped"
	cfg.Confluence.CloudID = "not-a-uuid"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for non-UUID cloud_id")
	}
	if !strings.Contains(err.Error(), "confluence.cloud_id must be a valid UUID") {
		t.Fatalf("expected cloud_id validation message, got: %v", err)
	}
}

func TestValidate_EmptyCloudIDAccepted(t *testing.T) {
	cfg := baseValidConfig()
	// cloud_id absent entirely (auto-resolve case) must remain valid.
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config with empty cloud_id, got: %v", err)
	}
}

func TestValidate_ValidCloudIDAccepted(t *testing.T) {
	cfg := baseValidConfig()
	cfg.Confluence.AuthMode = "scoped"
	cfg.Confluence.CloudID = "11111111-1111-1111-1111-111111111111"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config with UUID cloud_id, got: %v", err)
	}
}

func TestEffectiveAuthMode_DefaultsToAuto(t *testing.T) {
	cfg := baseValidConfig()
	if got := cfg.EffectiveAuthMode(); got != AuthModeAuto {
		t.Fatalf("expected default auth mode %q, got %q", AuthModeAuto, got)
	}

	cfg.Confluence.AuthMode = "  Classic  "
	if got := cfg.EffectiveAuthMode(); got != AuthModeClassic {
		t.Fatalf("expected normalized auth mode %q, got %q", AuthModeClassic, got)
	}
}

func TestSiteURLAndAPIBaseURL_PerMode(t *testing.T) {
	const wantSite = "https://example.atlassian.net/wiki"

	t.Run("classic mode: API base equals site URL", func(t *testing.T) {
		cfg := baseValidConfig()
		cfg.Confluence.AuthMode = "classic"
		if got := cfg.SiteURL(); got != wantSite {
			t.Fatalf("SiteURL() = %q, want %q", got, wantSite)
		}
		if got := cfg.APIBaseURL(); got != wantSite {
			t.Fatalf("APIBaseURL() = %q, want %q (classic mode must match SiteURL)", got, wantSite)
		}
	})

	t.Run("auto mode: API base equals site URL (today's behaviour)", func(t *testing.T) {
		cfg := baseValidConfig()
		// AuthMode left empty -> auto.
		if got := cfg.APIBaseURL(); got != wantSite {
			t.Fatalf("APIBaseURL() = %q, want %q", got, wantSite)
		}
	})

	t.Run("scoped mode with configured cloud_id: gateway base, no /wiki suffix", func(t *testing.T) {
		cfg := baseValidConfig()
		cfg.Confluence.AuthMode = "scoped"
		cfg.Confluence.CloudID = "11111111-1111-1111-1111-111111111111"

		if got := cfg.SiteURL(); got != wantSite {
			t.Fatalf("SiteURL() = %q, want %q (must never vary with AuthMode, research R5)", got, wantSite)
		}

		wantAPI := "https://api.atlassian.com/ex/confluence/11111111-1111-1111-1111-111111111111"
		if got := cfg.APIBaseURL(); got != wantAPI {
			t.Fatalf("APIBaseURL() = %q, want %q", got, wantAPI)
		}
		if strings.HasSuffix(cfg.APIBaseURL(), "/wiki") {
			t.Fatalf("APIBaseURL() must not carry a /wiki suffix in scoped mode, got %q", cfg.APIBaseURL())
		}
	})

	t.Run("scoped mode without configured cloud_id: falls back to site URL pending runtime resolution", func(t *testing.T) {
		cfg := baseValidConfig()
		cfg.Confluence.AuthMode = "scoped"
		// cloud_id intentionally left empty: config.Config cannot perform the
		// network resolution itself (see APIBaseURL doc comment).
		if got := cfg.APIBaseURL(); got != wantSite {
			t.Fatalf("APIBaseURL() = %q, want %q", got, wantSite)
		}
	})
}

func TestLoad_LegacyConfigWithoutAuthModeOrCloudID_LoadsAndYieldsClassicBases(t *testing.T) {
	// A config.yaml written before this feature has neither key. It must
	// still load, validate, and produce classic-mode bases (FR-002, FR-018,
	// SC-003) — this is the back-compat contract this feature must not break.
	path := writeTempConfig(t, minimalValidYAML("yaml-user@example.com", "yaml-token"))

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected legacy config to load, got: %v", err)
	}
	if cfg.Confluence.AuthMode != "" {
		t.Fatalf("expected AuthMode to be absent/empty in a legacy config, got %q", cfg.Confluence.AuthMode)
	}
	if cfg.Confluence.CloudID != "" {
		t.Fatalf("expected CloudID to be absent/empty in a legacy config, got %q", cfg.Confluence.CloudID)
	}
	if got := cfg.EffectiveAuthMode(); got != AuthModeAuto {
		t.Fatalf("expected legacy config to resolve to auto mode, got %q", got)
	}
	if cfg.APIBaseURL() != cfg.SiteURL() {
		t.Fatalf("expected legacy config's APIBaseURL to equal SiteURL (classic-equivalent), got API=%q site=%q", cfg.APIBaseURL(), cfg.SiteURL())
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
