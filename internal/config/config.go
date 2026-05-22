package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Confluence ConfluenceConfig `mapstructure:"confluence"`
	Crawl      CrawlConfig      `mapstructure:"crawl"`
	Output     OutputConfig     `mapstructure:"output"`
	Attachments AttachmentsConfig `mapstructure:"attachments"`
	Retry      RetryConfig      `mapstructure:"retry"`
}

type ConfluenceConfig struct {
	Username string `mapstructure:"username"`
	Token    string `mapstructure:"token"`
}

type CrawlConfig struct {
	Seeds        []string `mapstructure:"seeds"`
	MaxDepth     int      `mapstructure:"max_depth"`
	Concurrency  int      `mapstructure:"concurrency"`
	RateLimitRPM int      `mapstructure:"rate_limit_rpm"`
}

type OutputConfig struct {
	Dir string `mapstructure:"dir"`
}

type AttachmentsConfig struct {
	Download   bool `mapstructure:"download"`
	MaxSizeMB  int  `mapstructure:"max_size_mb"`
}

type RetryConfig struct {
	MaxAttempts      int `mapstructure:"max_attempts"`
	InitialBackoffMS int `mapstructure:"initial_backoff_ms"`
}

// Load reads the config file at the given path and returns a validated Config.
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
	if c.Output.Dir == "" {
		errs = append(errs, "output.dir is required")
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// BaseURL derives the Confluence API base URL from the first seed.
// e.g. https://org.atlassian.net/wiki/spaces/... -> https://org.atlassian.net/wiki
func (c *Config) BaseURL() string {
	u, err := url.Parse(c.Crawl.Seeds[0])
	if err != nil {
		return ""
	}
	// Keep scheme + host + /wiki prefix
	parts := strings.SplitN(u.Path, "/", 4) // ["", "wiki", "spaces", ...]
	if len(parts) >= 3 {
		return fmt.Sprintf("%s://%s/%s", u.Scheme, u.Host, parts[1])
	}
	return fmt.Sprintf("%s://%s", u.Scheme, u.Host)
}
