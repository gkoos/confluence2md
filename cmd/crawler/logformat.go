package main

import (
	"fmt"
	"os"

	"github.com/gkoos/confluence2md/internal/events"
)

// configureLogFormat switches to JSON events when asked. Stdout then carries
// only events, and the existing human-readable text moves to stderr.
func configureLogFormat(format string) error {
	switch format {
	case "", "text":
		return nil
	case "json":
		events.Enable(os.Stdout)
		os.Stdout = os.Stderr
		return nil
	default:
		return withExitCode(exitConfig, fmt.Errorf("--log-format must be 'text' or 'json', got %q", format))
	}
}
