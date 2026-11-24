package tsd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
)

// ExportMode controls whether word list extraction emits bare words or full tisane-build metadata.
type ExportMode uint8

const (
	// ExportSimple emits one word per line, discarding descriptions, annotations, qualifiers, and definitions.
	ExportSimple ExportMode = iota
	// ExportMetadata emits tisane-build source lines (optional | header, +annotations, +qualifiers, ;aliases, |definitions).
	ExportMetadata
)

// ExportSort controls the output ordering of extracted entries.
type ExportSort uint8

const (
	// ExportSortAuto uses global A-Z order for ExportSimple, and original definition section order + A-Z standalone words for ExportMetadata.
	ExportSortAuto ExportSort = iota
	// ExportSortAlpha sorts all emitted entries globally A-Z using standard TSD collation.
	ExportSortAlpha
	// ExportSortLength emits entries in canonical word-length bucket order (1..MaxWordLength).
	ExportSortLength
	// ExportSortSection preserves .tsd definition section order followed by standalone words.
	ExportSortSection
)

// ExportOptions configures word list extraction from an opened *Dictionary.
type ExportOptions struct {
	Mode              ExportMode
	Sort              ExportSort
	MinLen            int           // Default 1
	MaxLen            int           // Default d.MaxWordLength() (up to 100)
	OmitDefinitions   bool          // In ExportMetadata mode, emit word+annotation only without qualifiers/definitions
	IncludeHeader     bool          // In ExportMetadata mode, emit "| <description>" comment line when non-empty
	DedupeSimpleWords bool          // In ExportSimple mode, suppress duplicate words that differed only by annotation
	Encoding          InputEncoding // EncodingUTF8 (default) or EncodingCP1252
}

// DefaultExportOptions returns sensible defaults for the requested ExportMode.
func DefaultExportOptions(mode ExportMode) ExportOptions {
	return ExportOptions{
		Mode:              mode,
		Sort:              ExportSortAuto,
		MinLen:            1,
		MaxLen:            MaxWordLengthLimit,
		IncludeHeader:     mode == ExportMetadata,
		DedupeSimpleWords: true,
		Encoding:          EncodingUTF8,
	}
}

// ExportStats summarizes an extraction run.
type ExportStats struct {
	LinesWritten      int
	WordsExported     int
	DefinitionsExport int
}

// AlphabetMap reconstructs the 256-byte .tsm character equivalence map stored in the dictionary header.
func (d *Dictionary) AlphabetMap() TSM {
	var tsm TSM
	for i := 0; i < 256; i++ {
		tsm[i] = d.charTable[i].MappedChar
	}
	return tsm
}

