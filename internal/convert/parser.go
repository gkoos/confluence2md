package convert

import (
	"bytes"
	"encoding/xml"
	"html"
	"net/url"
	"strings"
)

type markdownParser struct {
	out                 bytes.Buffer
	inTable             bool
	inRow               bool
	inCell              bool
	cellBuffer          bytes.Buffer
	rowCells            []string
	needHeaderSeparator bool
	linkHref            string
	inACLink            bool
	acLinkHref          string
	acLinkPageID        string
	acLinkSpaceKey      string
	acLinkBuffer        bytes.Buffer
	acLinkTitle         string
	inCode              bool
	inJiraMacro         bool
	jiraParamName       string
	jiraKey             string
	inMacroParameter    bool
	macroParamName      string
	inCodeMacro         bool
	codeMacroLang       string
	inCodeMacroBody     bool
	codeMacroBody       bytes.Buffer
	inUserRef           bool
	userDisplay         string
	listStack           []string
	inACImage           bool
	acImageAlt          string
	acImageFilename     string
	acImageURL          string
}

func newMarkdownParser() *markdownParser {
	return &markdownParser{listStack: []string{}}
}

func (p *markdownParser) handleToken(token xml.Token) {
	switch t := token.(type) {
	case xml.StartElement:
		p.handleStartElement(t)
	case xml.EndElement:
		p.handleEndElement(t)
	case xml.CharData:
		p.handleCharData(t)
	}
}

func (p *markdownParser) handleStartElement(t xml.StartElement) {
	switch t.Name.Local {
	case "p":
		if !p.inTable && !p.inCell {
			p.out.WriteString("")
		}

	case "strong":
		if p.inCell {
			p.cellBuffer.WriteString("**")
		} else if !p.inTable {
			p.out.WriteString("**")
		}

	case "time":
		if p.inCell {
			for _, attr := range t.Attr {
				if attr.Name.Local == "datetime" {
					p.cellBuffer.WriteString(attr.Value)
				}
			}
		} else if !p.inTable {
			for _, attr := range t.Attr {
				if attr.Name.Local == "datetime" {
					p.out.WriteString(attr.Value)
				}
			}
		}

	case "em", "i":
		if p.inCell {
			p.cellBuffer.WriteString("*")
		} else if !p.inTable {
			p.out.WriteString("*")
		}

	case "h1", "h2", "h3", "h4", "h5", "h6":
		if !p.inTable && !p.inCell {
			level := 1
			if len(t.Name.Local) == 2 && t.Name.Local[1] >= '1' && t.Name.Local[1] <= '6' {
				level = int(t.Name.Local[1] - '0')
			}
			p.out.WriteString(strings.Repeat("#", level) + " ")
		}

	case "br":
		if p.inCell {
			p.cellBuffer.WriteString(" ")
		} else if !p.inTable {
			p.out.WriteString("\n")
		}

	case "ul":
		p.listStack = append(p.listStack, "ul")

	case "ol":
		p.listStack = append(p.listStack, "ol")

	case "li":
		if len(p.listStack) > 0 {
			if p.listStack[len(p.listStack)-1] == "ol" {
				p.out.WriteString("1. ")
			} else {
				p.out.WriteString("- ")
			}
		}

	case "table":
		p.inTable = true

	case "tr":
		p.inRow = true
		p.rowCells = []string{}
		p.needHeaderSeparator = false

	case "td":
		p.inCell = true
		p.cellBuffer.Reset()

	case "th":
		p.inCell = true
		p.needHeaderSeparator = true
		p.cellBuffer.Reset()

	case "a":
		p.linkHref = ""
		for _, attr := range t.Attr {
			if attr.Name.Local == "href" {
				p.linkHref = attr.Value
				break
			}
		}
		if p.inCell {
			if needsInterTokenSpace(p.cellBuffer.String(), "[") {
				p.cellBuffer.WriteString(" ")
			}
			p.cellBuffer.WriteString("[")
		} else if !p.inTable {
			if needsInterTokenSpace(p.out.String(), "[") {
				p.out.WriteString(" ")
			}
			p.out.WriteString("[")
		}

	case "code":
		p.inCode = true
		if p.inCell {
			if needsInterTokenSpace(p.cellBuffer.String(), "`") {
				p.cellBuffer.WriteString(" ")
			}
			p.cellBuffer.WriteString("`")
		} else if !p.inTable {
			if needsInterTokenSpace(p.out.String(), "`") {
				p.out.WriteString(" ")
			}
			p.out.WriteString("`")
		}

	case "pre":
		if !p.inTable {
			p.out.WriteString("```\n\n")
		}

	case "blockquote":
		if !p.inTable {
			p.out.WriteString("> ")
		}

	case "ac:link", "link":
		p.inACLink = true
		p.acLinkHref = ""
		p.acLinkPageID = ""
		p.acLinkSpaceKey = ""
		p.acLinkBuffer.Reset()
		p.acLinkTitle = ""

	case "ri:url", "url":
		if p.inACImage {
			for _, attr := range t.Attr {
				if attr.Name.Local == "value" {
					p.acImageURL = strings.TrimSpace(attr.Value)
					break
				}
			}
			return
		}
		for _, attr := range t.Attr {
			if attr.Name.Local == "value" {
				p.acLinkHref = attr.Value
				break
			}
		}

	case "ri:page", "page":
		for _, attr := range t.Attr {
			if attr.Name.Local == "content-title" {
				p.acLinkTitle = html.UnescapeString(attr.Value)
			}
			if attr.Name.Local == "content-id" {
				p.acLinkPageID = attr.Value
			}
			if attr.Name.Local == "space-key" {
				p.acLinkSpaceKey = attr.Value
			}
		}

	case "ri:user", "user":
		p.inUserRef = true
		p.userDisplay = ""
		for _, attr := range t.Attr {
			if attr.Name.Local == "username" || attr.Name.Local == "userkey" || attr.Name.Local == "account-id" {
				if strings.TrimSpace(attr.Value) != "" {
					p.userDisplay = "@" + strings.TrimSpace(attr.Value)
					break
				}
			}
		}

	case "ac:image", "image":
		p.inACImage = true
		p.acImageAlt = "image"
		p.acImageFilename = ""
		p.acImageURL = ""
		for _, attr := range t.Attr {
			if attr.Name.Local == "alt" && strings.TrimSpace(attr.Value) != "" {
				p.acImageAlt = html.UnescapeString(strings.TrimSpace(attr.Value))
			}
		}

	case "ri:attachment", "attachment":
		if p.inACImage {
			for _, attr := range t.Attr {
				if attr.Name.Local == "filename" {
					p.acImageFilename = html.UnescapeString(strings.TrimSpace(attr.Value))
					break
				}
			}
		}

	case "ac:structured-macro", "structured-macro":
		p.handleStructuredMacroStart(t)

	case "ac:parameter", "parameter":
		p.handleMacroParameterStart(t)

	case "ac:plain-text-body", "plain-text-body":
		p.handlePlainTextBodyStart()

	case "hr":
		if !p.inTable {
			p.out.WriteString("\n---\n")
		}
	}
}

