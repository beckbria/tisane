package tsd

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MaxWordLengthLimit is the architectural maximum word length supported by TEA/Tisane (.tsd).
const MaxWordLengthLimit = 100

// FormatVersion identifies the .tsd binary format revision.
type FormatVersion string

const (
	VersionTSD0 FormatVersion = "TSD0" // Legacy TEA 1.0 word-only format
	VersionTSD1 FormatVersion = "TSD1" // TEA 2.0 / Tisane format with definitions, links, and FTS
)

var (
	// ErrInvalidMagic indicates the file does not begin with "TSD0" or "TSD1".
	ErrInvalidMagic = errors.New("tsd: invalid magic header (expected TSD0 or TSD1)")
	// ErrInvalidHeader indicates inconsistent or out-of-bounds header fields.
	ErrInvalidHeader = errors.New("tsd: invalid or corrupt dictionary header")
)

// CharEntry is an 8-byte entry from the 256-slot character & Huffman table in the .tsd header.
type CharEntry struct {
	MappedChar  byte   // 0 if non-alphabetic, else canonical representative byte
	HuffCodeLen uint8  // Huffman codeword bit length (0 if unused, 1..32)
	LetterIndex int16  // -1 if non-alphabetic, else 0..NumTotalLetters-1
	HuffCode    uint32 // MSB-first codeword in low HuffCodeLen bits
}

// Dictionary is an immutable, concurrency-safe handle to an opened .tsd dictionary.
// Multiple goroutines may concurrently scan word lengths, resolve link groups,
// and read definitions without locking.
type Dictionary struct {
	r      io.ReaderAt
	closer io.Closer
	size   int64
	path   string
	name   string

	version           FormatVersion
	numLengths        int
	defPassOffsets    [3]uint32
	occIndexOffsets   []uint32
	defLinkOffsets    []uint32
	defLinkCounts     []uint32
	wordStreamOffsets []uint32
	wordCounts        []uint32
	totalWords        uint64

	rawDescription  []byte
	description     string
	numUsedLetters  int
	numTotalLetters int
	charTable       [256]CharEntry
	canonByIndex    [256]byte // letter_index -> canonical byte ('!' if unassigned)
	numCalRows      int
	calibration     []int8 // length numCalRows * numUsedLetters

	huff *huffmanDecoder
}

// Open opens a .tsd file from disk and parses its header tables into memory.
// Word streams, link tables, FTS indexes, and definitions are read on demand via ReadAt.
func Open(path string) (*Dictionary, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	d, err := OpenReaderAt(f, info.Size(), name)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	d.closer = f
	d.path = path
	return d, nil
}

// OpenBytes opens an in-memory .tsd byte slice as a Dictionary.
func OpenBytes(data []byte, name string) (*Dictionary, error) {
	return OpenReaderAt(bytes.NewReader(data), int64(len(data)), name)
}

