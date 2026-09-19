package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gkoos/confluence2md/internal/config"
	"github.com/spf13/cobra"
)

// version is the build version, injected at release time via
// -ldflags "-X main.version={{.Version}}". Local/dev builds report "dev".
var version = "dev"

var (
	cfgFile   string
	mode      string
	dryRun    bool
	startTime time.Time
)

var rootCmd = &cobra.Command{
	Use:     "confluence2md",
	Short:   "Crawl a Confluence Cloud instance and export pages to Markdown",
	Version: version,
	RunE:    run,
}

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration and exit",
	RunE:  validate,
}

func init() {
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.Flags().StringVar(&cfgFile, "config", "config.yaml", "Path to config file")
	rootCmd.Flags().StringVar(&mode, "mode", "", "Crawl mode: full or updates (required)")
	rootCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview crawl scope and decisions without writing output artifacts")
	rootCmd.AddCommand(validateCmd)
}

func run(cmd *cobra.Command, args []string) error {
	fmt.Printf("confluence2md version %s\n", version)

	rc, err := bootstrapRun(mode, cfgFile, dryRun)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if err := executeTraversal(ctx, rc); err != nil {
		return err
	}

	metrics := &runMetrics{}
	if err := processTraversalResults(ctx, rc, metrics); err != nil {
		return err
	}

	finalizeResult, err := finalizeRun(rc, metrics)
	if err != nil {
		return err
	}

	elapsed := time.Since(startTime)
	printRunSummary(rc, metrics, finalizeResult, elapsed)
	runPostCrawlHook(rc)
	return nil
}

func validate(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	printConfigSummary(cfg)

	client, err := newConfluenceClient(cfg)
	if err != nil {
		return err
	}

	if err := verifyConfluenceAccess(client); err != nil {
		return err
	}

	fmt.Println("\nValidation successful, exiting.")

	return nil
}

func main() {
	startTime = time.Now()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
