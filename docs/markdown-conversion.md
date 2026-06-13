# Markdown Conversion Internals

This document describes how page body conversion works in full crawl mode.

For comment API traversal and author enrichment details, see [Comments Fetching Internals](comments-fetching.md).
For attachment metadata/download/link-rewrite details, see [Attachments Retrieval Internals](attachments-fetching.md).

## Goal

Convert a Confluence page body — delivered as Atlassian Document Format (ADF) JSON — into stable, readable Markdown while preserving link intent and avoiding formatting regressions.

## Entry point

Conversion starts in [internal/convert/markdown.go](../internal/convert/markdown.go) via `ToMarkdown`.

High-level flow:

1. Receive the raw ADF JSON string (`atlas_doc_format` representation from the v2 API).
2. Unmarshal into `ADFDoc` / `ADFNode` structs defined in [internal/convert/adf_types.go](../internal/convert/adf_types.go).
3. Walk the node tree with `Walk` in [internal/convert/adf_render.go](../internal/convert/adf_render.go), dispatching each node type through the `Registry`.
4. Run normalization helpers to enforce safe spacing and block boundaries.

## Renderer architecture

The renderer is registry-based and single-pass — there are no intermediate placeholders and no second pass over the buffer.

```go
// Registry maps ADF node types to render functions.
var Registry = map[string]func(ADFNode, *RenderContext, *strings.Builder){
    "paragraph":     renderParagraph,
    "heading":       renderHeading,
    "bulletList":    renderBulletList,
    // …
}
```

`Walk` looks up each node's `type` in `Registry`. Unknown node types silently fall through to `walkChildren`, so future ADF additions degrade gracefully rather than erroring.

Render context (`RenderContext`) carries mutable state threaded through the walk:

- `ListDepth int` — current nesting level for bullets/ordered lists
- `ListType string` — `"bullet"` or `"ordered"` at the current level
- `OrderedIdx int` — sequential counter for ordered list items

### Block renderers

Located in [internal/convert/adf_blocks.go](../internal/convert/adf_blocks.go):

| Node type | Output |
|---|---|
| `paragraph` | blank-line-delimited prose |
| `heading` | ATX `#`–`######` prefix |
| `bulletList` / `orderedList` | nested `-` / `1.` items |
| `taskList` / `taskItem` | `- [x]` / `- [ ]` checkboxes |
| `decisionList` / `decisionItem` | `DECIDED:` / `UNDECIDED:` prefix |
| `blockquote` | `> ` prefix |
| `codeBlock` | fenced ` ``` ` with language hint |
| `rule` | `---` |
| `table` | GFM pipe table or HTML `<table>` for spans |
| `panel` / `expand` | blockquote-wrapped with heading |
| `caption` | italicised inline (`*text*`) |
| `layoutSection` / `layoutColumn` | transparent passthrough |

### Inline renderers

Located in [internal/convert/adf_inline.go](../internal/convert/adf_inline.go):

| Node type | Output |
|---|---|
| `text` | literal text with marks applied |
| `hardBreak` | `\n` |
| `mention` | `@DisplayName` (from `attrs.text`) |
| `emoji` | short-name or Unicode |
| `date` | ISO date string |
| `inlineCard` / `blockCard` | bare URL |
| `status` | `[STATUS]` label |
| `media` / `mediaInline` | `![alt](attachment://uuid)` |

### Marks

Located in [internal/convert/adf_marks.go](../internal/convert/adf_marks.go). Applied in this order to avoid nesting conflicts: `code` → `subsup` → `link` → `strong` → `em` → `strike` → `underline`. The `textColor` mark is intentionally ignored (Markdown has no colour syntax).

### Extension handler

Located in [internal/convert/adf_extension.go](../internal/convert/adf_extension.go):

- `inlineExtension` / `extension` with `extensionKey = "jira"` → `[KEY](https://jira.atlassian.net/browse/KEY)`
- All other extensions walk their children.

## Tables

`renderTable` in [internal/convert/adf_table.go](../internal/convert/adf_table.go) examines every cell's `rowspan` / `colspan` attrs before choosing an output format:

- No spans → GFM pipe table (fully Markdown-compatible)
- Any span > 1 → raw `<table>` HTML (Markdown cannot represent spanning cells)

## Media UUID resolution

ADF `media` nodes carry an `attrs.id` field that is the Media Services UUID — the same value exposed as `fileId` in the v2 attachments API (`AttachmentScheme.FileID`).

Renderers emit `![alt](attachment://uuid)` using that UUID as the key. After binary download, [cmd/crawler/link_utils.go](../cmd/crawler/link_utils.go) builds a `byFileID` map from `AttachmentResult.FileID` to local paths and rewrites those placeholders. This means the converter never needs to make an extra API call to resolve attachment identities.

## Link handling during conversion

### Inline links

ADF `text` nodes with a `link` mark → `[text](url)`. The URL comes directly from the mark's `attrs.href`.

### Page links

`inlineCard` and `blockCard` nodes carry the Confluence page URL in `attrs.url`. These are emitted as full URLs. A subsequent rewrite pass ([internal/links/rewriter.go](../internal/links/rewriter.go)) converts any URL that maps to a crawled page ID into a relative local path.

### Attachment image links

`media` / `mediaInline` nodes are converted to Markdown image syntax:

- `media` with `type = "file"` → `![alt](attachment://uuid)`
- `media` with `type = "external"` → `![alt](absolute-url)`

The `attachment://uuid` scheme is an internal placeholder resolved after binary downloads are attempted.

## Normalization stage

After raw rendering, `normalizeMarkdown` in [internal/convert/markdown.go](../internal/convert/markdown.go):

- Collapses excessive blank lines
- Preserves fenced code blocks
- Ensures horizontal rules are isolated with blank lines (avoids accidental Setext headings)
- Preserves table block boundaries
- Applies strict heading detection (only valid ATX heading syntax)

## What conversion does not do

- It does not rewrite internal Confluence URLs to local file paths.
  - That happens later in a separate rewrite pass via [internal/links/rewriter.go](../internal/links/rewriter.go).
- It does not download attachment binaries.
  - Downloads happen in the crawl write pipeline, then attachment:// placeholders are rewritten to local attachments/... paths.
- It does not fetch comments itself.
  - Comment retrieval is a separate pipeline concern.

## Regression safety

Conversion behavior is protected by tests in [internal/convert/markdown_test.go](../internal/convert/markdown_test.go), including:

- Targeted unit tests for known fragile formatting rules
- Golden fixture tests under [internal/convert/testdata/golden](../internal/convert/testdata/golden)

When changing conversion behavior, update or add golden fixtures intentionally so output drift is explicit and reviewed.
