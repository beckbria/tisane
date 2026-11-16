package search

import (
	"context"
	"iter"

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
			for raw, err := range d.ScanRawWords(length) {
				if err != nil {
					break
				}
				if raw.LinkGroupIndex >= 0 {
					m[tsd.FoldCP1252(raw.WordBytes)] = true
				}
			}
		}
		c.byLen[length] = m
	}
	return m[foldedKey]
}

// Search executes req to completion, applying cross-dictionary deduplication, sorting, and Limit,
// and returns a consolidated Summary.
func (e *Engine) Search(ctx context.Context, req Request) (*Summary, error) {
	req = normalizeRequest(req)
	sum := &Summary{}

	// Note: when Sort != SortDict and Limit > 0, we must collect all matches before sorting & slicing,
	// EXCEPT in legacy TEACON SortDict mode where Limit stops scanning early!
	streamReq := req
	if req.Sort != SortDict && req.Limit > 0 {
		streamReq.Limit = 0
	}

	mergeMap := make(map[string]int) // word -> index in sum.Matches when DedupeMergeSources
	for batch, err := range e.searchStreamInternal(ctx, streamReq, mergeMap, &sum.Matches, &sum.UsedIndexedFTS) {
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
		sum.Matches = sum.Matches[:req.Limit]
	}
	return sum, nil
}

// SearchStream streams batches of Matches and scan Progress across req.Dictionaries in priority order,
// halting immediately if the caller stops iterating or cancels ctx.
func (e *Engine) SearchStream(ctx context.Context, req Request) iter.Seq2[Batch, error] {
	req = normalizeRequest(req)
	var dummyUsed bool
	return e.searchStreamInternal(ctx, req, nil, nil, &dummyUsed)
}

func (e *Engine) searchStreamInternal(
	ctx context.Context,
	req Request,
	mergeIdxMap map[string]int,
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
		seenWords := make(map[string]bool)
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

				for raw, sErr := range d.ScanRawWords(L) {
					if sErr != nil {
						yield(Batch{}, sErr)
						return
					}
					wordsScanned++
					scannedSinceYield++

					if matcher.MatchEntry(raw) && (ClassifyEntry(raw.WordBytes, d)&req.EntryFilter) != 0 {
						wordUTF8 := tsd.DecodeCP1252(raw.WordBytes)
						foldedKey := tsd.FoldCP1252(raw.WordBytes)

						if req.DedupeMode == DedupeSuppress {
							if seenWords[foldedKey] {
								goto checkBatchFlush
							}
							seenWords[foldedKey] = true
						} else if req.DedupeMode == DedupeMergeSources && mergeTarget != nil {
							if prevIdx, exists := mergeIdxMap[foldedKey]; exists {
								prev := &(*mergeTarget)[prevIdx]
								if len(prev.AllDictIndices) == 0 || prev.AllDictIndices[len(prev.AllDictIndices)-1] != dictIdx {
									prev.AllDictIndices = append(prev.AllDictIndices, dictIdx)
								}
								if !prev.HasDefinition && d.HasDefinitions() && raw.LinkGroupIndex >= 0 {
									prev.HasDefinition = true
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

						hasDef := d.HasDefinitions() && raw.LinkGroupIndex >= 0
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
						if req.DedupeMode == DedupeMergeSources {
							hit.AllDictIndices = []int{dictIdx}
							if mergeTarget != nil {
								mergeIdxMap[foldedKey] = len(*mergeTarget)
								*mergeTarget = append(*mergeTarget, hit)
							}
						}
						batchMatches = append(batchMatches, hit)
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
	mergeIdxMap map[string]int,
	mergeTarget *[]Match,
	usedIndexedFTS *bool,
	yield func(Batch, error) bool,
) {
	// Note: in Full Text Search, leaf patterns match words of ANY length 1..100 inside definitions,
	// while req.MinLength..req.MaxLength filters the emitted dictionary headwords!
	ftsCompOpts := pattern.CompileOptions{
		MinLength:               1,
		MaxLength:               tsd.MaxWordLengthLimit,
		ExclusiveVars:           req.ExclusiveVars,
		IgnoreUnusedPunctuation: req.IgnoreUnusedPunctuation,
		ComputeBalances:         false,
	}

	seenWords := make(map[string]bool)
	totalEmitted := 0
	var defsScanned uint64

	emitHit := func(hit Match, batch *[]Match) bool {
		foldedKey := tsd.FoldCP1252(hit.rawWord)
		if req.DedupeMode == DedupeSuppress {
			if seenWords[foldedKey] {
				return false
			}
			seenWords[foldedKey] = true
		} else if req.DedupeMode == DedupeMergeSources && mergeTarget != nil {
			if prevIdx, exists := mergeIdxMap[foldedKey]; exists {
				prev := &(*mergeTarget)[prevIdx]
				if len(prev.AllDictIndices) == 0 || prev.AllDictIndices[len(prev.AllDictIndices)-1] != hit.DictIndex {
					prev.AllDictIndices = append(prev.AllDictIndices, hit.DictIndex)
				}
				return false
			}
			hit.AllDictIndices = []int{hit.DictIndex}
			mergeIdxMap[foldedKey] = len(*mergeTarget)
			*mergeTarget = append(*mergeTarget, hit)
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
				var batchMatches []Match
				for _, off := range offsets {
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
					if hit, ok := makeFTSMatchFromRecord(dictIdx, d, rec, req, req.EntryFilter); ok {
						if emitHit(hit, &batchMatches) {
							yield(Batch{
								Matches:  batchMatches,
								Progress: Progress{DictIndex: dictIdx, DictName: d.Name(), WordsScanned: defsScanned},
							}, nil)
							return
						}
					}
				}
				if !yield(Batch{
					Matches:  batchMatches,
					Progress: Progress{DictIndex: dictIdx, DictName: d.Name(), WordsScanned: defsScanned},
				}, nil) {
					return
				}
				continue
			}
		}

		// Sequential Pass 2 definition scan fallback.
		leafMatched := make([]bool, cq.NumLeaves())
		idxScratch := make([]byte, 0, 64)
		rawScratch := make([]byte, 0, 64)
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
				if hit, ok := makeFTSMatchFromRecord(dictIdx, d, &rec, req, req.EntryFilter); ok {
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
			if !yield(Batch{
				Matches:  batchMatches,
				Progress: Progress{DictIndex: dictIdx, DictName: d.Name(), WordsScanned: defsScanned},
			}, nil) {
				return
			}
		}
	}
}
