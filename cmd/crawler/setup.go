package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gkoos/confluence2md/internal/config"
	confluenceclient "github.com/gkoos/confluence2md/internal/confluence"
)

func printConfigSummary(cfg *config.Config) {
	fmt.Println("Config loaded successfully")
	fmt.Printf("  Base URL:    %s\n", cfg.BaseURL())
	fmt.Printf("  Username:    %s\n", cfg.Confluence.Username)
	fmt.Printf("  Seeds:       %v\n", cfg.Crawl.Seeds)
	fmt.Printf("  Max depth:   %d\n", cfg.Crawl.MaxDepth)
	fmt.Printf("  Concurrency: %d\n", cfg.Crawl.Concurrency)
	fmt.Printf("  Output dir:  %s\n", cfg.Output.Dir)
}

func clearDirectoryContents(dir string) error {
	cleanDir := filepath.Clean(strings.TrimSpace(dir))
	if cleanDir == "" || cleanDir == "." || cleanDir == string(filepath.Separator) {
		return fmt.Errorf("refusing to clear unsafe directory path %q", dir)
	}

	if err := os.MkdirAll(cleanDir, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	entries, err := os.ReadDir(cleanDir)
	if err != nil {
		return fmt.Errorf("read output directory: %w", err)
	}

	for _, entry := range entries {
		entryPath := filepath.Join(cleanDir, entry.Name())
		if err := os.RemoveAll(entryPath); err != nil {
			return fmt.Errorf("remove %s: %w", entryPath, err)
		}
	}

	return nil
}

func newConfluenceClient(cfg *config.Config) (*confluenceclient.Client, error) {
	client, err := confluenceclient.NewClient(cfg.BaseURL(), cfg.Confluence.Username, cfg.Confluence.Token, cfg.Retry, cfg.Crawl.RateLimitRPM)
	if err != nil {
		return nil, fmt.Errorf("creating Confluence client: %w", err)
	}
	return client, nil
}

func verifyConfluenceAccess(client *confluenceclient.Client) error {
	fmt.Println("\nChecking Confluence API access...")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		return err
	}

	fmt.Println("Confluence API access check passed.")
	return nil
}

func extractSpaceKeyFromSeed(seed string) string {
	parsed, err := url.Parse(seed)
	if err != nil {
		return ""
	}

	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "spaces" {
			return parts[i+1]
		}
	}

	return ""
}

// extractSeedPageIDs converts seed URLs to numeric page IDs
func extractSeedPageIDs(client *confluenceclient.Client, seeds []string) ([]int64, error) {
	var pageIDs []int64
	seen := make(map[int64]bool)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, seed := range seeds {
		page, err := client.GetPageBySeed(ctx, seed)
		if err != nil {
			return nil, fmt.Errorf("resolve seed %q: %w", seed, err)
		}

		id, err := strconv.ParseInt(page.ID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid page ID %q: %w", page.ID, err)
		}

		if !seen[id] {
			seen[id] = true
			pageIDs = append(pageIDs, id)
		}
	}

	return pageIDs, nil
}
