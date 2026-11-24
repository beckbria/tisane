package tsd

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"
)

// FTSOverflowSentinel is stored in occurrence pointers and link tables when a word's
// definition frequency exceeded TSBuild's max_fts_pct indexing threshold.
const FTSOverflowSentinel uint32 = 0x7FFFFFFF

const maxDefinitionRecordBytes = 64 << 20 // 64 MiB safety cap per single definition record

var (
	// ErrNoDefinitions indicates an operation requiring TSD1 definitions was called on a dictionary without them.
	ErrNoDefinitions = errors.New("tsd: dictionary does not contain definitions")
	// ErrInvalidOffset indicates an out-of-range file offset in a link, FTS, or definition lookup.
	ErrInvalidOffset = errors.New("tsd: offset out of valid section bounds")
	// ErrFTSOverflow indicates a word's full-text occurrence list was omitted due to high frequency (0x7FFFFFFF).
	ErrFTSOverflow = errors.New("tsd: word occurrences exceeded FTS indexing threshold")
)

// LinkResolution describes the decoded entries of a Sub-stream 1 word's link group in Block 4.
type LinkResolution struct {
	DirectOffsets []uint32 // File offsets in [def_pass_offsets[0], def_pass_offsets[2])
	FTSListOffset uint32   // Indirect file offset >= def_pass_offsets[2], FTSOverflowSentinel, or 0 if none
	FTSOverflow   bool     // True when FTSListOffset == FTSOverflowSentinel
}

// LinkTable holds all Block 4 uint32 link entries and group start indices for a single word length L,
// enabling O(1) resolution when looking up many linked words in the same length bucket.
type LinkTable struct {
	dict        *Dictionary
	raw         []uint32
	groupStarts []int // len == NumGroups + 1 (last element == len(raw))
}

// NumGroups returns the number of 0x80000000-terminated link groups in the table.
func (lt *LinkTable) NumGroups() int {
	if len(lt.groupStarts) == 0 {
		return 0
	}
	return len(lt.groupStarts) - 1
}

// Resolve decodes the k-th link group in O(group_len) time.
func (lt *LinkTable) Resolve(groupIndex int) (LinkResolution, error) {
	if groupIndex < 0 || groupIndex >= lt.NumGroups() {
		return LinkResolution{}, fmt.Errorf("%w: link group %d out of range [0, %d)",
			ErrInvalidOffset, groupIndex, lt.NumGroups())
	}
	slice := lt.raw[lt.groupStarts[groupIndex]:lt.groupStarts[groupIndex+1]]
	return lt.dict.classifyLinkGroupSlice(slice)
}

// HasDirectDefinition reports in O(1) time whether link group groupIndex contains at least one
// direct Pass 1 or Pass 2 definition offset (as opposed to only FTS body mentions).
func (lt *LinkTable) HasDirectDefinition(groupIndex int) bool {
	if lt == nil || lt.dict == nil || groupIndex < 0 || groupIndex >= lt.NumGroups() {
		return false
	}
	slice := lt.raw[lt.groupStarts[groupIndex]:lt.groupStarts[groupIndex+1]]
	if len(slice) == 0 {
		return false
	}
	if len(slice) > 1 {
		return true
	}
	lastVal := slice[0] & 0x7FFFFFFF
	return lastVal >= lt.dict.defPassOffsets[0] && lastVal < lt.dict.defPassOffsets[2]
}

