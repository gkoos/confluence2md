package convert

// ADFDoc is the top-level Atlassian Document Format document.
type ADFDoc struct {
	Version int       `json:"version"`
	Type    string    `json:"type"`
	Content []ADFNode `json:"content"`
}

// ADFNode represents any ADF node (block or inline).
type ADFNode struct {
	Type    string         `json:"type"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Content []ADFNode      `json:"content,omitempty"`
	Text    string         `json:"text,omitempty"`
	Marks   []ADFMark      `json:"marks,omitempty"`
}

// ADFMark represents a formatting mark applied to a text node.
type ADFMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// attrString returns attrs[key] as a string, or fallback if absent or wrong type.
func attrString(n ADFNode, key, fallback string) string {
	if n.Attrs == nil {
		return fallback
	}
	v, ok := n.Attrs[key]
	if !ok {
		return fallback
	}
	s, ok := v.(string)
	if !ok {
		return fallback
	}
	return s
}

// attrInt returns attrs[key] as an int, or fallback if absent or wrong type.
// ADF numbers unmarshal as float64 from JSON.
func attrInt(n ADFNode, key string, fallback int) int {
	if n.Attrs == nil {
		return fallback
	}
	v, ok := n.Attrs[key]
	if !ok {
		return fallback
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	default:
		return fallback
	}
}

// markAttrString returns mark.attrs[key] as a string, or fallback.
func markAttrString(m ADFMark, key, fallback string) string {
	if m.Attrs == nil {
		return fallback
	}
	v, ok := m.Attrs[key]
	if !ok {
		return fallback
	}
	s, ok := v.(string)
	if !ok {
		return fallback
	}
	return s
}
