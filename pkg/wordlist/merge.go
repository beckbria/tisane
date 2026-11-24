// Package wordlist provides utilities for converting and merging plain-text word lists.
package wordlist

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"tisane/internal/fsutil"
	"tisane/pkg/tsd"
)

const maxWordListFileBytes = 512 << 20 // 512 MiB

// Conversion controls how each word list entry is transformed prior to merging.
type Conversion string

const (
	// ConvertNone leaves entries unchanged (default).
	ConvertNone Conversion = "none"
	// ConvertASCII transliterates entries to printable ASCII characters.
	ConvertASCII Conversion = "ascii"
	// ConvertASCIILetters transliterates entries to ASCII and keeps only A-Z and a-z.
	ConvertASCIILetters Conversion = "ascii-letters"
	// ConvertASCIILower transliterates entries to ASCII and keeps only lowercase a-z.
	ConvertASCIILower Conversion = "ascii-lower"
	// ConvertASCIIUpper transliterates entries to ASCII and keeps only uppercase A-Z.
	ConvertASCIIUpper Conversion = "ascii-upper"
)

// Operation controls the set operation applied to the converted primary and secondary word lists.
type Operation string

const (
	// OpOR outputs entries present in either list (union).
	OpOR Operation = "or"
	// OpAND outputs entries present in both lists (intersection).
	OpAND Operation = "and"
	// OpEXCLUDE outputs entries present in the primary list and NOT in the secondary list (difference).
	OpEXCLUDE Operation = "exclude"
)

// MergeOptions configures in-memory word list merging.
type MergeOptions struct {
	Operation  Operation
	Conversion Conversion
	Encoding   tsd.InputEncoding
}

// MergeParams holds user-facing parameters shared by both the tisane-merge CLI and the Tisane GUI.
type MergeParams struct {
	PrimaryPath   string
	SecondaryPath string
	OutputPath    string
	Operation     string
	Conversion    string
	Encoding      string
}

// MergeStats summarizes a completed word list merge operation.
type MergeStats struct {
	PrimaryLines     int
	PrimaryEntries   int
	SecondaryLines   int
	SecondaryEntries int
	OutputEntries    int
	BytesWritten     int
}

// ParseConversion parses a user-facing conversion name into a Conversion value.
func ParseConversion(name string) (Conversion, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "none":
		return ConvertNone, nil
	case "ascii":
		return ConvertASCII, nil
	case "ascii-letters", "letters":
		return ConvertASCIILetters, nil
	case "ascii-lower", "lower":
		return ConvertASCIILower, nil
	case "ascii-upper", "upper":
		return ConvertASCIIUpper, nil
	default:
		return ConvertNone, fmt.Errorf("wordlist: unsupported conversion %q (expected none, ascii, ascii-letters, ascii-lower, or ascii-upper)", name)
	}
}

// ParseOperation parses a user-facing operation name into an Operation value.
func ParseOperation(name string) (Operation, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "or", "union":
		return OpOR, nil
	case "and", "intersect", "intersection":
		return OpAND, nil
	case "exclude", "diff", "difference", "not", "subtract":
		return OpEXCLUDE, nil
	default:
		return OpOR, fmt.Errorf("wordlist: unsupported operation %q (expected or, and, or exclude)", name)
	}
}

// DefaultMergedOutputPath suggests a default .txt output path derived from the primary input path.
func DefaultMergedOutputPath(primaryPath string) string {
	trimmed := strings.TrimSpace(primaryPath)
	if trimmed == "" || trimmed == "-" {
		return "merged.txt"
	}
	dir := filepath.Dir(trimmed)
	base := filepath.Base(trimmed)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		stem = "merged"
	} else {
		stem += "-merged"
	}
	name := stem + ".txt"
	if dir == "" || dir == "." {
		return name
	}
	return filepath.Join(dir, name)
}

// ResolveMergeOptions validates and converts user-facing MergeParams into MergeOptions.
func ResolveMergeOptions(params MergeParams) (MergeOptions, error) {
	op, err := ParseOperation(params.Operation)
	if err != nil {
		return MergeOptions{}, err
	}
	conv, err := ParseConversion(params.Conversion)
	if err != nil {
		return MergeOptions{}, err
	}
	enc, err := tsd.ParseInputEncoding(params.Encoding)
	if err != nil {
		return MergeOptions{}, err
	}
	return MergeOptions{
		Operation:  op,
		Conversion: conv,
		Encoding:   enc,
	}, nil
}

