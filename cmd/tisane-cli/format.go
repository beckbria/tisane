package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"tisane/pkg/lookup"
	"tisane/pkg/pattern"
	"tisane/pkg/search"
	"tisane/pkg/tsd"
)

// cp1252HighASCII maps Windows-1252 bytes (0x80..0xFF) to ASCII transliterations
// for /O:1..4 (--output-mode) text transforms.
var cp1252HighASCII = [128]string{
	0x8A - 0x80: "S",
	0x8C - 0x80: "OE",
	0x8E - 0x80: "Z",
	0x9A - 0x80: "s",
	0x9C - 0x80: "oe",
	0x9E - 0x80: "z",
	0x9F - 0x80: "Y",
	0xC0 - 0x80: "A",
	0xC1 - 0x80: "A",
	0xC2 - 0x80: "A",
	0xC3 - 0x80: "A",
	0xC4 - 0x80: "A",
	0xC5 - 0x80: "A",
	0xC6 - 0x80: "AE",
	0xC7 - 0x80: "C",
	0xC8 - 0x80: "E",
	0xC9 - 0x80: "E",
	0xCA - 0x80: "E",
	0xCB - 0x80: "E",
	0xCC - 0x80: "I",
	0xCD - 0x80: "I",
	0xCE - 0x80: "I",
	0xCF - 0x80: "I",
	0xD0 - 0x80: "D",
	0xD1 - 0x80: "N",
	0xD2 - 0x80: "O",
	0xD3 - 0x80: "O",
	0xD4 - 0x80: "O",
	0xD5 - 0x80: "O",
	0xD6 - 0x80: "O",
	0xD8 - 0x80: "O",
	0xD9 - 0x80: "U",
	0xDA - 0x80: "U",
	0xDB - 0x80: "U",
	0xDC - 0x80: "U",
	0xDD - 0x80: "Y",
	0xDE - 0x80: "TH",
	0xDF - 0x80: "ss",
	0xE0 - 0x80: "a",
	0xE1 - 0x80: "a",
	0xE2 - 0x80: "a",
	0xE3 - 0x80: "a",
	0xE4 - 0x80: "a",
	0xE5 - 0x80: "a",
	0xE6 - 0x80: "ae",
	0xE7 - 0x80: "c",
	0xE8 - 0x80: "e",
	0xE9 - 0x80: "e",
	0xEA - 0x80: "e",
	0xEB - 0x80: "e",
	0xEC - 0x80: "i",
	0xED - 0x80: "i",
	0xEE - 0x80: "i",
	0xEF - 0x80: "i",
	0xF0 - 0x80: "d",
	0xF1 - 0x80: "n",
	0xF2 - 0x80: "o",
	0xF3 - 0x80: "o",
	0xF4 - 0x80: "o",
	0xF5 - 0x80: "o",
	0xF6 - 0x80: "o",
	0xF8 - 0x80: "o",
	0xF9 - 0x80: "u",
	0xFA - 0x80: "u",
	0xFB - 0x80: "u",
	0xFC - 0x80: "u",
	0xFD - 0x80: "y",
	0xFE - 0x80: "th",
	0xFF - 0x80: "y",
}

// transformWordAndAnnot applies the /O:0..4 (--output-mode) text transform to word (+ optional annotation).
func transformWordAndAnnot(word, annot string, showAnnot bool, mode int) string {
	combined := word
	if showAnnot && annot != "" {
		if strings.HasPrefix(annot, " ") {
			combined += annot
		} else {
			combined += " " + annot
		}
	}
	if mode <= 0 {
		return combined
	}

	cpBytes, _ := tsd.EncodeCP1252(combined)
	var sb strings.Builder
	sb.Grow(len(cpBytes))

	for _, b := range cpBytes {
		if b >= 0x80 {
			repl := cp1252HighASCII[b-0x80]
			if repl == "" {
				continue
			}
			switch mode {
			case 1, 2:
				sb.WriteString(repl)
			case 3:
				sb.WriteString(strings.ToLower(repl))
			case 4:
				sb.WriteString(strings.ToUpper(repl))
			}
			continue
		}

		isUpper := b >= 'A' && b <= 'Z'
		isLower := b >= 'a' && b <= 'z'
		switch mode {
		case 1:
			sb.WriteByte(b)
		case 2:
			if isUpper || isLower {
				sb.WriteByte(b)
			}
		case 3:
			if isUpper {
				sb.WriteByte(b + ('a' - 'A'))
			} else if isLower {
				sb.WriteByte(b)
			}
		case 4:
			if isLower {
				sb.WriteByte(b - ('a' - 'A'))
			} else if isUpper {
				sb.WriteByte(b)
			}
		}
	}
	return sb.String()
}

