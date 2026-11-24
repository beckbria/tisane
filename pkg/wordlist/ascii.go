package wordlist

import (
	"strings"
	"unicode"
)

// ToASCII transliterates s into printable ASCII characters.
// Common Latin accented letters, digraphs/ligatures, fullwidth ASCII, and
// typographic punctuation are mapped to ASCII equivalents; combining marks
// and unmappable non-ASCII runes are stripped.
func ToASCII(s string) string {
	allASCII := true
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b < 0x20 || b > 0x7E {
			if b != '\t' {
				allASCII = false
				break
			}
		}
	}
	if allASCII {
		return s
	}

	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 0x20 && r <= 0x7E:
			sb.WriteByte(byte(r))
		case r == '\t':
			sb.WriteByte('\t')
		case r < 0x80:
			// Drop ASCII control characters.
		case r >= 0xFF01 && r <= 0xFF5E:
			// Fullwidth ASCII variants.
			sb.WriteByte(byte(r - 0xFF00 + 0x20))
		case unicode.Is(unicode.Mn, r):
			// Strip combining diacritical marks (handles NFD decomposed text).
		default:
			if mapped, ok := unicodeToASCII[r]; ok {
				sb.WriteString(mapped)
			} else if unicode.IsSpace(r) {
				sb.WriteByte(' ')
			}
		}
	}
	return sb.String()
}

