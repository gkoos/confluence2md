package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/gkoos/confluence2md/internal/events"
)

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
