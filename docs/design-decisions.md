# Design Decisions

This document captures the architectural decisions behind the conversion pipeline and crawler. It is intended as source material for engineering articles and as a reference for contributors who want to understand *why* things are built the way they are, not just *what* they do.

For the reference-style description of *what* the renderer does, see [Markdown Conversion Internals](markdown-conversion.md).

---

## Why ADF instead of storage XML

Confluence exposes two page body formats:

- **Storage format** — a Confluence-proprietary XML dialect (`<ac:structured-macro>`, `<ri:page>`, `<ac:rich-text-body>`, etc.). Shared between Confluence Cloud and Data Center.
- **ADF (Atlassian Document Format)** — a typed JSON document tree. Cloud-only, introduced as the canonical format for the Confluence editor.

The original converter used storage XML. This had several structural problems:

1. **It is not a document tree — it is serialised HTML with annotations.** Parsing it correctly requires handling arbitrary HTML mixed with Confluence-specific XML elements. Edge cases around whitespace, nested macros, and block vs inline context multiplied over time.
2. **Macro content is opaque.** Macros like `<ac:structured-macro ac:name="code">` nest their parameters and body as XML children in inconsistent ways. Each macro required its own parser branch.
3. **Links are fragile.** Confluence link types (`ri:page`, `ri:url`, `ri:attachment`) are separate XML element types, each with different attribute names. Regex extraction was error-prone.
4. **Tables with spans require DOM-aware traversal.** Storage XML tables contain `<td>` elements with HTML attributes but structured as XML — a mismatch that made span detection brittle.

ADF solves all of these:

1. **It is a proper typed tree.** Every node has a `type` string, an `attrs` map, a `content` array, and an optional `marks` array. The structure is uniform and machine-readable without a separate schema document.
2. **Inline content is first-class.** Marks (`strong`, `em`, `link`, `code`, `textColor`, etc.) are declared as arrays on text nodes, not wrapped in HTML tags.
3. **Links are explicit.** A `link` mark carries `attrs.href`. An `inlineCard` or `blockCard` carries `attrs.url`. No ambiguity.
4. **Tables are structured.** `tableCell` nodes carry `attrs.colspan` and `attrs.rowspan` directly.

The trade-off is Cloud lock-in: ADF is not supported on Confluence Data Center or Server. The decision to pivot to ADF was made accepting that this closes the Data Center roadmap item permanently. The fidelity gain over the entire remaining node type space was judged worth it.

---

## Registry-based single-pass renderer

The renderer is a dispatch table (Go `map[string]func`) rather than a switch statement, a visitor interface, or an AST transformation stage.

**Why not a visitor interface?**

A visitor forces you to define what you do with *every* node type upfront. For a converter targeting an evolving external format like ADF — where Atlassian adds new node types without notice — this creates a maintenance burden: every new type causes a compile error or forces an explicit no-op implementation. The registry approach inverts this: unknown types silently fall through to `walkChildren`, so future ADF additions degrade gracefully without breaking existing output.

**Why not an AST transformation stage?**

A multi-stage pipeline (parse → normalise AST → render) would be appropriate if multiple output formats were needed. For a single-target converter (Markdown), it adds indirection without benefit. The render context (`RenderContext`) carries enough mutable state (list depth, list type, ordered index) to handle the cases that would otherwise require AST rewriting.

**Why single-pass?**

Two common reasons to do a second rendering pass are: (1) to resolve forward references (e.g. footnote numbers), and (2) to handle content that depends on surrounding context. ADF has neither of these. Markdown doesn't have footnotes in the GFM subset we target. The only post-render pass needed is *normalisation* (collapsing blank lines, ensuring block boundaries), which operates on the final string — not on the node tree.

---

## Mark ordering

ADF marks are an unordered array on a text node. The renderer must choose an application order because nested Markdown delimiters must be balanced correctly.

The chosen order is: `code` → `subsup` → `link` → `strong` → `em` → `strike` → `underline`.

The constraint driving this is: **code spans must be outermost when combined with other marks.** A Markdown code span (`` `text` ``) does not interpret its contents, so applying other marks inside it is meaningless. More importantly, some Markdown parsers treat `` `**text**` `` as a code span containing literal asterisks — which is the correct rendering. Applying `code` first ensures this.

`link` is applied after `code` / `subsup` because `[**bold**](url)` is valid Markdown (bold inside a link label) but `` **[`code`](url)** `` is not consistently parsed across renderers.

`textColor` is intentionally ignored. Markdown has no colour syntax. Emitting HTML `<span style="color:...">` would break most Markdown renderers that strip inline HTML, and would make the output fragile against downstream processing.

---

## The `ensureBlankLine` problem

Early in the ADF renderer, images and headings collided on the same line in output like:

```
![image](attachment://uuid)## My Heading
```

The root cause was that ADF `mediaSingle` and `mediaGroup` nodes are block-level elements but were rendered without any block boundary enforcement. The renderer emitted the image reference and then immediately walked the next sibling, which happened to be a heading.

