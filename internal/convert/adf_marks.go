package convert

import (
	"cmp"
	"slices"
)

// ApplyMarks applies ADF marks to text in the correct order (innermost first).
// Order: code → subsup → link → strong → em → strike → underline → textColor (ignored)
func ApplyMarks(text string, marks []ADFMark) string {
	if len(marks) == 0 {
		return text
	}

	// Build a priority map so we can apply in the defined order regardless of
	// the order marks appear in the JSON.
	order := map[string]int{
		"code":      0,
		"subsup":    1,
		"link":      2,
		"strong":    3,
		"em":        4,
		"strike":    5,
		"underline": 6,
		"textColor": 7,
	}

	type indexedMark struct {
		mark  ADFMark
		order int
	}
	indexed := make([]indexedMark, 0, len(marks))
	for _, m := range marks {
		o, ok := order[m.Type]
		if !ok {
			o = 99
		}
		indexed = append(indexed, indexedMark{m, o})
	}
	// Stable sort by priority: marks with equal priority (a repeated mark, or
	// unknown types sharing the fallback priority) keep their original order.
	slices.SortStableFunc(indexed, func(a, b indexedMark) int {
		return cmp.Compare(a.order, b.order)
	})

	result := text
	for _, im := range indexed {
		result = applyMark(result, im.mark)
	}
	return result
}

func applyMark(text string, mark ADFMark) string {
	switch mark.Type {
	case "code":
		return "`" + text + "`"
	case "subsup":
		tag := markAttrString(mark, "type", "")
		switch tag {
		case "sub":
			return "<sub>" + text + "</sub>"
		case "sup":
			return "<sup>" + text + "</sup>"
		}
		return text
	case "link":
		href := markAttrString(mark, "href", "")
		if href == "" {
			return text
		}
		return "[" + text + "](" + href + ")"
	case "strong":
		return "**" + text + "**"
	case "em":
		return "_" + text + "_"
	case "strike":
		return "~~" + text + "~~"
	case "underline":
		return "<u>" + text + "</u>"
	case "textColor":
		// Intentionally ignored — no Markdown equivalent
		return text
	default:
		return text
	}
}
