package search

import (
	"context"
	"iter"
	"slices"

	"tisane/pkg/pattern"
	"tisane/pkg/tsd"
)

const streamBatchSize = 2000

// Engine executes multi-dictionary word and full-text searches.
// Engine is stateless and safe for concurrent use by multiple goroutines.
type Engine struct{}

// NewEngine returns a new multi-dictionary search Engine.
func NewEngine() *Engine {
	return &Engine{}
}

func normalizeRequest(req Request) Request {
	if req.MinLength <= 0 {
		req.MinLength = 1
	}
	if req.MaxLength <= 0 || req.MaxLength > tsd.MaxWordLengthLimit {
		req.MaxLength = tsd.MaxWordLengthLimit
	}
	if !req.DisableDefaultMacros {
		if req.AtMacro == "" {
			req.AtMacro = pattern.DefaultAtMacro
		}
		if req.HashMacro == "" {
			req.HashMacro = pattern.DefaultHashMacro
		}
	}
	if req.EntryFilter == 0 {
		req.EntryFilter = EntryAll
	}
	return req
}

// defPresenceCache lazily indexes defined headwords by length across definition-bearing dictionaries
// so matches emitted from TSD0/wordlist-only dictionaries accurately report HasDefinition.
type defPresenceCache struct {
	defDicts []*tsd.Dictionary
	byLen    [tsd.MaxWordLengthLimit + 1]map[string]bool
}

func newDefPresenceCache(req *Request) *defPresenceCache {
	seen := make(map[*tsd.Dictionary]bool)
	var defDicts []*tsd.Dictionary
	for _, d := range req.Dictionaries {
		if d != nil && d.HasDefinitions() && !seen[d] {
			seen[d] = true
			defDicts = append(defDicts, d)
		}
	}
	for _, d := range req.DefinitionDictionaries {
		if d != nil && d.HasDefinitions() && !seen[d] {
			seen[d] = true
			defDicts = append(defDicts, d)
		}
	}
	return &defPresenceCache{defDicts: defDicts}
}

func (c *defPresenceCache) hasDef(foldedKey string, length int) bool {
	if len(c.defDicts) == 0 || length <= 0 || length > tsd.MaxWordLengthLimit {
		return false
	}
	m := c.byLen[length]
	if m == nil {
		m = make(map[string]bool)
		for _, d := range c.defDicts {
			if d.WordCount(length) == 0 {
				continue
			}
			lt, ltErr := d.ReadLinkTable(length)
			if ltErr != nil || lt.NumGroups() == 0 {
				continue
			}
			for raw, err := range d.ScanRawWords(length) {
				if err != nil {
					break
				}
				if raw.LinkGroupIndex >= 0 && lt.HasDirectDefinition(int(raw.LinkGroupIndex)) {
					m[tsd.FoldCP1252(raw.WordBytes)] = true
				}
			}
		}
		c.byLen[length] = m
	}
	return m[foldedKey]
}

type mergeEntryState struct {
	firstDict int
	indices   []int // indices into *mergeTarget emitted by firstDict
}

// Search executes req to completion, applying cross-dictionary deduplication, sorting, and Limit,
// and returns a consolidated Summary.
func (e *Engine) Search(ctx context.Context, req Request) (*Summary, error) {
	req = normalizeRequest(req)
	sum := &Summary{}

	// When Sort != SortDict or DedupeMergeSources is active with Limit > 0, collect all matches before slicing.
	streamReq := req
	if (req.Sort != SortDict || req.DedupeMode == DedupeMergeSources) && req.Limit > 0 {
		streamReq.Limit = 0
	}

	var mergeMap map[string]*mergeEntryState
	var mergeTarget *[]Match
	if req.DedupeMode == DedupeMergeSources {
		mergeMap = make(map[string]*mergeEntryState)
		mergeTarget = &sum.Matches
	}

	for batch, err := range e.searchStreamInternal(ctx, streamReq, mergeMap, mergeTarget, &sum.UsedIndexedFTS) {
		if err != nil {
			return nil, err
		}
		sum.WordsScanned = batch.Progress.WordsScanned
		sum.TotalCandidateWords = batch.Progress.TotalCandidateWords
		if req.DedupeMode != DedupeMergeSources {
			sum.Matches = append(sum.Matches, batch.Matches...)
		}
	}

	if req.Sort != SortDict {
		SortMatches(sum.Matches, req.Sort)
	}
	if req.Limit > 0 && len(sum.Matches) > req.Limit {
		sum.Matches = slices.Clone(sum.Matches[:req.Limit])
	}
	if sum.Matches == nil {
		sum.Matches = []Match{}
	}
	return sum, nil
}