// ConvertEntry applies the requested Conversion to a single word list entry.
func ConvertEntry(s string, conv Conversion) string {
	switch conv {
	case "", ConvertNone:
		return s
	case ConvertASCII:
		return ToASCII(s)
	case ConvertASCIILetters:
		allLetters := true
		for i := 0; i < len(s); i++ {
			c := s[i]
			if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
				allLetters = false
				break
			}
		}
		if allLetters {
			return s
		}
		ascii := ToASCII(s)
		buf := make([]byte, 0, len(ascii))
		for i := 0; i < len(ascii); i++ {
			c := ascii[i]
			if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
				buf = append(buf, c)
			}
		}
		return string(buf)
	case ConvertASCIILower:
		allLower := true
		for i := 0; i < len(s); i++ {
			c := s[i]
			if c < 'a' || c > 'z' {
				allLower = false
				break
			}
		}
		if allLower {
			return s
		}
		ascii := ToASCII(s)
		buf := make([]byte, 0, len(ascii))
		for i := 0; i < len(ascii); i++ {
			c := ascii[i]
			if c >= 'A' && c <= 'Z' {
				buf = append(buf, c+('a'-'A'))
			} else if c >= 'a' && c <= 'z' {
				buf = append(buf, c)
			}
		}
		return string(buf)
	case ConvertASCIIUpper:
		allUpper := true
		for i := 0; i < len(s); i++ {
			c := s[i]
			if c < 'A' || c > 'Z' {
				allUpper = false
				break
			}
		}
		if allUpper {
			return s
		}
		ascii := ToASCII(s)
		buf := make([]byte, 0, len(ascii))
		for i := 0; i < len(ascii); i++ {
			c := ascii[i]
			if c >= 'a' && c <= 'z' {
				buf = append(buf, c-('a'-'A'))
			} else if c >= 'A' && c <= 'Z' {
				buf = append(buf, c)
			}
		}
		return string(buf)
	default:
		return s
	}
}

func validateMergeOptions(opts MergeOptions) (Operation, Conversion, tsd.InputEncoding, error) {
	op := opts.Operation
	if op == "" {
		op = OpOR
	}
	switch op {
	case OpOR, OpAND, OpEXCLUDE:
	default:
		return "", "", 0, fmt.Errorf("wordlist: unsupported operation %q", op)
	}

	conv := opts.Conversion
	if conv == "" {
		conv = ConvertNone
	}
	switch conv {
	case ConvertNone, ConvertASCII, ConvertASCIILetters, ConvertASCIILower, ConvertASCIIUpper:
	default:
		return "", "", 0, fmt.Errorf("wordlist: unsupported conversion %q", conv)
	}

	enc := opts.Encoding
	switch enc {
	case tsd.EncodingAuto, tsd.EncodingUTF8, tsd.EncodingCP1252:
	default:
		return "", "", 0, fmt.Errorf("wordlist: unsupported input encoding %d", enc)
	}
	return op, conv, enc, nil
}

// resolveByteEncoding resolves EncodingAuto on data to either EncodingUTF8 or EncodingCP1252
// and returns the slice with any UTF-8 BOM stripped.
func resolveByteEncoding(data []byte, enc tsd.InputEncoding) ([]byte, tsd.InputEncoding) {
	switch enc {
	case tsd.EncodingCP1252:
		return data, tsd.EncodingCP1252
	case tsd.EncodingUTF8:
		return bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}), tsd.EncodingUTF8
	default:
		if bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
			trimmed := data[3:]
			if utf8.Valid(trimmed) {
				return trimmed, tsd.EncodingUTF8
			}
		}
		hasHigh := false
		for _, b := range data {
			if b >= 0x80 {
				hasHigh = true
				break
			}
		}
		if !hasHigh || utf8.Valid(data) {
			return data, tsd.EncodingUTF8
		}
		return data, tsd.EncodingCP1252
	}
}

// detectFileEncoding streams f in 32 KiB chunks to classify EncodingAuto without loading the entire file into RAM.
func detectFileEncoding(f *os.File, enc tsd.InputEncoding) (startOffset int64, resolved tsd.InputEncoding, err error) {
	var bom [3]byte
	nBOM, readErr := io.ReadFull(f, bom[:])
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return 0, 0, readErr
	}
	hasBOM := nBOM == 3 && bom[0] == 0xEF && bom[1] == 0xBB && bom[2] == 0xBF

	switch enc {
	case tsd.EncodingCP1252:
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return 0, 0, err
		}
		return 0, tsd.EncodingCP1252, nil
	case tsd.EncodingUTF8:
		off := int64(0)
		if hasBOM {
			off = 3
		}
		if _, err := f.Seek(off, io.SeekStart); err != nil {
			return 0, 0, err
		}
		return off, tsd.EncodingUTF8, nil
	}

	scanStart := int64(0)
	if hasBOM {
		scanStart = 3
	}
	if _, err := f.Seek(scanStart, io.SeekStart); err != nil {
		return 0, 0, err
	}
	validUTF8, err := isValidUTF8Stream(f)
	if err != nil {
		return 0, 0, err
	}
	if validUTF8 {
		if _, err := f.Seek(scanStart, io.SeekStart); err != nil {
			return 0, 0, err
		}
		return scanStart, tsd.EncodingUTF8, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, 0, err
	}
	return 0, tsd.EncodingCP1252, nil
}

