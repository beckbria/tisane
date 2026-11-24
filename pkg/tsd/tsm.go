package tsd

import (
	"bytes"
	"fmt"
	"os"
)

// TSM represents a resolved 256-byte TEA/Tisane alphabet mapping table.
// For any raw byte b:
//   - tsm[b] == 0 means b is non-alphabetic (punctuation, digit, whitespace, control).
//   - tsm[b] != 0 means b is alphabetic and folds to canonical byte tsm[b] (where tsm[tsm[b]] == tsm[b]).
type TSM [256]byte

// defaultTSMTable is the resolved 256-byte table from Default.TSM / ANSI.TSM.
// Non-zero identity mappings on control bytes (0x01..0x1F except LF/CR) and smart quotes
// (0x91..0x94), alongside 0x00 on 0x8E/0x9E (Ž/ž), preserve exact binary compatibility
// with legacy Default.TSM dictionaries.
var defaultTSMTable = TSM{
	0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x00, 0x0B, 0x0C, 0x00, 0x0E, 0x0F,
	0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49, 0x4A, 0x4B, 0x4C, 0x4D, 0x4E, 0x4F,
	0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5A, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49, 0x4A, 0x4B, 0x4C, 0x4D, 0x4E, 0x4F,
	0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5A, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x53, 0x00, 0x8C, 0x00, 0x00, 0x00,
	0x00, 0x91, 0x92, 0x93, 0x94, 0x00, 0x00, 0x00, 0x00, 0x00, 0x53, 0x00, 0x8C, 0x00, 0x00, 0x59,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x41, 0x41, 0x41, 0x41, 0x41, 0x41, 0xC6, 0x43, 0x45, 0x45, 0x45, 0x45, 0x49, 0x49, 0x49, 0x49,
	0x44, 0x4E, 0x4F, 0x4F, 0x4F, 0x4F, 0x4F, 0x00, 0x4F, 0x55, 0x55, 0x55, 0x55, 0x59, 0xDE, 0xDF,
	0x41, 0x41, 0x41, 0x41, 0x41, 0x41, 0xC6, 0x43, 0x45, 0x45, 0x45, 0x45, 0x49, 0x49, 0x49, 0x49,
	0x44, 0x4E, 0x4F, 0x4F, 0x4F, 0x4F, 0x4F, 0x00, 0x4F, 0x55, 0x55, 0x55, 0x55, 0x59, 0xDE, 0x59,
}

// cp1252SortTable maps each Windows-1252 byte to its uppercase base letter for primary
// alphabetical sorting (0 for non-letters), preserving distinct ligatures (Œ, Æ, Þ, ß).
var cp1252SortTable = buildCP1252SortTable()

func buildCP1252SortTable() [256]byte {
	var tbl [256]byte
	for b := byte('A'); b <= 'Z'; b++ {
		tbl[b] = b
		tbl[b+'a'-'A'] = b
	}
	// Windows-1252 extended Latin letters (0x80..0x9F).
	tbl[0x8A], tbl[0x9A] = 'S', 'S'   // Š, š
	tbl[0x8C], tbl[0x9C] = 0x8C, 0x8C // Œ, œ
	tbl[0x8E], tbl[0x9E] = 'Z', 'Z'   // Ž, ž
	tbl[0x9F] = 'Y'                   // Ÿ

	// Latin-1 uppercase (0xC0..0xDF) and lowercase (0xE0..0xFF) pairs.
	setPair := func(upper, target byte) {
		tbl[upper] = target
		tbl[upper+0x20] = target
	}
	for b := byte(0xC0); b <= 0xC5; b++ {
		setPair(b, 'A') // À..Å / à..å
	}
	setPair(0xC6, 0xC6) // Æ / æ
	setPair(0xC7, 'C')  // Ç / ç
	for b := byte(0xC8); b <= 0xCB; b++ {
		setPair(b, 'E') // È..Ë / è..ë
	}
	for b := byte(0xCC); b <= 0xCF; b++ {
		setPair(b, 'I') // Ì..Ï / ì..ï
	}
	setPair(0xD0, 'D') // Ð / ð
	setPair(0xD1, 'N') // Ñ / ñ
	for b := byte(0xD2); b <= 0xD6; b++ {
		setPair(b, 'O') // Ò..Ö / ò..ö
	}
	setPair(0xD8, 'O') // Ø / ø
	for b := byte(0xD9); b <= 0xDC; b++ {
		setPair(b, 'U') // Ù..Ü / ù..ü
	}
	setPair(0xDD, 'Y')  // Ý / ý
	setPair(0xDE, 0xDE) // Þ / þ
	tbl[0xDF] = 0xDF    // ß
	tbl[0xFF] = 'Y'     // ÿ
	return tbl
}

