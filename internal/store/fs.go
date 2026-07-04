package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gosimple/slug"
)

// PageRecord represents a single page entry in metadata.json.
type PageRecord struct {
	ID                  string    `json:"id"`
	Title               string    `json:"title"`
	LocalPath           string    `json:"local_path"`
	Version             int       `json:"version"`
	CrawledAt           time.Time `json:"crawled_at"`
	CommentCount        int       `json:"comment_count,omitempty"`
	CommentsLastFetched time.Time `json:"comments_last_fetched"`
	CommentsFetchError  string    `json:"comments_fetch_error,omitempty"`
	SourceURL           string    `json:"source_url"`
	CanonicalURL        string    `json:"canonical_url"`
	SpaceKey            string    `json:"space_key"`
	Depth               int       `json:"depth"`
	FetchError          string    `json:"fetch_error,omitempty"`           // non-empty if fetch/convert failed
	OutgoingLinks       []string  `json:"outgoing_links"`                  // page IDs this page links to
	IncomingLinks       []string  `json:"incoming_links"`                  // page IDs that link to this page
	Attachments         []string  `json:"attachments,omitempty"`           // saved filenames in attachments/
	AttachmentSignature string    `json:"attachment_signature,omitempty"`  // stable attachment metadata signature for dirty checks
	StorageFormat       string    `json:"storage_format_sample,omitempty"` // First 500 chars for diagnostic

	// Temporal metadata
	CreatedAt      time.Time `json:"created_at"`
	LastModifiedAt time.Time `json:"last_modified_at"`

	// Author metadata
	CreatedByID        string `json:"created_by_id"`
	CreatedByName      string `json:"created_by_name,omitempty"`
	LastModifiedByID   string `json:"last_modified_by_id"`
	LastModifiedByName string `json:"last_modified_by_name,omitempty"`

	// Hierarchy metadata
	ConfluenceParentID *int64 `json:"confluence_parent_id,omitempty"`
}

// Metadata represents the top-level metadata.json structure.
type Metadata struct {
	CrawlStartedAt                 time.Time             `json:"crawl_started_at"`
	LastCompletedCrawlStartedAt    time.Time             `json:"last_completed_crawl_started_at"`
	LastCompletedCrawlCompletedAt  time.Time             `json:"last_completed_crawl_completed_at"`
	LastCompletedCrawlMode         string                `json:"last_completed_crawl_mode,omitempty"`
	LastSuccessfulCrawlStartedAt   time.Time             `json:"last_successful_crawl_started_at"`
	LastSuccessfulCrawlCompletedAt time.Time             `json:"last_successful_crawl_completed_at"`
	LastSuccessfulCrawlMode        string                `json:"last_successful_crawl_mode,omitempty"`
	SeedPageIDs                    []string              `json:"seed_page_ids,omitempty"`
	Pages                          map[string]PageRecord `json:"pages"`
}

// Writer handles deterministic file writes for crawled pages.
type Writer struct {
	outputDir string
	metadata  *Metadata
}

// CheckpointSnapshot captures last successful crawl checkpoint state.
type CheckpointSnapshot struct {
	Mode        string
	StartedAt   time.Time
	CompletedAt time.Time
	Present     bool
}

// NewWriter creates a new filesystem writer for the output directory.
func NewWriter(outputDir string) (*Writer, error) {
	runStartedAt := time.Now().UTC()

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}

	w := &Writer{
		outputDir: outputDir,
		metadata: &Metadata{
			CrawlStartedAt: runStartedAt,
			Pages:          make(map[string]PageRecord),
		},
	}

	if err := w.loadMetadata(); err != nil {
		// If metadata doesn't exist yet, that's OK, we'll create it
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("load metadata: %w", err)
		}
	}

	// Always track the current run start independently from prior runs.
	w.metadata.CrawlStartedAt = runStartedAt
	if w.metadata.Pages == nil {
		w.metadata.Pages = make(map[string]PageRecord)
	}

	return w, nil
}

// CrawlStartedAt returns the current crawl run start timestamp.
func (w *Writer) CrawlStartedAt() time.Time {
	return w.metadata.CrawlStartedAt
}

