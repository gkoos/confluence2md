package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/gkoos/confluence2md/internal/events"
)

func TestReportFailureHonoursJSONWhenFlagParsingFailedFirst(t *testing.T) {
	original := os.Stdout
	t.Cleanup(func() {
		os.Stdout = original
		events.Enable(nil)
		logFormat = "text"
	})

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = writer

	// --log-format json was parsed, then an unknown flag stopped the command before run().
	logFormat = "json"
	reportFailure(withExitCode(exitConfig, errors.New("unknown flag: --nope")))
	_ = writer.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(reader); err != nil {
		t.Fatalf("read: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"event":"run_failed"`) || !strings.Contains(out, `"exit_code":2`) {
		t.Fatalf("run_failed was not written to stdout: %q", out)
	}
}

func TestWarnLevelIsAWarningEventAndEventsCarryTheHost(t *testing.T) {
	var buf bytes.Buffer
	events.Enable(&buf)
	t.Cleanup(func() { events.Enable(nil) })

	logPageWithLevel("WARN", "a.example.test", 7, "comments failed: %v", "boom")
	logPageWithLevel("OK", "b.example.test", 7, "fine")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d events, want 2: %q", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], `"event":"warning"`) || !strings.Contains(lines[0], `"host":"a.example.test"`) {
		t.Fatalf("WARN must be a warning event with its host: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"event":"page_processed"`) || !strings.Contains(lines[1], `"host":"b.example.test"`) {
		t.Fatalf("OK must be a page_processed event with its host: %s", lines[1])
	}
}

func TestConfigureLogFormatRejectsUnknownFormat(t *testing.T) {
	err := configureLogFormat("yaml")
	if exitCodeFor(err) != exitConfig {
		t.Fatalf("exit code = %d, want %d (err: %v)", exitCodeFor(err), exitConfig, err)
	}
}

func TestConfigureLogFormatTextLeavesOutputAlone(t *testing.T) {
	original := os.Stdout
	if err := configureLogFormat("text"); err != nil {
		t.Fatalf("text: %v", err)
	}
	if os.Stdout != original || events.Enabled() {
		t.Fatal("text format must not touch stdout or enable events")
	}
}

func TestConfigureLogFormatJSONSendsEventsToStdoutAndTextToStderr(t *testing.T) {
	original := os.Stdout
	t.Cleanup(func() {
		os.Stdout = original
		events.Enable(nil)
	})

	// Stand in for the real stdout so the test can read what is written to it.
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = writer

	if err := configureLogFormat("json"); err != nil {
		t.Fatalf("json: %v", err)
	}
	if os.Stdout != os.Stderr {
		t.Fatal("human-readable output must move to stderr")
	}

	events.Emit("probe", nil)
	_ = writer.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(reader); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"event":"probe"`)) {
		t.Fatalf("event did not reach stdout: %q", buf.String())
	}
}
