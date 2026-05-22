package convert

import (
	"encoding/xml"
	"html"
	"strings"
)

func (p *markdownParser) handleStructuredMacroStart(t xml.StartElement) {
	macroName := ""
	for _, attr := range t.Attr {
		if attr.Name.Local == "name" {
			macroName = strings.TrimSpace(attr.Value)
			break
		}
	}

	switch macroName {
	case "jira":
		p.inJiraMacro = true
		p.jiraParamName = ""
		p.jiraKey = ""
	case "code":
		p.inCodeMacro = true
		p.codeMacroLang = ""
		p.inCodeMacroBody = false
		p.codeMacroBody.Reset()
	}
}

func (p *markdownParser) handleStructuredMacroEnd() {
	if p.inJiraMacro {
		if p.jiraKey != "" {
			rendered := "[" + p.jiraKey + "](/browse/" + p.jiraKey + ")"
			p.writeInline(rendered)
		}
		p.inJiraMacro = false
		p.jiraParamName = ""
		p.jiraKey = ""
	}

	if p.inCodeMacro {
		body := strings.TrimRight(p.codeMacroBody.String(), "\n")
		if body != "" && !p.inTable {
			if strings.TrimSpace(p.out.String()) != "" {
				p.out.WriteString("\n")
			}
			p.out.WriteString("```" + p.codeMacroLang + "\n")
			p.out.WriteString(body)
			p.out.WriteString("\n```\n\n")
		}
		p.inCodeMacro = false
		p.inCodeMacroBody = false
		p.codeMacroLang = ""
		p.codeMacroBody.Reset()
	}
}

func (p *markdownParser) handleMacroParameterStart(t xml.StartElement) {
	p.inMacroParameter = true
	p.macroParamName = ""
	for _, attr := range t.Attr {
		if attr.Name.Local == "name" {
			p.macroParamName = strings.TrimSpace(attr.Value)
			break
		}
	}
	if p.inJiraMacro {
		p.jiraParamName = p.macroParamName
	}
}

func (p *markdownParser) handleMacroParameterEnd() {
	p.inMacroParameter = false
	p.macroParamName = ""
	p.jiraParamName = ""
}

func (p *markdownParser) handlePlainTextBodyStart() {
	if p.inCodeMacro {
		p.inCodeMacroBody = true
	}
}

func (p *markdownParser) handlePlainTextBodyEnd() {
	p.inCodeMacroBody = false
}

func (p *markdownParser) handleCodeMacroBodyCharData(t xml.CharData) {
	p.codeMacroBody.WriteString(html.UnescapeString(string(t)))
}

func (p *markdownParser) handleMacroParameterCharData(t xml.CharData) {
	text := strings.TrimSpace(html.UnescapeString(string(t)))
	if text == "" {
		return
	}
	if p.inJiraMacro && p.jiraParamName == "key" {
		p.jiraKey = text
	}
	if p.inCodeMacro && (p.macroParamName == "language" || p.macroParamName == "lang") {
		p.codeMacroLang = strings.ToLower(text)
	}
}

func (p *markdownParser) writeInline(rendered string) {
	if p.inCell {
		if needsInterTokenSpace(p.cellBuffer.String(), rendered) {
			p.cellBuffer.WriteString(" ")
		}
		p.cellBuffer.WriteString(rendered)
		return
	}
	if !p.inTable {
		if needsInterTokenSpace(p.out.String(), rendered) {
			p.out.WriteString(" ")
		}
		p.out.WriteString(rendered)
	}
}
