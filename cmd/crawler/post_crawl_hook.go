package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

func runPostCrawlHook(rc *runContext) {
	runPostCrawlHookWithWriter(rc, os.Stdout)
}

func runPostCrawlHookWithWriter(rc *runContext, out io.Writer) {
	if rc == nil || rc.cfg == nil || out == nil {
		return
	}

	if rc.dryRun {
		return
	}

	command := normalizePostCrawlHookCommand(rc.cfg.PostCrawlHook.Command)
	if len(command) == 0 {
		return
	}

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		_, _ = fmt.Fprintf(out, "[WARN] post-crawl hook failed to start: %v\n", err)
		return
	}

	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		_, _ = fmt.Fprintf(out, "[WARN] post-crawl hook started (pid=%d), but process release failed: %v\n", pid, err)
		return
	}

	_, _ = fmt.Fprintf(out, "Post-crawl hook handed off (pid=%d): %s\n", pid, strings.Join(command, " "))
}

func normalizePostCrawlHookCommand(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}

	out := make([]string, 0, len(raw))
	for _, token := range raw {
		trimmed := strings.TrimSpace(token)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}

	if len(out) == 0 {
		return nil
	}

	return out
}