func (d *Dictionary) classifyLinkGroupSlice(group []uint32) (LinkResolution, error) {
	if len(group) == 0 {
		return LinkResolution{}, fmt.Errorf("%w: empty link group", ErrInvalidOffset)
	}
	defStart := d.defPassOffsets[0]
	defEnd := d.defPassOffsets[2]

	res := LinkResolution{
		DirectOffsets: make([]uint32, 0, len(group)),
	}
	for i := 0; i < len(group)-1; i++ {
		off := group[i]
		if off < defStart || off >= defEnd {
			return LinkResolution{}, fmt.Errorf("%w: non-final link entry 0x%X outside definitions [%d, %d)",
				ErrInvalidOffset, off, defStart, defEnd)
		}
		res.DirectOffsets = append(res.DirectOffsets, off)
	}

	lastVal := group[len(group)-1] & 0x7FFFFFFF
	switch {
	case lastVal >= defStart && lastVal < defEnd:
		res.DirectOffsets = append(res.DirectOffsets, lastVal)
	case lastVal == FTSOverflowSentinel:
		res.FTSListOffset = FTSOverflowSentinel
		res.FTSOverflow = true
	case lastVal >= defEnd && int64(lastVal) < d.size:
		res.FTSListOffset = lastVal
	default:
		return LinkResolution{}, fmt.Errorf("%w: final link entry 0x%X invalid", ErrInvalidOffset, lastVal)
	}
	return res, nil
}

// ReadLinkTable loads and indexes all link groups for word length L.
// If length L has no link table, an empty LinkTable (NumGroups() == 0) is returned.
func (d *Dictionary) ReadLinkTable(length int) (*LinkTable, error) {
	if d.version != VersionTSD1 || length <= 0 || length >= d.numLengths || d.wordCounts[length] == 0 {
		return &LinkTable{dict: d}, nil
	}
	cnt64 := uint64(d.defLinkCounts[length])
	if cnt64 == 0 {
		return &LinkTable{dict: d}, nil
	}
	off := int64(d.defLinkOffsets[length])
	if cnt64 > uint64(math.MaxInt/4) || off <= 0 || off+int64(cnt64*4) > d.size {
		return nil, fmt.Errorf("%w: invalid link table bounds for length %d", ErrInvalidOffset, length)
	}
	cnt := int(cnt64)
	byteLen := cnt * 4
	rawBytes := make([]byte, byteLen)
	if _, err := io.ReadFull(io.NewSectionReader(d.r, off, int64(byteLen)), rawBytes); err != nil {
		return nil, err
	}

	raw := make([]uint32, cnt)
	starts := make([]int, 0, cnt/2+2)
	starts = append(starts, 0)
	for i := 0; i < cnt; i++ {
		v := binary.LittleEndian.Uint32(rawBytes[i*4 : i*4+4])
		raw[i] = v
		if v&0x80000000 != 0 {
			starts = append(starts, i+1)
		}
	}
	if starts[len(starts)-1] != cnt {
		return nil, fmt.Errorf("%w: link table for length %d missing final 0x80000000 terminator", ErrInvalidOffset, length)
	}
	return &LinkTable{
		dict:        d,
		raw:         raw,
		groupStarts: starts,
	}, nil
}

// ResolveLinkGroup resolves a single link group index for word length L without keeping the whole table in memory.
func (d *Dictionary) ResolveLinkGroup(length int, linkGroupIndex int) (LinkResolution, error) {
	if d.version != VersionTSD1 {
		return LinkResolution{}, ErrNoDefinitions
	}
	if length <= 0 || length >= d.numLengths || linkGroupIndex < 0 || d.wordCounts[length] == 0 {
		return LinkResolution{}, ErrInvalidOffset
	}
	totalEntries := uint64(d.defLinkCounts[length])
	if totalEntries == 0 {
		return LinkResolution{}, ErrInvalidOffset
	}

	off := int64(d.defLinkOffsets[length])
	endOff := off + int64(totalEntries)*4
	if off <= 0 || endOff > d.size {
		return LinkResolution{}, ErrInvalidOffset
	}
	const chunkBytes = 4096
	var chunk [chunkBytes]byte

	curGroup := 0
	var groupBuf []uint32
	for off < endOff {
		toRead := chunkBytes
		if rem := endOff - off; rem < int64(toRead) {
			toRead = int(rem)
		}
		if _, err := io.ReadFull(io.NewSectionReader(d.r, off, int64(toRead)), chunk[:toRead]); err != nil {
			return LinkResolution{}, err
		}
		off += int64(toRead)

		for pos := 0; pos < toRead; pos += 4 {
			v := binary.LittleEndian.Uint32(chunk[pos : pos+4])
			if curGroup == linkGroupIndex {
				groupBuf = append(groupBuf, v)
				if v&0x80000000 != 0 {
					return d.classifyLinkGroupSlice(groupBuf)
				}
			} else if v&0x80000000 != 0 {
				curGroup++
			}
		}
	}
	return LinkResolution{}, fmt.Errorf("%w: link group %d not found in length %d", ErrInvalidOffset, linkGroupIndex, length)
}

