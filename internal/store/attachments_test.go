package store

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gkoos/confluence2md/internal/config"
	"github.com/gkoos/confluence2md/internal/confluence"
)

func TestDownloadPageAttachments_PropagatesFileID(t *testing.T) {
	router := http.NewServeMux()
	router.HandleFunc("/wiki/rest/api/content/p1/child/attachment/a1/download", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("filedata"))
	})
	ts := httptest.NewServer(router)
	defer ts.Close()

	client, err := confluence.NewClient(ts.URL, ts.URL, "u", "t", config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1}, 60000, 1)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	attachments := []confluence.AttachmentData{
		{
			ID:            "a1",
			PageID:        "p1",
			Filename:      "diagram.png",
			MediaType:     "image/png",
			FileSizeBytes: 8,
			FileID:        "test-uuid-abc-123",
		},
	}

	dir := t.TempDir()
	results := DownloadPageAttachments(t.Context(), dir, "p1", attachments, 0, client)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Error != nil {
		t.Fatalf("unexpected error: %v", r.Error)
	}
	if r.FileID != "test-uuid-abc-123" {
		t.Fatalf("expected FileID to be propagated, got %q", r.FileID)
	}
	if r.OriginalName != "diagram.png" {
		t.Fatalf("expected OriginalName %q, got %q", "diagram.png", r.OriginalName)
	}
}

func TestPageAttachmentFilename_SanitizesTraversalAndSeparators(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantSafe string // expected part after "{pageID}_"
	}{
		{
			name:     "unix separator traversal",
			input:    "../secret.txt",
			wantSafe: "_secret.txt",
		},
		{
			name:     "windows separator traversal",
			input:    "..\\..\\secret.txt",
			wantSafe: "_.._secret.txt",
		},
		{
			name:     "repeated traversal segments",
			input:    "../../../../etc/passwd",
			wantSafe: "_.._.._.._etc_passwd",
		},
		{
			name:     "absolute-looking unix path",
			input:    "/etc/passwd",
			wantSafe: "_etc_passwd",
		},
		{
			name:     "absolute-looking windows path",
			input:    "C:\\Windows\\system32\\evil.dll",
			wantSafe: "C:_Windows_system32_evil.dll",
		},
		{
			name:     "empty after stripping dots",
			input:    "...",
			wantSafe: "attachment",
		},
		{
			name:     "separators-only name becomes underscores, not empty",
			input:    "////",
			wantSafe: "____",
		},
		{
			name:     "empty after stripping control characters",
			input:    "\x00\x01\x1f",
			wantSafe: "attachment",
		},
		{
			name:     "genuinely empty input",
			input:    "",
			wantSafe: "attachment",
		},
		{
			name:     "control characters stripped",
			input:    "evil\x00name\x1f.txt",
			wantSafe: "evilname.txt",
		},
		{
			name:     "normal filename unchanged apart from spaces",
			input:    "My Diagram.png",
			wantSafe: "My_Diagram.png",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PageAttachmentFilename("123", tc.input)
			want := "123_" + tc.wantSafe
			if got != want {
				t.Fatalf("PageAttachmentFilename(%q) = %q, want %q", tc.input, got, want)
			}

			// The sanitized name must never contain path separators or ".."
			// segments, regardless of the specific expected string above.
			if strings.ContainsAny(got, "/\\") {
				t.Fatalf("sanitized filename %q still contains a path separator", got)
			}
		})
	}
}

