package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFullMode_InvalidSeedPreservesExistingOutput(t *testing.T) {
	// Create a temp config file and output directory
	root := t.TempDir()
	outputDir := filepath.Join(root, "output")
	configFile := filepath.Join(root, "config.yaml")

	// Create output directory with existing content that should NOT be cleared
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}
	existingFile := filepath.Join(outputDir, "existing.md")
	if err := os.WriteFile(existingFile, []byte("existing content"), 0644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	// Create mock Confluence server that returns error for GetPageBySeed
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Fail any request (simulating a bad seed endpoint)
		w.WriteHeader(http.StatusNotFound)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"statusCode": 404,
			"message":    "Page not found",
		})
	}))
	defer ts.Close()

	// Create config file with mock server URL and an invalid seed
	configYAML := fmt.Sprintf(`
confluence:
  base_url: %s
  username: testuser
  token: testtoken
crawl:
  seeds:
    - %s/wiki/spaces/INVALID/pages/999/NonexistentPage
  max_depth: 1
  concurrency: 1
  queue_size: 1000
  rate_limit_rpm: 60
  follow_children: false
retry:
  max_attempts: 1
  initial_backoff_ms: 1
output:
  dir: %s
`, ts.URL, ts.URL, outputDir)

	if err := os.WriteFile(configFile, []byte(configYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Call bootstrapRun with full mode
	_, err := bootstrapRun("full", configFile, false)

	// Expect error (seed extraction failed)
	if err == nil {
		t.Fatalf("expected bootstrapRun to return error for invalid seed")
	}

	// Verify existing file still exists (output was NOT cleared before seed validation failed)
	if _, err := os.Stat(existingFile); os.IsNotExist(err) {
		t.Fatalf("expected existing file to be preserved after invalid seed error, but file was deleted")
	}
}

func TestFullMode_ValidSeedsClearsOutput(t *testing.T) {
	// Create a temp config file and output directory
	root := t.TempDir()
	outputDir := filepath.Join(root, "output")
	configFile := filepath.Join(root, "config.yaml")

	// Create output directory with existing content that SHOULD be cleared
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}
	existingFile := filepath.Join(outputDir, "existing.md")
	if err := os.WriteFile(existingFile, []byte("old content"), 0644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	// Create mock Confluence server that returns valid seed page
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a valid page response for seed resolution
		w.Header().Set("Content-Type", "application/json")
		// Use a response structure that matches the Confluence API v2 response
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":    "123",
			"title": "Test Page",
			"version": map[string]interface{}{
				"number": 1,
			},
			"body": map[string]interface{}{
				"atlas_doc_format": map[string]interface{}{
					"value": "# Test",
				},
			},
		})
	}))
	defer ts.Close()

	// Create config file with mock server URL and a valid seed
	configYAML := fmt.Sprintf(`
confluence:
  base_url: %s
  username: testuser
  token: testtoken
crawl:
  seeds:
    - %s/wiki/spaces/TEST/pages/123/TestPage
  max_depth: 1
  concurrency: 1
  queue_size: 1000
  rate_limit_rpm: 60
  follow_children: false
retry:
  max_attempts: 1
  initial_backoff_ms: 1
output:
  dir: %s
`, ts.URL, ts.URL, outputDir)

	if err := os.WriteFile(configFile, []byte(configYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Call bootstrapRun with full mode
	_, err := bootstrapRun("full", configFile, false)

	// Expect NO error (seed validation succeeded)
	if err != nil {
		t.Fatalf("expected bootstrapRun to succeed with valid seed, got error: %v", err)
	}

	// Verify existing file was cleared (output directory should be empty)
	if _, err := os.Stat(existingFile); !os.IsNotExist(err) {
		t.Fatalf("expected existing file to be cleared during full mode initialization, but file still exists, stat err=%v", err)
	}

	// Verify output directory is empty (metadata.json is written later during finalize, not at bootstrap)
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected output dir to be empty after clearing, found %d entries", len(entries))
	}
}

func TestUpdatesMode_InvalidSeedPreservesExistingOutput(t *testing.T) {
	// Create a temp config file and output directory
	root := t.TempDir()
	outputDir := filepath.Join(root, "output")
	configFile := filepath.Join(root, "config.yaml")

	// Create output directory with existing metadata
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}
	metadataFile := filepath.Join(outputDir, "metadata.json")
	if err := os.WriteFile(metadataFile, []byte(`{"seed_page_ids":["123"],"pages":{}}`), 0644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	existingPageFile := filepath.Join(outputDir, "test_123.md")
	if err := os.WriteFile(existingPageFile, []byte("existing page"), 0644); err != nil {
		t.Fatalf("write existing page: %v", err)
	}

	// Create mock Confluence server that returns error for invalid seed
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"statusCode": 404,
			"message":    "Page not found",
		})
	}))
	defer ts.Close()

	// Create config file for updates mode
	configYAML := fmt.Sprintf(`
confluence:
  base_url: %s
  username: testuser
  token: testtoken
crawl:
  seeds:
    - %s/wiki/spaces/INVALID/pages/999/BadSeed
  max_depth: 1
  concurrency: 1
  queue_size: 1000
  rate_limit_rpm: 60
  follow_children: false
retry:
  max_attempts: 1
  initial_backoff_ms: 1
output:
  dir: %s
`, ts.URL, ts.URL, outputDir)

	if err := os.WriteFile(configFile, []byte(configYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Call bootstrapRun with updates mode
	_, err := bootstrapRun("updates", configFile, false)

	// Expect error (seed extraction failed)
	if err == nil {
		t.Fatalf("expected bootstrapRun to return error for invalid seed in updates mode")
	}

	// Verify existing output files are untouched (updates mode never clears)
	if _, err := os.Stat(metadataFile); os.IsNotExist(err) {
		t.Fatalf("expected metadata.json to be preserved in updates mode, but file was deleted")
	}
	if _, err := os.Stat(existingPageFile); os.IsNotExist(err) {
		t.Fatalf("expected existing page to be preserved in updates mode, but file was deleted")
	}
}
