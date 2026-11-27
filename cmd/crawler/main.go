package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gkoos/confluence2md/internal/config"
	"github.com/gkoos/confluence2md/internal/events"
	"github.com/spf13/cobra"
)

// version is the build version, injected at release time via
// -ldflags "-X main.version={{.Version}}". Local/dev builds report "dev".
var version = "dev"

var (
	cfgFile   string
	mode      string
	dryRun    bool
	logFormat string
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
	rootCmd.PersistentFlags().StringVar(&logFormat, "log-format", "text", "Output format: text, or json (one event per line on stdout, human-readable text on stderr)")
	rootCmd.AddCommand(validateCmd)
}

func run(cmd *cobra.Command, args []string) error {
	if err := configureLogFormat(logFormat); err != nil {
		return err
	}
	fmt.Printf("confluence2md version %s\n", version)
	events.Emit("run_started", map[string]any{"version": version, "mode": mode, "dry_run": dryRun})

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	go func() {
		// Restore default handling after the first signal so a second one ends the process.
		<-signalCtx.Done()
		stopSignals()
	}()

	rc, err := bootstrapRun(signalCtx, mode, cfgFile, dryRun)
	if err != nil {
		return interruptedOr(signalCtx, err)
	}

	ctx, cancel := context.WithTimeout(signalCtx, rc.cfg.Crawl.Timeout())
	defer cancel()

	if err := executeTraversal(ctx, rc); err != nil {
		return interruptedOr(signalCtx, err)
	}

	metrics := &runMetrics{}
	if err := processTraversalResults(ctx, rc, metrics); err != nil {
		return interruptedOr(signalCtx, err)
	}

	// Nothing is finalized after an interrupt, so the checkpoint is never advanced.
	if signalCtx.Err() != nil {
		return interruptedOr(signalCtx, signalCtx.Err())
	}

	// A signal from here on is not acted on: stopping partway would leave pages written
	// without their metadata.json.
	finalizeResult, err := finalizeRun(rc, metrics)
	if err != nil {
		return err
	}

	elapsed := time.Since(startTime)
	printRunSummary(rc, metrics, finalizeResult, elapsed)
	if metrics.errorCount > 0 {
		// The hook is documented to follow a successful crawl only.
		return partialCrawlError(metrics.errorCount)
	}
	runPostCrawlHook(rc)
	return nil
}

func validate(cmd *cobra.Command, args []string) error {
	if err := configureLogFormat(logFormat); err != nil {
		return err
	}
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return withExitCode(exitConfig, fmt.Errorf("config error: %w", err))
	}

	printConfigSummary(cfg)

	auth, err := resolveConfluenceAuth(context.Background(), cfg)
	if err != nil {
		return err
	}
	printCredentialStyles(auth)

	fmt.Println("\nValidation successful, exiting.")

	return nil
}

func main() {
	startTime = time.Now()
	if err := rootCmd.Execute(); err != nil {
		reportFailure(err)
		os.Exit(exitCodeFor(err))
	}
}

// reportFailure emits run_failed when JSON events were requested.
func reportFailure(err error) {
	// Flag parsing can fail before any command enabled the events the user asked for.
	if logFormat == "json" && !events.Enabled() {
		_ = configureLogFormat(logFormat)
	}
	events.Emit("run_failed", map[string]any{"message": err.Error(), "exit_code": exitCodeFor(err)})
}
