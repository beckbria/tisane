package lookup

import (
	"bytes"
	"context"
	"net/url"
	"strings"

	"tisane/pkg/tsd"
)

// WebProvider defines an external dictionary/encyclopedia web lookup template.
// The placeholder "{entry}" is substituted with the URL-encoded query word.
type WebProvider struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URLTemplate string `json:"urlTemplate"`
}

// WebLink is a resolved external web lookup URL ready to open in a browser.
type WebLink struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// DefaultWebProviders returns Tisane's built-in web lookup engines.
func DefaultWebProviders() []WebProvider {
	return []WebProvider{
		{ID: "onelook", Name: "OneLook", URLTemplate: "https://www.onelook.com/?w={entry}"},
		{ID: "wiktionary", Name: "Wiktionary", URLTemplate: "https://en.wiktionary.org/wiki/{entry}"},
		{ID: "wikipedia", Name: "Wikipedia", URLTemplate: "https://en.wikipedia.org/wiki/Special:Search?search={entry}"},
		{ID: "google", Name: "Google Define", URLTemplate: "https://www.google.com/search?q=define+{entry}"},
		{ID: "merriam-webster", Name: "Merriam-Webster", URLTemplate: "https://www.merriam-webster.com/dictionary/{entry}"},
	}
}

// BuildWebURL substitutes "{entry}" in urlTemplate with the properly escaped word.
// Query-parameter templates (after '?') use url.QueryEscape; URL path segments use url.PathEscape.
func BuildWebURL(urlTemplate, word string) string {
	trimmed := strings.TrimSpace(word)
	placeholderIdx := strings.Index(urlTemplate, "{entry}")
	if placeholderIdx < 0 {
		return urlTemplate
	}
	qIdx := strings.IndexByte(urlTemplate, '?')
	var escaped string
	if qIdx >= 0 && placeholderIdx > qIdx {
		escaped = url.QueryEscape(trimmed)
	} else {
		// Wiki-style path segments work cleanly with spaces mapped to '_' or %20; PathEscape handles all specials safely.
		if strings.Contains(urlTemplate, "/wiki/") {
			escaped = url.PathEscape(strings.ReplaceAll(trimmed, " ", "_"))
		} else {
			escaped = url.PathEscape(trimmed)
		}
		escaped = strings.ReplaceAll(escaped, "&", "%26")
		escaped = strings.ReplaceAll(escaped, "+", "%2B")
	}
	return strings.ReplaceAll(urlTemplate, "{entry}", escaped)
}

// BuildWebLinks resolves all configured WebProviders for word.
func BuildWebLinks(word string, providers []WebProvider) []WebLink {
	if len(providers) == 0 {
		providers = DefaultWebProviders()
	}
	links := make([]WebLink, len(providers))
	for i, p := range providers {
		links[i] = WebLink{
			ID:   p.ID,
			Name: p.Name,
			URL:  BuildWebURL(p.URLTemplate, word),
		}
	}
	return links
}

// HeadwordSense pairs a headword with its optional sense/POS qualifier.
type HeadwordSense struct {
	Headword  string `json:"headword"`
	Qualifier string `json:"qualifier,omitempty"`
}

// DefinitionEntry represents a single decoded dictionary definition formatted for both GUI and CLI.
type DefinitionEntry struct {
	DictIndex  int             `json:"dictIndex"`
	DictName   string          `json:"dict"`
	Offset     uint32          `json:"offset"`
	IsAliasRef bool            `json:"isAliasRef,omitempty"` // True for Pass 1 cross-reference stubs
	IsMention  bool            `json:"isMention,omitempty"`  // True when matched via FTS occurrence rather than direct headword/alias
	IsRTF      bool            `json:"isRTF"`
	Headwords  []HeadwordSense `json:"headwords,omitempty"`
	HTML       string          `json:"html"`
	Text       string          `json:"text"`
}

// Result bundles all local definitions found across dictionaries alongside fallback WebLinks.
type Result struct {
	Word     string            `json:"word"`
	Entries  []DefinitionEntry `json:"entries"`
	WebLinks []WebLink         `json:"webLinks"`
}