// ResolveAllDefinitionOffsets resolves both direct definition offsets and indirect FTS occurrence
// offsets for the given (length, linkGroupIndex).
func (d *Dictionary) ResolveAllDefinitionOffsets(length int, linkGroupIndex int) (direct []uint32, fts []uint32, ftsOverflow bool, err error) {
	res, err := d.ResolveLinkGroup(length, linkGroupIndex)
	if err != nil {
		return nil, nil, false, err
	}
	if res.FTSOverflow {
		return res.DirectOffsets, nil, true, nil
	}
	if res.FTSListOffset != 0 {
		fts, err = d.ReadFTSOccurrences(res.FTSListOffset)
		if err != nil {
			return nil, nil, false, err
		}
	}
	return res.DirectOffsets, fts, false, nil
}

// ReadFTSIndexPointers reads the Sub-stream 2 occurrence pointer table at occ_index_offsets[length].
// Each entry is either a file offset >= def_pass_offsets[2] or FTSOverflowSentinel (0x7FFFFFFF).
func (d *Dictionary) ReadFTSIndexPointers(length int) ([]uint32, error) {
	count := d.FTSOnlyWordCount(length)
	if count <= 0 {
		return nil, nil
	}
	if uint64(count) > uint64(math.MaxInt/4) {
		return nil, fmt.Errorf("%w: FTS pointer count %d too large", ErrInvalidOffset, count)
	}
	off := int64(d.occIndexOffsets[length])
	if off <= 0 || off+int64(count)*4 > d.size {
		return nil, fmt.Errorf("%w: FTS pointer table out of bounds", ErrInvalidOffset)
	}
	rawBytes := make([]byte, count*4)
	if _, err := io.ReadFull(io.NewSectionReader(d.r, off, int64(len(rawBytes))), rawBytes); err != nil {
		return nil, err
	}
	out := make([]uint32, count)
	for i := 0; i < count; i++ {
		out[i] = binary.LittleEndian.Uint32(rawBytes[i*4 : i*4+4])
	}
	return out, nil
}

// ReadFTSOccurrences reads a 0x80000000-terminated array of Pass 2 definition file offsets starting at offset.
func (d *Dictionary) ReadFTSOccurrences(offset uint32) ([]uint32, error) {
	if d.version != VersionTSD1 {
		return nil, ErrNoDefinitions
	}
	if offset == FTSOverflowSentinel {
		return nil, ErrFTSOverflow
	}
	if offset < d.defPassOffsets[2] || int64(offset)+4 > d.size {
		return nil, fmt.Errorf("%w: FTS occurrence offset %d outside [%d, %d)",
			ErrInvalidOffset, offset, d.defPassOffsets[2], d.size)
	}

	defP2Start := d.defPassOffsets[1]
	defP2End := d.defPassOffsets[2]

	pos := int64(offset)
	var chunk [1024]byte
	out := make([]uint32, 0, 8)
	for pos < d.size {
		toRead := len(chunk)
		if rem := d.size - pos; rem < int64(toRead) {
			r := int(rem)
			toRead = r - (r % 4)
			if toRead == 0 {
				break
			}
		}
		n, err := d.r.ReadAt(chunk[:toRead], pos)
		n -= n % 4
		if n == 0 {
			if err != nil {
				return nil, err
			}
			break
		}
		pos += int64(n)
		for i := 0; i < n; i += 4 {
			raw := binary.LittleEndian.Uint32(chunk[i : i+4])
			defOff := raw & 0x7FFFFFFF
			if defOff < defP2Start || defOff >= defP2End {
				return nil, fmt.Errorf("%w: FTS occurrence entry %d outside Pass 2 [%d, %d)",
					ErrInvalidOffset, defOff, defP2Start, defP2End)
			}
			out = append(out, defOff)
			if raw&0x80000000 != 0 {
				return out, nil
			}
		}
	}
	return nil, fmt.Errorf("%w: unterminated FTS occurrence list at %d", ErrInvalidOffset, offset)
}