// LastSuccessfulCheckpoint returns the last successful checkpoint tuple if present.
func (w *Writer) LastSuccessfulCheckpoint() CheckpointSnapshot {
	if strings.TrimSpace(w.metadata.LastSuccessfulCrawlMode) == "" {
		return CheckpointSnapshot{}
	}

	return CheckpointSnapshot{
		Mode:        w.metadata.LastSuccessfulCrawlMode,
		StartedAt:   w.metadata.LastSuccessfulCrawlStartedAt,
		CompletedAt: w.metadata.LastSuccessfulCrawlCompletedAt,
		Present:     true,
	}
}

// LastCompletedCheckpoint returns the last completed checkpoint tuple if present.
func (w *Writer) LastCompletedCheckpoint() CheckpointSnapshot {
	if strings.TrimSpace(w.metadata.LastCompletedCrawlMode) == "" {
		return CheckpointSnapshot{}
	}

	return CheckpointSnapshot{
		Mode:        w.metadata.LastCompletedCrawlMode,
		StartedAt:   w.metadata.LastCompletedCrawlStartedAt,
		CompletedAt: w.metadata.LastCompletedCrawlCompletedAt,
		Present:     true,
	}
}

// MarkSuccessfulCheckpoint records the last successful crawl checkpoint.
func (w *Writer) MarkSuccessfulCheckpoint(mode string, startedAt, completedAt time.Time) error {
	if err := validateCheckpointWriteInput(mode, startedAt, completedAt); err != nil {
		return err
	}

	w.metadata.LastSuccessfulCrawlMode = mode
	w.metadata.LastSuccessfulCrawlStartedAt = startedAt.UTC()
	w.metadata.LastSuccessfulCrawlCompletedAt = completedAt.UTC()
	return nil
}

// MarkCompletedCheckpoint records the last completed crawl checkpoint.
func (w *Writer) MarkCompletedCheckpoint(mode string, startedAt, completedAt time.Time) error {
	if err := validateCheckpointWriteInput(mode, startedAt, completedAt); err != nil {
		return err
	}

	w.metadata.LastCompletedCrawlMode = mode
	w.metadata.LastCompletedCrawlStartedAt = startedAt.UTC()
	w.metadata.LastCompletedCrawlCompletedAt = completedAt.UTC()
	return nil
}

// AddPage adds a crawled page to metadata with full graph information.
func (w *Writer) AddPage(pageID string, pageRecord PageRecord) error {
	filename := generateFilename(pageRecord.Title, pageID)
	filepath := filepath.Join(w.outputDir, filename)
	rendered := ComposeMarkdownWithFrontMatter(pageID, pageRecord, w.metadata.SeedPageIDs, pageRecord.StorageFormat)

	// Write markdown content to disk
	if err := os.WriteFile(filepath, []byte(rendered), 0644); err != nil {
		return fmt.Errorf("write page file %s: %w", filename, err)
	}

	// Update record with local path and store in metadata
	pageRecord.LocalPath = filename
	w.metadata.Pages[pageID] = pageRecord

	return nil
}

// AddPageMetadata stores a page record in metadata without writing page content.
// Used by updates mode for clean pages that reuse existing markdown artifacts.
func (w *Writer) AddPageMetadata(pageID string, pageRecord PageRecord) {
	if strings.TrimSpace(pageRecord.LocalPath) == "" {
		pageRecord.LocalPath = generateFilename(pageRecord.Title, pageID)
	}
	w.metadata.Pages[pageID] = pageRecord
}

// GetPages returns the current pages map reference.
func (w *Writer) GetPages() map[string]PageRecord {
	return w.metadata.Pages
}

// SetSeedPageIDs records canonical seed page IDs at metadata root.
func (w *Writer) SetSeedPageIDs(seedPageIDs []string) {
	w.metadata.SeedPageIDs = normalizeSeedPageIDs(seedPageIDs)
}

// GetSeedPageIDs returns a copy of canonical seed page IDs from metadata root.
func (w *Writer) GetSeedPageIDs() []string {
	out := make([]string, len(w.metadata.SeedPageIDs))
	copy(out, w.metadata.SeedPageIDs)
	return out
}

