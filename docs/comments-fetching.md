# Comments Fetching Internals

This document describes how page comments are fetched and enriched before rendering to markdown.

## Scope

Comments fetching is separate from page-body conversion:

- Fetching/enrichment: [internal/confluence/comments_client.go](../internal/confluence/comments_client.go)
- Rendering: [internal/convert/comments.go](../internal/convert/comments.go)

## API strategy

The crawler uses Confluence Cloud comments and users endpoints.

Root comments endpoint:

- `GET /wiki/api/v2/pages/{pageId}/footer-comments?limit=100&body-format=storage`

Child replies endpoint:

- `GET /wiki/api/v2/footer-comments/{commentId}/children?limit=100&body-format=storage`

Author enrichment endpoint:

- `POST /wiki/api/v2/users-bulk`

## Fetch flow

1. Fetch root footer comments for the page.
2. Traverse replies breadth-first via `.../children` until exhausted.
3. Collect unique `version.authorId` values while traversing.
4. Resolve author IDs to display names with one users-bulk request.
5. Map comments into `CommentData` for the convert pipeline.

## Pagination

Any response with `_links.next` continues pagination. Relative `next` links are resolved against the configured Confluence base URL.

## Author handling

- Source field in v2 comments is `version.authorId`.
- Display names are fetched in bulk (`accountId -> displayName`).
- If lookup fails or a user is missing, the raw author ID remains as fallback.
- **Rate limit note:** The bulk user fetch (`POST /wiki/api/v2/users-bulk`) counts against your Confluence Cloud API quota. For pages with many unique comment authors, this can incur measurable cost. Consider lowering `rate_limit_rpm` or increasing `retry.initial_backoff_ms` if you crawl high-comment-volume spaces.

## Failure behavior

- Comment fetch errors are non-fatal for page export.
- The page markdown still exports; comments section may be omitted.
- Crawl summary tracks comment-fetch warnings.

## Output contract

`GetPageComments` returns a flat `[]CommentData` including parent IDs.

- Thread/order logic is handled in [internal/convert/comments.go](../internal/convert/comments.go).
- Body text is passed in storage format and converted to plain text during rendering.