// SearchStream streams batches of Matches and scan Progress across req.Dictionaries in priority order,
// halting immediately if the caller stops iterating or cancels ctx.
// Note: DedupeMergeSources requires buffering across dictionaries; in streaming mode it suppresses
// lower-priority duplicates identically to DedupeSuppress.
func (e *Engine) SearchStream(ctx context.Context, req Request) iter.Seq2[Batch, error] {
	req = normalizeRequest(req)
	if req.DedupeMode == DedupeMergeSources {
		req.DedupeMode = DedupeSuppress
	}
	return func(yield func(Batch, error) bool) {
		var dummyUsed bool
		for batch, err := range e.searchStreamInternal(ctx, req, nil, nil, &dummyUsed) {
			if !yield(batch, err) {
				return
			}
		}
	}
}

// SearchStreamWithStats behaves like SearchStream and also populates *usedIndexedFTS when non-nil.
func (e *Engine) SearchStreamWithStats(ctx context.Context, req Request, usedIndexedFTS *bool) iter.Seq2[Batch, error] {
	req = normalizeRequest(req)
	if req.DedupeMode == DedupeMergeSources {
		req.DedupeMode = DedupeSuppress
	}
	return func(yield func(Batch, error) bool) {
		var localUsed bool
		target := &localUsed
		if usedIndexedFTS != nil {
			target = usedIndexedFTS
		}
		for batch, err := range e.searchStreamInternal(ctx, req, nil, nil, target) {
			if !yield(batch, err) {
				return
			}
		}
	}
}