The fix introduced `ensureBlankLine` — a helper that writes a blank line to the buffer only if one is not already present — called at the start of every block-level renderer (`renderParagraph`, `renderHeading`, `renderMediaSingle`, `renderMediaGroup`). This is cheaper than tracking block context in `RenderContext` and handles all combinations of adjacent block nodes without explicit pairwise rules.

---

## Table rendering fork: GFM vs HTML

GFM (GitHub-Flavored Markdown) pipe tables cannot represent:

- Cells that span multiple columns (`colspan > 1`)
- Cells that span multiple rows (`rowspan > 1`)
- Block content inside cells (headings, lists, code blocks)

When Confluence tables use any of these, a pipe table would silently drop or corrupt the content. The renderer detects these cases before choosing output format:

1. Scan all cells for `colspan` or `rowspan` > 1 → emit `<table>` HTML
2. Scan all cells for block-level child nodes (headings, lists, code blocks) → emit `<table>` HTML
3. Otherwise → emit GFM pipe table

The HTML fallback uses `<h1>`–`<h6>` tags for headings inside cells (not Markdown `#` syntax) because `#` inside an HTML `<td>` is not interpreted as a heading by any Markdown renderer.

This is a deliberate fidelity-over-portability choice. Pipe tables are more readable in raw Markdown, but silent data loss is worse than slightly less portable output.

---

## Two-pass link handling

Link rewriting is deliberately separated from rendering into two distinct passes:

**Pass 1 (render time):** The converter emits Confluence URLs verbatim. A `link` mark with `href` pointing to another Confluence page is rendered as `[text](https://org.atlassian.net/wiki/pages/viewpage.action?pageId=12345)`. The renderer has no knowledge of which pages were crawled.

**Pass 2 (post-crawl):** After all pages are written, `RewriteCrawledPageLinks` scans every output file for Markdown link targets, extracts page IDs from Confluence URL patterns, and replaces them with relative local paths if the target page was crawled.

Why separate passes?

- **The renderer runs concurrently during the crawl.** At render time, it is not yet known which pages will be in the final output set. A page linked from an early-crawled page may not have been processed yet.
- **Separation of concerns.** The renderer is a pure function of ADF JSON → Markdown string. It does not need to know about the crawl graph, the output directory layout, or which pages are in scope.
- **Idempotent rewriting.** The rewriter can be run again after partial crawls or updates without corrupting already-rewritten links.

---

## Dynamic macro problem: `children`, `contentbylabel`, and others

Several Confluence macros render page lists dynamically at display time. Their ADF representation is an `extension` node that carries only the macro configuration — not the resolved page list:

```json
{
  "type": "extension",
  "attrs": {
    "extensionKey": "children",
    ...
  }
}
```

There are no `href` or `url` fields in these nodes. A pure ADF renderer cannot discover the linked pages from the document alone.

**`children` / `pagetree` macros** — resolved by calling the Confluence v2 children API (`GET /wiki/api/v2/pages/{id}/children`) after fetching each page. The child page IDs are appended to `OutgoingLinks` for BFS traversal.

**`contentbylabel` macro** — carries a CQL (Confluence Query Language) expression in `attrs.parameters.macroParams.cql.value`. The crawler executes this CQL against the Confluence REST API (`GET /wiki/rest/api/search?cql=...`), collects the matching page IDs, adds them to `OutgoingLinks`, and appends a `## Related pages` section to the rendered Markdown. The link rewriter then converts these to local relative paths in pass 2, exactly like any other internal link.

The general principle: **for macros that the ADF document cannot resolve, the crawler must make additional API calls and inject the results into the crawl graph.** The render pipeline is responsible for injecting visible output (the Related pages section); the crawl pipeline is responsible for injecting discoverable pages (OutgoingLinks).

---

## Comment link extraction

Comments are fetched as ADF JSON bodies and rendered to Markdown, appended under a `## Comments` section. Originally, links inside comments were not extracted for crawl discovery — only the page body was scanned for `OutgoingLinks`.

This was a gap: a page referenced only in a comment thread would not be crawled. The fix scans each comment body through the same `ExtractPageIDsFromADFWithStats` function as the page body, merging results into `OutgoingLinks`. Comments are fetched before link extraction runs, so no ordering change was needed.

---

## Graceful degradation principle

Throughout the renderer and crawler, the design preference is **silent degradation over errors** for content-level issues, while maintaining **hard failures for configuration and infrastructure issues**.

Examples:
- Unknown ADF node type → walk children, emit whatever text is nested inside
- Children macro API call fails → log warning, continue with inline links only
- Comment fetch fails → log warning, page is still exported without comments
- Attachment download fails → log warning, attachment placeholder remains in Markdown

This matches the use case: a bulk export tool where a rendering imperfection on one page is far less harmful than aborting an entire crawl of hundreds of pages.

Infrastructure failures (bad credentials, network unreachable at start, invalid config) fail fast and loudly before any output is written.
