# Markdown Conversion Internals

This document describes how page body conversion works in full crawl mode.

For comment API traversal and author enrichment details, see [Comments Fetching Internals](comments-fetching.md).
For attachment metadata/download/link-rewrite details, see [Attachments Retrieval Internals](attachments-fetching.md).

## Goal

Convert Confluence storage-format XML into stable, readable Markdown while preserving link intent and avoiding formatting regressions.

## Entry point

Conversion starts in [internal/convert/markdown.go](../internal/convert/markdown.go) via ToMarkdown.

High-level flow:

1. Decode storage XML as a token stream using encoding/xml.
2. Feed tokens to a stateful parser in [internal/convert/parser.go](../internal/convert/parser.go).
3. Render Markdown incrementally to an output buffer.
4. Run normalization helpers to enforce safe spacing and block boundaries.

## Parser architecture

The parser is stateful. It tracks context such as:

- Current table/row/cell state
- Inline code state
- Structured link state (ac:link, ri:page, ri:url)
- Macro state (jira/code)
- List stack (ul/ol nesting)

Token handling is split by type:

- StartElement: opens blocks/inline markers and captures attributes
- EndElement: closes blocks and emits completed structures
- CharData: emits text according to the current state

### Macro handlers

Macro logic is isolated in [internal/convert/parser_macros.go](../internal/convert/parser_macros.go).

Current handled macros:

- jira: ac:structured-macro name=jira -> `[KEY](/browse/KEY)`
- code: ac:structured-macro name=code with language/lang + plain-text-body -> fenced code block

This separation keeps the core parser loop smaller and makes macro additions lower risk.

## Link handling during conversion

### HTML links

- &lt;a href="..."&gt;text&lt;/a&gt; -> \[text\](url)

### Structured Confluence links

For ac:link values, conversion prefers:

1. ri:url value when present
2. pageId link from ri:page content-id
3. title-based Confluence URL from space-key + content-title
4. search fallback when only title exists

Mentions with ri:user are rendered using a fallback handle (for example @account-id) when no visible link body is present.

### Attachment image links

Confluence image macros are converted to Markdown image syntax during parsing:

- ac:image with ri:attachment filename -> ![alt](attachment://filename.ext)
- ac:image with ri:url value -> ![alt](absolute-url)

The attachment:// scheme is an internal placeholder. It allows conversion to stay deterministic before binary downloads are attempted.

## Tables

Tables are buffered row-by-row and emitted as Markdown tables:

- Header rows emit separator row automatically
- Cell text is normalized for inline whitespace
- Normalization later preserves table block boundaries so renderers parse tables reliably

## Normalization stage

After raw rendering, conversion runs normalization in [internal/convert/markdown.go](../internal/convert/markdown.go):

- Collapses excessive blank lines
- Preserves fenced code blocks
- Ensures horizontal rules are isolated with blank lines (avoids accidental Setext headings)
- Preserves table block boundaries
- Applies strict heading detection (only valid ATX heading syntax)

Inline spacing helpers prevent token-join artifacts around punctuation, links, and emphasis markers.

## What conversion does not do

- It does not rewrite internal Confluence URLs to local file paths.
  - That happens later in pass 2 via [internal/links/rewriter.go](../internal/links/rewriter.go).
- It does not download attachment binaries.
  - Downloads happen in the crawl write pipeline, then attachment:// placeholders are rewritten to local attachments/... paths.
- It does not fetch comments itself.
  - Comment retrieval is a separate pipeline concern.

## Regression safety

Conversion behavior is protected by tests in [internal/convert/markdown_test.go](../internal/convert/markdown_test.go), including:

- Targeted unit tests for known fragile formatting rules
- Golden fixture tests under [internal/convert/testdata/golden](../internal/convert/testdata/golden)

When changing conversion behavior, update or add golden fixtures intentionally so output drift is explicit and reviewed.
