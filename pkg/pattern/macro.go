package pattern

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// DefaultAtMacro is TEA's default expansion for '@' (vowels).
	DefaultAtMacro = "[aeiou]"
	// DefaultHashMacro is TEA's default expansion for '#' (consonants).
	DefaultHashMacro = "[^aeiou]"
)

// ExpandMacros performs single-pass non-recursive substitution of unescaped '@' and '#'
// in raw, returning the expanded string and a per-byte Span table mapping each byte
// of expanded back to its originating byte range in raw.
func ExpandMacros(raw, atMacro, hashMacro string) (expanded string, spans []Span) {
	var sb strings.Builder
	sb.Grow(len(raw) + 16)
	spans = make([]Span, 0, len(raw)+16)

	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c == '\\' && i+1 < len(raw) {
			sb.WriteByte('\\')
			spans = append(spans, Span{Start: i, End: i + 1})
			sb.WriteByte(raw[i+1])
			spans = append(spans, Span{Start: i + 1, End: i + 2})
			i++
			continue
		}
		if c == '@' && atMacro != "" {
			sp := Span{Start: i, End: i + 1}
			sb.WriteString(atMacro)
			for k := 0; k < len(atMacro); k++ {
				spans = append(spans, sp)
			}
			continue
		}
		if c == '#' && hashMacro != "" {
			sp := Span{Start: i, End: i + 1}
			sb.WriteString(hashMacro)
			for k := 0; k < len(hashMacro); k++ {
				spans = append(spans, sp)
			}
			continue
		}
		sb.WriteByte(c)
		spans = append(spans, Span{Start: i, End: i + 1})
	}
	return sb.String(), spans
}

// EnumerationSep describes the separator following a word part in a crossword enumeration.
type EnumerationSep uint8

const (
	EnumSepNone   EnumerationSep = iota
	EnumSepSpace                 // ',' or ' ' -> multi-word space break (' ')
	EnumSepHyphen                // '-' -> hyphenated word break ('-')
)

// EnumerationPart describes one length segment in a crossword enumeration (e.g. "4,2-2").
type EnumerationPart struct {
	Length    int
	Separator EnumerationSep // Separator between this part and the next (EnumSepNone on final part)
}

// ParseEnumeration parses a crossword clue enumeration string such as "8", "4,3", "(4, 2, 2)", or "3-5"
// into structured EnumerationPart segments.
func ParseEnumeration(spec string) ([]EnumerationPart, error) {
	s := strings.TrimSpace(spec)
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	if s == "" {
		return nil, nil
	}

	var parts []EnumerationPart
	i := 0
	n := len(s)
	totalLetters := 0

	for i < n {
		for i < n && s[i] == ' ' {
			i++
		}
		if i >= n {
			break
		}
		numStart := i
		for i < n && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == numStart {
			return nil, fmt.Errorf("invalid enumeration %q near %q", spec, s[i:])
		}
		val, err := strconv.Atoi(s[numStart:i])
		if err != nil || val <= 0 || val > 100 {
			return nil, fmt.Errorf("enumeration segment %q out of range 1..100", s[numStart:i])
		}
		totalLetters += val
		if totalLetters > 100 {
			return nil, fmt.Errorf("total enumeration length %d exceeds maximum 100", totalLetters)
		}

		sawSpace := false
		for i < n && s[i] == ' ' {
			sawSpace = true
			i++
		}
		sep := EnumSepNone
		if i < n {
			switch s[i] {
			case ',':
				sep = EnumSepSpace
				i++
			case '-':
				sep = EnumSepHyphen
				i++
			default:
				if sawSpace && s[i] >= '0' && s[i] <= '9' {
					sep = EnumSepSpace
				} else {
					return nil, fmt.Errorf("unexpected separator %q in enumeration %q", s[i:i+1], spec)
				}
			}
			for i < n && s[i] == ' ' {
				i++
			}
			if i >= n {
				return nil, fmt.Errorf("trailing separator in enumeration %q", spec)
			}
		}
		parts = append(parts, EnumerationPart{Length: val, Separator: sep})
	}
	return parts, nil
}

// TotalLength returns the sum of canonical letter counts across all enumeration parts.
func TotalEnumerationLength(parts []EnumerationPart) int {
	sum := 0
	for _, p := range parts {
		sum += p.Length
	}
	return sum
}

// EnumerationTemplate converts parsed EnumerationPart segments into an equivalent TEA template
// (e.g. "4,3" -> ".... ...", "3-5" -> "...-.....").
func EnumerationTemplate(parts []EnumerationPart) string {
	if len(parts) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, p := range parts {
		for k := 0; k < p.Length; k++ {
			sb.WriteByte('.')
		}
		switch p.Separator {
		case EnumSepSpace:
			sb.WriteByte(' ')
		case EnumSepHyphen:
			sb.WriteByte('-')
		}
	}
	return sb.String()
}
