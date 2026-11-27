package tsd

import (
	"bytes"
	"context"
	"math"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
)

const (
	wordChunkShift = 16
	wordChunkCap   = 1 << wordChunkShift
	wordChunkMask  = wordChunkCap - 1
)

type wordChunk struct {
	data    []byte
	offsets []uint32
}

// SortedWordList stores all unique Sub-stream 1 headwords/entries of a Dictionary
// in alphabetical order (CompareWordsAlpha) using compact chunked byte arenas.
type SortedWordList struct {
	chunks []wordChunk
	count  int
}

// Len returns the total number of unique sorted words.
func (wl *SortedWordList) Len() int {
	if wl == nil {
		return 0
	}
	return wl.count
}

func (wl *SortedWordList) rawWord(i int) []byte {
	c := &wl.chunks[i>>wordChunkShift]
	idx := i & wordChunkMask
	return c.data[c.offsets[idx]:c.offsets[idx+1]]
}

// Word returns the UTF-8 decoded word at 0-based index i, or "" if out of range.
func (wl *SortedWordList) Word(i int) string {
	if wl == nil || i < 0 || i >= wl.count {
		return ""
	}
	return DecodeCP1252(wl.rawWord(i))
}

// Slice returns up to limit UTF-8 decoded words starting at offset.
// Always returns a non-nil slice.
func (wl *SortedWordList) Slice(offset, limit int) []string {
	if wl == nil || wl.count == 0 || limit <= 0 || offset >= wl.count {
		return []string{}
	}
	if offset < 0 {
		offset = 0
	}
	end := offset + limit
	if end > wl.count {
		end = wl.count
	}
	out := make([]string, end-offset)
	for i := offset; i < end; i++ {
		out[i-offset] = DecodeCP1252(wl.rawWord(i))
	}
	return out
}

// PrefixIndex returns the 0-based index of the first word whose alphabetical sort key
// is >= prefix's alphabetical sort key (clamped to [0, Len()-1], or 0 when empty).
func (wl *SortedWordList) PrefixIndex(prefix string) int {
	if wl == nil || wl.count == 0 {
		return 0
	}
	trimmed := strings.TrimSpace(prefix)
	if trimmed == "" {
		return 0
	}
	var key []byte
	for _, r := range trimmed {
		if b, ok := EncodeCP1252Rune(r); ok {
			if s := cp1252SortTable[b]; s != 0 {
				key = append(key, s)
			}
		}
	}
	if len(key) == 0 {
		return 0
	}
	idx := sort.Search(wl.count, func(i int) bool {
		return compareRawAgainstSortKey(wl.rawWord(i), key) >= 0
	})
	if idx >= wl.count {
		return wl.count - 1
	}
	return idx
}

func compareRawAgainstSortKey(raw, key []byte) int {
	ir, ik := 0, 0
	nr, nk := len(raw), len(key)
	for ik < nk {
		var cr byte
		for ir < nr {
			cr = cp1252SortTable[raw[ir]]
			ir++
			if cr != 0 {
				break
			}
		}
		ck := key[ik]
		ik++
		if cr == 0 {
			return -1
		}
		if cr != ck {
			if cr < ck {
				return -1
			}
			return 1
		}
	}
	return 0
}

type wordSpan struct {
	off uint32
	end uint32
}

type bucketRun struct {
	data  []byte
	spans []wordSpan
}

func (d *Dictionary) scanSortedBucket(ctx context.Context, length int) (bucketRun, error) {
	wc := int(d.WordCount(length))
	if wc == 0 {
		return bucketRun{}, nil
	}
	data := make([]byte, 0, wc*(length+1))
	spans := make([]wordSpan, 0, wc)
	isSorted := true
	step := 0

	for entry, err := range d.ScanRawWords(length) {
		if err != nil {
			return bucketRun{}, err
		}
		step++
		if step&0xFFF == 0 {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return bucketRun{}, ctxErr
			}
		}
		wb := entry.WordBytes
		if len(spans) > 0 {
			prevSpan := spans[len(spans)-1]
			prev := data[prevSpan.off:prevSpan.end]
			cmp := CompareWordsAlpha(prev, wb)
			if cmp == 0 {
				continue
			}
			if cmp > 0 {
				isSorted = false
			}
		}
		start := len(data)
		end := start + len(wb)
		if uint64(end) > math.MaxUint32 {
			return bucketRun{}, ErrInvalidHeader
		}
		data = append(data, wb...)
		spans = append(spans, wordSpan{off: uint32(start), end: uint32(end)})
	}

	if !isSorted && len(spans) > 1 {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return bucketRun{}, ctxErr
		}
		slices.SortFunc(spans, func(a, b wordSpan) int {
			return CompareWordsAlpha(data[a.off:a.end], data[b.off:b.end])
		})
		writeIdx := 1
		for i := 1; i < len(spans); i++ {
			prev := spans[writeIdx-1]
			cur := spans[i]
			if !bytes.Equal(data[prev.off:prev.end], data[cur.off:cur.end]) {
				spans[writeIdx] = cur
				writeIdx++
			}
		}
		spans = spans[:writeIdx]
	}

	return bucketRun{data: data, spans: spans}, nil
}

