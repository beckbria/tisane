package tsd

import (
	"fmt"
	"iter"
	"sync"
)

// RawEntry provides a zero-allocation view of a decoded Sub-stream 1 dictionary entry.
// All byte slices are backed by internal scanner buffers and remain valid only until
// the next iteration step; callers retaining data must copy or convert them.
type RawEntry struct {
	RawBytes         []byte // Full raw CP1252 display_word ("word" or "word\nannotation")
	WordBytes        []byte // Raw CP1252 bytes prior to '\n'
	AnnotBytes       []byte // Raw CP1252 bytes after '\n' (nil if unannotated)
	CanonicalLetters []byte // Mapped canonical bytes of WordBytes (len == length)
	LetterIndices    []byte // Equivalence class indices 0..NumUsedLetters-1 (len == length)
	LinkGroupIndex   int32  // 0-based index in def_link_offsets[length] if has_links==1, else -1
}

// WordEntry is an allocated UTF-8 representation of a Sub-stream 1 dictionary word.
type WordEntry struct {
	Word           string // Headword/alias decoded to UTF-8
	Annotation     string // Optional inline annotation decoded to UTF-8 ("" if none)
	Length         int    // Canonical letter count
	LinkGroupIndex int32  // >= 0 if word has definition or FTS links, else -1
}

// RawStreamEntry represents a word from either Sub-stream 1 (dictionary word)
// or Sub-stream 2 (FTS-only body word) when scanning an entire length bitstream.
type RawStreamEntry struct {
	RawEntry
	IsFTSOnly bool  // False for Sub-stream 1, true for Sub-stream 2
	FTSIndex  int32 // >= 0 index into occ_index_offsets[length] when IsFTSOnly is true, else -1
}

// FTSEntry is an allocated UTF-8 representation of a Sub-stream 2 FTS-only word
// along with its occurrence list pointer from occ_index_offsets[length].
type FTSEntry struct {
	Word          string // FTS-only word decoded to UTF-8
	Canonical     []byte // Canonical letter bytes (len == length)
	Length        int    // Canonical letter count
	FTSIndex      int    // 0-based index in occ_index_offsets[length]
	OccListOffset uint32 // File offset of occurrence list, or FTSOverflowSentinel (0x7FFFFFFF)
}

type streamScanner struct {
	br       bitStreamReader
	rawBuf   []byte
	canonBuf [MaxWordLengthLimit + 4]byte
	idxBuf   [MaxWordLengthLimit + 4]byte
}

var scannerPool = sync.Pool{
	New: func() any {
		return &streamScanner{
			rawBuf: make([]byte, 0, 256),
		}
	},
}

// wordStreamUpperBound returns a conservative upper byte bound on the word bitstream for length L.
func (d *Dictionary) wordStreamUpperBound(length int) int64 {
	if d.version == VersionTSD1 && d.defLinkCounts[length] > 0 {
		return int64(d.defLinkOffsets[length])
	}
	for nextL := length + 1; nextL < d.numLengths; nextL++ {
		if d.wordCounts[nextL] > 0 {
			return int64(d.wordStreamOffsets[nextL])
		}
	}
	return d.size
}

// decodeOneWord decodes a NUL-terminated Huffman string into sc.rawBuf, sc.canonBuf, and sc.idxBuf.
func (d *Dictionary) decodeOneWord(sc *streamScanner) (annotPos int, canonLen int, err error) {
	sc.rawBuf = sc.rawBuf[:0]
	annotPos = -1
	canonLen = 0
	for {
		sym, decErr := sc.br.decodeSymbol(d.huff)
		if decErr != nil {
			return -1, 0, decErr
		}
		if sym == 0 {
			return annotPos, canonLen, nil
		}
		if sym == 0x0A && annotPos < 0 {
			annotPos = len(sc.rawBuf)
		} else if annotPos < 0 {
			ce := d.charTable[sym]
			if ce.LetterIndex >= 0 {
				if canonLen >= len(sc.canonBuf) {
					return -1, 0, fmt.Errorf("%w: word canonical length exceeds %d", ErrCorruptBitstream, MaxWordLengthLimit)
				}
				sc.canonBuf[canonLen] = ce.MappedChar
				sc.idxBuf[canonLen] = byte(ce.LetterIndex)
				canonLen++
			}
		}
		sc.rawBuf = append(sc.rawBuf, sym)
	}
}

