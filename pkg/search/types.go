// Package search coordinates multi-dictionary crossword/anagram word searches
// and full-text definition searches with streaming batches, deduplication,
// 4-category entry filtering, cross-dictionary definition enrichment, and 6 sort modes.
package search

import (
	"fmt"
	"strings"

	"tisane/pkg/pattern"
	"tisane/pkg/tsd"
)

// SearchMode selects headword/alias search vs. definition full-text search.
type SearchMode uint8

const (
	ModeWord SearchMode = iota
	ModeFullText
)

// DedupeMode controls cross-dictionary duplicate handling.
type DedupeMode uint8

const (
	DedupeNone         DedupeMode = iota // Emit all matches from every dictionary independently
	DedupeSuppress                       // Keep only the first occurrence by dictionary priority order
	DedupeMergeSources                   // Keep first occurrence and record all matching dictionary indices in AllDictIndices
)

// EntryTypeMask is a bitmask of TEA's 4 mutually-exclusive display word categories.
type EntryTypeMask uint8

const (
	EntryLowercase   EntryTypeMask = 1 << 0 // Only lowercase letters (e.g. "apple")
	EntryCapitalized EntryTypeMask = 1 << 1 // Letters only, >=1 uppercase letter (e.g. "London", "NASA")
	EntryPunctuated  EntryTypeMask = 1 << 2 // Single-token with non-space punctuation (e.g. "don't", "by-law")
	EntryPhrase      EntryTypeMask = 1 << 3 // Multi-word entry containing space ' ' (e.g. "ice cream")

	EntryAll  EntryTypeMask = EntryLowercase | EntryCapitalized | EntryPunctuated | EntryPhrase
	EntryNone EntryTypeMask = 1 << 7 // Explicit empty filter sentinel (distinguished from zero-value default)
)

// ParseEntryFilter parses a comma-separated filter spec (such as "lower,upper,punct,phrase")
// into an EntryTypeMask. Empty string returns EntryAll; "none" returns EntryNone.
func ParseEntryFilter(spec string) (EntryTypeMask, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" || strings.EqualFold(spec, "all") {
		return EntryAll, nil
	}
	if strings.EqualFold(spec, "none") {
		return EntryNone, nil
	}
	var mask EntryTypeMask
	for _, tok := range strings.Split(spec, ",") {
		switch strings.ToLower(strings.TrimSpace(tok)) {
		case "all":
			mask |= EntryAll
		case "none":
			// Explicit none token without categories leaves mask empty -> EntryNone below.
		case "lower", "lowercase", "abc":
			mask |= EntryLowercase
		case "upper", "cap", "capitalized", "proper":
			mask |= EntryCapitalized
		case "punct", "punctuated", "hyphen":
			mask |= EntryPunctuated
		case "phrase", "phrases", "space", "multi":
			mask |= EntryPhrase
		case "":
			continue
		default:
			return 0, fmt.Errorf("unknown entry filter category %q (valid: lower, upper, punct, phrase, all, none)", tok)
		}
	}
	if mask == 0 {
		return EntryNone, nil
	}
	return mask, nil
}

// EntryFilterFromProfile converts profile string names into an EntryTypeMask.
func EntryFilterFromProfile(names []string) EntryTypeMask {
	if len(names) == 0 {
		return EntryAll
	}
	var mask EntryTypeMask
	for _, n := range names {
		trimmed := strings.TrimSpace(n)
		if trimmed == "" {
			continue
		}
		if m, err := ParseEntryFilter(trimmed); err == nil {
			if m == EntryNone {
				continue
			}
			mask |= m
		}
	}
	if mask == 0 {
		return EntryAll
	}
	return mask
}

// SortMode controls result ordering.
type SortMode uint8

const (
	SortDict      SortMode = iota // Dictionary emission order (streaming O(1))
	SortAlphaAsc                  // Case/accent-folded A..Z
	SortAlphaDesc                 // Case/accent-folded Z..A
	SortLenAsc                    // Canonical word length ascending, then A..Z
	SortLenSmart                  // Anagram balance letter count ascending (exact anagrams first), then length ascending, then A..Z
	SortLenDesc                   // Canonical word length descending, then A..Z
)