// DefaultTSM returns a copy of the built-in Default.TSM (Western European case/accent-insensitive) map.
func DefaultTSM() TSM {
	return defaultTSMTable
}

// ParseTSM parses a .TSM alphabet mapping file from raw bytes, computing transitive closure,
// breaking any cycles, and applying mandatory NUL/LF/CR non-alphabetic overrides.
func ParseTSM(data []byte) (TSM, error) {
	var temp [256]byte
	for i := 0; i < 256; i++ {
		temp[i] = byte(i)
	}

	// Normalize CRLF -> LF identically to C runtime text-mode fgets ("rt").
	norm := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	lineNum := 0
	for len(norm) > 0 {
		lineNum++
		var line []byte
		if idx := bytes.IndexByte(norm, '\n'); idx >= 0 {
			line = norm[:idx]
			norm = norm[idx+1:]
		} else {
			line = norm
			norm = nil
		}
		if len(line) == 0 {
			continue
		}
		if (len(line) != 2 && len(line) != 3) || line[1] != '=' {
			return TSM{}, fmt.Errorf("tsd: invalid .tsm line %d (%q)", lineNum, line)
		}
		x := line[0]
		if len(line) == 2 {
			temp[x] = 0
		} else {
			temp[x] = line[2]
		}
	}

	temp[0x00] = 0
	temp[0x0A] = 0
	temp[0x0D] = 0

	var out TSM
	for c := 0; c < 256; c++ {
		var visited [256]bool
		a := byte(c)
		for a != 0 && !visited[a] {
			visited[a] = true
			a = temp[a]
		}
		out[c] = a
	}
	// If a chain led into a cycle (e.g. x -> a <-> b), ensure every non-zero entry maps directly to its canonical self-mapped root.
	for c := 1; c < 256; c++ {
		if m := out[c]; m != 0 && out[m] != m {
			out[c] = out[m]
		}
	}

	classes := out.NumClasses()
	if classes < 1 || classes >= 254 {
		return TSM{}, fmt.Errorf("tsd: .tsm defines %d letter classes (must be in 1..253)", classes)
	}
	return out, nil
}

// LoadTSM reads and parses a .TSM file from disk.
func LoadTSM(path string) (TSM, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TSM{}, err
	}
	return ParseTSM(data)
}

// Mapped returns the canonical letter byte for b, or 0 if b is non-alphabetic.
func (t TSM) Mapped(b byte) byte { return t[b] }

// IsAlpha reports whether b is considered an alphabetic letter in t.
func (t TSM) IsAlpha(b byte) bool { return t[b] != 0 }

// IsCanonical reports whether b is the canonical representative of an alphabetic equivalence class.
func (t TSM) IsCanonical(b byte) bool { return b != 0 && t[b] == b }

// NumClasses returns the total number of distinct alphabetic equivalence classes in t.
func (t TSM) NumClasses() int {
	n := 0
	for b := 1; b < 256; b++ {
		if t[b] == byte(b) {
			n++
		}
	}
	return n
}

// CompareWordsAlpha compares two raw CP1252 display_word byte slices using a
// two-level alphabetical comparator:
//  1. Strip bytes where cp1252SortTable[b] == 0 and compare mapped bytes unsigned-lexicographically.
//  2. Tie-break with unsigned bytewise comparison of the full display_word.
func CompareWordsAlpha(a, b []byte) int {
	ia, ib := 0, 0
	na, nb := len(a), len(b)
	for {
		var ca, cb byte
		for ia < na {
			ca = cp1252SortTable[a[ia]]
			ia++
			if ca != 0 {
				break
			}
		}
		for ib < nb {
			cb = cp1252SortTable[b[ib]]
			ib++
			if cb != 0 {
				break
			}
		}
		if ca == 0 && cb == 0 {
			return bytes.Compare(a, b)
		}
		if ca != cb {
			if ca < cb {
				return -1
			}
			return 1
		}
	}
}
