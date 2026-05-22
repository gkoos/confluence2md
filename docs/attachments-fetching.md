# Attachments Retrieval Internals

This document explains how attachment metadata and binary files are retrieved, stored, and linked in exported Markdown.

## Goal

- Download attachments referenced by crawled pages.
- Save them with deterministic local filenames.
- Rewrite markdown image links so local renderers display the downloaded files.
- Keep page export resilient when attachment retrieval fails.

## End-to-end flow

1. Crawl fetches page storage XML and converts it to markdown.
2. During conversion, image attachment macros become markdown image links with a placeholder target:
   - ![alt](attachment://original-filename.ext)
3. Crawl fetches page attachment metadata from Confluence v2.
4. Store layer downloads attachment binaries and writes them to output/attachments.
5. Before writing page markdown to disk, placeholder links are rewritten:
   - attachment://original-filename.ext -> attachments/{page-id}_{original-filename.ext}

## Metadata discovery (v2)

Attachment listing uses Confluence v2 API via [internal/confluence/attachments_client.go](../internal/confluence/attachments_client.go):

- GetPageAttachments calls the v2 attachments list endpoint for the page.
- Pagination is followed using cursor links until exhausted.
- Each result is mapped to local AttachmentData:
  - id
  - page id
  - filename
  - media type
  - file size

This stage is best-effort: failures are stored as non-fatal warnings on the page result.

## Binary download strategy

Binary retrieval uses the documented redirect download route in [internal/confluence/attachments_client.go](../internal/confluence/attachments_client.go):

1. Build redirect endpoint:
   - /wiki/rest/api/content/{pageId}/child/attachment/{attachmentId}/download
2. Perform authenticated GET without auto-redirect.
3. If a redirect Location is returned, resolve to absolute URL and perform authenticated GET for file bytes.
4. If 200 is returned immediately, read the body directly.

Safety controls:

- Response body reads are capped (currently 100 MB in client download logic).
- Non-200 statuses return explicit errors with status and short body excerpt.

## Storage and naming

Attachment files are written by [internal/store/attachments.go](../internal/store/attachments.go):

- Output directory: output/attachments
- Deterministic filename format: {page-id}_{original-filename}
- Spaces in original filenames are replaced with underscores.
- Configured size limit is enforced before download (attachments.max_size_mb, 0 means unlimited).

The result for each attempted attachment includes:

- saved filename (if successful)
- original filename
- skipped flag
- error (if failed or skipped)

## Markdown rewrite integration

The page processing flow in [cmd/crawler/run_pipeline.go](../cmd/crawler/run_pipeline.go) and [cmd/crawler/link_utils.go](../cmd/crawler/link_utils.go):

- Downloads attachments for the page.
- Builds a map from original filename to saved local path.
- Rewrites markdown links matching attachment://<filename> to attachments/<saved-filename>.

This guarantees markdown output references the exact file that was persisted.

## Failure behavior

Attachment failures do not fail the page export.

- Metadata fetch failure: warning; page markdown is still written.
- Binary download failure: warning per attachment; page markdown is still written.
- Write failure for a specific file: warning; other attachments continue.

This matches the crawler's resilience model for comments and other non-critical page enrichments.

## Relevant code paths

- Discovery and download client: [internal/confluence/attachments_client.go](../internal/confluence/attachments_client.go)
- Crawl orchestration: [internal/crawl/full.go](../internal/crawl/full.go)
- Attachment file persistence: [internal/store/attachments.go](../internal/store/attachments.go)
- Placeholder rewrite during page write: [cmd/crawler/link_utils.go](../cmd/crawler/link_utils.go)
- Image placeholder emission in conversion: [internal/convert/parser.go](../internal/convert/parser.go)