func (e *Engine) searchStreamInternal(
	ctx context.Context,
	req Request,
	mergeIdxMap map[string]*mergeEntryState,
	mergeTarget *[]Match,
	usedIndexedFTS *bool,
) iter.Seq2[Batch, error] {
	return func(yield func(Batch, error) bool) {
		parseOpts := pattern.ParseOptions{
			AtMacro:   req.AtMacro,
			HashMacro: req.HashMacro,
		}
		if req.Mode == ModeWord {
			parseOpts.Enumeration = req.Enumeration
		}

		pq, err := pattern.Parse(req.Query, parseOpts)
		if err != nil {
			yield(Batch{}, err)
			return
		}

		if req.Mode == ModeFullText {
			e.streamFTS(ctx, &req, pq, mergeIdxMap, mergeTarget, usedIndexedFTS, yield)
			return
		}

		needBalances := req.ShowBalances || req.Sort == SortLenSmart
		compOpts := pattern.CompileOptions{
			MinLength:               req.MinLength,
			MaxLength:               req.MaxLength,
			ExclusiveVars:           req.ExclusiveVars,
			IgnoreUnusedPunctuation: req.IgnoreUnusedPunctuation,
			ComputeBalances:         needBalances,
		}

		compiled := make([]*pattern.CompiledQuery, len(req.Dictionaries))
		var totalCandidates uint64
		for i, d := range req.Dictionaries {
			if d == nil {
				continue
			}
			cq, cerr := pattern.Compile(pq, d, compOpts)
			if cerr != nil {
				yield(Batch{}, cerr)
				return
			}
			compiled[i] = cq
			for L := req.MinLength; L <= req.MaxLength && L <= d.MaxWordLength(); L++ {
				if cq.CanMatchLength(L) {
					totalCandidates += uint64(d.WordCount(L))
				}
			}
		}

		defCache := newDefPresenceCache(&req)
		var firstSeenDict map[string]int
		if req.DedupeMode == DedupeSuppress && len(req.Dictionaries) > 1 {
			firstSeenDict = make(map[string]int)
		}
		activeFilter := req.EntryFilter & EntryAll
		var wordsScanned uint64
		totalEmitted := 0

		for dictIdx, d := range req.Dictionaries {
			if d == nil {
				continue
			}
			if err := ctx.Err(); err != nil {
				yield(Batch{}, err)
				return
			}

			cq := compiled[dictIdx]
			if cq.MinCompatLength() == 0 {
				continue
			}
			matcher := cq.NewMatcher()
			maxL := d.MaxWordLength()
			if req.MaxLength < maxL {
				maxL = req.MaxLength
			}

			var batchMatches []Match
			scannedSinceYield := 0

			for L := req.MinLength; L <= maxL; L++ {
				if !cq.CanMatchLength(L) || d.WordCount(L) == 0 {
					continue
				}

				var lt *tsd.LinkTable
				ltLoaded := false
				hasDirectInCur := func(linkGroup int32) bool {
					if !d.HasDefinitions() || linkGroup < 0 {
						return false
					}
					if !ltLoaded {
						lt, _ = d.ReadLinkTable(L)
						ltLoaded = true
					}
					return lt != nil && lt.HasDirectDefinition(int(linkGroup))
				}

				for raw, sErr := range d.ScanRawWords(L) {
					if sErr != nil {
						yield(Batch{}, sErr)
						return
					}
					wordsScanned++
					scannedSinceYield++

					if matcher.MatchEntry(raw) && (ClassifyEntry(raw.WordBytes, d)&activeFilter) != 0 {
						wordUTF8 := tsd.DecodeCP1252(raw.WordBytes)
						foldedKey := tsd.FoldCP1252(raw.WordBytes)

						if firstSeenDict != nil {
							if prevDict, seen := firstSeenDict[foldedKey]; seen {
								if prevDict < dictIdx {
									goto checkBatchFlush
								}
							} else {
								firstSeenDict[foldedKey] = dictIdx
							}
						} else if req.DedupeMode == DedupeMergeSources && mergeTarget != nil {
							if st, exists := mergeIdxMap[foldedKey]; exists && st.firstDict < dictIdx {
								directHere := hasDirectInCur(raw.LinkGroupIndex)
								for _, mIdx := range st.indices {
									prev := &(*mergeTarget)[mIdx]
									if len(prev.AllDictIndices) == 0 || prev.AllDictIndices[len(prev.AllDictIndices)-1] != dictIdx {
										prev.AllDictIndices = append(prev.AllDictIndices, dictIdx)
									}
									if !prev.HasDefinition && directHere {
										prev.HasDefinition = true
									}
								}
								goto checkBatchFlush
							}
						}

						var annot string
						if req.ShowAnnotations && len(raw.AnnotBytes) > 0 {
							annot = tsd.DecodeCP1252(raw.AnnotBytes)
						}

						var balStr string
						var balLen int
						if needBalances {
							balStr, balLen = matcher.FormatBalance()
							if !req.ShowBalances {
								balStr = ""
							}
						}

						hasDef := hasDirectInCur(raw.LinkGroupIndex)
						if !hasDef {
							hasDef = defCache.hasDef(foldedKey, L)
						}

						hit := Match{
							Word:           wordUTF8,
							Annotation:     annot,
							AnagramBalance: balStr,
							Length:         L,
							DictIndex:      dictIdx,
							DictName:       d.Name(),
							LinkIndex:      raw.LinkGroupIndex,
							HasDefinition:  hasDef,
							rawWord:        append([]byte(nil), raw.WordBytes...),
							balanceLen:     balLen,
						}
						if req.DedupeMode == DedupeMergeSources && mergeTarget != nil {
							hit.AllDictIndices = []int{dictIdx}
							st, exists := mergeIdxMap[foldedKey]
							if !exists {
								st = &mergeEntryState{firstDict: dictIdx}
								mergeIdxMap[foldedKey] = st
							}
							st.indices = append(st.indices, len(*mergeTarget))
							*mergeTarget = append(*mergeTarget, hit)
						} else {
							batchMatches = append(batchMatches, hit)
						}
						totalEmitted++

						if req.Limit > 0 && totalEmitted >= req.Limit {
							yield(Batch{
								Matches: batchMatches,
								Progress: Progress{
									DictIndex:           dictIdx,
									DictName:            d.Name(),
									WordsScanned:        wordsScanned,
									TotalCandidateWords: totalCandidates,
								},
							}, nil)
							return
						}
					}

				checkBatchFlush:
					if scannedSinceYield >= streamBatchSize {
						if err := ctx.Err(); err != nil {
							yield(Batch{}, err)
							return
						}
						if !yield(Batch{
							Matches: batchMatches,
							Progress: Progress{
								DictIndex:           dictIdx,
								DictName:            d.Name(),
								WordsScanned:        wordsScanned,
								TotalCandidateWords: totalCandidates,
							},
						}, nil) {
							return
						}
						batchMatches = nil
						scannedSinceYield = 0
					}
				}
			}

			if len(batchMatches) > 0 || scannedSinceYield > 0 {
				if err := ctx.Err(); err != nil {
					yield(Batch{}, err)
					return
				}
				if !yield(Batch{
					Matches: batchMatches,
					Progress: Progress{
						DictIndex:           dictIdx,
						DictName:            d.Name(),
						WordsScanned:        wordsScanned,
						TotalCandidateWords: totalCandidates,
					},
				}, nil) {
					return
				}
			}
		}
	}
}

