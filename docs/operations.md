# Operations and Troubleshooting

This guide covers practical runtime behavior, failure modes, and recovery steps.

## Runtime model

- Full mode crawls from configured seeds up to max depth.
- Full mode clears the configured output directory before writing new crawl output.
- Updates mode (v1.0 scope) uses the same seed-based traversal as full mode, then selectively re-processes dirty pages and reuses clean-page artifacts.
- Output commit behavior is currently direct-write (non-transactional): files are written directly under the configured output directory during the run.
- Page export is resilient: non-critical failures (for example comment fetch or attachment retrieval failures) should not block page Markdown output.

## Common failure scenarios

### Authentication failures

Typical symptoms:

- Unauthorized responses from Confluence endpoints
- Validation command fails before crawl starts

Checks:

1. Confirm confluence.username and token are valid.
2. Confirm token has read access to target spaces.
3. Verify seed URLs point to accessible pages for that account.

### Rate limiting and transient API errors

Typical symptoms:

- 429 responses
- intermittent 5xx responses
- slower crawl completion than expected

Checks and actions:

1. Lower crawl.concurrency.
2. Lower crawl.rate_limit_rpm.
3. Increase retry.max_attempts and retry.initial_backoff_ms.
4. Re-run in full mode after major transient failures.

### Partial crawl output

Typical symptoms:

- Some pages missing from output
- metadata graph has fewer nodes than expected

Checks and actions:

1. Confirm max_depth is high enough for your link graph.
2. Confirm seeds include all intended entry points.
3. Re-run full mode to rebuild a complete local mirror.
4. Inspect summary counters (pages with errors, rewritten links, comment warnings).

### Link rewrite surprises

Expected behavior:

- Links to crawled pages are rewritten to local relative links.
- Links to uncrawled pages remain original URLs.
- External URLs remain unchanged.

If links are not rewritten as expected:

1. Confirm target pages were actually crawled.
2. Confirm page IDs exist in metadata.json.
3. Re-run full mode to rebuild the rewrite pass from a clean crawl set.

## Operational tips

- Keep config.yaml with your Atlassian credentials out of source control.
- Start with the default, conservative concurrency and rate limits, then increase gradually if needed.
- Treat full mode as a periodic baseline rebuild.
- Use updates mode between full runs for faster refresh cycles.

## Runtime guardrails

- `crawl.concurrency` must be greater than `0`.
- `crawl.rate_limit_rpm` must be greater than `0`.
- Invalid values are rejected at config validation time so runs fail fast before crawl startup.

## Updates-mode artifact self-healing

- For pages classified as clean/reused in updates mode, metadata is reused without full re-render.
- If a reused page's local markdown file is missing on disk, the crawler now recreates that file from stored content before rewrite/finalization.
- This prevents avoidable updates-run failures caused by missing local artifacts from partial/manual output cleanup.

## Updates summary semantics

When running in updates mode, summary counters are interpreted as follows:

- `Pages re-rendered`: pages that were fully re-processed because lightweight state checks identified changes (or a conservative fallback marked them dirty).
- `Pages reused without full re-processing`: pages treated as clean and carried forward via metadata-only upsert.
- `Checkpoint advanced`: `yes` only when the last successful checkpoint tuple (started_at/completed_at/mode) changed relative to the pre-run checkpoint snapshot.
- `Attachments downloaded/reused`: downloaded counts attachment fetches during dirty/full processing; reused counts attachments already present on disk for reused pages.

## Checkpoint model

The crawler persists two checkpoint tuples in `metadata.json`:

- `last_completed_*`: updated for every run that reaches finalize + metadata save.
- `last_successful_*`: updated only when the run has zero page errors.

Updates-mode dirty checks compare previous page metadata against current lightweight page state (version/title/attachment signature). Checkpoint tuples are persisted for reporting and operational auditing.

## Confluence Cloud API quotas and scaling

**Rate limits:**

Confluence Cloud enforces ~300 requests per minute per token. The crawler's default `rate_limit_rpm: 250` is conservative to leave headroom.

- If you only crawl one small space: 250 rpm is safe; no tuning needed.
- If you crawl multiple large spaces in the same account: measure your crawl time and quota usage. If you consistently hit 429 responses, reduce concurrency or rpm further.
- If you orchestrate multiple crawler instances in parallel (different tokens): each token has its own quota, so they don't interfere.

**Scaling considerations:**

- **Pages per crawl:** No hard limit. Crawl session memory grows with crawl size; a 10k-page crawl may use several hundred MB. Adjust concurrency down if you see memory pressure.
- **Attachment downloads:** Bandwidth and disk I/O are the typical bottlenecks, not API rate limits. If bandwidth is constrained, lower concurrency.
- **Comment fetch:** Each page with comments triggers a separate comments fetch. Non-fatal failures (404, 403) are logged as warnings; the page markdown still exports.

## metadata.json structure

The output `metadata.json` contains current-run timestamps, completed and successful crawl checkpoints, and a pages map with link graph fields on each page record.

Example structure:

```json
{
  "crawl_started_at": "2026-05-22T10:15:30Z",
  "last_completed_crawl_started_at": "2026-05-22T10:15:30Z",
  "last_completed_crawl_completed_at": "2026-05-22T10:20:03Z",
  "last_completed_crawl_mode": "updates",
  "last_successful_crawl_started_at": "2026-05-22T10:15:30Z",
  "last_successful_crawl_completed_at": "2026-05-22T10:20:03Z",
  "last_successful_crawl_mode": "full",
  "pages": {
    "123": {
      "id": "123",
      "title": "Page Title",
      "local_path": "page-title_123.md",
      "version": 7,
      "crawled_at": "2026-05-22T10:16:00Z",
      "source_url": "https://your-org.atlassian.net/wiki/pages/123",
      "canonical_url": "https://your-org.atlassian.net/wiki/spaces/SPACE/pages/123/Page+Title",
      "space_key": "SPACE",
      "depth": 0,
      "outgoing_links": ["456"],
      "incoming_links": ["789"],
      "attachments": ["123_design-spec.pdf"],
      "attachment_signature": "a1|design-spec.pdf|application/pdf|102400"
    }
  }
}
```

Top-level fields:
- `crawl_started_at`: Start timestamp of the current run.
- `last_completed_crawl_started_at`: Start timestamp of the last completed run.
- `last_completed_crawl_completed_at`: Completion timestamp of the last completed run.
- `last_completed_crawl_mode`: Mode of the last completed run (`full` or `updates`).
- `last_successful_crawl_started_at`: Start timestamp of the last successful run.
- `last_successful_crawl_completed_at`: Completion timestamp of the last successful run.
- `last_successful_crawl_mode`: Mode of the last successful run (`full` or `updates`).
- `pages`: Map of page ID → page metadata, including outgoing/incoming links.

Per-page fields commonly used by operations and integrations:
- `outgoing_links` and `incoming_links`: Bidirectional local graph edges.
- `attachments`: Saved attachment filenames under `output/attachments`.
- `attachment_signature`: Stable attachment metadata fingerprint used by updates mode dirty checks.

Use this to:
- Build secondary indices or search engines.
- Detect renames (title changed, ID stayed the same).
- Reconstruct the link graph for analysis or visualization.

## Useful commands

Validate config and credentials:

```sh
confluence2md validate
```

Full crawl:

```sh
confluence2md --mode full
```

Incremental refresh:

```sh
confluence2md --mode updates
```
