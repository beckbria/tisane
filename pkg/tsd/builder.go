package tsd

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// BuildOptions configures compilation of a word/definition source file into a TSD1 dictionary.
type BuildOptions struct {
	// MinLen is the shortest canonical word length to include (1..100).
	MinLen int
	// MaxLen is the longest canonical word length to include (MinLen..100).
	MaxLen int
	// MaxFTSPercent is the maximum percentage (0..100) of lines a word may appear in
	// before its FTS occurrence list is omitted (once > 100 occurrences). 0 disables FTS lists.
	MaxFTSPercent int
	// SortAlphabetically sorts words within each length bucket using TSBuild's collation order.
	// When false, words are emitted in order of first appearance in the source file.
	SortAlphabetically bool
	// Description is an optional human-readable description stored in the 1000-byte TSD1 header.
	Description string
	// Alphabet is the 256-byte character equivalence map (defaults to DefaultTSM() if zero).
	Alphabet TSM
	// Encoding controls UTF-8 vs. Windows-1252 source decoding (EncodingAuto by default).
	Encoding InputEncoding
	// TabSeparatedAnnotations allows unescaped '\t' to act as the '+' annotation delimiter
	// for TSV word lists when no '+' precedes it in an item.
	TabSeparatedAnnotations bool
}

// DefaultBuildOptions returns TSBuild.exe's standard defaults:
// MinLen=1, MaxLen=100, MaxFTSPercent=10, SortAlphabetically=true, Alphabet=DefaultTSM().
func DefaultBuildOptions() BuildOptions {
	return BuildOptions{
		MinLen:             1,
		MaxLen:             MaxWordLengthLimit,
		MaxFTSPercent:      10,
		SortAlphabetically: true,
		Alphabet:           DefaultTSM(),
		Encoding:           EncodingAuto,
	}
}

// BuildParams describes high-level, user-facing dictionary build parameters shared
// between the tisane-build CLI and the Tisane desktop GUI.
type BuildParams struct {
	InputPath               string
	OutputPath              string
	MapPath                 string
	Description             string
	MinLen                  int
	MaxLen                  int
	MaxFTSPercent           int
	SortAlphabetically      bool
	TabSeparatedAnnotations bool
	Encoding                string
}

// DefaultOutputPath derives the default .tsd output path for a given input word list path.
// Returns "" if inputPath is empty or "-".
func DefaultOutputPath(inputPath string) string {
	trimmed := strings.TrimSpace(inputPath)
	if trimmed == "" || trimmed == "-" {
		return ""
	}
	ext := filepath.Ext(trimmed)
	if ext != "" && !strings.EqualFold(ext, ".tsd") {
		return strings.TrimSuffix(trimmed, ext) + ".tsd"
	}
	return trimmed + ".tsd"
}

// ResolveBuildOptions converts user-facing BuildParams into validated BuildOptions,
// parsing the input encoding name and loading any custom .tsm alphabet map file.
func ResolveBuildOptions(params BuildParams) (BuildOptions, error) {
	opts := DefaultBuildOptions()
	opts.MinLen = params.MinLen
	opts.MaxLen = params.MaxLen
	opts.MaxFTSPercent = params.MaxFTSPercent
	opts.SortAlphabetically = params.SortAlphabetically
	opts.Description = params.Description
	opts.TabSeparatedAnnotations = params.TabSeparatedAnnotations

	enc, err := ParseInputEncoding(params.Encoding)
	if err != nil {
		return BuildOptions{}, err
	}
	opts.Encoding = enc

	mapPath := strings.TrimSpace(params.MapPath)
	if mapPath != "" && !strings.EqualFold(mapPath, "default") {
		alphabet, err := LoadTSM(mapPath)
		if err != nil {
			return BuildOptions{}, fmt.Errorf("failed to load alphabet map %q: %w", mapPath, err)
		}
		opts.Alphabet = alphabet
	}
	return opts, nil
}

// BuildFileWithParams resolves params, compiles params.InputPath into a TSD1 dictionary,
// and writes the resulting .tsd file to params.OutputPath (or DefaultOutputPath if empty).
func BuildFileWithParams(params BuildParams) (string, *BuildStats, error) {
	inputPath := strings.TrimSpace(params.InputPath)
	if inputPath == "" {
		return "", nil, fmt.Errorf("tsd: input word list path is required")
	}
	outputPath := strings.TrimSpace(params.OutputPath)
	if outputPath == "" {
		outputPath = DefaultOutputPath(inputPath)
	}
	if outputPath == "" {
		return "", nil, fmt.Errorf("tsd: output dictionary path is required")
	}
	opts, err := ResolveBuildOptions(params)
	if err != nil {
		return "", nil, err
	}
	stats, err := BuildFile(inputPath, outputPath, opts)
	if err != nil {
		return "", nil, err
	}
	return outputPath, stats, nil
}