// Options customizes multi-dictionary definition lookup.
type Options struct {
	// CaseInsensitiveFallback matches canonical letter sequences if no exact case/punctuation match has definitions.
	CaseInsensitiveFallback bool
	// IncludeMentions includes FTS body occurrences after direct headword/alias definitions.
	IncludeMentions bool
	// WebProviders overrides DefaultWebProviders() when building Result.WebLinks.
	WebProviders []WebProvider
}

// FormatDefinitionRecord converts a decoded *tsd.DefinitionRecord into a DefinitionEntry.
func FormatDefinitionRecord(dictIdx int, dictName string, rec *tsd.DefinitionRecord, isMention bool) DefinitionEntry {
	senses := make([]HeadwordSense, len(rec.Headwords))
	for i, hq := range rec.Headwords {
		senses[i] = HeadwordSense{
			Headword:  hq.Headword,
			Qualifier: hq.Qualifier,
		}
	}
	return DefinitionEntry{
		DictIndex:  dictIdx,
		DictName:   dictName,
		Offset:     rec.Offset,
		IsAliasRef: rec.Pass == 1,
		IsMention:  isMention,
		IsRTF:      rec.IsRTF,
		Headwords:  senses,
		HTML:       DefinitionToHTML(rec.RawBody),
		Text:       DefinitionToText(rec.RawBody),
	}
}

// Lookup retrieves all local definitions for word across dicts in priority order,
// returning decoded HTML + plain-text entries and web search fallback links.
func Lookup(ctx context.Context, word string, dicts []*tsd.Dictionary, opts Options) (*Result, error) {
	trimmed := strings.TrimSpace(word)
	res := &Result{
		Word:     trimmed,
		Entries:  []DefinitionEntry{},
		WebLinks: BuildWebLinks(trimmed, opts.WebProviders),
	}
	if trimmed == "" {
		return res, nil
	}

	cpWord, ok := tsd.EncodeCP1252(trimmed)
	if !ok {
		return res, nil
	}

	for dictIdx, d := range dicts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if d == nil || !d.HasDefinitions() {
			continue
		}

		// Compute canonical letter sequence of trimmed in d's alphabet.
		var targetCanon []byte
		for _, b := range cpWord {
			if mc := d.MappedChar(b); mc != 0 {
				targetCanon = append(targetCanon, mc)
			}
		}
		L := len(targetCanon)
		if L <= 0 || L > d.MaxWordLength() || d.WordCount(L) == 0 {
			continue
		}

		var exactGroups []int32
		var canonGroups []int32
		for raw, err := range d.ScanRawWords(L) {
			if err != nil {
				return nil, err
			}
			if raw.LinkGroupIndex < 0 {
				continue
			}
			if bytes.Equal(raw.WordBytes, cpWord) {
				exactGroups = append(exactGroups, raw.LinkGroupIndex)
			} else if opts.CaseInsensitiveFallback && bytes.Equal(raw.CanonicalLetters, targetCanon) {
				canonGroups = append(canonGroups, raw.LinkGroupIndex)
			}
		}

		chosen := exactGroups
		if len(chosen) == 0 && opts.CaseInsensitiveFallback {
			chosen = canonGroups
		}
		if len(chosen) == 0 {
			continue
		}

		seenOffsets := make(map[uint32]bool)
		var mentionOffsets []uint32
		for _, lg := range chosen {
			direct, fts, _, err := d.ResolveAllDefinitionOffsets(L, int(lg))
			if err != nil {
				return nil, err
			}
			for _, off := range direct {
				if seenOffsets[off] {
					continue
				}
				seenOffsets[off] = true
				rec, err := d.ReadDefinition(off)
				if err != nil {
					return nil, err
				}
				res.Entries = append(res.Entries, FormatDefinitionRecord(dictIdx, d.Name(), rec, false))
			}
			if opts.IncludeMentions {
				mentionOffsets = append(mentionOffsets, fts...)
			}
		}

		if opts.IncludeMentions {
			for _, off := range mentionOffsets {
				if seenOffsets[off] {
					continue
				}
				seenOffsets[off] = true
				rec, err := d.ReadDefinition(off)
				if err != nil {
					return nil, err
				}
				res.Entries = append(res.Entries, FormatDefinitionRecord(dictIdx, d.Name(), rec, true))
			}
		}
	}

	return res, nil
}