// HeadwordQualifier holds a headword and its optional sense/POS qualifier from a TSD1 definition record.
type HeadwordQualifier struct {
	Headword     string // UTF-8 decoded headword
	Qualifier    string // UTF-8 decoded qualifier (leading/trailing spaces preserved)
	RawHeadword  []byte // Un-XORed raw CP1252 headword bytes
	RawQualifier []byte // Un-XORed raw CP1252 qualifier bytes
}

// DefinitionRecord represents a decoded XOR-0xBD definition entry from the Definitions section.
type DefinitionRecord struct {
	Offset    uint32              // Exact byte offset in the .tsd file
	Pass      int                 // 1 (alias-only stub) or 2 (headword definition)
	Headwords []HeadwordQualifier // Empty for Pass 1 records; 1..255 entries for Pass 2 records
	Body      string              // Definition text decoded from CP1252 to UTF-8
	RawBody   []byte              // Un-XORed raw definition bytes (plain text or {\rtf1...})
	IsRTF     bool                // True if RawBody starts with "{\rtf1"
}

func decodeXORString(src []byte) (decoded []byte, consumed int, ok bool) {
	end := bytes.IndexByte(src, 0xBD)
	if end < 0 {
		return nil, 0, false
	}
	out := make([]byte, end)
	for i := 0; i < end; i++ {
		out[i] = src[i] ^ 0xBD
	}
	return out, end + 1, true
}

func parseDefinitionFromSlice(src []byte, offset uint32, pass1End uint32) (*DefinitionRecord, int, bool) {
	if len(src) < 2 {
		return nil, 0, false
	}
	numHW := int(src[0])
	pos := 1
	hws := make([]HeadwordQualifier, 0, numHW)
	for i := 0; i < numHW; i++ {
		hwRaw, n1, ok1 := decodeXORString(src[pos:])
		if !ok1 {
			return nil, 0, false
		}
		pos += n1
		qualRaw, n2, ok2 := decodeXORString(src[pos:])
		if !ok2 {
			return nil, 0, false
		}
		pos += n2
		hws = append(hws, HeadwordQualifier{
			Headword:     DecodeCP1252(hwRaw),
			Qualifier:    DecodeCP1252(qualRaw),
			RawHeadword:  hwRaw,
			RawQualifier: qualRaw,
		})
	}
	bodyRaw, nb, okb := decodeXORString(src[pos:])
	if !okb {
		return nil, 0, false
	}
	pos += nb

	pass := 2
	if offset < pass1End {
		pass = 1
	}
	rec := &DefinitionRecord{
		Offset:    offset,
		Pass:      pass,
		Headwords: hws,
		Body:      DecodeCP1252(bodyRaw),
		RawBody:   bodyRaw,
		IsRTF:     bytes.HasPrefix(bodyRaw, []byte(`{\rtf1`)),
	}
	return rec, pos, true
}