// ScanRawWords streams Sub-stream 1 dictionary entries of the given canonical length (1..MaxWordLength)
// with zero per-word heap allocations.
func (d *Dictionary) ScanRawWords(length int) iter.Seq2[RawEntry, error] {
	return func(yield func(RawEntry, error) bool) {
		if length <= 0 || length >= d.numLengths || d.wordCounts[length] == 0 {
			return
		}
		wsOff := int64(d.wordStreamOffsets[length])
		endBound := d.wordStreamUpperBound(length)

		sc := scannerPool.Get().(*streamScanner)
		defer scannerPool.Put(sc)
		sc.br.reset(d.r, wsOff, endBound)

		isTSD1 := d.version == VersionTSD1
		expectedCount := d.wordCounts[length]
		var decoded uint32
		var linkIdx int32

		for {
			annotPos, canonLen, err := d.decodeOneWord(sc)
			if err != nil {
				yield(RawEntry{}, fmt.Errorf("length %d word %d: %w", length, decoded, err))
				return
			}
			if len(sc.rawBuf) == 0 {
				if decoded != expectedCount {
					yield(RawEntry{}, fmt.Errorf("%w: length %d ended after %d words, expected %d",
						ErrCorruptBitstream, length, decoded, expectedCount))
				}
				return
			}
			if decoded >= expectedCount {
				yield(RawEntry{}, fmt.Errorf("%w: length %d exceeded expected word count %d",
					ErrCorruptBitstream, length, expectedCount))
				return
			}
			if canonLen != length {
				yield(RawEntry{}, fmt.Errorf("%w: word in bucket %d decoded with %d canonical letters",
					ErrCorruptBitstream, length, canonLen))
				return
			}

			curLink := int32(-1)
			if isTSD1 {
				hasLink, bitErr := sc.br.readBit()
				if bitErr != nil {
					yield(RawEntry{}, fmt.Errorf("length %d word %d link bit: %w", length, decoded, bitErr))
					return
				}
				if hasLink != 0 {
					curLink = linkIdx
					linkIdx++
				}
			}

			var wordBytes, annotBytes []byte
			if annotPos >= 0 {
				wordBytes = sc.rawBuf[:annotPos]
				annotBytes = sc.rawBuf[annotPos+1:]
			} else {
				wordBytes = sc.rawBuf
			}

			entry := RawEntry{
				RawBytes:         sc.rawBuf,
				WordBytes:        wordBytes,
				AnnotBytes:       annotBytes,
				CanonicalLetters: sc.canonBuf[:canonLen],
				LetterIndices:    sc.idxBuf[:canonLen],
				LinkGroupIndex:   curLink,
			}
			decoded++
			if !yield(entry, nil) {
				return
			}
		}
	}
}

// ScanWords streams Sub-stream 1 dictionary words of the given canonical length as UTF-8 WordEntry values.
func (d *Dictionary) ScanWords(length int) iter.Seq2[WordEntry, error] {
	return func(yield func(WordEntry, error) bool) {
		for raw, err := range d.ScanRawWords(length) {
			if err != nil {
				yield(WordEntry{}, err)
				return
			}
			var annot string
			if len(raw.AnnotBytes) > 0 {
				annot = DecodeCP1252(raw.AnnotBytes)
			}
			we := WordEntry{
				Word:           DecodeCP1252(raw.WordBytes),
				Annotation:     annot,
				Length:         length,
				LinkGroupIndex: raw.LinkGroupIndex,
			}
			if !yield(we, nil) {
				return
			}
		}
	}
}