// BuildStats summarizes the contents of a compiled TSD1 dictionary.
type BuildStats struct {
	InputLines       int
	UniqueWords      int
	MaxWordLength    int
	Pass1Definitions int
	Pass2Definitions int
	FTSOnlyWords     int
	UsedLetters      int
	TotalLetters     int
	BytesWritten     int
}

type wordBuildInfo struct {
	raw      []byte
	firstSeq int
	defLines []int
}

type ftsPosting struct {
	lines  []int
	cutoff bool
}

var rtfSpaceWords = []byte("|bin|par|line|tab|emdash|endash|bullet|lquote|rquote|ldblquote|rdblquote|")

func isASCIILetter(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func isASCIIDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

// StripRTFForFTS reproduces TSBuild.exe's RTF markup stripper (VA 0x4055f0) used when
// tokenizing Pass 2 definitions for Full Text Search.
func StripRTFForFTS(rawDef []byte) []byte {
	if !bytes.HasPrefix(rawDef, []byte(`{\rtf1`)) {
		if len(rawDef) > 0 && rawDef[len(rawDef)-1] == '\n' {
			return rawDef[:len(rawDef)-1]
		}
		return rawDef
	}

	out := make([]byte, 0, len(rawDef))
	n := len(rawDef)
	state := 0 // 0=normal, 1=hex \'XX, 2=\binN skip
	hexRem := 2
	var hexVal byte
	binRem := 0

	for i := 0; i < n; {
		b := rawDef[i]
		if b == '\n' {
			break
		}
		if state == 2 {
			binRem--
			if binRem <= 0 {
				state = 0
			}
			i++
			continue
		}
		if state == 1 {
			isHex := (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
			if !isHex {
				hexRem = 2
				hexVal = 0
				state = 0
				// Fall through to process b normally.
			} else {
				hexVal <<= 4
				switch {
				case b >= '0' && b <= '9':
					hexVal += b - '0'
				case b >= 'a' && b <= 'f':
					hexVal += b - 'a' + 10
				case b >= 'A' && b <= 'F':
					hexVal += b - 'A' + 10
				}
				hexRem--
				if hexRem == 0 {
					if hexVal != 0 {
						out = append(out, hexVal)
					}
					hexRem = 2
					hexVal = 0
					state = 0
				}
				i++
				continue
			}
		}

		if b == '{' || b == '}' {
			i++
			continue
		}
		if b == '\\' {
			i++
			if i >= n {
				break
			}
			nb := rawDef[i]
			switch {
			case nb == '\'':
				state = 1
				i++
			case nb == '*':
				i++
			case nb == '-' || nb == '_' || nb == '~':
				if nb == '~' {
					out = append(out, ' ')
				} else {
					out = append(out, '-')
				}
				i++
			case nb == '\\' || nb == '{' || nb == '}':
				out = append(out, nb)
				i++
			case isASCIILetter(nb):
				startCW := i
				for i < n && isASCIILetter(rawDef[i]) {
					i++
				}
				endCW := i
				negParam := false
				if i < n && rawDef[i] == '-' {
					negParam = true
					i++
				}
				numPos := i
				for i < n && isASCIIDigit(rawDef[i]) {
					i++
				}
				endNum := i
				if endCW-startCW < 12 {
					var needle [16]byte
					needle[0] = '|'
					copy(needle[1:], rawDef[startCW:endCW])
					needleLen := 1 + (endCW - startCW)
					needle[needleLen] = '|'
					needleLen++
					idx := bytes.Index(rtfSpaceWords, needle[:needleLen])
					if idx >= 0 {
						out = append(out, ' ')
						if idx == 0 && !negParam && endNum > numPos {
							val := 0
							for _, d := range rawDef[numPos:endNum] {
								if val <= n {
									val = val*10 + int(d-'0')
								}
							}
							if val > n {
								val = n
							}
							binRem = val
							if binRem > 0 {
								state = 2
							}
						}
					}
				}
				if i < n && rawDef[i] == ' ' {
					i++
				}
			default:
				i++
			}
			continue
		}

		start := i
		for i < n && rawDef[i] != '\\' && rawDef[i] != '{' && rawDef[i] != '}' && rawDef[i] != '\n' {
			i++
		}
		out = append(out, rawDef[start:i]...)
	}
	return out
}

func splitNormalizedLines(data []byte) [][]byte {
	norm := data
	if bytes.IndexByte(norm, '\r') >= 0 {
		norm = bytes.ReplaceAll(norm, []byte("\r\n"), []byte("\n"))
		norm = bytes.ReplaceAll(norm, []byte("\r"), []byte("\n"))
	}
	if len(norm) == 0 {
		return nil
	}
	var lines [][]byte
	for len(norm) > 0 {
		if idx := bytes.IndexByte(norm, '\n'); idx >= 0 {
			lines = append(lines, norm[:idx+1])
			norm = norm[idx+1:]
		} else {
			lines = append(lines, norm)
			break
		}
	}
	return lines
}

func trimASCIISpaces(b []byte) []byte {
	end := len(b)
	for end > 0 && b[end-1] == ' ' {
		end--
	}
	start := 0
	for start < end && b[start] == ' ' {
		start++
	}
	return b[start:end]
}

func appendXOR0xBD(dst, src []byte) []byte {
	for _, b := range src {
		dst = append(dst, b^0xBD)
	}
	return append(dst, 0xBD)
}

func containsLineCanon(haystack, tok []byte) bool {
	// haystack consists of "canon1\ncanon2\n..."
	start := 0
	for i := 0; i < len(haystack); i++ {
		if haystack[i] == '\n' {
			if bytes.Equal(haystack[start:i], tok) {
				return true
			}
			start = i + 1
		}
	}
	return false
}

// writeFileAtomic writes to a random temporary sibling file in outputPath's directory,
// syncs, sets 0o644 permissions, and renames into place so existing files are never
// partially truncated on failure or corrupted underneath concurrent ReadAt readers.
func writeFileAtomic(outputPath string, writeFn func(*os.File) error) error {
	if info, err := os.Lstat(outputPath); err == nil && (info.Mode()&os.ModeCharDevice) != 0 {
		f, openErr := os.OpenFile(outputPath, os.O_WRONLY, 0o644)
		if openErr != nil {
			return openErr
		}
		wErr := writeFn(f)
		cErr := f.Close()
		if wErr != nil {
			return wErr
		}
		return cErr
	}

	dir := filepath.Dir(outputPath)
	if dir == "" {
		dir = "."
	}
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	f, err := os.CreateTemp(dir, ".tisane-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := f.Name()
	keepTmp := false
	defer func() {
		if !keepTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := writeFn(f); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	_ = f.Chmod(0o644)
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, outputPath); err != nil {
		return err
	}
	keepTmp = true
	return nil
}

// BuildFile reads a .txt source file from inputPath, compiles a TSD1 dictionary with opts,
// and writes the resulting .tsd file atomically to outputPath.
func BuildFile(inputPath, outputPath string, opts BuildOptions) (*BuildStats, error) {
	inData, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("read input %q: %w", inputPath, err)
	}
	outBytes, stats, err := BuildBytes(inData, opts)
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(outputPath, func(f *os.File) error {
		_, wErr := f.Write(outBytes)
		return wErr
	}); err != nil {
		return nil, fmt.Errorf("write output %q: %w", outputPath, err)
	}
	return stats, nil
}

// Build reads source lines from r, compiles a TSD1 dictionary, and writes the binary output to w.
func Build(r io.Reader, w io.Writer, opts BuildOptions) (*BuildStats, error) {
	inData, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	outBytes, stats, err := BuildBytes(inData, opts)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(outBytes); err != nil {
		return nil, err
	}
	return stats, nil
}

// BuildBytes compiles raw .txt source data into an in-memory TSD1 byte slice.
func BuildBytes(rawInput []byte, opts BuildOptions) ([]byte, *BuildStats, error) {
	if opts.MinLen == 0 && opts.MaxLen == 0 {
		opts.MinLen = 1
		opts.MaxLen = MaxWordLengthLimit
	}
	if opts.MinLen < 1 || opts.MinLen > MaxWordLengthLimit {
		return nil, nil, fmt.Errorf("tsd: MinLen %d out of range 1..%d", opts.MinLen, MaxWordLengthLimit)
	}
	if opts.MaxLen < opts.MinLen || opts.MaxLen > MaxWordLengthLimit {
		return nil, nil, fmt.Errorf("tsd: MaxLen %d out of range %d..%d", opts.MaxLen, opts.MinLen, MaxWordLengthLimit)
	}
	if opts.MaxFTSPercent < 0 || opts.MaxFTSPercent > 100 {
		return nil, nil, fmt.Errorf("tsd: MaxFTSPercent %d out of range 0..100", opts.MaxFTSPercent)
	}
	tsm := opts.Alphabet
	if tsm == (TSM{}) {
		tsm = DefaultTSM()
	}
	tsm[0x00] = 0
	tsm[0x0A] = 0
	tsm[0x0D] = 0
	for b := 1; b < 256; b++ {
		if tsm[b] == 0x0A || tsm[b] == 0x0D {
			tsm[b] = 0
		}
	}
	for b := 1; b < 256; b++ {
		if m := tsm[b]; m != 0 && tsm[m] != m {
			return nil, nil, fmt.Errorf("tsd: alphabet map is not idempotent at byte 0x%02X -> 0x%02X", b, m)
		}
	}
	if classes := tsm.NumClasses(); classes < 1 || classes >= 254 {
		return nil, nil, fmt.Errorf("tsd: alphabet map defines %d letter classes (must be 1..253)", classes)
	}

	cp1252Data := normalizeBuilderInput(rawInput, opts.Encoding)
	if bytes.IndexByte(cp1252Data, 0) >= 0 {
		cp1252Data = bytes.ReplaceAll(cp1252Data, []byte{0}, nil)
	}
	lines := splitNormalizedLines(cp1252Data)
	totalLines := len(lines)

	var freq [256]uint64
	nextWordSeq := 0
	mapA := make([]map[string]*wordBuildInfo, opts.MaxLen+1)
	for L := opts.MinLen; L <= opts.MaxLen; L++ {
		mapA[L] = make(map[string]*wordBuildInfo)
	}
	mapB := make([]map[string]*ftsPosting, opts.MaxLen+1)
	for L := 1; L <= opts.MaxLen; L++ {
		mapB[L] = make(map[string]*ftsPosting)
	}

	linePass := make([]uint8, totalLines)
	cutoffMult := 0.0
	if opts.MaxFTSPercent > 0 {
		cutoffMult = 100.0 / float64(opts.MaxFTSPercent)
	}

	var dispScratch []byte
	var canonScratch []byte
	var lineHWCanon []byte
	var tokScratch []byte

	// Pass 0: Scan source lines, collect words (MapA), tokenize Pass 2 definitions (MapB), and count byte frequencies.
	for lineIdx, line := range lines {
		if len(line) > 0 && line[0] == '|' {
			linePass[lineIdx] = 0
			continue
		}

		p := 0
		n := len(line)
		validTokens := 0
		seenSemi := false
		headwordsBeforeSemi := 0
		lineHWCanon = lineHWCanon[:0]

		for p < n && line[p] != '\n' {
			tokStart := p
			plus1 := -1
			plus2 := -1
			for p < n && line[p] != '\n' {
				ch := line[p]
				if ch == '\\' && p+1 < n && line[p+1] != '\n' {
					p += 2
					continue
				}
				if ch == '+' || (opts.TabSeparatedAnnotations && ch == '\t' && plus1 < 0) {
					if plus1 < 0 {
						plus1 = p
					} else if ch == '+' && plus2 < 0 {
						plus2 = p
					}
				}
				if ch == ':' || ch == ';' || ch == '|' {
					break
				}
				p++
			}
			var delim byte
			if p < n {
				delim = line[p]
			}
			tokEnd := p
			if plus2 >= 0 {
				tokEnd = plus2
			}

			if tokEnd-tokStart <= 0x7F6 {
				dispScratch = dispScratch[:0]
				canonScratch = canonScratch[:0]
				inAnnot := false
				for q := tokStart; q < tokEnd; {
					if plus1 >= 0 && q == plus1 {
						inAnnot = true
						q++
						if q < tokEnd {
							dispScratch = append(dispScratch, '\n')
						}
						continue
					}
					if line[q] == '\\' && q+1 < tokEnd {
						q++
					}
					b := line[q]
					dispScratch = append(dispScratch, b)
					if !inAnnot {
						if mb := tsm[b]; mb != 0 {
							canonScratch = append(canonScratch, mb)
						}
					}
					q++
				}

				wlen := len(canonScratch)
				if wlen >= opts.MinLen && wlen <= opts.MaxLen {
					validTokens++
					trimmed := trimASCIISpaces(dispScratch)
					key := string(trimmed)
					dmap := mapA[wlen]
					if info, exists := dmap[key]; exists {
						info.defLines = append(info.defLines, lineIdx)
					} else {
						nextWordSeq++
						dmap[key] = &wordBuildInfo{
							raw:      append([]byte(nil), trimmed...),
							firstSeq: nextWordSeq,
							defLines: []int{lineIdx},
						}
					}

					lineHWCanon = append(lineHWCanon, canonScratch...)
					lineHWCanon = append(lineHWCanon, '\n')
					for _, b := range trimmed {
						freq[b]++
					}
					freq[0]++
				}
			}

			if delim == ';' && !seenSemi {
				seenSemi = true
				headwordsBeforeSemi = validTokens
			}
			if delim == '|' {
				if validTokens == 0 {
					linePass[lineIdx] = 0
				} else if seenSemi && headwordsBeforeSemi == 0 {
					linePass[lineIdx] = 1
				} else {
					linePass[lineIdx] = 2
					plainDef := StripRTFForFTS(line[p+1:])
					tokScratch = tokScratch[:0]
					for k := 0; k <= len(plainDef); k++ {
						var mb byte
						if k < len(plainDef) {
							mb = tsm[plainDef[k]]
						}
						if mb != 0 {
							if freq[mb] == 0 {
								freq[mb] = 1
							}
							tokScratch = append(tokScratch, mb)
						} else if len(tokScratch) > 0 {
							tlen := len(tokScratch)
							if tlen <= opts.MaxLen && !containsLineCanon(lineHWCanon, tokScratch) {
								tkey := string(tokScratch)
								bmap := mapB[tlen]
								if post, exists := bmap[tkey]; !exists {
									if opts.MaxFTSPercent > 0 {
										bmap[tkey] = &ftsPosting{lines: []int{lineIdx}}
									} else {
										bmap[tkey] = &ftsPosting{cutoff: true}
									}
								} else if !post.cutoff {
									if len(post.lines) > 100 && float64(len(post.lines))*cutoffMult > float64(lineIdx) {
										post.lines = nil
										post.cutoff = true
									} else if post.lines[len(post.lines)-1] != lineIdx {
										post.lines = append(post.lines, lineIdx)
									}
								}
							}
							tokScratch = tokScratch[:0]
						}
					}
				}
				break
			} else if delim == ':' || delim == ';' {
				p++
			} else {
				linePass[lineIdx] = 0
				break
			}
		}
	}

	// Build Huffman codes and alphabet letter indices.
	huffLen, huffCode := buildHuffmanCodes(&freq)

	var classFreq [256]uint64
	for b := 0; b < 256; b++ {
		if tsm[b] != 0 {
			classFreq[tsm[b]] += freq[b]
		}
	}

	var letterIndex [256]int16
	for b := range letterIndex {
		letterIndex[b] = -1
	}
	numUsedLetters := 0
	for b := 1; b < 256; b++ {
		if tsm[b] == byte(b) && classFreq[b] > 0 {
			letterIndex[b] = int16(numUsedLetters)
			numUsedLetters++
		}
	}
	numTotalLetters := numUsedLetters
	for b := 1; b < 256; b++ {
		if tsm[b] == byte(b) && classFreq[b] == 0 {
			letterIndex[b] = int16(numTotalLetters)
			numTotalLetters++
		}
	}
	for b := 1; b < 256; b++ {
		if tsm[b] != 0 && tsm[b] != byte(b) {
			letterIndex[b] = letterIndex[tsm[b]]
		}
	}

	var mostFreqChar [256]byte
	for b := 0; b < 256; b++ {
		li := letterIndex[b]
		if li >= 0 {
			bestC := byte(b)
			var bestF int64 = -1
			for x := 0; x < 256; x++ {
				if letterIndex[x] == li && int64(freq[x]) > bestF {
					bestF = int64(freq[x])
					bestC = byte(x)
				}
			}
			mostFreqChar[b] = bestC
		}
	}

	// Determine numLengths and sort each length bucket.
	maxNonEmptyL := 0
	totalUniqueWords := 0
	for L := opts.MinLen; L <= opts.MaxLen; L++ {
		cnt := len(mapA[L])
		if cnt > 0 {
			maxNonEmptyL = L
			totalUniqueWords += cnt
		}
	}
	numLengths := 0
	if maxNonEmptyL > 0 {
		numLengths = maxNonEmptyL + 1
	}
	wordCounts := make([]uint32, numLengths)
	sortedWords := make([][]*wordBuildInfo, numLengths)
	for L := 1; L < numLengths; L++ {
		if L < opts.MinLen || len(mapA[L]) == 0 {
			continue
		}
		dmap := mapA[L]
		wordCounts[L] = uint32(len(dmap))
		items := make([]*wordBuildInfo, 0, len(dmap))
		for _, info := range dmap {
			items = append(items, info)
		}
		if opts.SortAlphabetically {
			sort.Slice(items, func(i, j int) bool {
				return CompareWordsAlpha(items[i].raw, items[j].raw) < 0
			})
		} else {
			sort.Slice(items, func(i, j int) bool {
				return items[i].firstSeq < items[j].firstSeq
			})
		}
		sortedWords[L] = items
	}

	// Compute calibration matrix.
	numCalRows := 0
	if numLengths > 0 {
		numCalRows = (numLengths - 1) * numLengths / 2
	}
	calBytes := make([]byte, 0, numCalRows*numUsedLetters)
	for L := 1; L < numLengths; L++ {
		wc := int(wordCounts[L])
		posCounts := make([]int, L*numUsedLetters)
		for _, item := range sortedWords[L] {
			pIdx := 0
			for _, b := range item.raw {
				if b == '\n' {
					break
				}
				if li := int(letterIndex[b]); li >= 0 {
					posCounts[pIdx*numUsedLetters+li]++
					pIdx++
				}
			}
		}
		for pIdx := 0; pIdx < L; pIdx++ {
			row := posCounts[pIdx*numUsedLetters : (pIdx+1)*numUsedLetters]
			K := 0
			for _, c := range row {
				if c > 0 {
					K++
				}
			}
			for li := 0; li < numUsedLetters; li++ {
				c := row[li]
				if c == 0 || wc == 0 {
					calBytes = append(calBytes, 0x80) // -128
				} else {
					val := math.Log2((float64(c) * float64(K)) / float64(wc))
					var iv int
					if val >= 0 {
						iv = int(val + 0.5)
					} else {
						iv = int(val - 0.5)
					}
					calBytes = append(calBytes, byte(int8(iv)))
				}
			}
		}
	}

	headerSize := uint32(4 + 2 + 12 + 20*numLengths + 1000 + 4 + 2048 + 2 + len(calBytes))

	// Emit Pass 1 (alias-only, 256-entry dedup cache) and Pass 2 (headword) definitions.
	var defsBuf []byte
	defPassOffsets := [3]uint32{headerSize, 0, 0}
	lineDefOffset := make([]uint32, totalLines)
	pass1Cache := make(map[string]uint32, 256)
	pass1Count := 0
	pass2Count := 0

	type hwQualPair struct {
		hw   []byte
		qual []byte
	}
	var hws []hwQualPair

	for passNum := uint8(1); passNum <= 2; passNum++ {
		if passNum == 2 {
			defPassOffsets[1] = headerSize + uint32(len(defsBuf))
		}
		for lineIdx, line := range lines {
			if linePass[lineIdx] != passNum {
				continue
			}
			p := 0
			n := len(line)
			hws = hws[:0]
			seenSemi := false

			for p < n && line[p] != '\n' {
				tokStart := p
				plus1 := -1
				plus2 := -1
				for p < n && line[p] != '\n' {
					ch := line[p]
					if ch == '\\' && p+1 < n && line[p+1] != '\n' {
						p += 2
						continue
					}
					if ch == '+' || (opts.TabSeparatedAnnotations && ch == '\t' && plus1 < 0) {
						if plus1 < 0 {
							plus1 = p
						} else if ch == '+' && plus2 < 0 {
							plus2 = p
						}
					}
					if ch == ':' || ch == ';' || ch == '|' {
						break
					}
					p++
				}
				var delim byte
				if p < n {
					delim = line[p]
				}
				tokEnd := p
				if plus2 >= 0 {
					tokEnd = plus2
				}

				if !seenSemi && tokEnd-tokStart <= 0x7F6 {
					wEnd := p
					if plus1 >= 0 {
						wEnd = plus1
					}
					var hwBytes []byte
					canonLen := 0
					for q := tokStart; q < wEnd; {
						if line[q] == '\\' && q+1 < wEnd {
							q++
						}
						b := line[q]
						hwBytes = append(hwBytes, b)
						if tsm[b] != 0 {
							canonLen++
						}
						q++
					}
					var qualBytes []byte
					if plus2 >= 0 {
						for q := plus2 + 1; q < p; {
							if line[q] == '\\' && q+1 < p {
								q++
							}
							qualBytes = append(qualBytes, line[q])
							q++
						}
					}
					if canonLen >= opts.MinLen && canonLen <= opts.MaxLen {
						hws = append(hws, hwQualPair{
							hw:   append([]byte(nil), trimASCIISpaces(hwBytes)...),
							qual: qualBytes,
						})
					}
				}

				if delim == ';' {
					seenSemi = true
				}
				if delim == '|' {
					defText := line[p+1:]
					if len(defText) > 0 && defText[len(defText)-1] == '\n' {
						defText = defText[:len(defText)-1]
					}
					if passNum == 1 {
						key := string(defText)
						if cachedOff, ok := pass1Cache[key]; ok {
							lineDefOffset[lineIdx] = cachedOff
						} else {
							recOff := headerSize + uint32(len(defsBuf))
							defsBuf = append(defsBuf, 0)
							defsBuf = appendXOR0xBD(defsBuf, defText)
							lineDefOffset[lineIdx] = recOff
							pass1Count++
							if len(pass1Cache) >= 256 {
								var oldestKey string
								var oldestOff uint32 = math.MaxUint32
								for ck, co := range pass1Cache {
									if co < oldestOff {
										oldestOff = co
										oldestKey = ck
									}
								}
								delete(pass1Cache, oldestKey)
							}
							pass1Cache[key] = recOff
						}
					} else {
						recOff := headerSize + uint32(len(defsBuf))
						nh := len(hws)
						if nh > 255 {
							nh = 255
						}
						defsBuf = append(defsBuf, byte(nh))
						for i := 0; i < nh; i++ {
							defsBuf = appendXOR0xBD(defsBuf, hws[i].hw)
							defsBuf = appendXOR0xBD(defsBuf, hws[i].qual)
						}
						defsBuf = appendXOR0xBD(defsBuf, defText)
						lineDefOffset[lineIdx] = recOff
						pass2Count++
					}
					break
				} else if delim == ':' || delim == ';' {
					p++
				} else {
					break
				}
			}
		}
	}
	if uint64(headerSize)+uint64(len(defsBuf)) >= uint64(FTSOverflowSentinel) {
		return nil, nil, fmt.Errorf("tsd: definitions section exceeds 2 GiB TSD1 offset limit")
	}
	defPassOffsets[2] = headerSize + uint32(len(defsBuf))

	// Precompute bit-reversed Huffman codewords for LSB-first stream writing.
	var revCode [256]uint32
	for b := 0; b < 256; b++ {
		if huffLen[b] > 0 {
			revCode[b] = reverseBits(huffCode[b], huffLen[b])
		}
	}

	bodyBuf := defsBuf
	occIndexOffsets := make([]uint32, numLengths)
	defLinkOffsets := make([]uint32, numLengths)
	defLinkCounts := make([]uint32, numLengths)
	wordStreamOffsets := make([]uint32, numLengths)
	totalFTSOnlyWords := 0

	var bw bitStreamWriter
	for L := 1; L < numLengths; L++ {
		if wordCounts[L] == 0 {
			continue
		}
		bmap := mapB[L]
		items := sortedWords[L]

		// Step 1a: Occurrence lists for Sub-stream 1 dictionary words.
		dictOccPtrs := make([]uint32, len(items))
		consumedBMap := make(map[string]struct{}, len(items))
		for idxW, item := range items {
			canonScratch = canonScratch[:0]
			for _, b := range item.raw {
				if b == '\n' {
					break
				}
				if mb := tsm[b]; mb != 0 {
					canonScratch = append(canonScratch, mb)
				}
			}
			ckey := string(canonScratch)
			var occPtr uint32
			if post, exists := bmap[ckey]; exists {
				if _, already := consumedBMap[ckey]; !already {
					consumedBMap[ckey] = struct{}{}
					if post.cutoff || (len(post.lines) > 100 && float64(len(post.lines))*cutoffMult > float64(totalLines)) {
						occPtr = FTSOverflowSentinel
					} else {
						var validOffs []uint32
						for _, li := range post.lines {
							if doff := lineDefOffset[li]; doff > 0 {
								validOffs = append(validOffs, doff)
							}
						}
						if len(validOffs) > 0 {
							occPtr = headerSize + uint32(len(bodyBuf))
							for i, offVal := range validOffs {
								if i == len(validOffs)-1 {
									offVal |= 0x80000000
								}
								bodyBuf = binary.LittleEndian.AppendUint32(bodyBuf, offVal)
							}
						}
					}
				}
			}
			dictOccPtrs[idxW] = occPtr
		}

		// Step 1b: Remaining FTS-only words sorted unsigned-lexicographically by canonical bytes.
		remKeys := make([]string, 0, len(bmap))
		for k := range bmap {
			if _, used := consumedBMap[k]; !used {
				remKeys = append(remKeys, k)
			}
		}
		sort.Strings(remKeys)
		totalFTSOnlyWords += len(remKeys)

		ftsOccPtrs := make([]uint32, len(remKeys))
		for i, k := range remKeys {
			post := bmap[k]
			if post.cutoff || (len(post.lines) > 100 && float64(len(post.lines))*cutoffMult > float64(totalLines)) {
				ftsOccPtrs[i] = FTSOverflowSentinel
				continue
			}
			var validOffs []uint32
			for _, li := range post.lines {
				if doff := lineDefOffset[li]; doff > 0 {
					validOffs = append(validOffs, doff)
				}
			}
			if len(validOffs) > 0 {
				occPtr := headerSize + uint32(len(bodyBuf))
				for j, offVal := range validOffs {
					if j == len(validOffs)-1 {
						offVal |= 0x80000000
					}
					bodyBuf = binary.LittleEndian.AppendUint32(bodyBuf, offVal)
				}
				ftsOccPtrs[i] = occPtr
			} else {
				ftsOccPtrs[i] = FTSOverflowSentinel
			}
		}

		// Step 2: FTS-only occurrence pointer table (occ_index_offsets[L]).
		if len(remKeys) > 0 {
			occIndexOffsets[L] = headerSize + uint32(len(bodyBuf))
			for _, ptr := range ftsOccPtrs {
				bodyBuf = binary.LittleEndian.AppendUint32(bodyBuf, ptr)
			}
		}

		// Step 3: Huffman-compressed word bitstream (Sub-stream 1 + Sub-stream 2).
		wordStreamOffsets[L] = headerSize + uint32(len(bodyBuf))
		bw.dst = bodyBuf
		bw.acc = 0
		bw.bitCnt = 0

		putStr := func(bs []byte) {
			for _, b := range bs {
				bw.putBits(huffLen[b], revCode[b])
			}
			bw.putBits(huffLen[0], revCode[0])
		}

		var defLinks []uint32
		var wordGroup []uint32
		for idxW, item := range items {
			putStr(item.raw)
			wordGroup = wordGroup[:0]
			for _, li := range item.defLines {
				doff := lineDefOffset[li]
				if doff > 0 && (len(wordGroup) == 0 || wordGroup[len(wordGroup)-1] != doff) {
					wordGroup = append(wordGroup, doff)
				}
			}
			if optr := dictOccPtrs[idxW]; optr != 0 {
				wordGroup = append(wordGroup, optr)
			}
			if len(wordGroup) > 0 {
				bw.putBits(1, 1)
				wordGroup[len(wordGroup)-1] |= 0x80000000
				defLinks = append(defLinks, wordGroup...)
			} else {
				bw.putBits(1, 0)
			}
		}

		// End of Sub-stream 1
		bw.putBits(huffLen[0], revCode[0])

		// Sub-stream 2 (FTS-only words mapped via mostFreqChar)
		for _, k := range remKeys {
			for i := 0; i < len(k); i++ {
				c := mostFreqChar[k[i]]
				bw.putBits(huffLen[c], revCode[c])
			}
			bw.putBits(huffLen[0], revCode[0])
		}
		// End of Sub-stream 2
		bw.putBits(huffLen[0], revCode[0])
		bw.flush()
		bodyBuf = bw.dst

		// Step 4: Definition & Occurrence Link Table (def_link_offsets[L]).
		if len(defLinks) > 0 {
			defLinkOffsets[L] = headerSize + uint32(len(bodyBuf))
			defLinkCounts[L] = uint32(len(defLinks))
			for _, x := range defLinks {
				bodyBuf = binary.LittleEndian.AppendUint32(bodyBuf, x)
			}
		}
	}

	if uint64(headerSize)+uint64(len(bodyBuf)) >= uint64(FTSOverflowSentinel) {
		return nil, nil, fmt.Errorf("tsd: compiled dictionary exceeds 2 GiB TSD1 offset limit")
	}

	// Backward-fill interior zero word_stream_offsets[1..numLengths-2].
	for L := numLengths - 2; L >= 1; L-- {
		if wordStreamOffsets[L] == 0 {
			wordStreamOffsets[L] = wordStreamOffsets[L+1]
		}
	}

	// Assemble final TSD1 file.
	descCP, _ := EncodeCP1252(opts.Description)
	if len(descCP) > 999 {
		descCP = descCP[:999]
	}
	var descPadded [1000]byte
	copy(descPadded[:], descCP)

	out := make([]byte, 0, int(headerSize)+len(bodyBuf))
	out = append(out, "TSD1"...)
	out = binary.LittleEndian.AppendUint16(out, uint16(numLengths))
	for i := 0; i < 3; i++ {
		out = binary.LittleEndian.AppendUint32(out, defPassOffsets[i])
	}
	appendU32Slice := func(s []uint32) {
		for _, v := range s {
			out = binary.LittleEndian.AppendUint32(out, v)
		}
	}
	appendU32Slice(occIndexOffsets)
	appendU32Slice(defLinkOffsets)
	appendU32Slice(defLinkCounts)
	appendU32Slice(wordStreamOffsets)
	appendU32Slice(wordCounts)
	out = append(out, descPadded[:]...)
	out = binary.LittleEndian.AppendUint16(out, uint16(int16(numUsedLetters)))
	out = binary.LittleEndian.AppendUint16(out, uint16(int16(numTotalLetters)))
	for b := 0; b < 256; b++ {
		out = append(out, tsm[b], huffLen[b])
		out = binary.LittleEndian.AppendUint16(out, uint16(letterIndex[b]))
		out = binary.LittleEndian.AppendUint32(out, huffCode[b])
	}
	out = binary.LittleEndian.AppendUint16(out, uint16(numCalRows))
	out = append(out, calBytes...)
	out = append(out, bodyBuf...)

	stats := &BuildStats{
		InputLines:       totalLines,
		UniqueWords:      totalUniqueWords,
		MaxWordLength:    maxNonEmptyL,
		Pass1Definitions: pass1Count,
		Pass2Definitions: pass2Count,
		FTSOnlyWords:     totalFTSOnlyWords,
		UsedLetters:      numUsedLetters,
		TotalLetters:     numTotalLetters,
		BytesWritten:     len(out),
	}
	return out, stats, nil
}