// OpenReaderAt parses a .tsd dictionary backed by an arbitrary io.ReaderAt of the given byte size.
func OpenReaderAt(r io.ReaderAt, size int64, name string) (*Dictionary, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: nil ReaderAt", ErrInvalidHeader)
	}
	if size < 6 {
		return nil, fmt.Errorf("%w: file too small (%d bytes)", ErrInvalidHeader, size)
	}
	var head6 [6]byte
	if _, err := r.ReadAt(head6[:], 0); err != nil {
		return nil, err
	}

	magic := FormatVersion(head6[0:4])
	if magic != VersionTSD0 && magic != VersionTSD1 {
		return nil, ErrInvalidMagic
	}
	isTSD1 := magic == VersionTSD1

	numLengths := int(binary.LittleEndian.Uint16(head6[4:6]))
	if numLengths > MaxWordLengthLimit+1 {
		return nil, fmt.Errorf("%w: num_lengths %d exceeds maximum %d", ErrInvalidHeader, numLengths, MaxWordLengthLimit+1)
	}

	var prefixSize int
	if isTSD1 {
		prefixSize = 6 + 12 + 20*numLengths + 1000 + 4 + 2048 + 2
	} else {
		prefixSize = 6 + 8*numLengths + 80 + 4 + 2048 + 2
	}
	if int64(prefixSize) > size {
		return nil, fmt.Errorf("%w: header prefix (%d bytes) exceeds file size (%d bytes)", ErrInvalidHeader, prefixSize, size)
	}

	buf := make([]byte, prefixSize)
	if _, err := io.ReadFull(io.NewSectionReader(r, 0, int64(prefixSize)), buf); err != nil {
		return nil, err
	}

	d := &Dictionary{
		r:                 r,
		size:              size,
		name:              name,
		version:           magic,
		numLengths:        numLengths,
		occIndexOffsets:   make([]uint32, numLengths),
		defLinkOffsets:    make([]uint32, numLengths),
		defLinkCounts:     make([]uint32, numLengths),
		wordStreamOffsets: make([]uint32, numLengths),
		wordCounts:        make([]uint32, numLengths),
	}

	pos := 6
	readUint32Slice := func(dst []uint32) {
		for i := range dst {
			dst[i] = binary.LittleEndian.Uint32(buf[pos : pos+4])
			pos += 4
		}
	}

	if isTSD1 {
		for i := 0; i < 3; i++ {
			d.defPassOffsets[i] = binary.LittleEndian.Uint32(buf[pos : pos+4])
			pos += 4
		}
		readUint32Slice(d.occIndexOffsets)
		readUint32Slice(d.defLinkOffsets)
		readUint32Slice(d.defLinkCounts)
	}
	readUint32Slice(d.wordStreamOffsets)
	readUint32Slice(d.wordCounts)

	for _, wc := range d.wordCounts {
		d.totalWords += uint64(wc)
	}

	descLen := 80
	if isTSD1 {
		descLen = 1000
	}
	descField := buf[pos : pos+descLen]
	pos += descLen
	if nul := bytes.IndexByte(descField, 0); nul >= 0 {
		descField = descField[:nul]
	}
	d.rawDescription = append([]byte(nil), descField...)
	d.description = DecodeCP1252(d.rawDescription)

	numUsed := int(int16(binary.LittleEndian.Uint16(buf[pos : pos+2])))
	numTotal := int(int16(binary.LittleEndian.Uint16(buf[pos+2 : pos+4])))
	pos += 4
	if numUsed < 0 || numUsed >= 254 || numTotal < 1 || numTotal >= 254 || numUsed > numTotal {
		return nil, fmt.Errorf("%w: invalid letter counts (used=%d, total=%d)", ErrInvalidHeader, numUsed, numTotal)
	}
	d.numUsedLetters = numUsed
	d.numTotalLetters = numTotal

	for i := range d.canonByIndex {
		d.canonByIndex[i] = '!'
	}
	for b := 0; b < 256; b++ {
		ce := CharEntry{
			MappedChar:  buf[pos],
			HuffCodeLen: buf[pos+1],
			LetterIndex: int16(binary.LittleEndian.Uint16(buf[pos+2 : pos+4])),
			HuffCode:    binary.LittleEndian.Uint32(buf[pos+4 : pos+8]),
		}
		pos += 8
		if ce.LetterIndex >= int16(numTotal) || ce.LetterIndex < -1 {
			return nil, fmt.Errorf("%w: byte 0x%02X has out-of-range letter_index %d", ErrInvalidHeader, b, ce.LetterIndex)
		}
		if ce.LetterIndex >= 0 {
			d.canonByIndex[ce.LetterIndex] = ce.MappedChar
		}
		d.charTable[b] = ce
	}

	numCalRows := int(binary.LittleEndian.Uint16(buf[pos : pos+2]))
	pos += 2
	expectedCalRows := 0
	if numLengths > 0 {
		expectedCalRows = (numLengths - 1) * numLengths / 2
	}
	if numCalRows != expectedCalRows {
		return nil, fmt.Errorf("%w: num_cal_rows=%d, expected %d", ErrInvalidHeader, numCalRows, expectedCalRows)
	}
	d.numCalRows = numCalRows

	calByteLen := numCalRows * numUsed
	headerEnd := int64(prefixSize) + int64(calByteLen)
	if headerEnd > size {
		return nil, fmt.Errorf("%w: calibration matrix (%d bytes) exceeds file size", ErrInvalidHeader, calByteLen)
	}

	d.calibration = make([]int8, calByteLen)
	if calByteLen > 0 {
		calRaw := make([]byte, calByteLen)
		if _, err := io.ReadFull(io.NewSectionReader(r, int64(prefixSize), int64(calByteLen)), calRaw); err != nil {
			return nil, err
		}
		for i, b := range calRaw {
			d.calibration[i] = int8(b)
		}
	}

	minSectionOff := headerEnd
	if isTSD1 {
		if int64(d.defPassOffsets[0]) != headerEnd ||
			d.defPassOffsets[0] > d.defPassOffsets[1] ||
			d.defPassOffsets[1] > d.defPassOffsets[2] ||
			int64(d.defPassOffsets[2]) > size {
			return nil, fmt.Errorf("%w: invalid def_pass_offsets %v (headerEnd=%d, size=%d)",
				ErrInvalidHeader, d.defPassOffsets, headerEnd, size)
		}
		minSectionOff = int64(d.defPassOffsets[2])
	}

	if numLengths > 0 {
		if d.wordCounts[0] != 0 || d.wordStreamOffsets[0] != 0 {
			return nil, fmt.Errorf("%w: non-zero slot 0 in word tables", ErrInvalidHeader)
		}
		if isTSD1 && (d.occIndexOffsets[0] != 0 || d.defLinkOffsets[0] != 0 || d.defLinkCounts[0] != 0) {
			return nil, fmt.Errorf("%w: non-zero slot 0 in TSD1 index/link tables", ErrInvalidHeader)
		}
	}

	prevEnd := minSectionOff
	for L := 1; L < numLengths; L++ {
		if d.wordCounts[L] == 0 {
			if d.wordStreamOffsets[L] != 0 && int64(d.wordStreamOffsets[L]) > size {
				return nil, fmt.Errorf("%w: invalid word_stream_offsets[%d]=%d", ErrInvalidHeader, L, d.wordStreamOffsets[L])
			}
			if isTSD1 && (d.occIndexOffsets[L] != 0 || d.defLinkOffsets[L] != 0 || d.defLinkCounts[L] != 0) {
				return nil, fmt.Errorf("%w: non-zero TSD1 tables for empty length %d", ErrInvalidHeader, L)
			}
			continue
		}
		wsOff := d.wordStreamOffsets[L]
		if int64(wsOff) < prevEnd || int64(wsOff) >= size {
			return nil, fmt.Errorf("%w: invalid word_stream_offsets[%d]=%d", ErrInvalidHeader, L, wsOff)
		}

		var ftsWords uint64
		streamEndBound := size
		if isTSD1 {
			if occOff := d.occIndexOffsets[L]; occOff > 0 {
				if int64(occOff) < prevEnd || occOff >= wsOff || (wsOff-occOff)%4 != 0 {
					return nil, fmt.Errorf("%w: invalid occ_index_offsets[%d]=%d (ws=%d)", ErrInvalidHeader, L, occOff, wsOff)
				}
				ftsWords = uint64(wsOff-occOff) / 4
			}
			linkCnt := d.defLinkCounts[L]
			linkOff := d.defLinkOffsets[L]
			if linkCnt > 0 {
				if linkOff <= wsOff || int64(linkOff)+4*int64(linkCnt) > size {
					return nil, fmt.Errorf("%w: invalid def_link_offsets[%d]=%d (count=%d)", ErrInvalidHeader, L, linkOff, linkCnt)
				}
				streamEndBound = int64(linkOff)
			} else if linkOff != 0 {
				return nil, fmt.Errorf("%w: non-zero def_link_offsets[%d]=%d with zero count", ErrInvalidHeader, L, linkOff)
			}
		}
		if streamEndBound == size {
			for nextL := L + 1; nextL < numLengths; nextL++ {
				if d.wordCounts[nextL] > 0 {
					nextBound := int64(d.wordStreamOffsets[nextL])
					if isTSD1 && d.occIndexOffsets[nextL] > 0 && int64(d.occIndexOffsets[nextL]) < nextBound {
						nextBound = int64(d.occIndexOffsets[nextL])
					}
					if nextBound < int64(wsOff) || nextBound > size {
						return nil, fmt.Errorf("%w: out-of-order stream offset at length %d", ErrInvalidHeader, nextL)
					}
					streamEndBound = nextBound
					break
				}
			}
		}

		// Every Huffman symbol uses >= 1 bit; each Sub-stream 1 word has L letters + NUL (+ 1 link bit in TSD1).
		bitsPerWord := uint64(L + 1)
		if isTSD1 {
			bitsPerWord = uint64(L + 2)
		}
		minBits := uint64(d.wordCounts[L])*bitsPerWord + 1
		if isTSD1 {
			minBits += ftsWords*uint64(L+1) + 1
		}
		availBits := uint64(streamEndBound-int64(wsOff)) * 8
		if minBits > availBits {
			return nil, fmt.Errorf("%w: word_counts[%d]=%d exceeds available bitstream (%d bits > %d bits)",
				ErrInvalidHeader, L, d.wordCounts[L], minBits, availBits)
		}

		minStreamBytes := int64((minBits + 7) / 8)
		if isTSD1 && d.defLinkCounts[L] > 0 {
			prevEnd = int64(d.defLinkOffsets[L]) + 4*int64(d.defLinkCounts[L])
		} else {
			prevEnd = int64(wsOff) + minStreamBytes
		}
	}

	huff, err := newHuffmanDecoder(&d.charTable)
	if err != nil {
		return nil, err
	}
	d.huff = huff
	return d, nil
}