// unicodeToASCII maps common non-ASCII Unicode runes to ASCII strings.
var unicodeToASCII = map[rune]string{
	// Latin-1 Supplement punctuation & symbols
	0x00A0: " ",   // NO-BREAK SPACE
	0x00A1: "!",   // INVERTED EXCLAMATION MARK
	0x00A2: "c",   // CENT SIGN
	0x00A3: "L",   // POUND SIGN
	0x00A5: "Y",   // YEN SIGN
	0x00A6: "|",   // BROKEN BAR
	0x00A9: "(c)", // COPYRIGHT SIGN
	0x00AA: "a",   // FEMININE ORDINAL INDICATOR
	0x00AB: "\"",  // LEFT-POINTING DOUBLE ANGLE QUOTATION MARK
	0x00AD: "",    // SOFT HYPHEN
	0x00AE: "(R)", // REGISTERED SIGN
	0x00B2: "2",   // SUPERSCRIPT TWO
	0x00B3: "3",   // SUPERSCRIPT THREE
	0x00B4: "'",   // ACUTE ACCENT
	0x00B7: ".",   // MIDDLE DOT
	0x00B9: "1",   // SUPERSCRIPT ONE
	0x00BA: "o",   // MASCULINE ORDINAL INDICATOR
	0x00BB: "\"",  // RIGHT-POINTING DOUBLE ANGLE QUOTATION MARK
	0x00BC: "1/4",
	0x00BD: "1/2",
	0x00BE: "3/4",
	0x00BF: "?", // INVERTED QUESTION MARK
	0x00D7: "x", // MULTIPLICATION SIGN
	0x00F7: "/", // DIVISION SIGN

	// Latin-1 Supplement letters
	'À': "A", 'Á': "A", 'Â': "A", 'Ã': "A", 'Ä': "A", 'Å': "A",
	'Æ': "AE",
	'Ç': "C",
	'È': "E", 'É': "E", 'Ê': "E", 'Ë': "E",
	'Ì': "I", 'Í': "I", 'Î': "I", 'Ï': "I",
	'Ð': "D",
	'Ñ': "N",
	'Ò': "O", 'Ó': "O", 'Ô': "O", 'Õ': "O", 'Ö': "O", 'Ø': "O",
	'Ù': "U", 'Ú': "U", 'Û': "U", 'Ü': "U",
	'Ý': "Y",
	'Þ': "TH",
	'ß': "ss",
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a",
	'æ': "ae",
	'ç': "c",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i",
	'ð': "d",
	'ñ': "n",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ö': "o", 'ø': "o",
	'ù': "u", 'ú': "u", 'û': "u", 'ü': "u",
	'ý': "y", 'ÿ': "y",
	'þ': "th",

	// Latin Extended-A
	'Ā': "A", 'ā': "a", 'Ă': "A", 'ă': "a", 'Ą': "A", 'ą': "a",
	'Ć': "C", 'ć': "c", 'Ĉ': "C", 'ĉ': "c", 'Ċ': "C", 'ċ': "c", 'Č': "C", 'č': "c",
	'Ď': "D", 'ď': "d", 'Đ': "D", 'đ': "d",
	'Ē': "E", 'ē': "e", 'Ĕ': "E", 'ĕ': "e", 'Ė': "E", 'ė': "e", 'Ę': "E", 'ę': "e", 'Ě': "E", 'ě': "e",
	'Ĝ': "G", 'ĝ': "g", 'Ğ': "G", 'ğ': "g", 'Ġ': "G", 'ġ': "g", 'Ģ': "G", 'ģ': "g",
	'Ĥ': "H", 'ĥ': "h", 'Ħ': "H", 'ħ': "h",
	'Ĩ': "I", 'ĩ': "i", 'Ī': "I", 'ī': "i", 'Ĭ': "I", 'ĭ': "i", 'Į': "I", 'į': "i", 'İ': "I", 'ı': "i",
	'Ĳ': "IJ", 'ĳ': "ij",
	'Ĵ': "J", 'ĵ': "j",
	'Ķ': "K", 'ķ': "k", 'ĸ': "k",
	'Ĺ': "L", 'ĺ': "l", 'Ļ': "L", 'ļ': "l", 'Ľ': "L", 'ľ': "l", 'Ŀ': "L", 'ŀ': "l", 'Ł': "L", 'ł': "l",
	'Ń': "N", 'ń': "n", 'Ņ': "N", 'ņ': "n", 'Ň': "N", 'ň': "n", 'ŉ': "n", 'Ŋ': "N", 'ŋ': "n",
	'Ō': "O", 'ō': "o", 'Ŏ': "O", 'ŏ': "o", 'Ő': "O", 'ő': "o",
	'Œ': "OE", 'œ': "oe",
	'Ŕ': "R", 'ŕ': "r", 'Ŗ': "R", 'ŗ': "r", 'Ř': "R", 'ř': "r",
	'Ś': "S", 'ś': "s", 'Ŝ': "S", 'ŝ': "s", 'Ş': "S", 'ş': "s", 'Š': "S", 'š': "s",
	'Ţ': "T", 'ţ': "t", 'Ť': "T", 'ť': "t", 'Ŧ': "T", 'ŧ': "t",
	'Ũ': "U", 'ũ': "u", 'Ū': "U", 'ū': "u", 'Ŭ': "U", 'ŭ': "u", 'Ů': "U", 'ů': "u", 'Ű': "U", 'ű': "u", 'Ų': "U", 'ų': "u",
	'Ŵ': "W", 'ŵ': "w",
	'Ŷ': "Y", 'ŷ': "y", 'Ÿ': "Y",
	'Ź': "Z", 'ź': "z", 'Ż': "Z", 'ż': "z", 'Ž': "Z", 'ž': "z",
	'ſ': "s",

	// Latin Extended-B & Additional common letters
	'ƒ': "f",
	'Ǎ': "A", 'ǎ': "a", 'Ǐ': "I", 'ǐ': "i", 'Ǒ': "O", 'ǒ': "o", 'Ǔ': "U", 'ǔ': "u",
	'Ǖ': "U", 'ǖ': "u", 'Ǘ': "U", 'ǘ': "u", 'Ǚ': "U", 'ǚ': "u", 'Ǜ': "U", 'ǜ': "u",
	'Ǟ': "A", 'ǟ': "a", 'Ǡ': "A", 'ǡ': "a", 'Ǣ': "AE", 'ǣ': "ae",
	'Ǥ': "G", 'ǥ': "g", 'Ǧ': "G", 'ǧ': "g", 'Ǩ': "K", 'ǩ': "k",
	'Ǫ': "O", 'ǫ': "o", 'Ǭ': "O", 'ǭ': "o", 'Ǯ': "Z", 'ǯ': "z", 'ǰ': "j",
	'Ǳ': "DZ", 'ǲ': "Dz", 'ǳ': "dz", 'Ǵ': "G", 'ǵ': "g", 'Ǹ': "N", 'ǹ': "n",
	'Ǻ': "A", 'ǻ': "a", 'Ǽ': "AE", 'ǽ': "ae", 'Ǿ': "O", 'ǿ': "o",
	'Ș': "S", 'ș': "s", 'Ț': "T", 'ț': "t",
	'ẞ': "SS",
	'Ẁ': "W", 'ẁ': "w", 'Ẃ': "W", 'ẃ': "w", 'Ẅ': "W", 'ẅ': "w",
	'Ỳ': "Y", 'ỳ': "y",

	// Typographic ligatures
	'ﬀ': "ff", 'ﬁ': "fi", 'ﬂ': "fl", 'ﬃ': "ffi", 'ﬄ': "ffl", 'ﬅ': "st", 'ﬆ': "st",

	// General punctuation & dashes / quotes
	0x2010: "-",   // HYPHEN
	0x2011: "-",   // NON-BREAKING HYPHEN
	0x2012: "-",   // FIGURE DASH
	0x2013: "-",   // EN DASH
	0x2014: "-",   // EM DASH
	0x2015: "-",   // HORIZONTAL BAR
	0x2212: "-",   // MINUS SIGN
	0x2018: "'",   // LEFT SINGLE QUOTATION MARK
	0x2019: "'",   // RIGHT SINGLE QUOTATION MARK
	0x201A: "'",   // SINGLE LOW-9 QUOTATION MARK
	0x201B: "'",   // SINGLE HIGH-REVERSED-9 QUOTATION MARK
	0x2032: "'",   // PRIME
	0x2039: "'",   // SINGLE LEFT-POINTING ANGLE QUOTATION MARK
	0x203A: "'",   // SINGLE RIGHT-POINTING ANGLE QUOTATION MARK
	0x201C: "\"",  // LEFT DOUBLE QUOTATION MARK
	0x201D: "\"",  // RIGHT DOUBLE QUOTATION MARK
	0x201E: "\"",  // DOUBLE LOW-9 QUOTATION MARK
	0x201F: "\"",  // DOUBLE HIGH-REVERSED-9 QUOTATION MARK
	0x2033: "\"",  // DOUBLE PRIME
	0x2022: "*",   // BULLET
	0x2026: "...", // HORIZONTAL ELLIPSIS
	0x20AC: "EUR", // EURO SIGN
	0x2122: "TM",  // TRADE MARK SIGN
}