func isValidUTF8Stream(r io.Reader) (bool, error) {
	buf := make([]byte, 32*1024+utf8.UTFMax)
	carry := 0
	for {
		n, err := r.Read(buf[carry : carry+32*1024])
		total := carry + n
		if total > 0 {
			if errors.Is(err, io.EOF) {
				return utf8.Valid(buf[:total]), nil
			}
			// Keep up to 3 trailing incomplete UTF-8 continuation lead bytes for the next chunk.
			cut := total
			for back := 1; back < utf8.UTFMax && back <= total; back++ {
				if utf8.RuneStart(buf[total-back]) {
					if !utf8.FullRune(buf[total-back : total]) {
						cut = total - back
					}
					break
				}
			}
			if !utf8.Valid(buf[:cut]) {
				return false, nil
			}
			carry = total - cut
			copy(buf[:carry], buf[cut:total])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return carry == 0, nil
			}
			return false, err
		}
	}
}

// forEachRawLine splits r on \r\n, \n, or standalone \r without loading all lines into memory at once.
func forEachRawLine(r io.Reader, fn func(rawLine []byte) error) error {
	br := bufio.NewReaderSize(r, 64*1024)
	var lineBuf []byte
	for {
		b, err := br.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if len(lineBuf) > 0 {
					if cbErr := fn(lineBuf); cbErr != nil {
						return cbErr
					}
				}
				return nil
			}
			return err
		}
		if b == '\r' {
			if next, peekErr := br.Peek(1); peekErr == nil && next[0] == '\n' {
				_, _ = br.ReadByte()
			}
			if cbErr := fn(lineBuf); cbErr != nil {
				return cbErr
			}
			lineBuf = lineBuf[:0]
			continue
		}
		if b == '\n' {
			if cbErr := fn(lineBuf); cbErr != nil {
				return cbErr
			}
			lineBuf = lineBuf[:0]
			continue
		}
		lineBuf = append(lineBuf, b)
	}
}

func collectWordSet(r io.Reader, resolvedEnc tsd.InputEncoding, conv Conversion) (map[string]struct{}, int, error) {
	set := make(map[string]struct{})
	lines := 0
	err := forEachRawLine(r, func(rawLine []byte) error {
		if len(rawLine) == 0 {
			return nil
		}
		lines++
		var line string
		if resolvedEnc == tsd.EncodingCP1252 {
			line = tsd.DecodeCP1252(rawLine)
		} else {
			line = string(rawLine)
		}
		converted := ConvertEntry(line, conv)
		if converted != "" {
			set[converted] = struct{}{}
		}
		return nil
	})
	return set, lines, err
}

func computeSetOpSorted(primarySet, secondarySet map[string]struct{}, op Operation) []string {
	var result []string
	switch op {
	case OpOR:
		result = make([]string, 0, len(primarySet)+len(secondarySet))
		for w := range primarySet {
			result = append(result, w)
		}
		for w := range secondarySet {
			if _, exists := primarySet[w]; !exists {
				result = append(result, w)
			}
		}
	case OpAND:
		small, large := primarySet, secondarySet
		if len(secondarySet) < len(primarySet) {
			small, large = secondarySet, primarySet
		}
		result = make([]string, 0, len(small))
		for w := range small {
			if _, exists := large[w]; exists {
				result = append(result, w)
			}
		}
	case OpEXCLUDE:
		result = make([]string, 0, len(primarySet))
		for w := range primarySet {
			if _, exists := secondarySet[w]; !exists {
				result = append(result, w)
			}
		}
	}
	slices.Sort(result)
	return result
}

// MergeBytes converts and merges two raw word list byte slices according to opts.
// Conversion is always applied to both lists before performing the set operation.
func MergeBytes(primaryData, secondaryData []byte, opts MergeOptions) ([]byte, *MergeStats, error) {
	op, conv, enc, err := validateMergeOptions(opts)
	if err != nil {
		return nil, nil, err
	}

	pSlice, pEnc := resolveByteEncoding(primaryData, enc)
	sSlice, sEnc := resolveByteEncoding(secondaryData, enc)

	primarySet, primaryLines, err := collectWordSet(bytes.NewReader(pSlice), pEnc, conv)
	if err != nil {
		return nil, nil, err
	}
	secondarySet, secondaryLines, err := collectWordSet(bytes.NewReader(sSlice), sEnc, conv)
	if err != nil {
		return nil, nil, err
	}

	result := computeSetOpSorted(primarySet, secondarySet, op)

	var out bytes.Buffer
	for _, w := range result {
		out.WriteString(w)
		out.WriteByte('\n')
	}
	outBytes := out.Bytes()

	stats := &MergeStats{
		PrimaryLines:     primaryLines,
		PrimaryEntries:   len(primarySet),
		SecondaryLines:   secondaryLines,
		SecondaryEntries: len(secondarySet),
		OutputEntries:    len(result),
		BytesWritten:     len(outBytes),
	}
	return outBytes, stats, nil
}

