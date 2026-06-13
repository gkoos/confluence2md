package main

import (
	"strings"
	"testing"

	"github.com/gkoos/confluence2md/internal/store"
)

func TestRewriteAttachmentLinks_RewritesByOriginalName(t *testing.T) {
	results := []store.AttachmentResult{
		{Filename: "123_diagram.png", OriginalName: "diagram.png"},
	}
	input := "![diagram](attachment://diagram.png)"
	got := rewriteAttachmentLinks(input, results)

	if strings.Contains(got, "attachment://diagram.png") {
		t.Fatalf("expected original name to be rewritten, got:\n%s", got)
	}
	if !strings.Contains(got, "attachments/123_diagram.png") {
		t.Fatalf("expected local path in output, got:\n%s", got)
	}
}

func TestRewriteAttachmentLinks_RewritesByFileID(t *testing.T) {
	results := []store.AttachmentResult{
		{Filename: "123_diagram.png", OriginalName: "diagram.png", FileID: "abc-def-uuid"},
	}
	input := "![media](attachment://abc-def-uuid)"
	got := rewriteAttachmentLinks(input, results)

	if strings.Contains(got, "attachment://abc-def-uuid") {
		t.Fatalf("expected UUID to be rewritten, got:\n%s", got)
	}
	if !strings.Contains(got, "attachments/123_diagram.png") {
		t.Fatalf("expected local path in output, got:\n%s", got)
	}
}

func TestRewriteAttachmentLinks_UnknownKeyUnchanged(t *testing.T) {
	results := []store.AttachmentResult{
		{Filename: "123_diagram.png", OriginalName: "diagram.png", FileID: "abc-def-uuid"},
	}
	input := "![other](attachment://unknown-key)"
	got := rewriteAttachmentLinks(input, results)

	if got != input {
		t.Fatalf("expected unchanged output for unknown key, got:\n%s", got)
	}
}

func TestRewriteAttachmentLinks_EmptyResultsNoop(t *testing.T) {
	input := "![media](attachment://some-uuid)"
	got := rewriteAttachmentLinks(input, nil)
	if got != input {
		t.Fatalf("expected no change for empty results, got:\n%s", got)
	}
}
