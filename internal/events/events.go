// Package events writes machine-readable progress as one JSON object per line.
package events

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

var (
	mu   sync.Mutex
	sink io.Writer
)

// Enable routes events to w. Until it is called, Emit does nothing.
func Enable(w io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	sink = w
}

// Enabled reports whether events are being written.
func Enabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return sink != nil
}

// Emit writes {"time", "event", ...fields} as a single line. Fields named
// "time" or "event" are ignored.
func Emit(event string, fields map[string]any) {
	mu.Lock()
	defer mu.Unlock()
	if sink == nil {
		return
	}

	record := make(map[string]any, len(fields)+2)
	for key, value := range fields {
		record[key] = value
	}
	record["time"] = time.Now().UTC().Format(time.RFC3339Nano)
	record["event"] = event

	line, err := json.Marshal(record)
	if err != nil {
		return
	}
	_, _ = sink.Write(append(line, '\n'))
}