// ReadDefinition decodes a single TSD1 definition record starting at the given file byte offset.
func (d *Dictionary) ReadDefinition(offset uint32) (*DefinitionRecord, error) {
	if !d.HasDefinitions() {
		return nil, ErrNoDefinitions
	}
	defStart := d.defPassOffsets[0]
	defEnd := d.defPassOffsets[2]
	if offset < defStart || offset >= defEnd {
		return nil, fmt.Errorf("%w: definition offset %d outside [%d, %d)", ErrInvalidOffset, offset, defStart, defEnd)
	}

	maxRem64 := int64(defEnd - offset)
	if maxRem64 > maxDefinitionRecordBytes {
		maxRem64 = maxDefinitionRecordBytes
	}
	maxRem := int(maxRem64)
	bufSize := 4096
	if bufSize > maxRem {
		bufSize = maxRem
	}
	buf := make([]byte, bufSize)
	readOff := int64(offset)
	filled := 0

	for {
		n, err := io.ReadFull(io.NewSectionReader(d.r, readOff+int64(filled), int64(len(buf)-filled)), buf[filled:])
		filled += n
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return nil, err
		}
		if rec, _, ok := parseDefinitionFromSlice(buf[:filled], offset, d.defPassOffsets[1]); ok {
			return rec, nil
		}
		// If ReadFull stopped short of len(buf), the underlying ReaderAt reached EOF early.
		if filled >= maxRem || filled < len(buf) {
			return nil, fmt.Errorf("%w: truncated XOR-0xBD definition record at %d", ErrInvalidOffset, offset)
		}
		nextSize := len(buf) * 2
		if nextSize > maxRem {
			nextSize = maxRem
		}
		grown := make([]byte, nextSize)
		copy(grown, buf[:filled])
		buf = grown
	}
}

// ScanDefinitions streams definition records sequentially.
// Pass selection:
//   - pass == 0: both Pass 1 and Pass 2 definitions ([def_pass_offsets[0], def_pass_offsets[2]))
//   - pass == 1: Pass 1 alias-only definitions only ([def_pass_offsets[0], def_pass_offsets[1]))
//   - pass == 2: Pass 2 headword definitions only ([def_pass_offsets[1], def_pass_offsets[2]))
func (d *Dictionary) ScanDefinitions(pass int) iter.Seq2[DefinitionRecord, error] {
	return func(yield func(DefinitionRecord, error) bool) {
		if !d.HasDefinitions() {
			return
		}
		var startOff, endOff uint32
		switch pass {
		case 0:
			startOff, endOff = d.defPassOffsets[0], d.defPassOffsets[2]
		case 1:
			startOff, endOff = d.defPassOffsets[0], d.defPassOffsets[1]
		case 2:
			startOff, endOff = d.defPassOffsets[1], d.defPassOffsets[2]
		default:
			yield(DefinitionRecord{}, fmt.Errorf("tsd: invalid definition pass %d (must be 0, 1, or 2)", pass))
			return
		}
		if startOff >= endOff {
			return
		}

		sr := io.NewSectionReader(d.r, int64(startOff), int64(endOff-startOff))
		buf := make([]byte, 64*1024)
		valid := 0
		curFileOff := startOff

		for curFileOff < endOff {
			n, err := sr.Read(buf[valid:])
			valid += n
			if err != nil && err != io.EOF {
				yield(DefinitionRecord{}, err)
				return
			}
			if n == 0 && err == nil {
				yield(DefinitionRecord{}, fmt.Errorf("%w: zero-progress definition read at %d", ErrInvalidOffset, curFileOff))
				return
			}

			consumed := 0
			for consumed < valid {
				rec, recLen, ok := parseDefinitionFromSlice(buf[consumed:valid], curFileOff, d.defPassOffsets[1])
				if !ok {
					break
				}
				consumed += recLen
				curFileOff += uint32(recLen)
				if !yield(*rec, nil) {
					return
				}
			}

			if consumed > 0 {
				copy(buf, buf[consumed:valid])
				valid -= consumed
			} else if curFileOff < endOff {
				if err == io.EOF {
					yield(DefinitionRecord{}, fmt.Errorf("%w: truncated definition at %d", ErrInvalidOffset, curFileOff))
					return
				}
				// Only grow when the buffer is actually full; short reads simply continue filling buf[valid:].
				if valid == len(buf) {
					if len(buf) >= maxDefinitionRecordBytes {
						yield(DefinitionRecord{}, fmt.Errorf("%w: definition record at %d exceeds %d bytes", ErrInvalidOffset, curFileOff, maxDefinitionRecordBytes))
						return
					}
					nextLen := len(buf) * 2
					if nextLen > maxDefinitionRecordBytes {
						nextLen = maxDefinitionRecordBytes
					}
					grown := make([]byte, nextLen)
					copy(grown, buf[:valid])
					buf = grown
				}
			}
		}
	}
}
