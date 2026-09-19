package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/viper"
)

// gatewayBaseURL is the fixed host Atlassian serves scoped API tokens from.
// See research.md R1: scoped tokens are never valid against the tenant's own
// domain, only against this central gateway.
const gatewayBaseURL = "https://api.atlassian.com/ex/confluence"

// Accepted values for confluence.auth_mode. See data-model.md "Entity: AuthMode".
const (
	AuthModeAuto    = "auto"
	AuthModeClassic = "classic"
	AuthModeScoped  = "scoped"
)

type Config struct {
	Confluence    ConfluenceConfig    `mapstructure:"confluence"`
	Crawl         CrawlConfig         `mapstructure:"crawl"`
	Output        OutputConfig        `mapstructure:"output"`
	Attachments   AttachmentsConfig   `mapstructure:"attachments"`
	PostCrawlHook PostCrawlHookConfig `mapstructure:"post_crawl_hook"`
	Retry         RetryConfig         `mapstructure:"retry"`
}

type ConfluenceConfig struct {
	Username string `mapstructure:"username"`
	Token    string `mapstructure:"token"`
	// AuthMode selects which Atlassian credential style requests are routed
	// for: "auto" (default, probed at startup), "classic", or "scoped". See
	// data-model.md "Entity: AuthMode".
	AuthMode string `mapstructure:"auth_mode"`
	// CloudID overrides automatic cloud-ID resolution (confluence/_edge/tenant_info)
	// for scoped mode. Optional; only used when the effective mode is "scoped".
	CloudID string `mapstructure:"cloud_id"`
}

type CrawlConfig struct {
	Seeds          []string `mapstructure:"seeds"`
	MaxDepth       int      `mapstructure:"max_depth"`
	Concurrency    int      `mapstructure:"concurrency"`
	RateLimitRPM   int      `mapstructure:"rate_limit_rpm"`
	QueueSize      int      `mapstructure:"queue_size"`
	FollowChildren bool     `mapstructure:"follow_children"`
}

type OutputConfig struct {
	Dir string `mapstructure:"dir"`
}

type AttachmentsConfig struct {
	Download  bool `mapstructure:"download"`
	MaxSizeMB int  `mapstructure:"max_size_mb"`
}

type RetryConfig struct {
	MaxAttempts      int `mapstructure:"max_attempts"`
	InitialBackoffMS int `mapstructure:"initial_backoff_ms"`
}

type PostCrawlHookConfig struct {
	Command []string `mapstructure:"command"`
}