// Close releases the underlying file handle if the Dictionary was opened via Open.
func (d *Dictionary) Close() error {
	if d.closer != nil {
		return d.closer.Close()
	}
	return nil
}

// Path returns the filesystem path passed to Open, or "" for in-memory/ReaderAt dictionaries.
func (d *Dictionary) Path() string { return d.path }

// Name returns the dictionary's display identifier (typically filename without .tsd).
func (d *Dictionary) Name() string { return d.name }

// Size returns the total size in bytes of the backing .tsd data.
func (d *Dictionary) Size() int64 { return d.size }

// Version returns VersionTSD0 or VersionTSD1.
func (d *Dictionary) Version() FormatVersion { return d.version }

// Description returns the dictionary description decoded from Windows-1252 to UTF-8.
func (d *Dictionary) Description() string { return d.description }

// RawDescription returns a copy of the raw CP1252 description bytes.
func (d *Dictionary) RawDescription() []byte {
	return append([]byte(nil), d.rawDescription...)
}

// NumLengths returns the length of the per-length header tables (MaxWordLength + 1, or 0 if empty).
func (d *Dictionary) NumLengths() int { return d.numLengths }

// MaxWordLength returns the longest canonical word length present in the dictionary (0 if empty).
func (d *Dictionary) MaxWordLength() int {
	if d.numLengths <= 1 {
		return 0
	}
	return d.numLengths - 1
}

