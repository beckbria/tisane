package search

import (
	"context"
	"slices"
	"strings"

	"tisane/pkg/pattern"
	"tisane/pkg/tsd"
)

// unionUint32 returns the sorted deduplicated union of two sorted deduplicated uint32 slices.
func unionUint32(a, b []uint32) []uint32 {
	out := make([]uint32, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			out = append(out, a[i])
			i++
		case a[i] > b[j]:
			out = append(out, b[j])
			j++
		default:
			out = append(out, a[i])
			i++
			j++
		}
	}
	out = append(out, a[i:]...)
	out = append(out, b[j:]...)
	return out
}

// intersectUint32 returns the sorted intersection of two sorted deduplicated uint32 slices.
func intersectUint32(a, b []uint32) []uint32 {
	minCap := len(a)
	if len(b) < minCap {
		minCap = len(b)
	}
	out := make([]uint32, 0, minCap)
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			i++
		case a[i] > b[j]:
			j++
		default:
			out = append(out, a[i])
			i++
			j++
		}
	}
	return out
}

// differenceUint32 returns sorted elements in a that are NOT in b (sorted set_difference).
// This powers fast indexed evaluation of FTS queries like `eagle & !bald`.
func differenceUint32(a, b []uint32) []uint32 {
	out := make([]uint32, 0, len(a))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			out = append(out, a[i])
			i++
		case a[i] > b[j]:
			j++
		default:
			i++
			j++
		}
	}
	out = append(out, a[i:]...)
	return out
}

func sortAndDedupeUint32(s []uint32) []uint32 {
	if len(s) <= 1 {
		return s
	}
	slices.Sort(s)
	return slices.Compact(s)
}

// collectIndexedLeafOffsets resolves all Pass 2 definition offsets matching leafIdx via dict's
// word + FTS streams and Link/FTS tables. If leafIdx uses raw byte/punctuation matching or
// hits an FTSOverflowSentinel (0x7FFFFFFF), exact=false is returned so FTS falls back cleanly.
func collectIndexedLeafOffsets(
	ctx context.Context,
	dict *tsd.Dictionary,
	cq *pattern.CompiledQuery,
	matcher *pattern.Matcher,
	leafIdx int,
) (offsets []uint32, exact bool, err error) {
	if cq.LeafNeedsRawBytes(leafIdx) {
		return nil, false, nil
	}

	minL, maxL := cq.LeafBounds(leafIdx)
	if minL <= 0 || maxL > dict.MaxWordLength() {
		// TSD1 dictionaries only store FTS streams for lengths 1..MaxWordLength where WordCount(L) > 0.
		return nil, false, nil
	}
	for L := minL; L <= maxL; L++ {
		if dict.WordCount(L) == 0 {
			return nil, false, nil
		}
	}

	pass2Start := dict.DefPassOffsets()[1]
	pass2End := dict.DefPassOffsets()[2]

	for L := minL; L <= maxL; L++ {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}

		// Quick length check by testing a dummy length check via MatchLeafEntry:
		// Note: ScanAllRawWords only opened if L can match leafIdx.
		var lt *tsd.LinkTable
		var ftsPtrs []uint32
		loadedFTSPtrs := false

		for streamEntry, sErr := range dict.ScanAllRawWords(L) {
			if sErr != nil {
				return nil, false, sErr
			}
			if !matcher.MatchLeafEntry(leafIdx, streamEntry.RawEntry) {
				continue
			}

			if !streamEntry.IsFTSOnly {
				if streamEntry.LinkGroupIndex < 0 {
					continue
				}
				if lt == nil {
					lt, err = dict.ReadLinkTable(L)
					if err != nil {
						return nil, false, err
					}
				}
				res, rErr := lt.Resolve(int(streamEntry.LinkGroupIndex))
				if rErr != nil {
					return nil, false, rErr
				}
				if res.FTSOverflow {
					return nil, false, nil
				}
				for _, doff := range res.DirectOffsets {
					if doff >= pass2Start && doff < pass2End {
						offsets = append(offsets, doff)
					}
				}
				if res.FTSListOffset != 0 {
					occ, oErr := dict.ReadFTSOccurrences(res.FTSListOffset)
					if oErr != nil {
						return nil, false, oErr
					}
					offsets = append(offsets, occ...)
				}
			} else {
				if !loadedFTSPtrs {
					ftsPtrs, err = dict.ReadFTSIndexPointers(L)
					if err != nil {
						return nil, false, err
					}
					loadedFTSPtrs = true
				}
				idx := int(streamEntry.FTSIndex)
				if idx < 0 || idx >= len(ftsPtrs) {
					continue
				}
				ptr := ftsPtrs[idx]
				if ptr == tsd.FTSOverflowSentinel {
					return nil, false, nil
				}
				occ, oErr := dict.ReadFTSOccurrences(ptr)
				if oErr != nil {
					return nil, false, oErr
				}
				offsets = append(offsets, occ...)
			}
		}
	}

	return sortAndDedupeUint32(offsets), true, nil
}