func (p *markdownParser) handleEndElement(t xml.EndElement) {
	switch t.Name.Local {
	case "p":
		if p.inCell {
			p.cellBuffer.WriteString(" ")
		} else if !p.inTable {
			p.out.WriteString("\n")
		}

	case "strong":
		if p.inCell {
			p.cellBuffer.WriteString("**")
		} else if !p.inTable {
			p.out.WriteString("**")
		}

	case "em", "i":
		if p.inCell {
			p.cellBuffer.WriteString("*")
		} else if !p.inTable {
			p.out.WriteString("*")
		}

	case "h1", "h2", "h3", "h4", "h5", "h6":
		if !p.inTable && !p.inCell {
			p.out.WriteString("\n")
		}

	case "ul", "ol":
		if len(p.listStack) > 0 {
			p.listStack = p.listStack[:len(p.listStack)-1]
		}
		if !p.inTable {
			p.out.WriteString("\n")
		}

	case "li":
		if !p.inTable {
			p.out.WriteString("\n")
		}

	case "table":
		p.inTable = false
		p.out.WriteString("\n")

	case "tr":
		if p.inRow && len(p.rowCells) > 0 {
			p.out.WriteString("| " + strings.Join(p.rowCells, " | ") + " |\n")
			if p.needHeaderSeparator {
				separators := make([]string, len(p.rowCells))
				for i := range separators {
					separators[i] = "---"
				}
				p.out.WriteString("| " + strings.Join(separators, " | ") + " |\n")
			}
		}
		p.inRow = false

	case "td", "th":
		if p.inCell {
			cellContent := strings.TrimSpace(p.cellBuffer.String())
			cellContent = strings.ReplaceAll(cellContent, "\n", " ")
			cellContent = normalizeInlineSpacing(cellContent)
			p.rowCells = append(p.rowCells, cellContent)
			p.inCell = false
			p.cellBuffer.Reset()
			p.linkHref = ""
			p.acLinkTitle = ""
		}

	case "a":
		if p.inCell {
			if p.linkHref != "" {
				p.cellBuffer.WriteString("](" + p.linkHref + ")")
			} else {
				p.cellBuffer.WriteString("]")
			}
		} else if !p.inTable {
			if p.linkHref != "" {
				p.out.WriteString("](" + p.linkHref + ")")
			} else {
				p.out.WriteString("]")
			}
		}
		p.linkHref = ""

	case "code":
		p.inCode = false
		if p.inCell {
			p.cellBuffer.WriteString("`")
		} else if !p.inTable {
			p.out.WriteString("`")
		}

	case "pre":
		if !p.inTable {
			p.out.WriteString("```\n")
		}

	case "blockquote":
		if !p.inTable {
			p.out.WriteString("\n")
		}

	case "ac:link", "link":
		if p.acLinkHref == "" && p.acLinkPageID != "" {
			p.acLinkHref = "/wiki/pages/viewpage.action?pageId=" + p.acLinkPageID
		}
		if p.acLinkHref == "" && p.acLinkSpaceKey != "" && p.acLinkTitle != "" {
			p.acLinkHref = "/wiki/pages/viewpage.action?spaceKey=" + url.QueryEscape(p.acLinkSpaceKey) + "&title=" + url.QueryEscape(p.acLinkTitle)
		}
		if p.acLinkHref == "" && p.acLinkTitle != "" {
			p.acLinkHref = "/wiki/search?text=" + url.QueryEscape(p.acLinkTitle)
		}

		renderedText := strings.TrimSpace(p.acLinkBuffer.String())
		if renderedText == "" {
			renderedText = p.acLinkTitle
		}

		if renderedText != "" {
			rendered := renderedText
			if p.acLinkHref != "" {
				rendered = "[" + renderedText + "](" + p.acLinkHref + ")"
			}

			if p.inCell {
				if needsInterTokenSpace(p.cellBuffer.String(), rendered) {
					p.cellBuffer.WriteString(" ")
				}
				p.cellBuffer.WriteString(rendered)
			} else if !p.inTable {
				if needsInterTokenSpace(p.out.String(), rendered) {
					p.out.WriteString(" ")
				}
				p.out.WriteString(rendered)
			}
		}

		p.inACLink = false
		p.acLinkHref = ""
		p.acLinkPageID = ""
		p.acLinkSpaceKey = ""
		p.acLinkBuffer.Reset()
		p.acLinkTitle = ""

	case "ac:parameter", "parameter":
		p.handleMacroParameterEnd()

	case "ac:plain-text-body", "plain-text-body":
		p.handlePlainTextBodyEnd()

	case "ac:structured-macro", "structured-macro":
		p.handleStructuredMacroEnd()

	case "ri:user", "user":
		if p.inUserRef && p.inACLink && strings.TrimSpace(p.acLinkBuffer.String()) == "" && p.userDisplay != "" {
			p.acLinkBuffer.WriteString(p.userDisplay)
		}
		p.inUserRef = false
		p.userDisplay = ""

	case "ac:image", "image":
		alt := strings.TrimSpace(p.acImageAlt)
		if alt == "" {
			alt = "image"
		}

		target := ""
		if strings.TrimSpace(p.acImageFilename) != "" {
			target = "attachment://" + p.acImageFilename
		} else if strings.TrimSpace(p.acImageURL) != "" {
			target = p.acImageURL
		}

		rendered := "![" + alt + "]"
		if target != "" {
			rendered += "(" + target + ")"
		}

		if p.inCell {
			if needsInterTokenSpace(p.cellBuffer.String(), rendered) {
				p.cellBuffer.WriteString(" ")
			}
			p.cellBuffer.WriteString(rendered)
		} else if !p.inTable {
			if strings.TrimSpace(p.out.String()) != "" {
				p.out.WriteString("\n\n")
			}
			p.out.WriteString(rendered)
			p.out.WriteString("\n\n")
		}

		p.inACImage = false
		p.acImageAlt = ""
		p.acImageFilename = ""
		p.acImageURL = ""
	}
}

func (p *markdownParser) handleCharData(t xml.CharData) {
	if p.inCodeMacro && p.inCodeMacroBody {
		p.handleCodeMacroBodyCharData(t)
		return
	}

	if p.inMacroParameter {
		p.handleMacroParameterCharData(t)
		return
	}

	text := html.UnescapeString(string(t))
	if p.inCode {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		if p.inCell {
			p.cellBuffer.WriteString(text)
		} else if !p.inTable {
			p.out.WriteString(text)
		}
		return
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	if p.inJiraMacro {
		return
	}

	if p.inACLink {
		if needsInterTokenSpace(p.acLinkBuffer.String(), text) {
			p.acLinkBuffer.WriteString(" ")
		}
		p.acLinkBuffer.WriteString(text)
		return
	}

	if p.inCell {
		if needsInterTokenSpace(p.cellBuffer.String(), text) {
			p.cellBuffer.WriteString(" ")
		}
		p.cellBuffer.WriteString(text)
		return
	}

	if !p.inTable {
		if needsInterTokenSpace(p.out.String(), text) {
			p.out.WriteString(" ")
		}
		p.out.WriteString(text)
	}
}
