// Package lookup decodes local .tsd dictionary definitions, converts RTF markup
// into sanitized semantic HTML and plain text, and builds fallback web lookup URLs.
package lookup

import (
	"bytes"
	"html"
	"strings"
	"unicode"
	"unicode/utf16"

	"tisane/pkg/tsd"
)

const (
	maxRTFParam      = 1 << 28
	maxRTFGroupDepth = 512
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
	"fonttbl":           true,
	"colortbl":          true,
	"stylesheet":        true,
	"info":              true,
	"pict":              true,
	"object":            true,
	"header":            true,
	"headerl":           true,
	"headerr":           true,
	"headerf":           true,
	"footer":            true,
	"footerl":           true,
	"footerr":           true,
	"footerf":           true,
	"footnote":          true,
	"fldinst":           true,
	"listtable":         true,
	"listoverridetable": true,
}

// parseRTFDocument parses an RTF byte slice (starting with "{\rtf1") into structured paragraphs.
func parseRTFDocument(raw []byte) []rtfParagraph {
	stack := make([]rtfStyle, 0, 16)
	overflowDepth := 0
	cur := rtfStyle{ucSkip: 1}

	var paragraphs []rtfParagraph
	var curSpans []rtfSpan
	var textBuf strings.Builder
	curTextStyle := cur
	skipFallbackChars := 0
	justOpenedGroup := false
	var highSurrogate rune

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

	flushPendingSurrogate := func() {
		if highSurrogate != 0 {
			highSurrogate = 0
			if !cur.ignoreDest {
				syncStyle()
				textBuf.WriteRune(unicode.ReplacementChar)
			}
		}
	}

	emitRune := func(r rune) {
		if r == 0 || cur.ignoreDest {
			highSurrogate = 0
			return
		}
		flushPendingSurrogate()
		syncStyle()
		textBuf.WriteRune(r)
	}

	flushParagraph := func() {
		if cur.ignoreDest {
			return
		}
		flushPendingSurrogate()
		flushSpan()
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
			if len(stack) >= maxRTFGroupDepth {
				overflowDepth++
			} else {
				stack = append(stack, cur)
			}
			justOpenedGroup = true
			skipFallbackChars = 0
			i++
			continue
		}
		if b == '}' {
			flushPendingSurrogate()
			flushSpan()
			if overflowDepth > 0 {
				overflowDepth--
			} else if len(stack) > 0 {
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
				if i+1 < n {
					h1, ok1 := parseHexNibble(raw[i])
					h2, ok2 := parseHexNibble(raw[i+1])
					if ok1 && ok2 {
						i += 2
						hexVal := (h1 << 4) | h2
						if skipFallbackChars > 0 {
							skipFallbackChars--
						} else if hexVal != 0 {
							emitRune(tsd.DecodeCP1252Byte(hexVal))
						}
					}
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
					if paramVal < maxRTFParam {
						paramVal = paramVal*10 + int(raw[i]-'0')
						if paramVal > maxRTFParam {
							paramVal = maxRTFParam
						}
					}
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
						if paramVal >= n-i {
							i = n
						} else {
							i += paramVal
						}
					}
				case "uc":
					if hasParam && paramVal >= 0 {
						cur.ucSkip = paramVal
					}
				case "u":
					if hasParam {
						u := rune(uint16(int16(paramVal)))
						if u >= 0xD800 && u <= 0xDBFF {
							flushPendingSurrogate()
							if !cur.ignoreDest {
								highSurrogate = u
							}
						} else if u >= 0xDC00 && u <= 0xDFFF && highSurrogate != 0 {
							combined := utf16.DecodeRune(highSurrogate, u)
							highSurrogate = 0
							emitRune(combined)
						} else {
							emitRune(u)
						}
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

	flushPendingSurrogate()
	flushSpan()
	if len(curSpans) > 0 {
		paragraphs = append(paragraphs, rtfParagraph{
			leftIndent: cur.leftIndent,
			spans:      curSpans,
		})
	}

	// Drop trailing empty paragraphs produced by closing "\par }" before normalizing indentation.
	for len(paragraphs) > 0 && isParagraphEmpty(paragraphs[len(paragraphs)-1]) {
		paragraphs = paragraphs[:len(paragraphs)-1]
	}

	// Normalize relative indentation (e.g. \li180 base header vs \li360 body in TEA dictionaries,
	// while preserving \li360 when a \pard header paragraph has leftIndent == 0).
	if len(paragraphs) > 0 {
		minIndent := -1
		for _, p := range paragraphs {
			if p.leftIndent >= 0 && (minIndent < 0 || p.leftIndent < minIndent) {
				minIndent = p.leftIndent
			}
		}
		if minIndent > 0 {
			for idx := range paragraphs {
				if paragraphs[idx].leftIndent >= minIndent {
					paragraphs[idx].leftIndent -= minIndent
				}
			}
		}
	}
	return paragraphs
}

func parseHexNibble(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	default:
		return 0, false
	}
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
// into safe clickable anchor tags in O(len(s)) time.
func linkifyAndEscapeHTML(s string) string {
	var out strings.Builder
	out.Grow(len(s) + 16)

	i := 0
	for i < len(s) {
		idxURL, prefixLen := indexURLStart(s[i:])
		if idxURL < 0 {
			out.WriteString(html.EscapeString(s[i:]))
			break
		}
		urlStart := i + idxURL
		out.WriteString(html.EscapeString(s[i:urlStart]))

		urlEnd := urlStart + prefixLen
		openParens := 0
		closeParens := 0
		for urlEnd < len(s) {
			c := s[urlEnd]
			if c <= ' ' || c == '<' || c == '>' || c == '"' || c == '{' || c == '}' {
				break
			}
			if c == '(' {
				openParens++
			} else if c == ')' {
				closeParens++
			}
			urlEnd++
		}
		minBodyEnd := urlStart + prefixLen
		// Strip trailing sentence punctuation unless balanced '(' inside URL.
		for urlEnd > minBodyEnd {
			last := s[urlEnd-1]
			if last == '.' || last == ',' || last == ';' || last == ':' || last == '!' || last == '?' || last == '\'' || last == ']' {
				urlEnd--
				continue
			}
			if last == ')' && closeParens > openParens {
				urlEnd--
				closeParens--
				continue
			}
			break
		}

		if urlEnd <= minBodyEnd {
			out.WriteString(html.EscapeString(s[urlStart:minBodyEnd]))
			i = minBodyEnd
			continue
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

func indexURLStart(s string) (idx int, prefixLen int) {
	best := -1
	bestLen := 0
	for _, prefix := range []string{"http://", "https://", "mailto:"} {
		if pos := strings.Index(s, prefix); pos >= 0 && (best < 0 || pos < best) {
			best = pos
			bestLen = len(prefix)
		}
	}
	return best, bestLen
}

func renderParagraphsHTML(paras []rtfParagraph) string {
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

func renderParagraphsText(paras []rtfParagraph) string {
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
	return strings.TrimRight(sb.String(), "\n")
}

func renderDefinitionBoth(rawBody []byte) (htmlOut string, textOut string) {
	if !bytes.HasPrefix(rawBody, []byte(`{\rtf1`)) {
		plain := strings.ReplaceAll(tsd.DecodeCP1252(rawBody), "\r\n", "\n")
		plain = strings.TrimSpace(plain)
		if plain == "" {
			return "", ""
		}
		lines := strings.Split(plain, "\n")
		var sb strings.Builder
		for _, ln := range lines {
			sb.WriteString(`<p class="tsd-par">`)
			sb.WriteString(linkifyAndEscapeHTML(ln))
			sb.WriteString(`</p>`)
		}
		return sb.String(), plain
	}
	paras := parseRTFDocument(rawBody)
	return renderParagraphsHTML(paras), strings.TrimSpace(renderParagraphsText(paras))
}

// DefinitionToHTML converts a raw CP1252 definition payload (plain text or {\rtf1...})
// into sanitized semantic HTML.
func DefinitionToHTML(rawBody []byte) string {
	if !bytes.HasPrefix(rawBody, []byte(`{\rtf1`)) {
		htmlOut, _ := renderDefinitionBoth(rawBody)
		return htmlOut
	}
	return renderParagraphsHTML(parseRTFDocument(rawBody))
}

// DefinitionToText converts a raw CP1252 definition payload (plain text or {\rtf1...})
// into clean UTF-8 plain text suitable for terminal display or clipboard copy.
func DefinitionToText(rawBody []byte) string {
	if !bytes.HasPrefix(rawBody, []byte(`{\rtf1`)) {
		plain := strings.ReplaceAll(tsd.DecodeCP1252(rawBody), "\r\n", "\n")
		return strings.TrimSpace(plain)
	}
	return strings.TrimSpace(renderParagraphsText(parseRTFDocument(rawBody)))
}