func formatTextMatchLine(m search.Match, dicts []*tsd.Dictionary, opts *cliOptions) string {
	base := transformWordAndAnnot(m.Word, m.Annotation, opts.ShowAnnotations, opts.OutputTransform)
	var sb strings.Builder
	sb.Grow(len(base) + 24)
	sb.WriteString(base)

	if opts.ShowBalances && m.AnagramBalance != "" {
		sb.WriteByte(' ')
		sb.WriteString(m.AnagramBalance)
	}

	if opts.ShowDict {
		sb.WriteString(" [")
		if len(m.AllDictIndices) > 1 {
			for i, idx := range m.AllDictIndices {
				if i > 0 {
					sb.WriteString(", ")
				}
				if idx >= 0 && idx < len(dicts) {
					sb.WriteString(dicts[idx].Name())
				} else {
					sb.WriteString(strconv.Itoa(idx))
				}
			}
		} else {
			sb.WriteString(m.DictName)
		}
		sb.WriteByte(']')
	}
	return sb.String()
}

type jsonMatchRecord struct {
	Pattern       string   `json:"pattern,omitempty"`
	Word          string   `json:"word"`
	Formatted     string   `json:"formatted,omitempty"`
	Length        int      `json:"length"`
	Balance       string   `json:"balance,omitempty"`
	Annotation    string   `json:"annotation,omitempty"`
	Dict          string   `json:"dict,omitempty"`
	Sources       []string `json:"sources,omitempty"`
	HasDefinition bool     `json:"hasDefinition"`
}

func toJSONRecord(pat string, includePat bool, m search.Match, dicts []*tsd.Dictionary, opts *cliOptions) jsonMatchRecord {
	var sources []string
	if len(m.AllDictIndices) > 1 {
		sources = make([]string, 0, len(m.AllDictIndices))
		for _, idx := range m.AllDictIndices {
			if idx >= 0 && idx < len(dicts) {
				sources = append(sources, dicts[idx].Name())
			}
		}
	}
	var formatted string
	if opts.OutputTransform > 0 {
		formatted = transformWordAndAnnot(m.Word, "", false, opts.OutputTransform)
	}
	rec := jsonMatchRecord{
		Word:          m.Word,
		Formatted:     formatted,
		Length:        m.Length,
		Balance:       m.AnagramBalance,
		Annotation:    strings.TrimSpace(m.Annotation),
		Dict:          m.DictName,
		Sources:       sources,
		HasDefinition: m.HasDefinition,
	}
	if includePat {
		rec.Pattern = pat
	}
	return rec
}