// WordCount returns the number of unique Sub-stream 1 dictionary words of canonical length L.
func (d *Dictionary) WordCount(length int) uint32 {
	if length <= 0 || length >= d.numLengths {
		return 0
	}
	return d.wordCounts[length]
}

// TotalWords returns the sum of WordCount(L) across all word lengths.
func (d *Dictionary) TotalWords() uint64 { return d.totalWords }

// DefPassOffsets returns [Pass1Start, Pass2Start, DefsEnd] file offsets (all 0 for TSD0).
func (d *Dictionary) DefPassOffsets() [3]uint32 { return d.defPassOffsets }

// HasDefinitions reports whether the dictionary is TSD1 and contains at least one definition record.
func (d *Dictionary) HasDefinitions() bool {
	return d.version == VersionTSD1 && d.defPassOffsets[2] > d.defPassOffsets[0]
}

// HasPass2Definitions reports whether the dictionary contains headword definitions (searchable via FTS).
func (d *Dictionary) HasPass2Definitions() bool {
	return d.version == VersionTSD1 && d.defPassOffsets[2] > d.defPassOffsets[1]
}

// HasFTSIndex reports whether the dictionary contains full-text search index structures.
func (d *Dictionary) HasFTSIndex() bool {
	if d.version != VersionTSD1 {
		return false
	}
	for _, off := range d.occIndexOffsets {
		if off > 0 {
			return true
		}
	}
	for _, cnt := range d.defLinkCounts {
		if cnt > 0 {
			return true
		}
	}
	return false
}