// Load reads the config file at the given path and returns a validated Config.
//
// CONFLUENCE_TOKEN and CONFLUENCE_USERNAME, if set to a non-empty value,
// override confluence.token and confluence.username from the YAML file
// respectively. This lets credentials be kept out of config.yaml entirely.
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if envToken := os.Getenv("CONFLUENCE_TOKEN"); envToken != "" {
		cfg.Confluence.Token = envToken
	}
	if envUsername := os.Getenv("CONFLUENCE_USERNAME"); envUsername != "" {
		cfg.Confluence.Username = envUsername
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// Validate checks that all required fields are present and consistent.
func (c *Config) Validate() error {
	var errs []string

	if c.Confluence.Username == "" {
		errs = append(errs, "confluence.username is required")
	}
	if c.Confluence.Token == "" {
		errs = append(errs, "confluence.token is required")
	}
	if len(c.Crawl.Seeds) == 0 {
		errs = append(errs, "crawl.seeds must contain at least one URL")
	}

	for i, seed := range c.Crawl.Seeds {
		u, err := url.Parse(seed)
		if err != nil || u.Scheme == "" || u.Host == "" {
			errs = append(errs, fmt.Sprintf("crawl.seeds[%d] is not a valid URL: %s", i, seed))
		}
	}

	if c.Crawl.MaxDepth < 0 {
		errs = append(errs, "crawl.max_depth must be >= 0")
	}
	if c.Crawl.Concurrency <= 0 {
		errs = append(errs, "crawl.concurrency must be > 0")
	}
	if c.Crawl.RateLimitRPM <= 0 {
		errs = append(errs, "crawl.rate_limit_rpm must be > 0")
	}
	if c.Crawl.QueueSize <= 0 {
		errs = append(errs, "crawl.queue_size must be > 0")
	}
	if c.Output.Dir == "" {
		errs = append(errs, "output.dir is required")
	}
	if c.Retry.MaxAttempts < 1 {
		errs = append(errs, "retry.max_attempts must be >= 1")
	}
	if c.Retry.InitialBackoffMS < 1 {
		errs = append(errs, "retry.initial_backoff_ms must be >= 1")
	}

	if mode := strings.ToLower(strings.TrimSpace(c.Confluence.AuthMode)); mode != "" &&
		mode != AuthModeAuto && mode != AuthModeClassic && mode != AuthModeScoped {
		errs = append(errs, fmt.Sprintf("confluence.auth_mode must be one of %q, %q, %q (got %q)", AuthModeAuto, AuthModeClassic, AuthModeScoped, c.Confluence.AuthMode))
	}
	if cloudID := strings.TrimSpace(c.Confluence.CloudID); cloudID != "" {
		if _, err := uuid.Parse(cloudID); err != nil {
			errs = append(errs, fmt.Sprintf("confluence.cloud_id must be a valid UUID (got %q)", c.Confluence.CloudID))
		}
	}

	if len(c.PostCrawlHook.Command) > 0 {
		hasExecutable := false
		for _, token := range c.PostCrawlHook.Command {
			if strings.TrimSpace(token) != "" {
				hasExecutable = true
				break
			}
		}
		if !hasExecutable {
			errs = append(errs, "post_crawl_hook.command must include a non-empty executable")
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// EffectiveAuthMode normalizes confluence.auth_mode (trimmed, lower-cased)
// and returns AuthModeAuto when it is absent, so an empty/legacy config
// resolves to the auto-probe path — the property that keeps a pre-feature
// config.yaml behaving exactly as before (FR-018, SC-003).
func (c *Config) EffectiveAuthMode() string {
	mode := strings.ToLower(strings.TrimSpace(c.Confluence.AuthMode))
	if mode == "" {
		return AuthModeAuto
	}
	return mode
}

// SiteURL derives the Confluence tenant URL from the first seed, e.g.
// https://org.atlassian.net/wiki/spaces/... -> https://org.atlassian.net/wiki
//
// This value is used for anything that lands in an emitted artifact — the
// canonical webui URL, link-scope host matching, link absolutisation — and
// MUST NOT vary with AuthMode or credential style (research R5, FR-013).
// Everything that issues an actual API request must use APIBaseURL instead.
//
// For multi-host crawls this returns the primary (first seed) site only; use
// SiteURLs/SiteHosts to enumerate every distinct site.
func (c *Config) SiteURL() string {
	return siteURLFromSeed(c.Crawl.Seeds[0])
}

// siteURLFromSeed derives the tenant site URL from a seed URL, e.g.
// https://org.atlassian.net/wiki/spaces/... -> https://org.atlassian.net/wiki.
func siteURLFromSeed(seed string) string {
	u, err := url.Parse(seed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	// Keep scheme + host + /wiki prefix
	parts := strings.SplitN(u.Path, "/", 4) // ["", "wiki", "spaces", ...]
	if len(parts) >= 3 {
		return fmt.Sprintf("%s://%s/%s", u.Scheme, u.Host, parts[1])
	}
	return fmt.Sprintf("%s://%s", u.Scheme, u.Host)
}

// SiteURLs returns the distinct site URLs across all seeds, in seed order.
func (c *Config) SiteURLs() []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(c.Crawl.Seeds))
	for _, seed := range c.Crawl.Seeds {
		s := siteURLFromSeed(seed)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// SiteHosts returns the distinct lower-cased tenant hosts across all seeds,
// in seed order.
func (c *Config) SiteHosts() []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(c.Crawl.Seeds))
	for _, site := range c.SiteURLs() {
		u, err := url.Parse(site)
		if err != nil || u.Host == "" {
			continue
		}
		h := strings.ToLower(u.Host)
		if seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	return out
}

// HostForSeed returns the lower-cased tenant host for a seed URL.
func HostForSeed(seed string) string {
	u, err := url.Parse(seed)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

// APIBaseURL returns the request target for API calls: the site URL in
// classic mode, or the Atlassian gateway URL (no /wiki suffix — see
// contracts/api-endpoints.md) in scoped mode.
//
// This accessor only has enough information to resolve the scoped-mode base
// when confluence.cloud_id is explicitly configured (FR-007). When the
// effective mode is "scoped" but cloud_id is empty, the cloud ID must be
// resolved at runtime over the network (internal/confluence.ResolveCloudID);
// that resolution and the resulting gateway URL are owned by the confluence
// package's auth-mode resolver, not by this method, since config.Config must
// not perform network I/O. Callers that have already resolved a cloud ID
// should build the gateway URL directly rather than relying on this method.
func (c *Config) APIBaseURL() string {
	if c.EffectiveAuthMode() == AuthModeScoped {
		if cloudID := strings.TrimSpace(c.Confluence.CloudID); cloudID != "" {
			return gatewayBaseURL + "/" + cloudID
		}
	}
	return c.SiteURL()
}
