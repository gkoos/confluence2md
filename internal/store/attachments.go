package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gkoos/confluence2md/internal/confluence"
)

// AttachmentResult holds the outcome of downloading a single attachment.
type AttachmentResult struct {
	Filename     string // saved as {page-id}_{original-filename}
	OriginalName string
	FileID       string // Confluence Media Services UUID (fileId); matches ADF media.attrs.id
	Skipped      bool   // true if over size limit
	Error        error  // non-fatal: page export still proceeds
}

// DownloadPageAttachments downloads all attachments for a page and writes them to output/attachments/.
// Returns the list of results (including failures — callers should treat failures as non-fatal).
func DownloadPageAttachments(
	ctx context.Context,
	outputDir string,
	pageID string,
	attachments []confluence.AttachmentData,
	maxSizeMB int,
	client *confluence.Client,
) []AttachmentResult {
	if len(attachments) == 0 {
		return nil
	}

	attachDir := filepath.Join(outputDir, "attachments")
	if err := os.MkdirAll(attachDir, 0755); err != nil {
		// If we can't even create the dir, return everything as failed.
		results := make([]AttachmentResult, len(attachments))
		for i, a := range attachments {
			results[i] = AttachmentResult{
				OriginalName: a.Filename,
				Error:        fmt.Errorf("create attachments dir: %w", err),
			}
		}
		return results
	}

	maxBytes := int64(maxSizeMB) * 1024 * 1024

	results := make([]AttachmentResult, 0, len(attachments))
	for _, a := range attachments {
		result := AttachmentResult{
			OriginalName: a.Filename,
		}

		// Apply size limit check (0 = no limit)
		if maxBytes > 0 && a.FileSizeBytes > maxBytes {
			result.Skipped = true
			result.Error = fmt.Errorf("attachment %q skipped: size %d bytes exceeds limit of %d bytes",
				a.Filename, a.FileSizeBytes, maxBytes)
			results = append(results, result)
			continue
		}

		if strings.TrimSpace(a.ID) == "" {
			result.Error = fmt.Errorf("attachment %q has no attachment ID", a.Filename)
			results = append(results, result)
			continue
		}

		data, err := client.DownloadAttachment(ctx, a, maxBytes)
		if err != nil {
			result.Error = fmt.Errorf("download %q: %w", a.Filename, err)
			results = append(results, result)
			continue
		}

		savedFilename := PageAttachmentFilename(pageID, a.Filename)
		destPath := filepath.Join(attachDir, savedFilename)
		if err := verifyWithinDir(attachDir, destPath); err != nil {
			result.Error = fmt.Errorf("attachment %q: %w", a.Filename, err)
			results = append(results, result)
			continue
		}
		if err := os.WriteFile(destPath, data, 0644); err != nil {
			result.Error = fmt.Errorf("write %q: %w", savedFilename, err)
			results = append(results, result)
			continue
		}

		result.Filename = savedFilename
		result.FileID = a.FileID
		results = append(results, result)
	}

	return results
}

// PageAttachmentFilename returns the deterministic saved filename for an attachment.
// Format: {page-id}_{original-filename}, with spaces replaced by underscores and
// path separators, traversal segments, and control characters sanitized away.
func PageAttachmentFilename(pageID, originalFilename string) string {
	safe := sanitizeAttachmentFilename(originalFilename)
	return fmt.Sprintf("%s_%s", pageID, safe)
}

// sanitizeAttachmentFilename strips or replaces characters that could be used
// to escape the attachments directory (path separators, ".." segments,
// control characters) or that are invalid on common filesystems. Spaces are
// replaced with underscores to preserve prior deterministic naming behavior.
// Falls back to "attachment" if sanitization leaves nothing usable.
func sanitizeAttachmentFilename(name string) string {
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")

	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if r < 0x20 || r == 0x7F {
			continue
		}
		b.WriteRune(r)
	}
	name = b.String()

	// Trim leading/trailing dots and whitespace repeatedly to neutralize
	// "..", "...", trailing-dot/space quirks on Windows, and hidden-dotfile
	// leading dots.
	for {
		trimmed := strings.Trim(name, ". ")
		if trimmed == name {
			break
		}
		name = trimmed
	}

	if name == "" {
		name = "attachment"
	}

	return name
}

// verifyWithinDir returns an error if dest does not resolve to a path inside
// dir. This is a defense-in-depth check against path traversal, in addition
// to filename sanitization.
func verifyWithinDir(dir, dest string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve attachments dir: %w", err)
	}
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return fmt.Errorf("resolve destination path: %w", err)
	}

	rel, err := filepath.Rel(absDir, absDest)
	if err != nil {
		return fmt.Errorf("resolves outside attachments directory")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("resolves outside attachments directory")
	}

	return nil
}

// AttachmentLocalPath returns the relative path from a page file to its attachment.
// e.g. "attachments/123_diagram.svg"
func AttachmentLocalPath(savedFilename string) string {
	return filepath.ToSlash(filepath.Join("attachments", savedFilename))
}