// MarshalTSM serializes t into standard .TSM line format ("x=\n" / "x=y\n") compatible with LoadTSM and tisane-build -m.
func (t TSM) MarshalTSM() []byte {
	var buf bytes.Buffer
	for c := 1; c < 256; c++ {
		if c == 0x0A || c == 0x0D {
			continue
		}
		target := t[c]
		if target == byte(c) {
			continue
		}
		buf.WriteByte(byte(c))
		buf.WriteByte('=')
		if target != 0 && target != 0x0A && target != 0x0D {
			buf.WriteByte(target)
		}
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

type exportWord struct {
	id       int
	length   int
	rawFull  []byte // wordRaw or wordRaw + '\n' + annotRaw (for CompareWordsAlpha)
	wordRaw  []byte // CP1252 word bytes
	annotRaw []byte // CP1252 annotation bytes (nil if none)
	defOffs  []uint32
	emitted  bool
}

type exportRecord struct {
	sortKey []byte // Primary CP1252 word for ExportSortAlpha / tie-breaking
	keyLen  int    // Canonical letter count of primary word for ExportSortLength
	line    []byte // Complete CP1252 line without trailing '\n'
	isDef   bool
}

func (d *Dictionary) canonicalWordLen(raw []byte) int {
	n := 0
	for _, b := range raw {
		if b == '\n' {
			break
		}
		if d.charTable[b].MappedChar != 0 {
			n++
		}
	}
	return n
}

func appendEscapedSourceToken(dst, src []byte) []byte {
	for _, b := range src {
		switch b {
		case '\r', '\n':
			dst = append(dst, ' ')
		case '\\', '+', ':', ';', '|':
			dst = append(dst, '\\', b)
		default:
			dst = append(dst, b)
		}
	}
	return dst
}

func appendSanitizedSingleLine(dst, src []byte, isRTF bool) []byte {
	for i := 0; i < len(src); i++ {
		b := src[i]
		if b == '\r' {
			continue
		}
		if b == '\n' {
			if isRTF {
				dst = append(dst, []byte(`\par `)...)
			} else {
				dst = append(dst, ' ')
			}
			continue
		}
		dst = append(dst, b)
	}
	return dst
}

func formatWordAnnotCP1252(dst, wordRaw, annotRaw []byte) []byte {
	dst = appendEscapedSourceToken(dst, wordRaw)
	if len(annotRaw) > 0 {
		dst = append(dst, '+')
		dst = appendEscapedSourceToken(dst, annotRaw)
	}
	return dst
}

func formatHeadwordCP1252(dst, wordRaw, annotRaw, qualRaw []byte) []byte {
	dst = appendEscapedSourceToken(dst, wordRaw)
	if len(qualRaw) > 0 {
		dst = append(dst, '+')
		if len(annotRaw) > 0 {
			dst = appendEscapedSourceToken(dst, annotRaw)
		}
		dst = append(dst, '+')
		dst = appendEscapedSourceToken(dst, qualRaw)
	} else if len(annotRaw) > 0 {
		dst = append(dst, '+')
		dst = appendEscapedSourceToken(dst, annotRaw)
	}
	return dst
}

func writeEncodedLine(bw *bufio.Writer, cp1252Line []byte, enc InputEncoding) error {
	if enc == EncodingCP1252 {
		if _, err := bw.Write(cp1252Line); err != nil {
			return err
		}
	} else {
		// Fast path for pure-ASCII lines (avoids string allocation)
		ascii := true
		for _, b := range cp1252Line {
			if b >= 0x80 {
				ascii = false
				break
			}
		}
		if ascii {
			if _, err := bw.Write(cp1252Line); err != nil {
				return err
			}
		} else {
			if _, err := bw.WriteString(DecodeCP1252(cp1252Line)); err != nil {
				return err
			}
		}
	}
	return bw.WriteByte('\n')
}

// ExportFile extracts a word list (simple or metadata) from d and writes it atomically to outputPath.
func (d *Dictionary) ExportFile(outputPath string, opts ExportOptions) (*ExportStats, error) {
	var stats *ExportStats
	err := writeFileAtomic(outputPath, func(f *os.File) error {
		var exportErr error
		stats, exportErr = d.Export(f, opts)
		return exportErr
	})
	if err != nil {
		return nil, err
	}
	return stats, nil
}

// Export extracts words (and optional metadata/definitions) from d into w.
func (d *Dictionary) Export(w io.Writer, opts ExportOptions) (*ExportStats, error) {
	minL := opts.MinLen
	if minL <= 0 {
		minL = 1
	}
	maxL := opts.MaxLen
	if maxL <= 0 || maxL > MaxWordLengthLimit {
		maxL = MaxWordLengthLimit
	}
	if minL > maxL {
		return nil, fmt.Errorf("tsd: MinLen (%d) exceeds MaxLen (%d)", minL, maxL)
	}
	if maxL > d.MaxWordLength() {
		maxL = d.MaxWordLength()
	}

	bw := bufio.NewWriterSize(w, 64*1024)
	stats := &ExportStats{}
	if maxL < minL || d.MaxWordLength() == 0 {
		if opts.Mode == ExportMetadata && opts.IncludeHeader && len(d.rawDescription) > 0 {
			hdr := append([]byte("| "), appendSanitizedSingleLine(nil, d.rawDescription, false)...)
			if err := writeEncodedLine(bw, hdr, opts.Encoding); err != nil {
				return nil, err
			}
			stats.LinesWritten++
		}
		return stats, bw.Flush()
	}

	sortMode := opts.Sort
	if sortMode == ExportSortAuto {
		if opts.Mode == ExportSimple {
			sortMode = ExportSortAlpha
		} else {
			sortMode = ExportSortSection
		}
	}

	// Fast streaming O(1)-memory path for ExportSimple + ExportSortLength.
	if opts.Mode == ExportSimple && sortMode == ExportSortLength {
		var prevWord []byte
		for L := minL; L <= maxL; L++ {
			var seenInBucket map[string]struct{}
			if opts.DedupeSimpleWords {
				seenInBucket = make(map[string]struct{})
			}
			for entry, err := range d.ScanRawWords(L) {
				if err != nil {
					return nil, err
				}
				if opts.DedupeSimpleWords {
					if bytes.Equal(entry.WordBytes, prevWord) {
						continue
					}
					key := string(entry.WordBytes)
					if _, dup := seenInBucket[key]; dup {
						continue
					}
					seenInBucket[key] = struct{}{}
					prevWord = append(prevWord[:0], entry.WordBytes...)
				}
				if err := writeEncodedLine(bw, entry.WordBytes, opts.Encoding); err != nil {
					return nil, err
				}
				stats.LinesWritten++
				stats.WordsExported++
			}
		}
		return stats, bw.Flush()
	}

	// ExportSimple with global A-Z sorting.
	if opts.Mode == ExportSimple {
		capHint := 0
		for L := minL; L <= maxL; L++ {
			capHint += int(d.WordCount(L))
		}
		prealloc := min(capHint, 1<<20)
		words := make([][]byte, 0, prealloc)
		var seen map[string]struct{}
		if opts.DedupeSimpleWords {
			seen = make(map[string]struct{}, prealloc)
		}
		for L := minL; L <= maxL; L++ {
			for entry, err := range d.ScanRawWords(L) {
				if err != nil {
					return nil, err
				}
				if opts.DedupeSimpleWords {
					key := string(entry.WordBytes)
					if _, dup := seen[key]; dup {
						continue
					}
					seen[key] = struct{}{}
				}
				words = append(words, append([]byte(nil), entry.WordBytes...))
			}
		}
		sort.Slice(words, func(i, j int) bool {
			return CompareWordsAlpha(words[i], words[j]) < 0
		})
		for _, wb := range words {
			if err := writeEncodedLine(bw, wb, opts.Encoding); err != nil {
				return nil, err
			}
			stats.LinesWritten++
			stats.WordsExported++
		}
		return stats, bw.Flush()
	}

	// ExportMetadata mode (tisane-build source format).
	if opts.IncludeHeader && len(d.rawDescription) > 0 {
		hdr := append([]byte("| "), appendSanitizedSingleLine(nil, d.rawDescription, false)...)
		if err := writeEncodedLine(bw, hdr, opts.Encoding); err != nil {
			return nil, err
		}
		stats.LinesWritten++
	}

	needDefs := !opts.OmitDefinitions && d.HasDefinitions()
	capHint := 0
	for L := minL; L <= maxL; L++ {
		capHint += int(d.WordCount(L))
	}
	allWords := make([]*exportWord, 0, min(capHint, 1<<20))
	defToWords := make(map[uint32][]*exportWord)

	for L := minL; L <= maxL; L++ {
		if d.WordCount(L) == 0 {
			continue
		}
		var lt *LinkTable
		if needDefs {
			var err error
			lt, err = d.ReadLinkTable(L)
			if err != nil {
				return nil, err
			}
		}
		for entry, err := range d.ScanRawWords(L) {
			if err != nil {
				return nil, err
			}
			ew := &exportWord{
				id:      len(allWords),
				length:  L,
				rawFull: append([]byte(nil), entry.RawBytes...),
				wordRaw: append([]byte(nil), entry.WordBytes...),
			}
			if len(entry.AnnotBytes) > 0 {
				ew.annotRaw = append([]byte(nil), entry.AnnotBytes...)
			}
			if needDefs && entry.LinkGroupIndex >= 0 && lt != nil {
				res, rErr := lt.Resolve(int(entry.LinkGroupIndex))
				if rErr != nil {
					return nil, rErr
				}
				if len(res.DirectOffsets) > 0 {
					ew.defOffs = res.DirectOffsets
					for _, off := range res.DirectOffsets {
						defToWords[off] = append(defToWords[off], ew)
					}
				}
			}
			allWords = append(allWords, ew)
		}
	}
	stats.WordsExported = len(allWords)

	lengthFiltered := minL > 1 || maxL < d.MaxWordLength()
	var records []exportRecord

	if needDefs {
		var lineBuf []byte
		for rec, err := range d.ScanDefinitions(0) {
			if err != nil {
				return nil, err
			}
			linked := defToWords[rec.Offset]
			if lengthFiltered && len(linked) == 0 {
				continue
			}

			lineBuf = lineBuf[:0]
			var primaryKey []byte

			if rec.Pass == 1 || len(rec.Headwords) == 0 {
				if len(linked) == 0 {
					continue
				}
				// Sort aliases deterministically A-Z
				sort.Slice(linked, func(i, j int) bool {
					return CompareWordsAlpha(linked[i].rawFull, linked[j].rawFull) < 0
				})
				primaryKey = linked[0].rawFull
				lineBuf = append(lineBuf, ';')
				for i, lw := range linked {
					if i > 0 {
						lineBuf = append(lineBuf, ':')
					}
					lineBuf = formatWordAnnotCP1252(lineBuf, lw.wordRaw, lw.annotRaw)
					lw.emitted = true
				}
			} else {
				used := make([]bool, len(linked))
				wroteHW := 0
				for _, hw := range rec.Headwords {
					matchIdx := -1
					for i, lw := range linked {
						if !used[i] && bytes.Equal(lw.wordRaw, hw.RawHeadword) {
							matchIdx = i
							break
						}
					}
					if matchIdx >= 0 {
						used[matchIdx] = true
						linked[matchIdx].emitted = true
						if wroteHW > 0 {
							lineBuf = append(lineBuf, ':')
						} else {
							primaryKey = linked[matchIdx].rawFull
						}
						lineBuf = formatHeadwordCP1252(lineBuf, hw.RawHeadword, linked[matchIdx].annotRaw, hw.RawQualifier)
						wroteHW++
					} else if !lengthFiltered {
						if wroteHW > 0 {
							lineBuf = append(lineBuf, ':')
						} else {
							primaryKey = hw.RawHeadword
						}
						lineBuf = formatHeadwordCP1252(lineBuf, hw.RawHeadword, nil, hw.RawQualifier)
						wroteHW++
					}
				}
				if wroteHW == 0 {
					// All headwords were filtered out by length; emit remaining linked words as Pass 1 alias line or skip
					if len(linked) == 0 {
						continue
					}
				}

				// Remaining linked words not consumed as headwords are aliases (after ';')
				var aliases []*exportWord
				for i, lw := range linked {
					if !used[i] {
						aliases = append(aliases, lw)
					}
				}
				if len(aliases) > 0 {
					sort.Slice(aliases, func(i, j int) bool {
						return CompareWordsAlpha(aliases[i].rawFull, aliases[j].rawFull) < 0
					})
					if primaryKey == nil {
						primaryKey = aliases[0].rawFull
					}
					lineBuf = append(lineBuf, ';')
					for i, al := range aliases {
						if i > 0 {
							lineBuf = append(lineBuf, ':')
						}
						lineBuf = formatWordAnnotCP1252(lineBuf, al.wordRaw, al.annotRaw)
						al.emitted = true
					}
				}
			}

			lineBuf = append(lineBuf, '|')
			lineBuf = appendSanitizedSingleLine(lineBuf, rec.RawBody, rec.IsRTF)

			records = append(records, exportRecord{
				sortKey: primaryKey,
				keyLen:  d.canonicalWordLen(primaryKey),
				line:    append([]byte(nil), lineBuf...),
				isDef:   true,
			})
			stats.DefinitionsExport++
		}
	}

	var standalone []*exportWord
	for _, ew := range allWords {
		if !ew.emitted {
			standalone = append(standalone, ew)
		}
	}

	if sortMode != ExportSortLength {
		sort.Slice(standalone, func(i, j int) bool {
			return CompareWordsAlpha(standalone[i].rawFull, standalone[j].rawFull) < 0
		})
	}

	standaloneRecords := make([]exportRecord, 0, len(standalone))
	for _, ew := range standalone {
		line := formatWordAnnotCP1252(nil, ew.wordRaw, ew.annotRaw)
		standaloneRecords = append(standaloneRecords, exportRecord{
			sortKey: ew.rawFull,
			keyLen:  ew.length,
			line:    line,
			isDef:   false,
		})
	}

	switch sortMode {
	case ExportSortAlpha:
		records = append(records, standaloneRecords...)
		sort.SliceStable(records, func(i, j int) bool {
			return CompareWordsAlpha(records[i].sortKey, records[j].sortKey) < 0
		})
	case ExportSortLength:
		combined := make([]exportRecord, 0, len(standaloneRecords)+len(records))
		combined = append(combined, standaloneRecords...)
		combined = append(combined, records...)
		sort.SliceStable(combined, func(i, j int) bool {
			if combined[i].keyLen != combined[j].keyLen {
				return combined[i].keyLen < combined[j].keyLen
			}
			return CompareWordsAlpha(combined[i].sortKey, combined[j].sortKey) < 0
		})
		records = combined
	default:
		// ExportSortSection / ExportSortAuto: emit standalone words first, then definitions in offset order.
		combined := make([]exportRecord, 0, len(standaloneRecords)+len(records))
		combined = append(combined, standaloneRecords...)
		combined = append(combined, records...)
		records = combined
	}

	for _, r := range records {
		if err := writeEncodedLine(bw, r.line, opts.Encoding); err != nil {
			return nil, err
		}
		stats.LinesWritten++
	}

	return stats, bw.Flush()
}