type bucketCursor struct {
	runIdx int
	pos    int
	cur    []byte
}

func siftDownMergeHeap(h []bucketCursor, i int) {
	n := len(h)
	for {
		smallest := i
		left := 2*i + 1
		right := left + 1
		if left < n && CompareWordsAlpha(h[left].cur, h[smallest].cur) < 0 {
			smallest = left
		}
		if right < n && CompareWordsAlpha(h[right].cur, h[smallest].cur) < 0 {
			smallest = right
		}
		if smallest == i {
			return
		}
		h[i], h[smallest] = h[smallest], h[i]
		i = smallest
	}
}

// BuildSortedWordList decodes and alphabetically sorts all unique Sub-stream 1 words in d.
func (d *Dictionary) BuildSortedWordList(ctx context.Context) (*SortedWordList, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	maxL := d.MaxWordLength()
	if maxL == 0 || d.TotalWords() == 0 {
		return &SortedWordList{}, nil
	}

	var nonEmptyLengths []int
	for l := 1; l <= maxL; l++ {
		if d.WordCount(l) > 0 {
			nonEmptyLengths = append(nonEmptyLengths, l)
		}
	}
	if len(nonEmptyLengths) == 0 {
		return &SortedWordList{}, nil
	}

	runs := make([]bucketRun, len(nonEmptyLengths))
	workers := min(len(nonEmptyLengths), max(1, runtime.GOMAXPROCS(0)))

	ctxScan, cancelScan := context.WithCancel(ctx)
	defer cancelScan()

	var (
		nextIdx  int
		mu       sync.Mutex
		firstErr error
		wg       sync.WaitGroup
	)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				if firstErr != nil || nextIdx >= len(nonEmptyLengths) {
					mu.Unlock()
					return
				}
				idx := nextIdx
				nextIdx++
				mu.Unlock()

				run, err := d.scanSortedBucket(ctxScan, nonEmptyLengths[idx])
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
						cancelScan()
					}
					mu.Unlock()
					return
				}
				runs[idx] = run
			}
		}()
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	totalUpperBound := 0
	heap := make([]bucketCursor, 0, len(runs))
	for i := range runs {
		if len(runs[i].spans) == 0 {
			runs[i] = bucketRun{}
			continue
		}
		totalUpperBound += len(runs[i].spans)
		sp := runs[i].spans[0]
		heap = append(heap, bucketCursor{
			runIdx: i,
			pos:    0,
			cur:    runs[i].data[sp.off:sp.end],
		})
	}
	if len(heap) == 0 {
		return &SortedWordList{}, nil
	}
	for i := len(heap)/2 - 1; i >= 0; i-- {
		siftDownMergeHeap(heap, i)
	}

	numChunksHint := (totalUpperBound + wordChunkCap - 1) / wordChunkCap
	chunks := make([]wordChunk, 0, numChunksHint)

	var (
		curData    []byte
		curOffsets []uint32
		prevWord   []byte
		hasPrev    bool
		totalCount int
	)

	startNewChunk := func() {
		rem := min(wordChunkCap, max(1, totalUpperBound-totalCount))
		curData = make([]byte, 0, rem*12)
		curOffsets = make([]uint32, 1, rem+1)
		curOffsets[0] = 0
	}
	flushChunk := func() {
		if len(curOffsets) > 1 {
			chunks = append(chunks, wordChunk{
				data:    curData,
				offsets: curOffsets,
			})
		}
		curData = nil
		curOffsets = nil
	}

	startNewChunk()
	mergeSteps := 0

	for len(heap) > 0 {
		mergeSteps++
		if mergeSteps&0x3FFF == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

		top := heap[0]
		wb := top.cur
		if !hasPrev || !bytes.Equal(prevWord, wb) {
			if len(curOffsets)-1 == wordChunkCap {
				flushChunk()
				startNewChunk()
			}
			curData = append(curData, wb...)
			curOffsets = append(curOffsets, uint32(len(curData)))
			prevWord = curData[len(curData)-len(wb):]
			hasPrev = true
			totalCount++
		}

		nextPos := top.pos + 1
		r := &runs[top.runIdx]
		if nextPos < len(r.spans) {
			sp := r.spans[nextPos]
			heap[0].pos = nextPos
			heap[0].cur = r.data[sp.off:sp.end]
			siftDownMergeHeap(heap, 0)
		} else {
			// Release completed bucket buffer immediately so memory stays bounded during merge.
			*r = bucketRun{}
			last := len(heap) - 1
			heap[0] = heap[last]
			heap = heap[:last]
			if len(heap) > 0 {
				siftDownMergeHeap(heap, 0)
			}
		}
	}
	flushChunk()

	return &SortedWordList{
		chunks: chunks,
		count:  totalCount,
	}, nil
}