func TestDownloadPageAttachments_TraversalNamesStayWithinAttachmentsDir(t *testing.T) {
	router := http.NewServeMux()
	router.HandleFunc("/wiki/rest/api/content/p1/child/attachment/a1/download", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("filedata"))
	})
	ts := httptest.NewServer(router)
	defer ts.Close()

	client, err := confluence.NewClient(ts.URL, ts.URL, "u", "t", config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1}, 60000, 1)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	maliciousNames := []string{
		"../../../etc/passwd",
		"..\\..\\..\\Windows\\system32\\config",
		"/etc/passwd",
	}

	dir := t.TempDir()
	for i, name := range maliciousNames {
		attachments := []confluence.AttachmentData{
			{
				ID:            "a1",
				PageID:        "p1",
				Filename:      name,
				MediaType:     "text/plain",
				FileSizeBytes: 8,
			},
		}

		results := DownloadPageAttachments(t.Context(), dir, "p1", attachments, 0, client)
		if len(results) != 1 {
			t.Fatalf("case %d: expected 1 result, got %d", i, len(results))
		}
		r := results[0]
		if r.Error != nil {
			t.Fatalf("case %d: unexpected error for %q: %v", i, name, r.Error)
		}
	}

	// Every file written must live directly inside dir/attachments, never
	// having escaped via traversal.
	attachDir := filepath.Join(dir, "attachments")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "attachments" {
			t.Fatalf("unexpected entry escaped into temp root: %q", e.Name())
		}
	}

	attachEntries, err := os.ReadDir(attachDir)
	if err != nil {
		t.Fatalf("read attachments dir: %v", err)
	}
	if len(attachEntries) == 0 {
		t.Fatal("expected files to be written inside attachments dir")
	}
	for _, e := range attachEntries {
		if strings.ContainsAny(e.Name(), "/\\") {
			t.Fatalf("attachment file name contains path separator: %q", e.Name())
		}
	}
}

func TestVerifyWithinDir_RejectsEscape(t *testing.T) {
	dir := t.TempDir()
	attachDir := filepath.Join(dir, "attachments")
	if err := os.MkdirAll(attachDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// A destination that resolves outside attachDir must be rejected, even
	// if filepath.Join already "cleaned" the .. segments syntactically.
	escaped := filepath.Join(dir, "outside.txt")
	if err := verifyWithinDir(attachDir, escaped); err == nil {
		t.Fatal("expected error for path outside attachments dir, got nil")
	}

	// A legitimate destination inside attachDir must be accepted.
	inside := filepath.Join(attachDir, "123_file.txt")
	if err := verifyWithinDir(attachDir, inside); err != nil {
		t.Fatalf("unexpected error for path inside attachments dir: %v", err)
	}
}

func TestDownloadPageAttachments_PartialFileCleanedUpOnError(t *testing.T) {
	// Server returns a redirect, then the file endpoint errors mid-stream.
	router := http.NewServeMux()
	router.HandleFunc("/wiki/rest/api/content/p1/child/attachment/a1/download", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	ts := httptest.NewServer(router)
	defer ts.Close()

	client, err := confluence.NewClient(ts.URL, ts.URL, "u", "t", config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1}, 60000, 1)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	attachments := []confluence.AttachmentData{
		{
			ID:            "a1",
			PageID:        "p1",
			Filename:      "report.pdf",
			MediaType:     "application/pdf",
			FileSizeBytes: 1024,
		},
	}

	dir := t.TempDir()
	results := DownloadPageAttachments(t.Context(), dir, "p1", attachments, 0, client)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Error == nil {
		t.Fatal("expected download error, got nil")
	}

	// No partial file should be left behind.
	attachDir := filepath.Join(dir, "attachments")
	entries, _ := os.ReadDir(attachDir)
	for _, e := range entries {
		t.Errorf("unexpected file left in attachments dir after error: %q", e.Name())
	}
}

func TestPageAttachmentFilename_HostQualifiedKey(t *testing.T) {
	cases := []struct {
		name    string
		pageKey string
		want    string
	}{
		{name: "single-host bare page ID", pageKey: "123", want: "123_diagram.png"},
		{name: "multi-host host-qualified key", pageKey: "company1.atlassian.net/123", want: "company1.atlassian.net_123_diagram.png"},
		{name: "port-carrying host key", pageKey: "127.0.0.1:8080/123", want: "127.0.0.1_8080_123_diagram.png"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PageAttachmentFilename(tc.pageKey, "diagram.png")
			if got != tc.want {
				t.Fatalf("PageAttachmentFilename(%q) = %q, want %q", tc.pageKey, got, tc.want)
			}
			if strings.ContainsAny(got, `/\:`) {
				t.Fatalf("saved filename %q still contains a path separator or a colon", got)
			}
		})
	}
}

