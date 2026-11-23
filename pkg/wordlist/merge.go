// Package wordlist provides utilities for converting and merging plain-text word lists.
package wordlist

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"tisane/pkg/tsd"
)

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
		ascii := ToASCII(s)
		var buf []byte
		for i := 0; i < len(ascii); i++ {
			c := ascii[i]
			if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
				buf = append(buf, c)
			}
		}
		return string(buf)
	case ConvertASCIILower:
		ascii := ToASCII(s)
		var buf []byte
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
		ascii := ToASCII(s)
		var buf []byte
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

// decodeText decodes raw file bytes into a UTF-8 string according to enc.
func decodeText(data []byte, enc tsd.InputEncoding) string {
	switch enc {
	case tsd.EncodingCP1252:
		return tsd.DecodeCP1252(data)
	case tsd.EncodingUTF8:
		return string(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}))
	default: // tsd.EncodingAuto
		if bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
			trimmed := data[3:]
			if utf8.Valid(trimmed) {
				return string(trimmed)
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
			return string(data)
		}
		return tsd.DecodeCP1252(data)
	}
}

// parseAndConvertEntries splits text into lines, converts each non-empty line,
// and returns the deduplicated set of non-empty converted entries along with the raw non-empty line count.
func parseAndConvertEntries(text string, conv Conversion) (set map[string]struct{}, lines int) {
	set = make(map[string]struct{})
	for len(text) > 0 {
		var line string
		if idx := strings.IndexByte(text, '\n'); idx >= 0 {
			line = text[:idx]
			text = text[idx+1:]
		} else {
			line = text
			text = ""
		}
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		lines++
		converted := ConvertEntry(line, conv)
		if converted == "" {
			continue
		}
		set[converted] = struct{}{}
	}
	return set, lines
}

// MergeBytes converts and merges two raw word list byte slices according to opts.
// Conversion is always applied to both lists before performing the set operation.
func MergeBytes(primaryData, secondaryData []byte, opts MergeOptions) ([]byte, *MergeStats, error) {
	op := opts.Operation
	if op == "" {
		op = OpOR
	}
	switch op {
	case OpOR, OpAND, OpEXCLUDE:
	default:
		return nil, nil, fmt.Errorf("wordlist: unsupported operation %q", op)
	}

	conv := opts.Conversion
	if conv == "" {
		conv = ConvertNone
	}
	switch conv {
	case ConvertNone, ConvertASCII, ConvertASCIILetters, ConvertASCIILower, ConvertASCIIUpper:
	default:
		return nil, nil, fmt.Errorf("wordlist: unsupported conversion %q", conv)
	}

	primaryText := decodeText(primaryData, opts.Encoding)
	secondaryText := decodeText(secondaryData, opts.Encoding)

	primarySet, primaryLines := parseAndConvertEntries(primaryText, conv)
	secondarySet, secondaryLines := parseAndConvertEntries(secondaryText, conv)

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
		// Iterate over the smaller set for efficiency.
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

// MergeFiles reads primaryPath and secondaryPath, merges them with opts, and writes the result to outputPath.
func MergeFiles(primaryPath, secondaryPath, outputPath string, opts MergeOptions) (*MergeStats, error) {
	primaryData, err := os.ReadFile(primaryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read primary word list %q: %w", primaryPath, err)
	}
	secondaryData, err := os.ReadFile(secondaryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read secondary word list %q: %w", secondaryPath, err)
	}

	outBytes, stats, err := MergeBytes(primaryData, secondaryData, opts)
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(outputPath, outBytes, 0o644); err != nil {
		return nil, fmt.Errorf("failed to write output word list %q: %w", outputPath, err)
	}
	return stats, nil
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
