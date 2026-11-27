package main

import (
	"errors"
	"fmt"
)

// Exit codes let a script tell why a run failed without parsing output.
const (
	exitOK      = 0
	exitFailure = 1 // any failure not covered below
	exitConfig  = 2 // configuration or flags rejected before any request was made
	exitAuth    = 3 // Confluence rejected the credentials or the token lacks permission
	exitPartial = 4 // crawl finished but some pages failed; the checkpoint was not advanced
)

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

func withExitCode(code int, err error) error {
	if err == nil {
		return nil
	}
	return &exitError{code: code, err: err}
}

func exitCodeFor(err error) int {
	if err == nil {
		return exitOK
	}
	var coded *exitError
	if errors.As(err, &coded) {
		return coded.code
	}
	return exitFailure
}

func partialCrawlError(failedPages int) error {
	return withExitCode(exitPartial, fmt.Errorf("%d page(s) failed; output is incomplete and the successful-crawl checkpoint was not advanced", failedPages))
}