// evalFTSIndexedAST attempts to evaluate expr into an exact sorted slice of Pass 2 definition offsets
// using union, intersection, and set_difference (for A & !B).
// Note leafCursor advances across AtomExpr nodes in exact left-to-right DFS order regardless of fallback!
func evalFTSIndexedAST(
	ctx context.Context,
	expr pattern.Expr,
	dict *tsd.Dictionary,
	cq *pattern.CompiledQuery,
	matcher *pattern.Matcher,
	leafCursor *int,
) (offsets []uint32, exact bool, err error) {
	switch v := expr.(type) {
	case *pattern.AtomExpr:
		idx := *leafCursor
		*leafCursor++
		return collectIndexedLeafOffsets(ctx, dict, cq, matcher, idx)

	case *pattern.NotExpr:
		// Advance leafCursor over the negated subtree even though pure top-level !X is unindexed.
		advanceLeafCursor(v.Inner, leafCursor)
		return nil, false, nil

	case *pattern.BinaryExpr:
		subStart := *leafCursor
		if v.Op == pattern.OpOr {
			leftOffs, leftOK, err := evalFTSIndexedAST(ctx, v.Left, dict, cq, matcher, leafCursor)
			if err != nil {
				return nil, false, err
			}
			if !leftOK {
				advanceLeafCursor(v.Right, leafCursor)
				return nil, false, nil
			}
			rightOffs, rightOK, err := evalFTSIndexedAST(ctx, v.Right, dict, cq, matcher, leafCursor)
			if err != nil {
				return nil, false, err
			}
			if !rightOK {
				return nil, false, nil
			}
			return unionUint32(leftOffs, rightOffs), true, nil
		}

		// OpAnd: support (A & B), (A & !B) via set_difference, and (!A & B) via set_difference!
		if leftNot, isLeftNot := v.Left.(*pattern.NotExpr); isLeftNot {
			negOffs, negOK, err := evalFTSIndexedAST(ctx, leftNot.Inner, dict, cq, matcher, leafCursor)
			if err != nil {
				return nil, false, err
			}
			rightOffs, rightOK, err := evalFTSIndexedAST(ctx, v.Right, dict, cq, matcher, leafCursor)
			if err != nil {
				return nil, false, err
			}
			if rightOK && len(rightOffs) == 0 {
				return nil, true, nil
			}
			if negOK && rightOK {
				return differenceUint32(rightOffs, negOffs), true, nil
			}
			if rightOK {
				filtered, err := filterOffsetsSequentially(ctx, dict, cq, matcher, v, subStart, rightOffs)
				return filtered, err == nil, err
			}
			return nil, false, nil
		}

		if rightNot, isRightNot := v.Right.(*pattern.NotExpr); isRightNot {
			leftOffs, leftOK, err := evalFTSIndexedAST(ctx, v.Left, dict, cq, matcher, leafCursor)
			if err != nil {
				return nil, false, err
			}
			if leftOK && len(leftOffs) == 0 {
				advanceLeafCursor(rightNot.Inner, leafCursor)
				return nil, true, nil
			}
			negOffs, negOK, err := evalFTSIndexedAST(ctx, rightNot.Inner, dict, cq, matcher, leafCursor)
			if err != nil {
				return nil, false, err
			}
			if leftOK && negOK {
				return differenceUint32(leftOffs, negOffs), true, nil
			}
			if leftOK {
				filtered, err := filterOffsetsSequentially(ctx, dict, cq, matcher, v, subStart, leftOffs)
				return filtered, err == nil, err
			}
			return nil, false, nil
		}

		leftOffs, leftOK, err := evalFTSIndexedAST(ctx, v.Left, dict, cq, matcher, leafCursor)
		if err != nil {
			return nil, false, err
		}
		if leftOK && len(leftOffs) == 0 {
			advanceLeafCursor(v.Right, leafCursor)
			return nil, true, nil
		}
		rightOffs, rightOK, err := evalFTSIndexedAST(ctx, v.Right, dict, cq, matcher, leafCursor)
		if err != nil {
			return nil, false, err
		}
		if leftOK && rightOK {
			return intersectUint32(leftOffs, rightOffs), true, nil
		}
		if leftOK {
			filtered, err := filterOffsetsSequentially(ctx, dict, cq, matcher, v, subStart, leftOffs)
			return filtered, err == nil, err
		}
		if rightOK {
			filtered, err := filterOffsetsSequentially(ctx, dict, cq, matcher, v, subStart, rightOffs)
			return filtered, err == nil, err
		}
		return nil, false, nil
	}
	return nil, false, nil
}