// ParseSortMode parses a CLI or config sort mode name into SortMode.
func ParseSortMode(s string) (SortMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "dict", "dictionary", "none":
		return SortDict, nil
	case "alpha", "az", "a-z", "asc":
		return SortAlphaAsc, nil
	case "alpha-desc", "za", "z-a", "desc":
		return SortAlphaDesc, nil
	case "len", "length", "len-asc":
		return SortLenAsc, nil
	case "smart", "balance":
		return SortLenSmart, nil
	case "len-desc", "length-desc":
		return SortLenDesc, nil
	default:
		return SortDict, fmt.Errorf("unknown sort mode %q (valid: dict, alpha, alpha-desc, len, len-desc, smart)", s)
	}
}

// Request specifies a multi-dictionary word or full-text search operation.
type Request struct {
	Query                   string
	Dictionaries            []*tsd.Dictionary         // Active search dictionaries in priority order
	DefinitionDictionaries  []*tsd.Dictionary         // Optional additional dictionaries checked for HasDefinition
	Mode                    SearchMode                // ModeWord or ModeFullText
	MinLength               int                       // Default 1
	MaxLength               int                       // Default 100
	Enumeration             []pattern.EnumerationPart // Optional word/hyphen break constraint (ModeWord)
	AtMacro                 string                    // Defaults to "[aeiou]" unless DisableDefaultMacros is true
	HashMacro               string                    // Defaults to "[^aeiou]" unless DisableDefaultMacros is true
	DisableDefaultMacros    bool                      // Set true ONLY when empty "" AtMacro/HashMacro is intentional
	ExclusiveVars           bool
	IgnoreUnusedPunctuation bool
	ShowBalances            bool
	ShowAnnotations         bool
	DedupeMode              DedupeMode
	EntryFilter             EntryTypeMask // 0 is treated as EntryAll; set EntryNone explicitly to exclude all categories
	Sort                    SortMode
	Limit                   int // 0 = unlimited
	ForceSequentialFTS      bool
}

// DefaultRequest returns a Request populated with standard Tisane defaults.
func DefaultRequest(query string, dicts ...*tsd.Dictionary) Request {
	return Request{
		Query:                   query,
		Dictionaries:            dicts,
		Mode:                    ModeWord,
		MinLength:               1,
		MaxLength:               tsd.MaxWordLengthLimit,
		AtMacro:                 pattern.DefaultAtMacro,
		HashMacro:               pattern.DefaultHashMacro,
		IgnoreUnusedPunctuation: true,
		ShowBalances:            true,
		ShowAnnotations:         true,
		DedupeMode:              DedupeSuppress,
		EntryFilter:             EntryAll,
		Sort:                    SortDict,
	}
}

// Match represents one matched dictionary entry.
type Match struct {
	Word           string `json:"word"`
	Annotation     string `json:"annotation,omitempty"`
	AnagramBalance string `json:"balance,omitempty"`
	Length         int    `json:"length"`
	DictIndex      int    `json:"dictIndex"`
	DictName       string `json:"dict,omitempty"`
	AllDictIndices []int  `json:"sources,omitempty"`
	LinkIndex      int32  `json:"-"` // >=0 link group index in winning dict, or -1
	DefOffset      uint32 `json:"-"` // Direct definition offset for FTS matches, or 0
	HasDefinition  bool   `json:"hasDefinition"`

	rawWord    []byte // retained internally for TEA collation sorting
	balanceLen int    // letter count in AnagramBalance (populated even when ShowBalances=false for SortLenSmart)
}

// Progress reports incremental scan telemetry for UI progress bars.
type Progress struct {
	DictIndex           int    `json:"dictIndex"`
	DictName            string `json:"dictName"`
	WordsScanned        uint64 `json:"wordsScanned"`
	TotalCandidateWords uint64 `json:"totalCandidateWords"`
}

// Batch is yielded incrementally by Engine.SearchStream.
type Batch struct {
	Matches  []Match  `json:"matches,omitempty"`
	Progress Progress `json:"progress"`
}

// Summary contains complete results and timing/scan counts from Engine.Search.
type Summary struct {
	Matches             []Match `json:"matches"`
	WordsScanned        uint64  `json:"wordsScanned"`
	TotalCandidateWords uint64  `json:"totalCandidateWords"`
	UsedIndexedFTS      bool    `json:"usedIndexedFTS,omitempty"`
}
