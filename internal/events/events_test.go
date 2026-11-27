package events

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestEmitIsSilentUntilEnabled(t *testing.T) {
	Enable(nil)
	if Enabled() {
		t.Fatal("events must be disabled with no sink")
	}
	Emit("ignored", nil) // must not panic
}

func TestEmitWritesOneJSONObjectPerLine(t *testing.T) {
	var buf bytes.Buffer
	Enable(&buf)
	t.Cleanup(func() { Enable(nil) })

	Emit("crawl_page", map[string]any{"page_id": 42, "title": "A \"quoted\"\ntitle", "event": "spoofed"})
	Emit("run_finished", nil)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(lines), buf.String())
	}

	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("line is not JSON: %v", err)
	}
	if first["event"] != "crawl_page" {
		t.Fatalf("event = %v, want crawl_page", first["event"])
	}
	if first["title"] != "A \"quoted\"\ntitle" {
		t.Fatalf("title was not preserved: %v", first["title"])
	}
	if _, ok := first["time"].(string); !ok {
		t.Fatal("missing time")
	}
}

func TestEmitIsSafeConcurrently(t *testing.T) {
	var buf bytes.Buffer
	Enable(&buf)
	t.Cleanup(func() { Enable(nil) })

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Emit("tick", map[string]any{"n": i})
		}()
	}
	wg.Wait()

	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("interleaved output %q: %v", line, err)
		}
	}
}
