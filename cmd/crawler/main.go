package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gkoos/confluence2md/internal/config"
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	mode    string
)

var rootCmd = &cobra.Command{
	Use:   "confluence2md",
	Short: "Crawl a Confluence Cloud instance and export pages to Markdown",
	RunE:  run,
}

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration and exit",
	RunE:  validate,
}

func init() {
	rootCmd.Flags().StringVar(&cfgFile, "config", "config.yaml", "Path to config file")
	rootCmd.Flags().StringVar(&mode, "mode", "", "Crawl mode: full or updates (required)")
	rootCmd.AddCommand(validateCmd)
}

func run(cmd *cobra.Command, args []string) error {
	rc, err := bootstrapRun(mode, cfgFile)
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

	printRunSummary(rc, metrics, finalizeResult)

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
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
