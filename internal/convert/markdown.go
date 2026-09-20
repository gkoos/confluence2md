package convert

import (
	"strings"
)

// ToMarkdown converts Confluence ADF JSON to Markdown.
func ToMarkdown(adfJSON string) (string, error) {
	return adfToMarkdown(adfJSON)
}

// normalizeMarkdown collapses blank-line runs and cleans up spacing around
// headings, tables and horizontal rules. Fenced code blocks are passed through
// verbatim: collapsing happens per line in the loop below and only outside a
// fence, never as a whole-document replacement, which would also rewrite code
// content.
func normalizeMarkdown(s string) string {
	lines := strings.Split(s, "\n")
	var result []string
	inFence := false
	inTable := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") {
			if inTable && len(result) > 0 && result[len(result)-1] != "" {
				result = append(result, "")
			}
			inTable = false
			inFence = !inFence
			result = append(result, trimmed)
			continue
		}

		if inFence {
			result = append(result, line)
			continue
		}

		if isHorizontalRule(trimmed) {
			if inTable && len(result) > 0 && result[len(result)-1] != "" {
				result = append(result, "")
			}
			inTable = false
			if len(result) > 0 && result[len(result)-1] != "" {
				result = append(result, "")
			}
			result = append(result, trimmed)
			result = append(result, "")
			continue
		}

		isTableLine := strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|")
		if isTableLine {
			if !inTable && len(result) > 0 && result[len(result)-1] != "" {
				result = append(result, "")
			}
			inTable = true
			result = append(result, trimmed)
			continue
		}

		if inTable && trimmed != "" {
			if len(result) > 0 && result[len(result)-1] != "" {
				result = append(result, "")
			}
			inTable = false
		}

		if isMarkdownHeading(trimmed) {
			if len(result) > 0 && result[len(result)-1] != "" {
				result = append(result, "")
			}
			result = append(result, trimmed)
			result = append(result, "")
			continue
		}

		if trimmed != "" {
			result = append(result, line)
			continue
		}

		if len(result) > 0 && result[len(result)-1] != "" {
			result = append(result, "")
		}
	}

	return strings.Join(result, "\n")
}

func isMarkdownHeading(line string) bool {
	if line == "" || line[0] != '#' {
		return false
	}

	count := 0
	for count < len(line) && line[count] == '#' {
		count++
	}

	if count == 0 || count > 6 {
		return false
	}

	if len(line) == count {
		return false
	}

	return line[count] == ' '
}

func isHorizontalRule(line string) bool {
	return line == "---" || line == "***" || line == "___"
}
