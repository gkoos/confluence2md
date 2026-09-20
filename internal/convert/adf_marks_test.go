package convert

import "testing"

// TestApplyMarks_AppliesMarksInDocumentedPriorityOrder pins the documented mark
// order (code → subsup → link → strong → em → strike → underline → textColor),
// which must hold regardless of the order the marks arrive in from ADF.
func TestApplyMarks_AppliesMarksInDocumentedPriorityOrder(t *testing.T) {
	code := ADFMark{Type: "code"}
	strong := ADFMark{Type: "strong"}
	em := ADFMark{Type: "em"}
	underline := ADFMark{Type: "underline"}
	link := ADFMark{Type: "link", Attrs: map[string]any{"href": "https://example.com"}}

	// code innermost, then strong, em and finally underline.
	const formattedWant = "<u>_**`value`**_</u>"

	t.Run("unordered marks render in priority order", func(t *testing.T) {
		got := ApplyMarks("value", []ADFMark{underline, strong, code, em})
		if got != formattedWant {
			t.Fatalf("ApplyMarks = %q, want %q", got, formattedWant)
		}
	})

	t.Run("input order does not affect the result", func(t *testing.T) {
		got := ApplyMarks("value", []ADFMark{code, underline, em, strong})
		if got != formattedWant {
			t.Fatalf("ApplyMarks = %q, want %q", got, formattedWant)
		}
	})

	t.Run("link is applied before strong and em", func(t *testing.T) {
		// link (2) is applied before strong (3) and em (4), so those marks wrap the
		// finished link instead of only its text.
		const want = "_**[docs](https://example.com)**_"
		got := ApplyMarks("docs", []ADFMark{strong, link, em})
		if got != want {
			t.Fatalf("ApplyMarks = %q, want %q", got, want)
		}
	})

	t.Run("marks without a Markdown equivalent leave the text untouched", func(t *testing.T) {
		marks := []ADFMark{
			{Type: "textColor", Attrs: map[string]any{"color": "#333"}},
			{Type: "subsup"}, // no attrs: neither sub nor sup
			{Type: "someFutureMark"},
		}
		if got := ApplyMarks("plain", marks); got != "plain" {
			t.Fatalf("ApplyMarks = %q, want %q", got, "plain")
		}
	})

	t.Run("no marks returns the text unchanged", func(t *testing.T) {
		if got := ApplyMarks("plain", nil); got != "plain" {
			t.Fatalf("ApplyMarks = %q, want %q", got, "plain")
		}
	})
}
