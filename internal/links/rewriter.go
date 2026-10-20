package links

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gkoos/confluence2md/internal/store"
)

// RewriteStats captures pass-2 local-link rewrite metrics.
type RewriteStats struct {
	PagesProcessed int
	PagesUpdated   int
	LinksSeen      int
	LinksRewritten int
}

// RewriteCrawledPageLinks rewrites links to crawled Confluence pages into relative
// local Markdown paths, leaving unresolved/external links unchanged.
//
// Link destinations are located by scanning the rendered Markdown instead of
// matching a regular expression, because a link label may itself contain
// balanced brackets (e.g. "[[fcp-sfd-crm] Decision title](https://...)"), which
// a "no closing bracket in the label" pattern cannot see. Destinations inside
// fenced code blocks and inline code spans are left untouched, so example links
// kept in documentation survive the pass verbatim.
func RewriteCrawledPageLinks(outputDir string, pages map[string]store.PageRecord) (RewriteStats, error) {
	stats := RewriteStats{}

	idToLocal := make(map[string]string, len(pages))
	for pageID, record := range pages {
		if strings.TrimSpace(record.LocalPath) != "" {
			idToLocal[pageID] = record.LocalPath
		}
	}

	for pageID, record := range pages {
		if strings.TrimSpace(record.LocalPath) == "" {
			continue
		}

		host := record.Host
		srcPath := filepath.Join(outputDir, record.LocalPath)
		srcDir := filepath.Dir(srcPath)

		contentBytes, err := os.ReadFile(srcPath)
		if err != nil {
			return stats, fmt.Errorf("read page %s: %w", srcPath, err)
		}

		stats.PagesProcessed++

		rewritten, seen, pageRewrites := rewriteLinkDestinations(string(contentBytes), func(target string) (string, bool) {
			return localLinkTarget(target, pageID, host, outputDir, srcDir, idToLocal)
		})
		stats.LinksSeen += seen
		stats.LinksRewritten += pageRewrites

		if pageRewrites == 0 {
			continue
		}

		if err := os.WriteFile(srcPath, []byte(rewritten), 0644); err != nil {
			return stats, fmt.Errorf("write rewritten page %s: %w", srcPath, err)
		}

		record.StorageFormat = rewritten
		pages[pageID] = record
		stats.PagesUpdated++
	}

	return stats, nil
}

// localLinkTarget maps a single link destination to the path the page file
// should use instead. It reports false when the destination must stay as it is:
// external link, page outside the crawled set, or no change.
func localLinkTarget(target, pageID, host, outputDir, srcDir string, idToLocal map[string]string) (string, bool) {
	targetRef := ExtractPageRefFromURL(target, host)
	if targetRef.ID <= 0 {
		return "", false
	}

	targetKey := store.PageKey(targetRef.Host, targetRef.ID)
	fragment := extractFragment(target)

	// A link into the page being rewritten only needs its fragment: the anchor
	// already resolves inside the file we are writing.
	if targetKey == pageID && fragment != "" {
		return "#" + fragment, true
	}

	targetLocal, ok := idToLocal[targetKey]
	if !ok {
		return "", false
	}

	relPath, err := filepath.Rel(srcDir, filepath.Join(outputDir, targetLocal))
	if err != nil {
		return "", false
	}

	relPath = filepath.ToSlash(relPath)
	if fragment != "" {
		relPath += "#" + fragment
	}
	if relPath == "" || relPath == target {
		return "", false
	}

	return relPath, true
}

func extractFragment(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return u.Fragment
}

// rewriteLinkDestinations rewrites every inline Markdown link and image
// destination in content, skipping destinations inside fenced code blocks and
// inline code spans. It returns the rewritten content, how many destinations
// were seen, and how many of them the callback changed.
func rewriteLinkDestinations(content string, rewrite func(dest string) (string, bool)) (string, int, int) {
	if !strings.Contains(content, "](") {
		return content, 0, 0
	}

	var out strings.Builder
	out.Grow(len(content))

	seen, rewritten := 0, 0
	inFence := false
	var fenceChar byte
	var fenceLength int

	for _, line := range strings.SplitAfter(content, "\n") {
		text := strings.TrimRight(line, "\r\n")
		ending := line[len(text):]

		if inFence {
			out.WriteString(text)
			out.WriteString(ending)
			if closesFence(text, fenceChar, fenceLength) {
				inFence = false
			}
			continue
		}

		if char, length, ok := openingFence(text); ok {
			inFence, fenceChar, fenceLength = true, char, length
			out.WriteString(text)
			out.WriteString(ending)
			continue
		}

		lineText, lineSeen, lineRewritten := rewriteLineLinkDestinations(text, rewrite)
		seen += lineSeen
		rewritten += lineRewritten
		out.WriteString(lineText)
		out.WriteString(ending)
	}

	return out.String(), seen, rewritten
}

