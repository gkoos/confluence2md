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
	"github.com/gkoos/confluence2md/internal/store"
)

func printConfigSummary(cfg *config.Config) {
	fmt.Println("Config loaded successfully")
	fmt.Printf("  Site URL:    %s\n", cfg.SiteURL())
	fmt.Printf("  Username:    %s\n", cfg.Confluence.Username)
	fmt.Printf("  Seeds:       %v\n", cfg.Crawl.Seeds)
	fmt.Printf("  Max depth:      %d\n", cfg.Crawl.MaxDepth)
	fmt.Printf("  Concurrency:    %d\n", cfg.Crawl.Concurrency)
	fmt.Printf("  Follow children: %v\n", cfg.Crawl.FollowChildren)
	fmt.Printf("  Output dir:     %s\n", cfg.Output.Dir)
}

// printCredentialStyles reports the resolved credential style in the
// pre-crawl summary (FR-005), including the resolved cloud ID in scoped mode
// so an operator can cross-check it, but never any credential material. A
// single-host crawl keeps the previous unqualified format.
func printCredentialStyles(auth *confluenceclient.AuthResolutions) {
	if len(auth.Order) == 1 {
		r := auth.ForHost(auth.Order[0])
		fmt.Printf("  Credential style: %s\n", r.Mode)
		if r.Mode == config.AuthModeScoped {
			fmt.Printf("  Cloud ID:         %s\n", r.CloudID)
		}
		return
	}
	for _, host := range auth.Order {
		r := auth.ForHost(host)
		fmt.Printf("  Credential style: %s (host %s)\n", r.Mode, host)
		if r.Mode == config.AuthModeScoped {
			fmt.Printf("  Cloud ID:         %s (%s)\n", r.CloudID, host)
		}
	}
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

// resolveConfluenceAuth determines the credential style (auto/classic/scoped)
// and returns a Client already validated against the same endpoint the
// crawl's first real request will use (FR-008, SC-002). This replaces the
// old two-step "construct client, then Ping" flow: mode resolution *is* the
// validation probe now, since which base to construct the client against is
// exactly what's being resolved.
func resolveConfluenceAuth(cfg *config.Config) (*confluenceclient.AuthResolutions, error) {
	fmt.Println("\nResolving Confluence credential style and checking API access...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	auth, err := confluenceclient.ResolveAuth(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("confluence credential validation failed: %w", err)
	}

	fmt.Println("Confluence API access check passed.")
	return auth, nil
}

// extractSpaceKeyFromSeed extracts the alphanumeric space key (e.g., "SFD", "DS")
// from a Confluence URL. This key is used for metadata, front matter, and CQL queries.
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

// extractSeedPageRefs converts seed URLs to host-scoped page references,
// resolving each seed through its own host's client.
func extractSeedPageRefs(clients *confluenceclient.ClientSet, seeds []string) ([]store.PageRef, error) {
	var refs []store.PageRef
	seen := make(map[string]bool)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, seed := range seeds {
		host := config.HostForSeed(seed)
		client := clients.ForHost(host)
		if client == nil {
			return nil, fmt.Errorf("resolve seed %q: no client for host %q", seed, host)
		}

		page, err := client.GetPageBySeed(ctx, seed)
		if err != nil {
			return nil, fmt.Errorf("resolve seed %q: %w", seed, err)
		}

		id, err := strconv.ParseInt(page.ID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid page ID %q: %w", page.ID, err)
		}

		key := store.PageKey(host, id)
		if !seen[key] {
			seen[key] = true
			refs = append(refs, store.PageRef{Host: host, ID: id})
		}
	}

	return refs, nil
}