func writeJSONLMatch(w io.Writer, pat string, includePat bool, m search.Match, dicts []*tsd.Dictionary, opts *cliOptions) error {
	rec := toJSONRecord(pat, includePat, m, dicts, opts)
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

func writeCSVHeader(cw *csv.Writer, multiPattern bool) error {
	if multiPattern {
		return cw.Write([]string{"pattern", "word", "length", "balance", "annotation", "dict", "has_definition"})
	}
	return cw.Write([]string{"word", "length", "balance", "annotation", "dict", "has_definition"})
}

func writeCSVMatch(cw *csv.Writer, pat string, multiPattern bool, m search.Match, opts *cliOptions) error {
	word := m.Word
	if opts.OutputTransform > 0 {
		word = transformWordAndAnnot(m.Word, "", false, opts.OutputTransform)
	}
	annot := ""
	if opts.ShowAnnotations {
		annot = strings.TrimSpace(m.Annotation)
	}
	bal := ""
	if opts.ShowBalances {
		bal = m.AnagramBalance
	}
	hasDef := "false"
	if m.HasDefinition {
		hasDef = "true"
	}
	if multiPattern {
		return cw.Write([]string{pat, word, strconv.Itoa(m.Length), bal, annot, m.DictName, hasDef})
	}
	return cw.Write([]string{word, strconv.Itoa(m.Length), bal, annot, m.DictName, hasDef})
}

type jsonCountRecord struct {
	Pattern string `json:"pattern,omitempty"`
	Count   int    `json:"count"`
}

func formatPatternError(w io.Writer, query string, err error, quiet bool) {
	var se *pattern.SyntaxError
	if errors.As(err, &se) {
		fmt.Fprintf(w, "Pattern error: %s\n", se.Error())
		if !quiet && query != "" && se.Span.Start >= 0 && se.Span.Start <= len(query) {
			fmt.Fprintf(w, "  %s\n", query)
			end := se.Span.End
			if end < se.Span.Start {
				end = se.Span.Start
			}
			if end > len(query) {
				end = len(query)
			}
			colOffset := utf8.RuneCountInString(query[:se.Span.Start])
			caretLen := utf8.RuneCountInString(query[se.Span.Start:end])
			if caretLen < 1 {
				caretLen = 1
			}
			fmt.Fprintf(w, "  %s%s\n", strings.Repeat(" ", colOffset), strings.Repeat("^", caretLen))
		}
		return
	}
	fmt.Fprintf(w, "Pattern error: %v\n", err)
}

func formatHeadwordsCSV(headwords []lookup.HeadwordSense) string {
	if len(headwords) == 0 {
		return ""
	}
	parts := make([]string, len(headwords))
	for i, hw := range headwords {
		if q := strings.TrimSpace(hw.Qualifier); q != "" {
			parts[i] = hw.Headword + " (" + q + ")"
		} else {
			parts[i] = hw.Headword
		}
	}
	return strings.Join(parts, " / ")
}

func writeDefinitionCSVHeader(cw *csv.Writer) error {
	return cw.Write([]string{"word", "dict", "type", "headwords", "text"})
}

func writeDefinitionCSVRows(cw *csv.Writer, res *lookup.Result) error {
	if len(res.Entries) == 0 {
		return cw.Write([]string{res.Word, "", "none", "", ""})
	}
	for _, entry := range res.Entries {
		kind := "definition"
		if entry.IsAliasRef {
			kind = "cross-reference"
		} else if entry.IsMention {
			kind = "mention"
		}
		if err := cw.Write([]string{res.Word, entry.DictName, kind, formatHeadwordsCSV(entry.Headwords), entry.Text}); err != nil {
			return err
		}
	}
	return nil
}

func writeDefinitionResult(w io.Writer, res *lookup.Result, format OutputFormat) error {
	switch format {
	case FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	case FormatJSONL:
		enc := json.NewEncoder(w)
		return enc.Encode(res)
	default:
		if len(res.Entries) == 0 {
			fmt.Fprintf(w, "No local definitions found for %q.\n", res.Word)
			if len(res.WebLinks) > 0 {
				fmt.Fprintln(w, "Web lookup links:")
				for _, wl := range res.WebLinks {
					fmt.Fprintf(w, "  %-16s %s\n", wl.Name+":", wl.URL)
				}
			}
			return nil
		}

		for idx, entry := range res.Entries {
			if idx > 0 {
				fmt.Fprintln(w, strings.Repeat("─", 60))
			}
			var header strings.Builder
			fmt.Fprintf(&header, "[%s]", entry.DictName)
			if len(entry.Headwords) > 0 {
				header.WriteByte(' ')
				for i, hw := range entry.Headwords {
					if i > 0 {
						header.WriteString(" / ")
					}
					header.WriteString(hw.Headword)
					if q := strings.TrimSpace(hw.Qualifier); q != "" {
						fmt.Fprintf(&header, " (%s)", q)
					}
				}
			}
			if entry.IsAliasRef {
				header.WriteString(" [cross-reference]")
			} else if entry.IsMention {
				header.WriteString(" [mentioned in definition]")
			}
			fmt.Fprintln(w, header.String())
			fmt.Fprintln(w, entry.Text)
		}
		return nil
	}
}