func TestPageAttachmentFilename_DistinctHostsSharingPageIDAndName(t *testing.T) {
	first := PageAttachmentFilename("company1.atlassian.net/123", "diagram.png")
	second := PageAttachmentFilename("company2.atlassian.net/123", "diagram.png")

	if first == second {
		t.Fatalf("expected distinct filenames for distinct hosts, both were %q", first)
	}
	if bare := PageAttachmentFilename("123", "diagram.png"); first == bare || second == bare {
		t.Fatalf("host-qualified names must differ from the single-host name %q", bare)
	}
}

func TestDownloadPageAttachments_MultiHostFlatNames(t *testing.T) {
	const attachmentName = "diagram.png"

	type hostFixture struct {
		host    string
		payload string
		client  *confluence.Client
	}

	payloads := []string{"payload-from-company1", "payload-from-company2"}
	fixtures := make([]hostFixture, 0, len(payloads))

	for i, payload := range payloads {
		router := http.NewServeMux()
		router.HandleFunc("/wiki/rest/api/content/123/child/attachment/a1/download", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(payload))
		})
		ts := httptest.NewServer(router)
		t.Cleanup(ts.Close)

		client, err := confluence.NewClient(ts.URL, ts.URL, "u", "t", config.RetryConfig{MaxAttempts: 1, InitialBackoffMS: 1}, 60000, 1)
		if err != nil {
			t.Fatalf("host %d: new client: %v", i, err)
		}

		// httptest hosts carry a port, so these keys also exercise the ":"
		// escaping required for Windows filenames.
		fixtures = append(fixtures, hostFixture{host: strings.TrimPrefix(ts.URL, "http://"), payload: payload, client: client})
	}

	dir := t.TempDir()
	savedContents := make(map[string]string, len(fixtures))

	for i, fx := range fixtures {
		pageKey := fx.host + "/123"
		attachments := []confluence.AttachmentData{
			{ID: "a1", PageID: "123", Filename: attachmentName, MediaType: "image/png", FileSizeBytes: int64(len(fx.payload))},
		}

		results := DownloadPageAttachments(t.Context(), dir, pageKey, attachments, 0, fx.client)
		if len(results) != 1 {
			t.Fatalf("host %d: expected 1 result, got %d", i, len(results))
		}

		result := results[0]
		if result.Error != nil {
			t.Fatalf("host %d: unexpected error: %v", i, result.Error)
		}
		if strings.ContainsAny(result.Filename, `/\:`) {
			t.Fatalf("host %d: saved filename %q is not a flat path segment", i, result.Filename)
		}

		saved, err := os.ReadFile(filepath.Join(dir, "attachments", result.Filename))
		if err != nil {
			t.Fatalf("host %d: read saved attachment %q: %v", i, result.Filename, err)
		}
		if string(saved) != fx.payload {
			t.Fatalf("host %d: saved contents = %q, want %q", i, string(saved), fx.payload)
		}
		if previous, duplicate := savedContents[result.Filename]; duplicate {
			t.Fatalf("host %d reuses filename %q (already written with contents %q)", i, result.Filename, previous)
		}
		savedContents[result.Filename] = fx.payload
	}

	if len(savedContents) != len(fixtures) {
		t.Fatalf("expected %d distinct attachment files, got %d", len(fixtures), len(savedContents))
	}

	entries, err := os.ReadDir(filepath.Join(dir, "attachments"))
	if err != nil {
		t.Fatalf("read attachments dir: %v", err)
	}
	if len(entries) != len(fixtures) {
		t.Fatalf("expected %d entries under attachments/, got %d", len(fixtures), len(entries))
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("expected only flat files under attachments/, found directory %q", entry.Name())
		}
	}
}
