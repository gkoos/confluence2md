package convert

import (
	"encoding/xml"
	"strings"
	"unicode"
)

// ToMarkdown converts Confluence storage format XML to Markdown.
func ToMarkdown(storageXML string) (string, error) {
	parser := newMarkdownParser()

	decoder := xml.NewDecoder(strings.NewReader(storageXML))
	decoder.Strict = false
	decoder.AutoClose = nil

	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		parser.handleToken(token)
	}

	// Clean up and normalize
	result := parser.out.String()
	result = strings.TrimSpace(result)
	result = normalizeMarkdown(result)

	return result, nil
}

func normalizeMarkdown(s string) string {
	// Remove multiple consecutive newlines
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}

	// Clean up spacing around headers and tables while preserving fenced code blocks.
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

func normalizeInlineSpacing(s string) string {
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}

	// Avoid artifacts around formatting and link delimiters.
	replacer := strings.NewReplacer(
		"[ ", "[",
		" ]", "]",
		"( ", "(",
		" )", ")",
	)

	return strings.TrimSpace(replacer.Replace(s))
}

func needsInterTokenSpace(current, next string) bool {
	if current == "" || next == "" {
		return false
	}

	last := current[len(current)-1]
	first := next[0]

	// If we already have whitespace at the join point, never add another.
	if unicode.IsSpace(rune(last)) || unicode.IsSpace(rune(first)) {
		return false
	}

	if last == '*' || last == '_' {
		if strings.HasSuffix(current, "**") || strings.HasSuffix(current, "__") {
			if len(current) == 2 {
				return false
			}
			prev := rune(current[len(current)-3])
			if unicode.IsSpace(prev) || strings.ContainsRune("([{/>", prev) {
				return false
			}
		} else {
			if len(current) == 1 {
				return false
			}
			prev := rune(current[len(current)-2])
			if unicode.IsSpace(prev) || strings.ContainsRune("([{/>", prev) {
				return false
			}
		}
	}

	if strings.ContainsRune("[(/", rune(last)) {
		return false
	}
	if strings.ContainsRune(")]*,.;:!?/", rune(first)) {
		return false
	}

	return true
}
