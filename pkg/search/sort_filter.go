package search

import (
	"sort"

	"tisane/pkg/tsd"
)

// isCP1252Uppercase reports whether Windows-1252 byte b is an uppercase letter.
func isCP1252Uppercase(b byte) bool {
	if b >= 'A' && b <= 'Z' {
		return true
	}
	if (b >= 0xC0 && b <= 0xD6) || (b >= 0xD8 && b <= 0xDE) {
		return true
	}
	switch b {
	case 0x8A, 0x8C, 0x8E, 0x9F: // Š, Œ, Ž, Ÿ
		return true
	}
	return false
}

// ClassifyEntry categorizes a raw CP1252 word byte slice (excluding annotation) into one of
// TEA's four mutually-exclusive entry types: EntryPhrase, EntryPunctuated, EntryCapitalized, or EntryLowercase.
func ClassifyEntry(wordBytes []byte, dict *tsd.Dictionary) EntryTypeMask {
	hasPunct := false
	hasUpper := false
	for _, b := range wordBytes {
		if b == ' ' {
			return EntryPhrase
		}
		if dict != nil {
			if !dict.IsLetter(b) {
				hasPunct = true
				continue
			}
		} else if !tsd.DefaultTSM().IsAlpha(b) {
			hasPunct = true
			continue
		}
		if isCP1252Uppercase(b) {
			hasUpper = true
		}
	}
	if hasPunct {
		return EntryPunctuated
	}
	if hasUpper {
		return EntryCapitalized
	}
	return EntryLowercase
}

// SortMatches sorts matches in-place according to mode using TEA's collation rules.
func SortMatches(matches []Match, mode SortMode) {
	if mode == SortDict || len(matches) <= 1 {
		return
	}

	ensureRaw := func(m *Match) []byte {
		if m.rawWord != nil {
			return m.rawWord
		}
		cp, _ := tsd.EncodeCP1252(m.Word)
		m.rawWord = cp
		return cp
	}
	for i := range matches {
		ensureRaw(&matches[i])
	}

	sort.SliceStable(matches, func(i, j int) bool {
		a, b := &matches[i], &matches[j]
		switch mode {
		case SortAlphaAsc:
			return tsd.CompareWordsAlpha(a.rawWord, b.rawWord) < 0
		case SortAlphaDesc:
			return tsd.CompareWordsAlpha(a.rawWord, b.rawWord) > 0
		case SortLenAsc:
			if a.Length != b.Length {
				return a.Length < b.Length
			}
			return tsd.CompareWordsAlpha(a.rawWord, b.rawWord) < 0
		case SortLenDesc:
			if a.Length != b.Length {
				return a.Length > b.Length
			}
			return tsd.CompareWordsAlpha(a.rawWord, b.rawWord) < 0
		case SortLenSmart:
			if a.balanceLen != b.balanceLen {
				return a.balanceLen < b.balanceLen
			}
			if a.Length != b.Length {
				return a.Length < b.Length
			}
			return tsd.CompareWordsAlpha(a.rawWord, b.rawWord) < 0
		default:
			return false
		}
	})
}