func advanceLeafCursor(expr pattern.Expr, cursor *int) {
	switch v := expr.(type) {
	case *pattern.AtomExpr:
		*cursor++
	case *pattern.NotExpr:
		advanceLeafCursor(v.Inner, cursor)
	case *pattern.BinaryExpr:
		advanceLeafCursor(v.Left, cursor)
		advanceLeafCursor(v.Right, cursor)
	}
}

func evalSubExprFlags(expr pattern.Expr, cursor *int, leafMatched []bool) bool {
	switch v := expr.(type) {
	case *pattern.AtomExpr:
		idx := *cursor
		*cursor++
		return leafMatched[idx]
	case *pattern.NotExpr:
		return !evalSubExprFlags(v.Inner, cursor, leafMatched)
	case *pattern.BinaryExpr:
		l := evalSubExprFlags(v.Left, cursor, leafMatched)
		r := evalSubExprFlags(v.Right, cursor, leafMatched)
		if v.Op == pattern.OpAnd {
			return l && r
		}
		return l || r
	}
	return false
}

func populateRecordLeafMatches(
	dict *tsd.Dictionary,
	rec *tsd.DefinitionRecord,
	cq *pattern.CompiledQuery,
	matcher *pattern.Matcher,
	leafMatched []bool,
	idxScratch []byte,
	rawScratch []byte,
) {
	numLeaves := cq.NumLeaves()
	clear(leafMatched[:numLeaves])
	remaining := numLeaves

	// Headwords in TSD1 are indexed as complete canonical entries (e.g. "Crossword Man" -> CROSSWORDMAN),
	// not split on spaces or hyphens.
	for _, hq := range rec.Headwords {
		if remaining == 0 {
			break
		}
		idxScratch = idxScratch[:0]
		overflow := false
		for _, b := range hq.RawHeadword {
			if li := dict.LetterIndex(b); li >= 0 {
				if len(idxScratch) >= tsd.MaxWordLengthLimit {
					overflow = true
					break
				}
				idxScratch = append(idxScratch, byte(li))
			}
		}
		if overflow || len(idxScratch) == 0 {
			continue
		}
		for leafIdx := 0; leafIdx < numLeaves; leafIdx++ {
			if !leafMatched[leafIdx] && matcher.MatchLeafToken(leafIdx, idxScratch, hq.RawHeadword) {
				leafMatched[leafIdx] = true
				remaining--
			}
		}
	}

	if remaining > 0 {
		plain := tsd.StripRTFForFTS(rec.RawBody)
		idxScratch = idxScratch[:0]
		rawScratch = rawScratch[:0]
		overflow := false
		for k := 0; k <= len(plain) && remaining > 0; k++ {
			var li int16 = -1
			var b byte
			if k < len(plain) {
				b = plain[k]
				li = dict.LetterIndex(b)
			}
			if li >= 0 {
				if !overflow {
					if len(idxScratch) >= tsd.MaxWordLengthLimit {
						overflow = true
					} else {
						idxScratch = append(idxScratch, byte(li))
						rawScratch = append(rawScratch, b)
					}
				}
			} else if len(idxScratch) > 0 || overflow {
				if !overflow {
					for leafIdx := 0; leafIdx < numLeaves; leafIdx++ {
						if !leafMatched[leafIdx] && matcher.MatchLeafToken(leafIdx, idxScratch, rawScratch) {
							leafMatched[leafIdx] = true
							remaining--
						}
					}
				}
				idxScratch = idxScratch[:0]
				rawScratch = rawScratch[:0]
				overflow = false
			}
		}
	}
}