// ScanAllRawWords streams Sub-stream 1 dictionary entries followed seamlessly by Sub-stream 2
// (FTS-only) entries for length L in a single pass over the Huffman bitstream.
// This powers single-pass Indexed Full Text Search in TSD1 dictionaries.
func (d *Dictionary) ScanAllRawWords(length int) iter.Seq2[RawStreamEntry, error] {
	return func(yield func(RawStreamEntry, error) bool) {
		if length <= 0 || length >= d.numLengths || d.wordCounts[length] == 0 {
			return
		}
		wsOff := int64(d.wordStreamOffsets[length])
		endBound := d.wordStreamUpperBound(length)

		sc := scannerPool.Get().(*streamScanner)
		defer scannerPool.Put(sc)
		sc.br.reset(d.r, wsOff, endBound)

		isTSD1 := d.version == VersionTSD1
		expectedCount := d.wordCounts[length]
		var decoded uint32
		var linkIdx int32

		// Sub-stream 1
		for {
			annotPos, canonLen, err := d.decodeOneWord(sc)
			if err != nil {
				yield(RawStreamEntry{}, fmt.Errorf("length %d word %d: %w", length, decoded, err))
				return
			}
			if len(sc.rawBuf) == 0 {
				if decoded != expectedCount {
					yield(RawStreamEntry{}, fmt.Errorf("%w: length %d ended after %d words, expected %d",
						ErrCorruptBitstream, length, decoded, expectedCount))
					return
				}
				break
			}
			if decoded >= expectedCount || canonLen != length {
				yield(RawStreamEntry{}, fmt.Errorf("%w: length %d Sub-stream 1 invariant violation", ErrCorruptBitstream, length))
				return
			}

			curLink := int32(-1)
			if isTSD1 {
				hasLink, bitErr := sc.br.readBit()
				if bitErr != nil {
					yield(RawStreamEntry{}, bitErr)
					return
				}
				if hasLink != 0 {
					curLink = linkIdx
					linkIdx++
				}
			}

			var wordBytes, annotBytes []byte
			if annotPos >= 0 {
				wordBytes = sc.rawBuf[:annotPos]
				annotBytes = sc.rawBuf[annotPos+1:]
			} else {
				wordBytes = sc.rawBuf
			}

			entry := RawStreamEntry{
				RawEntry: RawEntry{
					RawBytes:         sc.rawBuf,
					WordBytes:        wordBytes,
					AnnotBytes:       annotBytes,
					CanonicalLetters: sc.canonBuf[:canonLen],
					LetterIndices:    sc.idxBuf[:canonLen],
					LinkGroupIndex:   curLink,
				},
				IsFTSOnly: false,
				FTSIndex:  -1,
			}
			decoded++
			if !yield(entry, nil) {
				return
			}
		}

		if !isTSD1 {
			return
		}

		// Sub-stream 2 (FTS-only words)
		expectedFTS := d.FTSOnlyWordCount(length)
		var ftsIdx int32
		for {
			_, canonLen, err := d.decodeOneWord(sc)
			if err != nil {
				yield(RawStreamEntry{}, fmt.Errorf("length %d FTS word %d: %w", length, ftsIdx, err))
				return
			}
			if len(sc.rawBuf) == 0 {
				if int(ftsIdx) != expectedFTS {
					yield(RawStreamEntry{}, fmt.Errorf("%w: length %d decoded %d FTS words, expected %d",
						ErrCorruptBitstream, length, ftsIdx, expectedFTS))
				}
				return
			}
			if canonLen != length {
				yield(RawStreamEntry{}, fmt.Errorf("%w: FTS word in bucket %d decoded with %d canonical letters",
					ErrCorruptBitstream, length, canonLen))
				return
			}
			entry := RawStreamEntry{
				RawEntry: RawEntry{
					RawBytes:         sc.rawBuf,
					WordBytes:        sc.rawBuf,
					CanonicalLetters: sc.canonBuf[:canonLen],
					LetterIndices:    sc.idxBuf[:canonLen],
					LinkGroupIndex:   -1,
				},
				IsFTSOnly: true,
				FTSIndex:  ftsIdx,
			}
			ftsIdx++
			if !yield(entry, nil) {
				return
			}
		}
	}
}

// ScanFTSOnlyWords decodes Sub-stream 2 (FTS-only) words for length L paired with their
// occurrence list offsets from occ_index_offsets[L].
func (d *Dictionary) ScanFTSOnlyWords(length int) iter.Seq2[FTSEntry, error] {
	return func(yield func(FTSEntry, error) bool) {
		if d.version != VersionTSD1 || length <= 0 || length >= d.numLengths || d.wordCounts[length] == 0 {
			return
		}
		ptrs, err := d.ReadFTSIndexPointers(length)
		if err != nil {
			yield(FTSEntry{}, err)
			return
		}
		if len(ptrs) == 0 {
			return
		}
		for entry, err := range d.ScanAllRawWords(length) {
			if err != nil {
				yield(FTSEntry{}, err)
				return
			}
			if !entry.IsFTSOnly {
				continue
			}
			idx := int(entry.FTSIndex)
			var occOff uint32
			if idx >= 0 && idx < len(ptrs) {
				occOff = ptrs[idx]
			}
			fe := FTSEntry{
				Word:          DecodeCP1252(entry.WordBytes),
				Canonical:     append([]byte(nil), entry.CanonicalLetters...),
				Length:        length,
				FTSIndex:      idx,
				OccListOffset: occOff,
			}
			if !yield(fe, nil) {
				return
			}
		}
	}
}
