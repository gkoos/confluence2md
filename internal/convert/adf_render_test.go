package convert

import (
	"strings"
	"testing"
)

// adf builds a minimal ADF doc JSON string from the given content array JSON.
func adf(content string) string {
	return `{"version":1,"type":"doc","content":[` + content + `]}`
}

func TestADFRender_Heading(t *testing.T) {
	got, err := ToMarkdown(adf(`{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"My Section"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "## My Section") {
		t.Fatalf("expected heading, got:\n%s", got)
	}
}

func TestADFRender_TaskList(t *testing.T) {
	got, err := ToMarkdown(adf(`{"type":"taskList","content":[` +
		`{"type":"taskItem","attrs":{"state":"DONE"},"content":[{"type":"paragraph","content":[{"type":"text","text":"done task"}]}]},` +
		`{"type":"taskItem","attrs":{"state":"TODO"},"content":[{"type":"paragraph","content":[{"type":"text","text":"pending task"}]}]}` +
		`]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "- [x] done task") {
		t.Fatalf("expected checked task item, got:\n%s", got)
	}
	if !strings.Contains(got, "- [ ] pending task") {
		t.Fatalf("expected unchecked task item, got:\n%s", got)
	}
}

func TestADFRender_DecisionList(t *testing.T) {
	got, err := ToMarkdown(adf(`{"type":"decisionList","content":[` +
		`{"type":"decisionItem","attrs":{"state":"DECIDED"},"content":[{"type":"paragraph","content":[{"type":"text","text":"approved"}]}]},` +
		`{"type":"decisionItem","attrs":{"state":"UNDECIDED"},"content":[{"type":"paragraph","content":[{"type":"text","text":"open"}]}]}` +
		`]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "- [x] approved") {
		t.Fatalf("expected checked decision item, got:\n%s", got)
	}
	if !strings.Contains(got, "- [ ] open") {
		t.Fatalf("expected unchecked decision item, got:\n%s", got)
	}
}

func TestADFRender_Caption(t *testing.T) {
	got, err := ToMarkdown(adf(
		`{"type":"mediaSingle","content":[` +
			`{"type":"media","attrs":{"id":"abc-uuid","type":"file","collection":""}},` +
			`{"type":"caption","content":[{"type":"text","text":"Figure 1"}]}` +
			`]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "*Figure 1*") {
		t.Fatalf("expected italic caption, got:\n%s", got)
	}
}

func TestADFRender_CodeBlock(t *testing.T) {
	got, err := ToMarkdown(adf(`{"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"fmt.Println(\"hi\")"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "```go\nfmt.Println(\"hi\")\n```") {
		t.Fatalf("expected fenced code block, got:\n%s", got)
	}
}

func TestADFRender_GFMTable(t *testing.T) {
	got, err := ToMarkdown(adf(
		`{"type":"table","content":[` +
			`{"type":"tableRow","content":[` +
			`{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"A"}]}]},` +
			`{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"B"}]}]}` +
			`]},` +
			`{"type":"tableRow","content":[` +
			`{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"1"}]}]},` +
			`{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"2"}]}]}` +
			`]}` +
			`]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "| A | B |") {
		t.Fatalf("expected GFM header row, got:\n%s", got)
	}
	if !strings.Contains(got, "| --- | --- |") {
		t.Fatalf("expected GFM separator, got:\n%s", got)
	}
	if !strings.Contains(got, "| 1 | 2 |") {
		t.Fatalf("expected GFM data row, got:\n%s", got)
	}
}

func TestADFRender_HTMLTable_Colspan(t *testing.T) {
	got, err := ToMarkdown(adf(
		`{"type":"table","content":[` +
			`{"type":"tableRow","content":[` +
			`{"type":"tableCell","attrs":{"colspan":2},"content":[{"type":"paragraph","content":[{"type":"text","text":"merged"}]}]}` +
			`]}` +
			`]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<table>") {
		t.Fatalf("expected HTML table for colspan, got:\n%s", got)
	}
	if !strings.Contains(got, `colspan="2"`) {
		t.Fatalf("expected colspan attribute, got:\n%s", got)
	}
}

func TestADFRender_InlineCard(t *testing.T) {
	got, err := ToMarkdown(adf(`{"type":"paragraph","content":[{"type":"inlineCard","attrs":{"url":"https://example.com/page"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "[https://example.com/page](https://example.com/page)") {
		t.Fatalf("expected inlineCard link, got:\n%s", got)
	}
}

func TestADFRender_BlockCard(t *testing.T) {
	got, err := ToMarkdown(adf(`{"type":"paragraph","content":[{"type":"blockCard","attrs":{"url":"https://example.com/page"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "[https://example.com/page](https://example.com/page)") {
		t.Fatalf("expected blockCard link, got:\n%s", got)
	}
}

func TestADFRender_Mention(t *testing.T) {
	got, err := ToMarkdown(adf(`{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"acc-123","text":"@Jane Smith"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "@Jane Smith") {
		t.Fatalf("expected mention display name, got:\n%s", got)
	}
	if strings.Contains(got, "acc-123") {
		t.Fatalf("expected no raw accountId in output, got:\n%s", got)
	}
}

func TestADFRender_Status(t *testing.T) {
	got, err := ToMarkdown(adf(`{"type":"paragraph","content":[{"type":"status","attrs":{"text":"In Progress","color":"blue"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "**In Progress**") {
		t.Fatalf("expected bold status text, got:\n%s", got)
	}
}

func TestADFRender_Media_AttachmentUUID(t *testing.T) {
	got, err := ToMarkdown(adf(`{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"abc-def-uuid","type":"file","collection":""}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "attachment://abc-def-uuid") {
		t.Fatalf("expected attachment UUID placeholder, got:\n%s", got)
	}
}

func TestADFRender_Marks_StrongEmCode(t *testing.T) {
	got, err := ToMarkdown(adf(
		`{"type":"paragraph","content":[` +
			`{"type":"text","text":"bold","marks":[{"type":"strong"}]},` +
			`{"type":"text","text":" "},` +
			`{"type":"text","text":"italic","marks":[{"type":"em"}]},` +
			`{"type":"text","text":" "},` +
			`{"type":"text","text":"code","marks":[{"type":"code"}]}` +
			`]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "**bold**") {
		t.Fatalf("expected bold, got:\n%s", got)
	}
	if !strings.Contains(got, "_italic_") {
		t.Fatalf("expected italic, got:\n%s", got)
	}
	if !strings.Contains(got, "`code`") {
		t.Fatalf("expected code, got:\n%s", got)
	}
}

func TestADFRender_LinkMark(t *testing.T) {
	got, err := ToMarkdown(adf(`{"type":"paragraph","content":[{"type":"text","text":"click here","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "[click here](https://example.com)") {
		t.Fatalf("expected link, got:\n%s", got)
	}
}

func TestADFRender_JiraExtension(t *testing.T) {
	got, err := ToMarkdown(adf(`{"type":"paragraph","content":[{"type":"inlineExtension","attrs":{"extensionType":"com.atlassian.confluence.macro.core","extensionKey":"jira","macroParams":{"key":{"value":"PROJ-42"}}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "[PROJ-42](https://jira.atlassian.net/browse/PROJ-42)") {
		t.Fatalf("expected jira link, got:\n%s", got)
	}
}

func TestADFRender_UnknownNode_WalksChildren(t *testing.T) {
	got, err := ToMarkdown(adf(`{"type":"unknownFutureNode","content":[{"type":"paragraph","content":[{"type":"text","text":"inner text"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "inner text") {
		t.Fatalf("expected inner text from unknown node, got:\n%s", got)
	}
}

func TestAttrString_FallbackOnMissing(t *testing.T) {
	node := ADFNode{Type: "test", Attrs: nil}
	if got := attrString(node, "key", "default"); got != "default" {
		t.Fatalf("expected default, got %q", got)
	}
}

func TestAttrString_FallbackOnWrongType(t *testing.T) {
	node := ADFNode{Type: "test", Attrs: map[string]any{"key": 42.0}}
	if got := attrString(node, "key", "default"); got != "default" {
		t.Fatalf("expected default for wrong type, got %q", got)
	}
}

func TestAttrInt_FallbackOnMissing(t *testing.T) {
	node := ADFNode{Type: "test", Attrs: nil}
	if got := attrInt(node, "level", 3); got != 3 {
		t.Fatalf("expected 3, got %d", got)
	}
}

func TestAttrInt_FallbackOnWrongType(t *testing.T) {
	node := ADFNode{Type: "test", Attrs: map[string]any{"level": "two"}}
	if got := attrInt(node, "level", 1); got != 1 {
		t.Fatalf("expected fallback 1 for wrong type, got %d", got)
	}
}

func TestAttrInt_ReadsFloat64(t *testing.T) {
	node := ADFNode{Type: "test", Attrs: map[string]any{"level": float64(2)}}
	if got := attrInt(node, "level", 1); got != 2 {
		t.Fatalf("expected 2, got %d", got)
	}
}