func (e *Engine) streamFTS(
	ctx context.Context,
	req *Request,
	pq *pattern.ParsedQuery,
	mergeIdxMap map[string]*mergeEntryState,
	mergeTarget *[]Match,
	usedIndexedFTS *bool,
	yield func(Batch, error) bool,
) {
	if req.MinLength > req.MaxLength {
		return
	}
	// Note: in Full Text Search, leaf patterns match words of ANY length 1..100 inside definitions,
	// while req.MinLength..req.MaxLength filters the emitted dictionary headwords!
	ftsCompOpts := pattern.CompileOptions{
		MinLength:               1,
		MaxLength:               tsd.MaxWordLengthLimit,
		ExclusiveVars:           req.ExclusiveVars,
		IgnoreUnusedPunctuation: req.IgnoreUnusedPunctuation,
		ComputeBalances:         false,
	}

	var firstSeenDict map[string]int
	if req.DedupeMode == DedupeSuppress && len(req.Dictionaries) > 1 {
		firstSeenDict = make(map[string]int)
	}
	totalEmitted := 0
	var defsScanned uint64
	var recHits []Match

	emitHit := func(hit Match, batch *[]Match) bool {
		foldedKey := tsd.FoldCP1252(hit.rawWord)
		if firstSeenDict != nil {
			if prevDict, seen := firstSeenDict[foldedKey]; seen {
				if prevDict < hit.DictIndex {
					return false
				}
			} else {
				firstSeenDict[foldedKey] = hit.DictIndex
			}
		} else if req.DedupeMode == DedupeMergeSources && mergeTarget != nil {
			if st, exists := mergeIdxMap[foldedKey]; exists && st.firstDict < hit.DictIndex {
				for _, mIdx := range st.indices {
					prev := &(*mergeTarget)[mIdx]
					if len(prev.AllDictIndices) == 0 || prev.AllDictIndices[len(prev.AllDictIndices)-1] != hit.DictIndex {
						prev.AllDictIndices = append(prev.AllDictIndices, hit.DictIndex)
					}
				}
				return false
			}
			hit.AllDictIndices = []int{hit.DictIndex}
			st, exists := mergeIdxMap[foldedKey]
			if !exists {
				st = &mergeEntryState{firstDict: hit.DictIndex}
				mergeIdxMap[foldedKey] = st
			}
			st.indices = append(st.indices, len(*mergeTarget))
			*mergeTarget = append(*mergeTarget, hit)
			totalEmitted++
			return req.Limit > 0 && totalEmitted >= req.Limit
		}
		*batch = append(*batch, hit)
		totalEmitted++
		return req.Limit > 0 && totalEmitted >= req.Limit
	}

	for dictIdx, d := range req.Dictionaries {
		if d == nil || !d.HasPass2Definitions() {
			continue
		}
		if err := ctx.Err(); err != nil {
			yield(Batch{}, err)
			return
		}

		cq, err := pattern.Compile(pq, d, ftsCompOpts)
		if err != nil {
			yield(Batch{}, err)
			return
		}
		matcher := cq.NewMatcher()

		// Try Indexed FTS first (unless ForceSequentialFTS is set).
		if !req.ForceSequentialFTS && d.HasFTSIndex() {
			cursor := 0
			offsets, exact, iErr := evalFTSIndexedAST(ctx, pq.Root, d, cq, matcher, &cursor)
			if iErr != nil {
				yield(Batch{}, iErr)
				return
			}
			if exact {
				*usedIndexedFTS = true
				totalCand := uint64(len(offsets))
				var batchMatches []Match
				for idxOff, off := range offsets {
					if err := ctx.Err(); err != nil {
						yield(Batch{}, err)
						return
					}
					rec, rErr := d.ReadDefinition(off)
					if rErr != nil {
						yield(Batch{}, rErr)
						return
					}
					defsScanned++
					recHits = appendFTSMatchesFromRecord(recHits[:0], dictIdx, d, rec, req, req.EntryFilter)
					for _, hit := range recHits {
						if emitHit(hit, &batchMatches) {
							yield(Batch{
								Matches: batchMatches,
								Progress: Progress{
									DictIndex:           dictIdx,
									DictName:            d.Name(),
									WordsScanned:        defsScanned,
									TotalCandidateWords: totalCand,
								},
							}, nil)
							return
						}
					}
					if (idxOff+1)%500 == 0 {
						if !yield(Batch{
							Matches: batchMatches,
							Progress: Progress{
								DictIndex:           dictIdx,
								DictName:            d.Name(),
								WordsScanned:        defsScanned,
								TotalCandidateWords: totalCand,
							},
						}, nil) {
							return
						}
						batchMatches = nil
					}
				}
				if err := ctx.Err(); err != nil {
					yield(Batch{}, err)
					return
				}
				if !yield(Batch{
					Matches: batchMatches,
					Progress: Progress{
						DictIndex:           dictIdx,
						DictName:            d.Name(),
						WordsScanned:        defsScanned,
						TotalCandidateWords: totalCand,
					},
				}, nil) {
					return
				}
				continue
			}
		}

		// Sequential Pass 2 definition scan fallback.
		leafMatched := make([]bool, cq.NumLeaves())
		idxScratch := make([]byte, 0, tsd.MaxWordLengthLimit)
		rawScratch := make([]byte, 0, tsd.MaxWordLengthLimit)
		var batchMatches []Match
		sinceYield := 0

		for rec, sErr := range d.ScanDefinitions(2) {
			if sErr != nil {
				yield(Batch{}, sErr)
				return
			}
			defsScanned++
			sinceYield++

			if testDefinitionRecordSequential(d, &rec, cq, matcher, leafMatched, idxScratch, rawScratch) {
				recHits = appendFTSMatchesFromRecord(recHits[:0], dictIdx, d, &rec, req, req.EntryFilter)
				for _, hit := range recHits {
					if emitHit(hit, &batchMatches) {
						yield(Batch{
							Matches:  batchMatches,
							Progress: Progress{DictIndex: dictIdx, DictName: d.Name(), WordsScanned: defsScanned},
						}, nil)
						return
					}
				}
			}

			if sinceYield >= 500 {
				if err := ctx.Err(); err != nil {
					yield(Batch{}, err)
					return
				}
				if !yield(Batch{
					Matches:  batchMatches,
					Progress: Progress{DictIndex: dictIdx, DictName: d.Name(), WordsScanned: defsScanned},
				}, nil) {
					return
				}
				batchMatches = nil
				sinceYield = 0
			}
		}

		if len(batchMatches) > 0 || sinceYield > 0 {
			if err := ctx.Err(); err != nil {
				yield(Batch{}, err)
				return
			}
			if !yield(Batch{
				Matches:  batchMatches,
				Progress: Progress{DictIndex: dictIdx, DictName: d.Name(), WordsScanned: defsScanned},
			}, nil) {
				return
			}
		}
	}
}
