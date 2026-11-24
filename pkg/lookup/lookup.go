package lookup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
// Returns "" if the resulting URL does not use http or https with a non-empty host.
func BuildWebURL(urlTemplate, word string) string {
	tmpl := strings.TrimSpace(urlTemplate)
	if tmpl == "" {
		return ""
	}
	trimmed := strings.TrimSpace(word)
	var built string
	if !strings.Contains(tmpl, "{entry}") {
		built = tmpl
	} else {
		qIdx := strings.IndexByte(tmpl, '?')
		escapePath := func() string {
			var esc string
			if strings.Contains(tmpl, "/wiki/") {
				esc = url.PathEscape(strings.ReplaceAll(trimmed, " ", "_"))
			} else {
				esc = url.PathEscape(trimmed)
			}
			esc = strings.ReplaceAll(esc, "&", "%26")
			return strings.ReplaceAll(esc, "+", "%2B")
		}
		if qIdx < 0 {
			built = strings.ReplaceAll(tmpl, "{entry}", escapePath())
		} else {
			pathPart := strings.ReplaceAll(tmpl[:qIdx], "{entry}", escapePath())
			queryPart := strings.ReplaceAll(tmpl[qIdx:], "{entry}", url.QueryEscape(trimmed))
			built = pathPart + queryPart
		}
	}

	parsed, err := url.Parse(built)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	return built
}

// BuildWebLinks resolves all configured WebProviders with valid http/https URLs for word.
func BuildWebLinks(word string, providers []WebProvider) []WebLink {
	if len(providers) == 0 {
		providers = DefaultWebProviders()
	}
	links := make([]WebLink, 0, len(providers))
	for _, p := range providers {
		u := BuildWebURL(p.URLTemplate, word)
		if u == "" {
			continue
		}
		links = append(links, WebLink{
			ID:   p.ID,
			Name: p.Name,
			URL:  u,
		})
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
	if rec == nil {
		return DefinitionEntry{}
	}
	senses := make([]HeadwordSense, len(rec.Headwords))
	for i, hq := range rec.Headwords {
		senses[i] = HeadwordSense{
			Headword:  hq.Headword,
			Qualifier: hq.Qualifier,
		}
	}
	htmlOut, textOut := renderDefinitionBoth(rec.RawBody)
	return DefinitionEntry{
		DictIndex:  dictIdx,
		DictName:   dictName,
		Offset:     rec.Offset,
		IsAliasRef: rec.Pass == 1,
		IsMention:  isMention,
		IsRTF:      rec.IsRTF,
		Headwords:  senses,
		HTML:       htmlOut,
		Text:       textOut,
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
		var ftsOnlyIndices []int32

		if opts.IncludeMentions {
			for entry, err := range d.ScanAllRawWords(L) {
				if err != nil {
					return nil, fmt.Errorf("scan words in %s: %w", d.Name(), err)
				}
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if entry.IsFTSOnly {
					if bytes.Equal(entry.CanonicalLetters, targetCanon) {
						ftsOnlyIndices = append(ftsOnlyIndices, entry.FTSIndex)
					}
					continue
				}
				if entry.LinkGroupIndex < 0 {
					continue
				}
				if bytes.Equal(entry.WordBytes, cpWord) {
					exactGroups = append(exactGroups, entry.LinkGroupIndex)
				} else if opts.CaseInsensitiveFallback && bytes.Equal(entry.CanonicalLetters, targetCanon) {
					canonGroups = append(canonGroups, entry.LinkGroupIndex)
				}
			}
		} else {
			for raw, err := range d.ScanRawWords(L) {
				if err != nil {
					return nil, fmt.Errorf("scan words in %s: %w", d.Name(), err)
				}
				if err := ctx.Err(); err != nil {
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
		}

		if len(exactGroups) == 0 && len(canonGroups) == 0 && len(ftsOnlyIndices) == 0 {
			continue
		}

		var lt *tsd.LinkTable
		if len(exactGroups)+len(canonGroups) > 1 {
			var ltErr error
			lt, ltErr = d.ReadLinkTable(L)
			if ltErr != nil {
				return nil, fmt.Errorf("read link table in %s: %w", d.Name(), ltErr)
			}
		}
		resolveGroup := func(lg int32) (tsd.LinkResolution, error) {
			if lt != nil {
				return lt.Resolve(int(lg))
			}
			return d.ResolveLinkGroup(L, int(lg))
		}

		var directOffsets []uint32
		var ftsListOffsets []uint32

		for _, lg := range exactGroups {
			lr, err := resolveGroup(lg)
			if err != nil {
				return nil, fmt.Errorf("resolve link group in %s: %w", d.Name(), err)
			}
			directOffsets = append(directOffsets, lr.DirectOffsets...)
			if opts.IncludeMentions && lr.FTSListOffset != 0 && !lr.FTSOverflow {
				ftsListOffsets = append(ftsListOffsets, lr.FTSListOffset)
			}
		}

		// Fall back to canonical matches when there were no exact direct definitions
		// (so a mention-only exact match does not suppress a case-insensitive headword definition).
		if len(directOffsets) == 0 && opts.CaseInsensitiveFallback {
			for _, lg := range canonGroups {
				lr, err := resolveGroup(lg)
				if err != nil {
					return nil, fmt.Errorf("resolve link group in %s: %w", d.Name(), err)
				}
				directOffsets = append(directOffsets, lr.DirectOffsets...)
				if opts.IncludeMentions && lr.FTSListOffset != 0 && !lr.FTSOverflow {
					ftsListOffsets = append(ftsListOffsets, lr.FTSListOffset)
				}
			}
		}

		if opts.IncludeMentions && len(ftsOnlyIndices) > 0 {
			ptrs, err := d.ReadFTSIndexPointers(L)
			if err != nil {
				return nil, fmt.Errorf("read FTS pointers in %s: %w", d.Name(), err)
			}
			for _, fIdx := range ftsOnlyIndices {
				if int(fIdx) >= 0 && int(fIdx) < len(ptrs) {
					ptr := ptrs[fIdx]
					if ptr != 0 && ptr != tsd.FTSOverflowSentinel {
						ftsListOffsets = append(ftsListOffsets, ptr)
					}
				}
			}
		}

		seenOffsets := make(map[uint32]bool, len(directOffsets))
		for _, off := range directOffsets {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if seenOffsets[off] {
				continue
			}
			seenOffsets[off] = true
			rec, err := d.ReadDefinition(off)
			if err != nil {
				return nil, fmt.Errorf("read definition in %s: %w", d.Name(), err)
			}
			res.Entries = append(res.Entries, FormatDefinitionRecord(dictIdx, d.Name(), rec, false))
		}

		if opts.IncludeMentions {
			for _, listOff := range ftsListOffsets {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				occ, err := d.ReadFTSOccurrences(listOff)
				if err != nil {
					if errors.Is(err, tsd.ErrFTSOverflow) {
						continue
					}
					return nil, fmt.Errorf("read FTS occurrences in %s: %w", d.Name(), err)
				}
				for _, off := range occ {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					if seenOffsets[off] {
						continue
					}
					seenOffsets[off] = true
					rec, err := d.ReadDefinition(off)
					if err != nil {
						return nil, fmt.Errorf("read mention definition in %s: %w", d.Name(), err)
					}
					res.Entries = append(res.Entries, FormatDefinitionRecord(dictIdx, d.Name(), rec, true))
				}
			}
		}
	}

	return res, nil
}
