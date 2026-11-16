// Package tsd reads, inspects, and builds TEA/Tisane (.tsd) dictionary files
// (both legacy TSD0 and modern TSD1) and alphabet mapping (.tsm) files.
package tsd

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

// InputEncoding controls how raw text input is interpreted when building a .tsd file.
type InputEncoding uint8

const (
	// EncodingAuto treats pure-ASCII or raw Windows-1252 files as CP1252,
	// while automatically transcoding valid UTF-8 input (including UTF-8 BOM)
	// whose runes map into Windows-1252.
	EncodingAuto InputEncoding = iota
	// EncodingCP1252 treats input bytes verbatim as Windows-1252 / raw single-byte text
	// (matching legacy TSBuild.exe byte-for-byte).
	EncodingCP1252
	// EncodingUTF8 decodes input text as UTF-8 and maps runes into Windows-1252.
	EncodingUTF8
)

// cp1252High maps bytes 0x80..0x9F in Windows-1252 to Unicode code points.
// The five unassigned C1 bytes (0x81, 0x8D, 0x8F, 0x90, 0x9D) map to U+0081..U+009D
// so arbitrary byte slices round-trip losslessly.
var cp1252High = [32]rune{
	0x20AC, // 0x80 €
	0x0081, // 0x81 (unassigned C1)
	0x201A, // 0x82 ‚
	0x0192, // 0x83 ƒ
	0x201E, // 0x84 „
	0x2026, // 0x85 …
	0x2020, // 0x86 †
	0x2021, // 0x87 ‡
	0x02C6, // 0x88 ˆ
	0x2030, // 0x89 ‰
	0x0160, // 0x8A Š
	0x2039, // 0x8B ‹
	0x0152, // 0x8C Œ
	0x008D, // 0x8D (unassigned C1)
	0x017D, // 0x8E Ž
	0x008F, // 0x8F (unassigned C1)
	0x0090, // 0x90 (unassigned C1)
	0x2018, // 0x91 ‘
	0x2019, // 0x92 ’
	0x201C, // 0x93 “
	0x201D, // 0x94 ”
	0x2022, // 0x95 •
	0x2013, // 0x96 –
	0x2014, // 0x97 —
	0x02DC, // 0x98 ˜
	0x2122, // 0x99 ™
	0x0161, // 0x9A š
	0x203A, // 0x9B ›
	0x0153, // 0x9C œ
	0x009D, // 0x9D (unassigned C1)
	0x017E, // 0x9E ž
	0x0178, // 0x9F Ÿ
}

// DecodeCP1252Byte converts a single Windows-1252 byte to its Unicode rune.
func DecodeCP1252Byte(b byte) rune {
	if b >= 0x80 && b <= 0x9F {
		return cp1252High[b-0x80]
	}
	return rune(b)
}

// EncodeCP1252Rune converts a Unicode rune to its Windows-1252 byte,
// returning false if the rune is not representable in Windows-1252.
func EncodeCP1252Rune(r rune) (byte, bool) {
	if r < 0x80 {
		return byte(r), true
	}
	if r >= 0xA0 && r <= 0xFF {
		return byte(r), true
	}
	switch r {
	case 0x20AC:
		return 0x80, true
	case 0x0081:
		return 0x81, true
	case 0x201A:
		return 0x82, true
	case 0x0192:
		return 0x83, true
	case 0x201E:
		return 0x84, true
	case 0x2026:
		return 0x85, true
	case 0x2020:
		return 0x86, true
	case 0x2021:
		return 0x87, true
	case 0x02C6:
		return 0x88, true
	case 0x2030:
		return 0x89, true
	case 0x0160:
		return 0x8A, true
	case 0x2039:
		return 0x8B, true
	case 0x0152:
		return 0x8C, true
	case 0x008D:
		return 0x8D, true
	case 0x017D:
		return 0x8E, true
	case 0x008F:
		return 0x8F, true
	case 0x0090:
		return 0x90, true
	case 0x2018:
		return 0x91, true
	case 0x2019:
		return 0x92, true
	case 0x201C:
		return 0x93, true
	case 0x201D:
		return 0x94, true
	case 0x2022:
		return 0x95, true
	case 0x2013:
		return 0x96, true
	case 0x2014:
		return 0x97, true
	case 0x02DC:
		return 0x98, true
	case 0x2122:
		return 0x99, true
	case 0x0161:
		return 0x9A, true
	case 0x203A:
		return 0x9B, true
	case 0x0153:
		return 0x9C, true
	case 0x009D:
		return 0x9D, true
	case 0x017E:
		return 0x9E, true
	case 0x0178:
		return 0x9F, true
	default:
		return 0, false
	}
}

// DecodeCP1252 converts a Windows-1252 byte slice into a valid UTF-8 Go string.
// Pure-ASCII slices take a fast single-allocation conversion path.
func DecodeCP1252(b []byte) string {
	for i := 0; i < len(b); i++ {
		if b[i] >= 0x80 {
			var sb strings.Builder
			sb.Grow(len(b) + 8)
			sb.Write(b[:i])
			for ; i < len(b); i++ {
				c := b[i]
				if c < 0x80 {
					sb.WriteByte(c)
				} else {
					sb.WriteRune(DecodeCP1252Byte(c))
				}
			}
			return sb.String()
		}
	}
	return string(b)
}

// EncodeCP1252 converts a UTF-8 Go string into Windows-1252 bytes.
// If any rune cannot be represented in Windows-1252, ok is false and unrepresentable
// runes are replaced with '?'.
func EncodeCP1252(s string) (out []byte, ok bool) {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return []byte(s), true
	}
	out = make([]byte, 0, len(s))
	ok = true
	for _, r := range s {
		if b, valid := EncodeCP1252Rune(r); valid {
			out = append(out, b)
		} else {
			out = append(out, '?')
			ok = false
		}
	}
	return out, ok
}

// normalizeBuilderInput converts raw source bytes to Windows-1252 according to enc.
func normalizeBuilderInput(data []byte, enc InputEncoding) []byte {
	switch enc {
	case EncodingCP1252:
		return data
	case EncodingUTF8:
		trimmed := bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
		out, _ := EncodeCP1252(string(trimmed))
		return out
	default: // EncodingAuto
		if bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
			trimmed := data[3:]
			if utf8.Valid(trimmed) {
				out, _ := EncodeCP1252(string(trimmed))
				return out
			}
		}
		hasHigh := false
		for _, b := range data {
			if b >= 0x80 {
				hasHigh = true
				break
			}
		}
		if !hasHigh {
			return data
		}
		if utf8.Valid(data) {
			if out, ok := EncodeCP1252(string(data)); ok {
				return out
			}
		}
		return data
	}
}
