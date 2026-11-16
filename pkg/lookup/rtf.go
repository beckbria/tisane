// Package lookup decodes local .tsd dictionary definitions, converts RTF markup
// into sanitized semantic HTML and plain text, and builds fallback web lookup URLs.
package lookup

import (
	"bytes"
	"html"
	"strings"

	"tisane/pkg/tsd"
)

type rtfStyle struct {
	bold       bool
	italic     bool
	underline  bool
	sub        bool
	super      bool
	ucSkip     int
	leftIndent int
	ignoreDest bool
}

type rtfSpan struct {
	text      string
	bold      bool
	italic    bool
	underline bool
	sub       bool
	super     bool
	lineBreak bool
}

type rtfParagraph struct {
	leftIndent int
	spans      []rtfSpan
}

var ignoredRTFDestinations = map[string]bool{
	"fonttbl":    true,
	"colortbl":   true,
	"stylesheet": true,
	"info":       true,
	"pict":       true,
	"object":     true,
	"header":     true,
	"footer":     true,
	"footnote":   true,
	"fldinst":    true,
}

// parseRTFDocument parses an RTF byte slice (starting with "{\rtf1") into structured paragraphs.
func parseRTFDocument(raw []byte) []rtfParagraph {
	stack := make([]rtfStyle, 0, 16)
	cur := rtfStyle{ucSkip: 1}

	var paragraphs []rtfParagraph
	var curSpans []rtfSpan
	var textBuf strings.Builder
	curTextStyle := cur
	skipFallbackChars := 0
	justOpenedGroup := false
	minNonZeroIndent := 0

	flushSpan := func() {
		if textBuf.Len() == 0 {
			return
		}
		curSpans = append(curSpans, rtfSpan{
			text:      textBuf.String(),
			bold:      curTextStyle.bold,
			italic:    curTextStyle.italic,
			underline: curTextStyle.underline,
			sub:       curTextStyle.sub,
			super:     curTextStyle.super,
		})
		textBuf.Reset()
	}

	syncStyle := func() {
		if cur.bold != curTextStyle.bold ||
			cur.italic != curTextStyle.italic ||
			cur.underline != curTextStyle.underline ||
			cur.sub != curTextStyle.sub ||
			cur.super != curTextStyle.super {
			flushSpan()
			curTextStyle = cur
		}
	}

	emitRune := func(r rune) {
		if cur.ignoreDest {
			return
		}
		syncStyle()
		textBuf.WriteRune(r)
	}

	flushParagraph := func() {
		if cur.ignoreDest {
			return
		}
		flushSpan()
		// Trim trailing whitespace on final span; skip completely empty trailing paragraphs at end.
		if cur.leftIndent > 0 && (minNonZeroIndent == 0 || cur.leftIndent < minNonZeroIndent) {
			minNonZeroIndent = cur.leftIndent
		}
		paragraphs = append(paragraphs, rtfParagraph{
			leftIndent: cur.leftIndent,
			spans:      curSpans,
		})
		curSpans = nil
	}

	n := len(raw)
	for i := 0; i < n; {
		b := raw[i]
		if b == '\r' || b == '\n' {
			i++
			continue
		}
		if b == '{' {
			stack = append(stack, cur)
			justOpenedGroup = true
			skipFallbackChars = 0
			i++
			continue
		}
		if b == '}' {
			flushSpan()
			if len(stack) > 0 {
				cur = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
			curTextStyle = cur
			justOpenedGroup = false
			skipFallbackChars = 0
			i++
			continue
		}

		if b == '\\' {
			i++
			if i >= n {
				break
			}
			nb := raw[i]
			switch {
			case nb == '*':
				if justOpenedGroup {
					cur.ignoreDest = true
				}
				justOpenedGroup = false
				i++
			case nb == '\\' || nb == '{' || nb == '}':
				justOpenedGroup = false
				if skipFallbackChars > 0 {
					skipFallbackChars--
				} else {
					emitRune(rune(nb))
				}
				i++
			case nb == '~':
				justOpenedGroup = false
				if skipFallbackChars > 0 {
					skipFallbackChars--
				} else {
					emitRune('\u00A0')
				}
				i++
			case nb == '_':
				justOpenedGroup = false
				if skipFallbackChars > 0 {
					skipFallbackChars--
				} else {
					emitRune('-')
				}
				i++
			case nb == '-':
				// Optional hyphen
				justOpenedGroup = false
				if skipFallbackChars > 0 {
					skipFallbackChars--
				}
				i++
			case nb == '\'':
				justOpenedGroup = false
				i++
				var hexVal byte
				for k := 0; k < 2 && i < n; k++ {
					hb := raw[i]
					hexVal <<= 4
					switch {
					case hb >= '0' && hb <= '9':
						hexVal += hb - '0'
					case hb >= 'a' && hb <= 'f':
						hexVal += hb - 'a' + 10
					case hb >= 'A' && hb <= 'F':
						hexVal += hb - 'A' + 10
					}
					i++
				}
				if skipFallbackChars > 0 {
					skipFallbackChars--
				} else {
					emitRune(tsd.DecodeCP1252Byte(hexVal))
				}
			case (nb >= 'A' && nb <= 'Z') || (nb >= 'a' && nb <= 'z'):
				cwStart := i
				for i < n && ((raw[i] >= 'A' && raw[i] <= 'Z') || (raw[i] >= 'a' && raw[i] <= 'z')) {
					i++
				}
				cw := string(raw[cwStart:i])
				hasParam := false
				negParam := false
				paramVal := 0
				if i < n && raw[i] == '-' {
					negParam = true
					i++
				}
				for i < n && raw[i] >= '0' && raw[i] <= '9' {
					hasParam = true
					paramVal = paramVal*10 + int(raw[i]-'0')
					i++
				}
				if negParam {
					paramVal = -paramVal
				}
				if i < n && raw[i] == ' ' {
					i++
				}

				if justOpenedGroup && ignoredRTFDestinations[cw] {
					cur.ignoreDest = true
				}
				justOpenedGroup = false

				if cur.ignoreDest && cw != "bin" {
					continue
				}

				switch cw {
				case "bin":
					if paramVal > 0 {
						i += paramVal
						if i > n {
							i = n
						}
					}
				case "uc":
					if hasParam && paramVal >= 0 {
						cur.ucSkip = paramVal
					}
				case "u":
					if hasParam {
						emitRune(rune(uint16(int16(paramVal))))
						skipFallbackChars = cur.ucSkip
					}
				case "b":
					cur.bold = !hasParam || paramVal != 0
				case "i":
					cur.italic = !hasParam || paramVal != 0
				case "ul":
					cur.underline = !hasParam || paramVal != 0
				case "ulnone":
					cur.underline = false
				case "sub":
					cur.sub = !hasParam || paramVal != 0
					if cur.sub {
						cur.super = false
					}
				case "super":
					cur.super = !hasParam || paramVal != 0
					if cur.super {
						cur.sub = false
					}
				case "nosupersub":
					cur.sub = false
					cur.super = false
				case "plain":
					cur.bold = false
					cur.italic = false
					cur.underline = false
					cur.sub = false
					cur.super = false
				case "pard":
					cur.leftIndent = 0
				case "li":
					if hasParam {
						cur.leftIndent = paramVal
					}
				case "par", "sect", "page":
					skipFallbackChars = 0
					flushParagraph()
				case "line":
					skipFallbackChars = 0
					flushSpan()
					curSpans = append(curSpans, rtfSpan{lineBreak: true})
				case "tab":
					emitRune('\t')
				case "emdash":
					emitRune('—')
				case "endash":
					emitRune('–')
				case "bullet":
					emitRune('•')
				case "lquote":
					emitRune('‘')
				case "rquote":
					emitRune('’')
				case "ldblquote":
					emitRune('“')
				case "rdblquote":
					emitRune('”')
				}
			default:
				justOpenedGroup = false
				i++
			}
			continue
		}

		justOpenedGroup = false
		if skipFallbackChars > 0 {
			skipFallbackChars--
			i++
			continue
		}
		emitRune(tsd.DecodeCP1252Byte(b))
		i++
	}

	flushSpan()
	if len(curSpans) > 0 {
		if cur.leftIndent > 0 && (minNonZeroIndent == 0 || cur.leftIndent < minNonZeroIndent) {
			minNonZeroIndent = cur.leftIndent
		}
		paragraphs = append(paragraphs, rtfParagraph{
			leftIndent: cur.leftIndent,
			spans:      curSpans,
		})
	}

	// Normalize relative indentation (e.g. \li180 base header vs \li360 body in TEA dictionaries).
	if minNonZeroIndent > 0 {
		for idx := range paragraphs {
			if paragraphs[idx].leftIndent >= minNonZeroIndent {
				paragraphs[idx].leftIndent -= minNonZeroIndent
			}
		}
	}

	// Drop trailing empty paragraphs produced by closing "\par }".
	for len(paragraphs) > 0 && isParagraphEmpty(paragraphs[len(paragraphs)-1]) {
		paragraphs = paragraphs[:len(paragraphs)-1]
	}
	return paragraphs
}

func isParagraphEmpty(p rtfParagraph) bool {
	for _, s := range p.spans {
		if s.lineBreak || strings.TrimSpace(s.text) != "" {
			return false
		}
	}
	return true
}

// linkifyAndEscapeHTML escapes text for HTML and converts http://, https://, and mailto: URLs
// into safe clickable anchor tags.
func linkifyAndEscapeHTML(s string) string {
	var out strings.Builder
	out.Grow(len(s) + 16)

	i := 0
	for i < len(s) {
		idxHTTP := indexURLStart(s[i:])
		if idxHTTP < 0 {
			out.WriteString(html.EscapeString(s[i:]))
			break
		}
		urlStart := i + idxHTTP
		out.WriteString(html.EscapeString(s[i:urlStart]))

		urlEnd := urlStart
		for urlEnd < len(s) {
			c := s[urlEnd]
			if c <= ' ' || c == '<' || c == '>' || c == '"' || c == '{' || c == '}' {
				break
			}
			urlEnd++
		}
		// Strip trailing sentence punctuation ('.', ',', ';', ')', '!') unless balanced '(' inside URL.
		for urlEnd > urlStart {
			last := s[urlEnd-1]
			if last == '.' || last == ',' || last == ';' || last == ':' || last == '!' || last == '?' {
				urlEnd--
				continue
			}
			if last == ')' && strings.Count(s[urlStart:urlEnd], "(") < strings.Count(s[urlStart:urlEnd], ")") {
				urlEnd--
				continue
			}
			break
		}

		rawURL := s[urlStart:urlEnd]
		escapedURL := html.EscapeString(rawURL)
		out.WriteString(`<a href="`)
		out.WriteString(escapedURL)
		out.WriteString(`" target="_blank" rel="noopener noreferrer">`)
		out.WriteString(escapedURL)
		out.WriteString(`</a>`)
		i = urlEnd
	}
	return out.String()
}

func indexURLStart(s string) int {
	best := -1
	for _, prefix := range []string{"http://", "https://", "mailto:"} {
		if idx := strings.Index(s, prefix); idx >= 0 && (best < 0 || idx < best) {
			best = idx
		}
	}
	return best
}

// DefinitionToHTML converts a raw CP1252 definition payload (plain text or {\rtf1...})
// into sanitized semantic HTML.
func DefinitionToHTML(rawBody []byte) string {
	if !bytes.HasPrefix(rawBody, []byte(`{\rtf1`)) {
		plain := strings.ReplaceAll(tsd.DecodeCP1252(rawBody), "\r\n", "\n")
		plain = strings.TrimSpace(plain)
		if plain == "" {
			return ""
		}
		lines := strings.Split(plain, "\n")
		var sb strings.Builder
		for _, ln := range lines {
			sb.WriteString(`<p class="tsd-par">`)
			sb.WriteString(linkifyAndEscapeHTML(ln))
			sb.WriteString(`</p>`)
		}
		return sb.String()
	}

	paras := parseRTFDocument(rawBody)
	var sb strings.Builder
	for _, p := range paras {
		if p.leftIndent > 0 {
			sb.WriteString(`<p class="tsd-par tsd-indent">`)
		} else {
			sb.WriteString(`<p class="tsd-par">`)
		}
		for _, sp := range p.spans {
			if sp.lineBreak {
				sb.WriteString(`<br>`)
				continue
			}
			if sp.text == "" {
				continue
			}
			if sp.bold {
				sb.WriteString(`<strong>`)
			}
			if sp.italic {
				sb.WriteString(`<em>`)
			}
			if sp.underline {
				sb.WriteString(`<u>`)
			}
			if sp.sub {
				sb.WriteString(`<sub>`)
			} else if sp.super {
				sb.WriteString(`<sup>`)
			}

			sb.WriteString(linkifyAndEscapeHTML(sp.text))

			if sp.sub {
				sb.WriteString(`</sub>`)
			} else if sp.super {
				sb.WriteString(`</sup>`)
			}
			if sp.underline {
				sb.WriteString(`</u>`)
			}
			if sp.italic {
				sb.WriteString(`</em>`)
			}
			if sp.bold {
				sb.WriteString(`</strong>`)
			}
		}
		sb.WriteString(`</p>`)
	}
	return sb.String()
}

// DefinitionToText converts a raw CP1252 definition payload (plain text or {\rtf1...})
// into clean UTF-8 plain text suitable for terminal display or clipboard copy.
func DefinitionToText(rawBody []byte) string {
	if !bytes.HasPrefix(rawBody, []byte(`{\rtf1`)) {
		plain := strings.ReplaceAll(tsd.DecodeCP1252(rawBody), "\r\n", "\n")
		return strings.TrimSpace(plain)
	}

	paras := parseRTFDocument(rawBody)
	var sb strings.Builder
	for pIdx, p := range paras {
		if pIdx > 0 {
			sb.WriteByte('\n')
		}
		indentPrefix := ""
		if p.leftIndent > 0 {
			indentPrefix = "  "
		}
		var lineBuf strings.Builder
		for _, sp := range p.spans {
			if sp.lineBreak {
				lineBuf.WriteByte('\n')
				lineBuf.WriteString(indentPrefix)
				continue
			}
			lineBuf.WriteString(strings.ReplaceAll(sp.text, "\u00A0", " "))
		}
		text := strings.TrimRight(lineBuf.String(), " \t")
		if text != "" {
			sb.WriteString(indentPrefix)
			sb.WriteString(strings.TrimLeft(text, " "))
		}
	}
	return strings.TrimSpace(sb.String())
}