// NumUsedLetters returns the count of alphabetic equivalence classes with non-zero frequency.
func (d *Dictionary) NumUsedLetters() int { return d.numUsedLetters }

// NumTotalLetters returns the total count of alphabetic equivalence classes in the dictionary's .TSM.
func (d *Dictionary) NumTotalLetters() int { return d.numTotalLetters }

// CharInfo returns the 8-byte character & Huffman metadata for raw CP1252 byte b.
func (d *Dictionary) CharInfo(b byte) CharEntry { return d.charTable[b] }

// CharTable returns a copy of the full 256-entry character & Huffman table.
func (d *Dictionary) CharTable() [256]CharEntry { return d.charTable }

// LetterIndex returns the 0-based equivalence class index for raw byte b, or -1 if non-alphabetic.
func (d *Dictionary) LetterIndex(b byte) int16 { return d.charTable[b].LetterIndex }

// MappedChar returns the canonical byte for raw byte b, or 0 if non-alphabetic.
func (d *Dictionary) MappedChar(b byte) byte { return d.charTable[b].MappedChar }

// IsLetter reports whether raw byte b is alphabetic in this dictionary.
func (d *Dictionary) IsLetter(b byte) bool { return d.charTable[b].LetterIndex >= 0 }

// CanonicalByteForIndex returns the canonical byte (e.g. 'A'..'Z') for letter index li,
// matching TEA.exe's +0x2bf8 lookup used when formatting anagram balances.
func (d *Dictionary) CanonicalByteForIndex(letterIdx int) byte {
	if letterIdx < 0 || letterIdx >= d.numTotalLetters {
		return '!'
	}
	return d.canonByIndex[letterIdx]
}

// Calibration returns the log-probability score for (length, pos, letterIdx),
// or -128 if the letter never occurs at (length, pos) or parameters are out of range.
func (d *Dictionary) Calibration(length, pos, letterIdx int) int8 {
	if length <= 0 || length >= d.numLengths || pos < 0 || pos >= length {
		return -128
	}
	if letterIdx < 0 || letterIdx >= d.numUsedLetters {
		return -128
	}
	row := length*(length-1)/2 + pos
	return d.calibration[row*d.numUsedLetters+letterIdx]
}

// WordStreamOffset returns the raw byte offset of the Huffman bitstream for length L (0 if out of range).
func (d *Dictionary) WordStreamOffset(length int) uint32 {
	if length <= 0 || length >= d.numLengths {
		return 0
	}
	return d.wordStreamOffsets[length]
}

// FTSOnlyWordCount returns the number of Sub-stream 2 (FTS-only) words for length L.
func (d *Dictionary) FTSOnlyWordCount(length int) int {
	if d.version != VersionTSD1 || length <= 0 || length >= d.numLengths || d.wordCounts[length] == 0 {
		return 0
	}
	occOff := d.occIndexOffsets[length]
	wsOff := d.wordStreamOffsets[length]
	if occOff == 0 || occOff >= wsOff {
		return 0
	}
	return int((wsOff - occOff) / 4)
}