// rewriteLineLinkDestinations rewrites the link destinations found on one line
// of Markdown. It returns the line, how many destinations were seen, and how
// many the callback changed.
func rewriteLineLinkDestinations(line string, rewrite func(dest string) (string, bool)) (string, int, int) {
	if !strings.Contains(line, "](") {
		return line, 0, 0
	}

	var out strings.Builder
	out.Grow(len(line))

	seen, rewritten := 0, 0
	copied := 0

	for i := 0; i < len(line); {
		switch line[i] {
		case '`':
			// Inline code spans are literal: step over the span (or over the
			// unmatched backtick run) so example links are not rewritten.
			i = codeSpanEnd(line, i)
		case ']':
			if i+1 >= len(line) || line[i+1] != '(' || isEscaped(line, i) {
				i++
				continue
			}
			if !closesLinkLabel(line, i) {
				i++
				continue
			}

			destStart, destEnd, ok := linkDestination(line, i+2)
			if !ok {
				i++
				continue
			}

			seen++
			dest := line[destStart:destEnd]
			if replacement, changed := rewrite(dest); changed && replacement != dest {
				out.WriteString(line[copied:destStart])
				out.WriteString(replacement)
				copied = destEnd
				rewritten++
			}
			i = destEnd
		default:
			i++
		}
	}

	if rewritten == 0 {
		return line, seen, rewritten
	}

	out.WriteString(line[copied:])
	return out.String(), seen, rewritten
}

// closesLinkLabel reports whether the "]" at closeIndex closes a link label,
// i.e. whether an unescaped "[" that pairs with it exists earlier on the line.
// Labels may nest balanced brackets ("[[tag] title](url)"), which is exactly
// what a "no bracket in the label" pattern cannot express.
func closesLinkLabel(s string, closeIndex int) bool {
	depth := 0
	for i := closeIndex; i >= 0; i-- {
		if isEscaped(s, i) {
			continue
		}
		switch s[i] {
		case ']':
			depth++
		case '[':
			depth--
			if depth == 0 {
				return true
			}
		}
	}
	return false
}

// linkDestination parses the destination of an inline link whose "(" sits at
// start-1. It returns the destination boundaries, or ok=false when the
// destination is missing or unterminated.
func linkDestination(s string, start int) (int, int, bool) {
	i := start
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	if i >= len(s) {
		return 0, 0, false
	}

	// Angle-bracketed destination: [text](<destination>).
	if s[i] == '<' {
		for j := i + 1; j < len(s); j++ {
			if s[j] == '>' && !isEscaped(s, j) {
				if j == i+1 {
					return 0, 0, false
				}
				return i + 1, j, true
			}
		}
		return 0, 0, false
	}

	begin := i
	depth := 0
	for ; i < len(s); i++ {
		if isEscaped(s, i) {
			continue
		}
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				if i == begin {
					return 0, 0, false
				}
				return begin, i, true
			}
			depth--
		case ' ', '\t':
			// A link title follows the destination; rewrite the path only.
			if i == begin {
				return 0, 0, false
			}
			return begin, i, true
		}
	}

	return 0, 0, false
}

// codeSpanEnd returns the index just past the inline code span that starts at
// start, or just past the unmatched backtick run when the span never closes.
func codeSpanEnd(s string, start int) int {
	runLength := 0
	for start+runLength < len(s) && s[start+runLength] == '`' {
		runLength++
	}

	for i := start + runLength; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		closing := 0
		for i+closing < len(s) && s[i+closing] == '`' {
			closing++
		}
		if closing == runLength {
			return i + closing
		}
		i += closing
	}

	return start + runLength
}

// isEscaped reports whether the character at index i is preceded by an odd
// number of backslashes, i.e. whether Markdown treats it as a literal.
func isEscaped(s string, i int) bool {
	backslashes := 0
	for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
		backslashes++
	}
	return backslashes%2 == 1
}

// openingFence reports whether line opens a fenced code block. A backtick
// fence's info string may not itself contain backticks (CommonMark), which
// keeps a longer closing fence from being read as a new opening fence.
func openingFence(line string) (byte, int, bool) {
	char, length, rest, ok := fenceRun(line)
	if !ok {
		return 0, 0, false
	}
	if char == '`' && strings.ContainsRune(rest, '`') {
		return 0, 0, false
	}
	return char, length, true
}

// closesFence reports whether line closes a fenced code block opened with char
// repeated length times.
func closesFence(line string, char byte, length int) bool {
	openedChar, openedLength, rest, ok := fenceRun(line)
	if !ok || openedChar != char || openedLength < length {
		return false
	}
	return strings.TrimSpace(rest) == ""
}

// fenceRun reports a fenced-code-block delimiter: up to three leading spaces,
// then three or more backticks or tildes. rest is whatever follows the run.
func fenceRun(line string) (char byte, length int, rest string, ok bool) {
	indent := 0
	for indent < len(line) && line[indent] == ' ' {
		indent++
	}
	if indent > 3 {
		return 0, 0, "", false
	}

	trimmed := line[indent:]
	if len(trimmed) < 3 {
		return 0, 0, "", false
	}

	char = trimmed[0]
	if char != '`' && char != '~' {
		return 0, 0, "", false
	}
	for length < len(trimmed) && trimmed[length] == char {
		length++
	}
	if length < 3 {
		return 0, 0, "", false
	}

	return char, length, trimmed[length:], true
}