func readFileWordSet(path, label string, enc tsd.InputEncoding, conv Conversion) (map[string]struct{}, int, os.FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("failed to open %s word list %q: %w", label, path, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, 0, nil, fmt.Errorf("failed to stat %s word list %q: %w", label, path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, 0, nil, fmt.Errorf("%s word list %q is not a regular file", label, path)
	}
	if info.Size() > maxWordListFileBytes {
		return nil, 0, nil, fmt.Errorf("%s word list %q exceeds maximum size (%d MiB)", label, path, maxWordListFileBytes>>20)
	}

	_, resolvedEnc, err := detectFileEncoding(f, enc)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("failed to read %s word list %q: %w", label, path, err)
	}
	set, lines, err := collectWordSet(io.LimitReader(f, maxWordListFileBytes), resolvedEnc, conv)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("failed to read %s word list %q: %w", label, path, err)
	}
	return set, lines, info, nil
}

func writeLinesBuffered(w io.Writer, lines []string) (int, error) {
	bw := bufio.NewWriterSize(w, 64*1024)
	total := 0
	for _, s := range lines {
		n, err := bw.WriteString(s)
		total += n
		if err != nil {
			return total, err
		}
		if err := bw.WriteByte('\n'); err != nil {
			return total, err
		}
		total++
	}
	if err := bw.Flush(); err != nil {
		return total, err
	}
	return total, nil
}

func writeLinesAtomic(outputPath string, lines []string) (int, error) {
	var written int
	err := fsutil.WritePublicFile(outputPath, func(f *os.File) error {
		var wErr error
		written, wErr = writeLinesBuffered(f, lines)
		return wErr
	})
	return written, err
}

// MergeFiles streams primaryPath and secondaryPath, merges them with opts, and writes the result atomically to outputPath.
func MergeFiles(primaryPath, secondaryPath, outputPath string, opts MergeOptions) (*MergeStats, error) {
	op, conv, enc, err := validateMergeOptions(opts)
	if err != nil {
		return nil, err
	}

	if filepath.Clean(outputPath) == filepath.Clean(primaryPath) || filepath.Clean(outputPath) == filepath.Clean(secondaryPath) {
		return nil, fmt.Errorf("output word list %q cannot overwrite an input word list", outputPath)
	}

	primarySet, primaryLines, pInfo, err := readFileWordSet(primaryPath, "primary", enc, conv)
	if err != nil {
		return nil, err
	}
	secondarySet, secondaryLines, sInfo, err := readFileWordSet(secondaryPath, "secondary", enc, conv)
	if err != nil {
		return nil, err
	}

	if outInfo, statErr := os.Stat(outputPath); statErr == nil {
		if os.SameFile(outInfo, pInfo) || os.SameFile(outInfo, sInfo) {
			return nil, fmt.Errorf("output word list %q refers to the same file as an input word list", outputPath)
		}
	}

	result := computeSetOpSorted(primarySet, secondarySet, op)
	written, err := writeLinesAtomic(outputPath, result)
	if err != nil {
		return nil, fmt.Errorf("failed to write output word list %q: %w", outputPath, err)
	}

	return &MergeStats{
		PrimaryLines:     primaryLines,
		PrimaryEntries:   len(primarySet),
		SecondaryLines:   secondaryLines,
		SecondaryEntries: len(secondarySet),
		OutputEntries:    len(result),
		BytesWritten:     written,
	}, nil
}

// MergeFilesWithParams resolves params, reads the two input files, merges them, and writes the output file.
func MergeFilesWithParams(params MergeParams) (string, *MergeStats, error) {
	primaryPath := strings.TrimSpace(params.PrimaryPath)
	if primaryPath == "" {
		return "", nil, fmt.Errorf("primary word list file is required")
	}
	secondaryPath := strings.TrimSpace(params.SecondaryPath)
	if secondaryPath == "" {
		return "", nil, fmt.Errorf("secondary word list file is required")
	}
	outputPath := strings.TrimSpace(params.OutputPath)
	if outputPath == "" {
		return "", nil, fmt.Errorf("output word list file is required")
	}

	opts, err := ResolveMergeOptions(params)
	if err != nil {
		return "", nil, err
	}

	stats, err := MergeFiles(primaryPath, secondaryPath, outputPath, opts)
	if err != nil {
		return "", nil, err
	}
	return outputPath, stats, nil
}