// SaveMetadata writes the metadata.json file to disk.
func (w *Writer) SaveMetadata() error {
	metaPath := filepath.Join(w.outputDir, "metadata.json")
	data, err := json.MarshalIndent(w.metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	if err := os.WriteFile(metaPath, data, 0644); err != nil {
		return fmt.Errorf("write metadata.json: %w", err)
	}

	return nil
}

// generateFilename creates a deterministic filename from page title and ID.
// Format: {title-slug}_{page-id}.md
func generateFilename(title, pageID string) string {
	s := slug.Make(title)
	if s == "" {
		s = "page"
	}
	return fmt.Sprintf("%s_%s.md", s, pageID)
}

// loadMetadata loads existing metadata.json from disk if it exists.
func (w *Writer) loadMetadata() error {
	metaPath := filepath.Join(w.outputDir, "metadata.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return err
	}

	var m Metadata
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("unmarshal metadata: %w", err)
	}
	if err := validateMetadata(&m); err != nil {
		return fmt.Errorf("validate metadata: %w", err)
	}

	w.metadata = &m
	return nil
}

func validateMetadata(m *Metadata) error {
	if m == nil {
		return fmt.Errorf("metadata is nil")
	}
	if m.Pages == nil {
		return fmt.Errorf("pages map is missing")
	}

	for i, seedID := range m.SeedPageIDs {
		if strings.TrimSpace(seedID) == "" {
			return fmt.Errorf("seed_page_ids[%d] is empty", i)
		}
	}

	for id, page := range m.Pages {
		if id == "" {
			return fmt.Errorf("found page with empty map key")
		}
		if page.ID == "" {
			return fmt.Errorf("page %q has empty id field", id)
		}
		if page.ID != id {
			return fmt.Errorf("page id mismatch for key %q: record id=%q", id, page.ID)
		}
	}

	if err := validateCheckpointTuple(
		"last completed",
		m.LastCompletedCrawlMode,
		m.LastCompletedCrawlStartedAt,
		m.LastCompletedCrawlCompletedAt,
	); err != nil {
		return err
	}

	if err := validateCheckpointTuple(
		"last successful",
		m.LastSuccessfulCrawlMode,
		m.LastSuccessfulCrawlStartedAt,
		m.LastSuccessfulCrawlCompletedAt,
	); err != nil {
		return err
	}

	return nil
}

func normalizeSeedPageIDs(seedPageIDs []string) []string {
	if len(seedPageIDs) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(seedPageIDs))
	seen := make(map[string]bool, len(seedPageIDs))
	for _, id := range seedPageIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		normalized = append(normalized, id)
	}

	if len(normalized) == 0 {
		return nil
	}

	return normalized
}

func validateCheckpointWriteInput(mode string, startedAt, completedAt time.Time) error {
	if mode != "full" && mode != "updates" {
		return fmt.Errorf("invalid crawl mode %q", mode)
	}
	if startedAt.IsZero() {
		return fmt.Errorf("checkpoint start time must be non-zero")
	}
	if completedAt.IsZero() {
		return fmt.Errorf("checkpoint completion time must be non-zero")
	}
	if completedAt.Before(startedAt) {
		return fmt.Errorf("checkpoint completion time %s is before start time %s", completedAt, startedAt)
	}
	return nil
}

func validateCheckpointTuple(name, mode string, startedAt, completedAt time.Time) error {
	hasMode := strings.TrimSpace(mode) != ""
	hasStarted := !startedAt.IsZero()
	hasCompleted := !completedAt.IsZero()

	if !hasMode && !hasStarted && !hasCompleted {
		return nil
	}

	if !hasMode || !hasStarted || !hasCompleted {
		return fmt.Errorf("%s checkpoint fields must be all set or all unset", name)
	}

	if mode != "full" && mode != "updates" {
		return fmt.Errorf("invalid %s crawl mode %q", name, mode)
	}

	if completedAt.Before(startedAt) {
		return fmt.Errorf("%s checkpoint completion is before start", name)
	}

	return nil
}