func testDefinitionRecordSequential(
	dict *tsd.Dictionary,
	rec *tsd.DefinitionRecord,
	cq *pattern.CompiledQuery,
	matcher *pattern.Matcher,
	leafMatched []bool,
	idxScratch []byte,
	rawScratch []byte,
) bool {
	populateRecordLeafMatches(dict, rec, cq, matcher, leafMatched, idxScratch, rawScratch)
	return cq.EvaluateLeafFlags(leafMatched)
}

func filterOffsetsSequentially(
	ctx context.Context,
	dict *tsd.Dictionary,
	cq *pattern.CompiledQuery,
	matcher *pattern.Matcher,
	subExpr pattern.Expr,
	subLeafStart int,
	candidates []uint32,
) ([]uint32, error) {
	leafMatched := make([]bool, cq.NumLeaves())
	idxScratch := make([]byte, 0, tsd.MaxWordLengthLimit)
	rawScratch := make([]byte, 0, tsd.MaxWordLengthLimit)
	out := make([]uint32, 0, len(candidates))

	for _, off := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rec, err := dict.ReadDefinition(off)
		if err != nil {
			return nil, err
		}
		populateRecordLeafMatches(dict, rec, cq, matcher, leafMatched, idxScratch, rawScratch)
		cur := subLeafStart
		if evalSubExprFlags(subExpr, &cur, leafMatched) {
			out = append(out, off)
		}
	}
	return out, nil
}

func appendFTSMatchesFromRecord(
	dst []Match,
	dictIdx int,
	dict *tsd.Dictionary,
	rec *tsd.DefinitionRecord,
	req *Request,
	filter EntryTypeMask,
) []Match {
	activeFilter := filter & EntryAll
	for _, hq := range rec.Headwords {
		canonLen := 0
		for _, b := range hq.RawHeadword {
			if dict.IsLetter(b) {
				canonLen++
			}
		}
		if canonLen < req.MinLength || canonLen > req.MaxLength {
			continue
		}
		if ClassifyEntry(hq.RawHeadword, dict)&activeFilter == 0 {
			continue
		}
		var annot string
		if req.ShowAnnotations {
			annot = strings.TrimSpace(hq.Qualifier)
		}
		dst = append(dst, Match{
			Word:          hq.Headword,
			Annotation:    annot,
			Length:        canonLen,
			DictIndex:     dictIdx,
			DictName:      dict.Name(),
			LinkIndex:     -1,
			DefOffset:     rec.Offset,
			HasDefinition: true,
			rawWord:       append([]byte(nil), hq.RawHeadword...),
		})
	}
	return dst
}
