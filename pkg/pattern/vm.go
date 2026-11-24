package pattern

import "tisane/pkg/tsd"

const (
	maxMatcherSteps = 200_000
	maxMemoOps      = 256
	memoStride      = tsd.MaxWordLengthLimit + 2
)

// Matcher holds reusable per-goroutine scratch buffers for executing a CompiledQuery
// against dictionary entries with zero per-word heap allocations.
type Matcher struct {
	cq *CompiledQuery

	// Backtracking step budget & variable-free (opIdx, pos) failure memoization
	steps             int
	allowTotalLetters bool
	memoAtom          *compiledAtom
	failGen           uint32
	failStamp         [maxMemoOps * memoStride]uint32

	// Raw punctuation & case-exact prep buffers (populated lazily only when needsRawPrep is true)
	filteredRaw       [768]byte
	punctPrefixCounts [tsd.MaxWordLengthLimit + 2]int

	// Variable unification state (per leaf MatchEngine)
	varBound [10]int16 // -1 if unbound, else 0..255 letterIdx
	varUsed  [256]bool

	// Anagram solver scratch buffers
	avail        [256]int
	savedBalance [256]int
	hasBalance   bool
	balanceBuf   [256]byte
}

// NewMatcher allocates a reusable Matcher bound to cq.
// A single Matcher must not be used concurrently by multiple goroutines;
// create one Matcher per worker goroutine.
func (cq *CompiledQuery) NewMatcher() *Matcher {
	return &Matcher{cq: cq}
}

// MatchEntry tests a decoded RawEntry against the full boolean CompiledQuery.
func (m *Matcher) MatchEntry(entry tsd.RawEntry) bool {
	m.hasBalance = false
	m.allowTotalLetters = false
	L := len(entry.LetterIndices)
	if L <= 0 || L > tsd.MaxWordLengthLimit || !m.cq.compatLengths[L] {
		return false
	}
	return m.evalNode(m.cq.root, entry)
}

// MatchCanonicalToken tests a tokenized plain/RTF word (canonical letter indices + optional raw bytes)
// against leaf idx of the CompiledQuery (used by Sequential Full Text Search).
func (m *Matcher) MatchLeafToken(leafIdx int, letterIndices []byte, rawWordBytes []byte) bool {
	if leafIdx < 0 || leafIdx >= len(m.cq.leaves) {
		return false
	}
	leaf := m.cq.leaves[leafIdx]
	L := len(letterIndices)
	if L <= 0 || L > tsd.MaxWordLengthLimit || L < leaf.minLen || L > leaf.maxLen {
		return false
	}
	m.allowTotalLetters = true
	entry := tsd.RawEntry{
		WordBytes:     rawWordBytes,
		LetterIndices: letterIndices,
	}
	return m.matchAtom(leaf, entry, false)
}

// MatchLeafEntry tests a dictionary/FTS stream entry against leaf idx (used by Indexed Full Text Search).
func (m *Matcher) MatchLeafEntry(leafIdx int, entry tsd.RawEntry) bool {
	if leafIdx < 0 || leafIdx >= len(m.cq.leaves) {
		return false
	}
	leaf := m.cq.leaves[leafIdx]
	L := len(entry.LetterIndices)
	if L <= 0 || L > tsd.MaxWordLengthLimit || L < leaf.minLen || L > leaf.maxLen {
		return false
	}
	m.allowTotalLetters = false
	return m.matchAtom(leaf, entry, false)
}

// FormatBalance returns the "-OMITTED+ADDED" anagram balance string and balance letter count
// for the most recently matched entry ("" and 0 if not an anagram query or exact balance).
func (m *Matcher) FormatBalance() (balanceStr string, balanceLetters int) {
	if !m.hasBalance {
		return "", 0
	}
	dict := m.cq.dict
	numTotal := dict.NumTotalLetters()
	out := m.balanceBuf[:0]

	wroteMinus := false
	for li := 0; li < numTotal; li++ {
		cnt := m.savedBalance[li]
		if cnt < 0 {
			if !wroteMinus {
				out = append(out, '-')
				wroteMinus = true
			}
			ch := dict.CanonicalByteForIndex(li)
			for k := 0; k < -cnt; k++ {
				out = append(out, ch)
				balanceLetters++
			}
		}
	}

	wrotePlus := false
	for li := 0; li < numTotal; li++ {
		cnt := m.savedBalance[li]
		if cnt > 0 {
			if !wrotePlus {
				out = append(out, '+')
				wrotePlus = true
			}
			ch := dict.CanonicalByteForIndex(li)
			for k := 0; k < cnt; k++ {
				out = append(out, ch)
				balanceLetters++
			}
		}
	}

	if len(out) == 0 {
		return "", 0
	}
	return tsd.DecodeCP1252(out), balanceLetters
}

func (m *Matcher) evalNode(node compiledNode, entry tsd.RawEntry) bool {
	switch v := node.(type) {
	case *compiledBinary:
		if v.op == OpAnd {
			return m.evalNode(v.left, entry) && m.evalNode(v.right, entry)
		}
		return m.evalNode(v.left, entry) || m.evalNode(v.right, entry)
	case *compiledNot:
		return !m.evalNode(v.inner, entry)
	case *compiledLeaf:
		L := len(entry.LetterIndices)
		if L < v.atom.minLen || L > v.atom.maxLen {
			return false
		}
		recordBal := m.cq.opts.ComputeBalances && m.cq.singleAnag == v.atom
		return m.matchAtom(v.atom, entry, recordBal)
	}
	return false
}

func (m *Matcher) resetVars() {
	for i := range m.varBound {
		m.varBound[i] = -1
	}
	if m.cq.opts.ExclusiveVars {
		clear(m.varUsed[:])
	}
}

func (m *Matcher) prepRawWord(atom *compiledAtom, wordBytes []byte, L int) bool {
	if wordBytes == nil {
		return false
	}
	fLen := 0
	pCnt := 0
	letterPos := 0
	for _, b := range wordBytes {
		mode := atom.punctMode[b]
		if mode == 0xFE {
			if letterPos <= L {
				m.punctPrefixCounts[letterPos] = pCnt
			}
			letterPos++
			if fLen >= len(m.filteredRaw) {
				return false
			}
			m.filteredRaw[fLen] = b
			fLen++
		} else if mode == 0xFF {
			pCnt++
			if fLen >= len(m.filteredRaw) {
				return false
			}
			m.filteredRaw[fLen] = b
			fLen++
		}
	}
	if letterPos != L {
		return false
	}
	m.punctPrefixCounts[L] = pCnt
	return true
}

func (m *Matcher) matchAtom(atom *compiledAtom, entry tsd.RawEntry, recordBalance bool) bool {
	if atom.universal && !recordBalance {
		return true
	}
	word := entry.LetterIndices
	L := len(word)

	switch atom.fast {
	case fastFixedLetters:
		for _, chk := range atom.fixedLetChk {
			if word[chk.pos] != chk.letterIdx {
				return false
			}
		}
		return true

	case fastFixedMasks:
		for _, chk := range atom.fixedMskChk {
			if !chk.mask.Contains(word[chk.pos]) {
				return false
			}
		}
		return true

	case fastDistinctAnagram:
		if atom.anag.impossible && !m.allowTotalLetters {
			return false
		}
		numTotal := atom.dict.NumTotalLetters()
		clear(m.avail[:numTotal])
		for _, li := range word {
			m.avail[li]++
		}
		for _, reqLI := range atom.anag.distinctLIs {
			if m.avail[reqLI] == 0 {
				return false
			}
		}
		if recordBalance {
			m.savedBalance = m.avail
			for _, reqLI := range atom.anag.distinctLIs {
				m.savedBalance[reqLI]--
			}
			m.hasBalance = true
		}
		return true

	case fastMultiAnagram:
		if atom.anag.impossible && !m.allowTotalLetters {
			return false
		}
		numTotal := atom.dict.NumTotalLetters()
		clear(m.avail[:numTotal])
		for _, li := range word {
			m.avail[li]++
		}
		for _, req := range atom.anag.fixed {
			if m.avail[req.letterIdx] < req.mult {
				return false
			}
		}
		if recordBalance {
			m.savedBalance = m.avail
			for _, req := range atom.anag.fixed {
				m.savedBalance[req.letterIdx] -= req.mult
			}
			m.hasBalance = true
		}
		return true
	}

	// General VM path (handles '*', variables 0..9, sub-patterns, punctuation/escapes, and hybrid Template;Anagram)
	if atom.needsRawPrep {
		if !m.prepRawWord(atom, entry.WordBytes, L) {
			return false
		}
	}
	m.steps = 0
	if !atom.hasVars && len(atom.tmplOps) <= maxMemoOps {
		m.failGen++
		if m.failGen == 0 {
			clear(m.failStamp[:])
			m.failGen = 1
		}
		m.memoAtom = atom
	} else {
		m.memoAtom = nil
	}
	m.resetVars()
	return m.matchSliceRangeCont(atom, word, 0, L, 0, atom.needsRawPrep, recordBalance, nil)
}

func (m *Matcher) subMatchesWithEndPCnt(
	sub *compiledAtom,
	word []byte,
	subStart, subEnd int,
	startP, targetEndP int,
	outerCont func() bool,
) bool {
	orig := m.punctPrefixCounts[subEnd]
	m.punctPrefixCounts[subEnd] = targetEndP
	var wrappedCont func() bool
	if outerCont != nil {
		wrappedCont = func() bool {
			m.punctPrefixCounts[subEnd] = orig
			if outerCont() {
				return true
			}
			m.punctPrefixCounts[subEnd] = targetEndP
			return false
		}
	}
	ok := m.matchSliceRangeCont(sub, word, subStart, subEnd, startP, true, false, wrappedCont)
	m.punctPrefixCounts[subEnd] = orig
	return ok
}

func (m *Matcher) matchSliceRangeCont(
	atom *compiledAtom,
	word []byte,
	sliceStart, sliceEnd int,
	startPCnt int,
	rawActive bool,
	recordBalance bool,
	cont func() bool,
) bool {
	m.steps++
	if m.steps > maxMatcherSteps {
		return false
	}
	if !rawActive {
		if atom.universal && !recordBalance {
			if cont != nil {
				return cont()
			}
			return true
		}
		if !atom.hasTemplate {
			return m.solveAnagramCont(atom, word, sliceStart, sliceEnd, 0, false, false, recordBalance, cont)
		}
		return m.execTmplSubNoRawCont(atom, atom.tmplOps, 0, word[sliceStart:sliceEnd], 0, recordBalance, cont)
	}
	if !atom.hasTemplate {
		if len(atom.anag.flatSubs) == 0 && startPCnt != m.punctPrefixCounts[sliceEnd] {
			return false
		}
		return m.solveAnagramCont(atom, word, sliceStart, sliceEnd, startPCnt, true, false, recordBalance, cont)
	}
	return m.execTmplOpsRangeCont(atom, atom.tmplOps, 0, word, sliceStart, sliceStart, sliceEnd, startPCnt, true, recordBalance, cont)
}

func (m *Matcher) execTmplSubNoRawCont(
	atom *compiledAtom,
	ops []compiledTmplOp,
	opIdx int,
	word []byte,
	pos int,
	recordBalance bool,
	cont func() bool,
) bool {
	m.steps++
	if m.steps > maxMatcherSteps {
		return false
	}
	L := len(word)
	useMemo := cont == nil && atom == m.memoAtom && opIdx < maxMemoOps && pos <= tsd.MaxWordLengthLimit+1
	memoCell := 0
	if useMemo {
		memoCell = opIdx*memoStride + pos
		if m.failStamp[memoCell] == m.failGen {
			return false
		}
	}
	fail := func() bool {
		if useMemo {
			m.failStamp[memoCell] = m.failGen
		}
		return false
	}
	for opIdx < len(ops) {
		op := ops[opIdx]
		if L-pos < op.minAfter {
			return fail()
		}
		switch op.kind {
		case tOpDot:
			if pos >= L {
				return fail()
			}
			pos++
			opIdx++
		case tOpLetter:
			if pos >= L || int16(word[pos]) != op.letterIdx {
				return fail()
			}
			pos++
			opIdx++
		case tOpChoice:
			if pos >= L || !op.mask.Contains(word[pos]) {
				return fail()
			}
			pos++
			opIdx++
		case tOpVar:
			if pos >= L {
				return fail()
			}
			v := op.rawByte
			candLI := word[pos]
			if m.varBound[v] >= 0 {
				if uint8(m.varBound[v]) != candLI {
					return fail()
				}
				pos++
				opIdx++
				continue
			}
			if m.cq.opts.ExclusiveVars && m.varUsed[candLI] {
				return fail()
			}
			m.varBound[v] = int16(candLI)
			if m.cq.opts.ExclusiveVars {
				m.varUsed[candLI] = true
			}
			if m.execTmplSubNoRawCont(atom, ops, opIdx+1, word, pos+1, recordBalance, cont) {
				return true
			}
			m.varBound[v] = -1
			if m.cq.opts.ExclusiveVars {
				m.varUsed[candLI] = false
			}
			return fail()
		case tOpTailStar:
			newPos := L - op.minAfter
			if newPos < pos {
				return fail()
			}
			pos = newPos
			opIdx++
		case tOpStar:
			maxTake := (L - pos) - op.minAfter
			savedVars := m.varBound
			savedUsed := m.varUsed
			for k := 0; k <= maxTake; k++ {
				if m.execTmplSubNoRawCont(atom, ops, opIdx+1, word, pos+k, recordBalance, cont) {
					return true
				}
				m.varBound = savedVars
				m.varUsed = savedUsed
				if m.steps > maxMatcherSteps {
					return false
				}
			}
			return fail()
		case tOpSub:
			sub := op.sub
			maxSub := (L - pos) - op.minAfter
			if sub.maxLen < maxSub {
				maxSub = sub.maxLen
			}
			savedVars := m.varBound
			savedUsed := m.varUsed
			nextOpIdx := opIdx + 1
			for slen := sub.minLen; slen <= maxSub; slen++ {
				subEnd := pos + slen
				if sub.hasVars {
					stepCont := func() bool {
						return m.execTmplSubNoRawCont(atom, ops, nextOpIdx, word, subEnd, recordBalance, cont)
					}
					if m.matchSliceRangeCont(sub, word, pos, subEnd, 0, false, false, stepCont) {
						return true
					}
				} else if m.matchSliceRangeCont(sub, word, pos, subEnd, 0, false, false, nil) &&
					m.execTmplSubNoRawCont(atom, ops, nextOpIdx, word, subEnd, recordBalance, cont) {
					return true
				}
				m.varBound = savedVars
				m.varUsed = savedUsed
				if m.steps > maxMatcherSteps {
					return false
				}
			}
			return fail()
		default:
			return fail()
		}
	}
	if pos != L {
		return fail()
	}
	if atom.anag != nil {
		if m.solveAnagramCont(atom, word, 0, L, 0, false, true, recordBalance, cont) {
			return true
		}
		return fail()
	}
	if cont != nil {
		return cont()
	}
	return true
}

func (m *Matcher) execTmplOpsRangeCont(
	atom *compiledAtom,
	ops []compiledTmplOp,
	opIdx int,
	word []byte,
	sliceStart int,
	pos int,
	sliceEnd int,
	pCnt int,
	rawActive bool,
	recordBalance bool,
	cont func() bool,
) bool {
	m.steps++
	if m.steps > maxMatcherSteps {
		return false
	}
	for opIdx < len(ops) {
		op := ops[opIdx]
		if sliceEnd-pos < op.minAfter {
			return false
		}
		switch op.kind {
		case tOpDot:
			if pos >= sliceEnd || pCnt != m.punctPrefixCounts[pos] {
				return false
			}
			pos++
			opIdx++
		case tOpLetter:
			if pos >= sliceEnd || int16(word[pos]) != op.letterIdx || pCnt != m.punctPrefixCounts[pos] {
				return false
			}
			pos++
			opIdx++
		case tOpExactLetter:
			if pos >= sliceEnd || int16(word[pos]) != op.letterIdx ||
				pCnt != m.punctPrefixCounts[pos] || m.filteredRaw[pos+pCnt] != op.rawByte {
				return false
			}
			pos++
			opIdx++
		case tOpChoice:
			if pos >= sliceEnd || !op.mask.Contains(word[pos]) || pCnt != m.punctPrefixCounts[pos] {
				return false
			}
			pos++
			opIdx++
		case tOpPunct:
			if pCnt >= m.punctPrefixCounts[pos] || m.filteredRaw[pos+pCnt] != op.rawByte {
				return false
			}
			pCnt++
			opIdx++
		case tOpVar:
			if pos >= sliceEnd || pCnt != m.punctPrefixCounts[pos] {
				return false
			}
			v := op.rawByte
			candLI := word[pos]
			if m.varBound[v] >= 0 {
				if uint8(m.varBound[v]) != candLI {
					return false
				}
				pos++
				opIdx++
				continue
			}
			if m.cq.opts.ExclusiveVars && m.varUsed[candLI] {
				return false
			}
			m.varBound[v] = int16(candLI)
			if m.cq.opts.ExclusiveVars {
				m.varUsed[candLI] = true
			}
			if m.execTmplOpsRangeCont(atom, ops, opIdx+1, word, sliceStart, pos+1, sliceEnd, pCnt, true, recordBalance, cont) {
				return true
			}
			m.varBound[v] = -1
			if m.cq.opts.ExclusiveVars {
				m.varUsed[candLI] = false
			}
			return false
		case tOpTailStar:
			newPos := sliceEnd - op.minAfter
			if newPos < pos {
				return false
			}
			minP := pCnt
			if newPos > pos && m.punctPrefixCounts[newPos-1] > minP {
				minP = m.punctPrefixCounts[newPos-1]
			}
			maxP := m.punctPrefixCounts[newPos]
			savedVars, savedUsed := m.varBound, m.varUsed
			for tryP := minP; tryP <= maxP; tryP++ {
				if m.execTmplOpsRangeCont(atom, ops, opIdx+1, word, sliceStart, newPos, sliceEnd, tryP, true, recordBalance, cont) {
					return true
				}
				m.varBound, m.varUsed = savedVars, savedUsed
				if m.steps > maxMatcherSteps {
					return false
				}
			}
			return false
		case tOpStar:
			maxTake := (sliceEnd - pos) - op.minAfter
			savedVars, savedUsed := m.varBound, m.varUsed
			for k := 0; k <= maxTake; k++ {
				newPos := pos + k
				minP := pCnt
				if k > 0 && m.punctPrefixCounts[newPos-1] > minP {
					minP = m.punctPrefixCounts[newPos-1]
				}
				maxP := m.punctPrefixCounts[newPos]
				for tryP := minP; tryP <= maxP; tryP++ {
					if m.execTmplOpsRangeCont(atom, ops, opIdx+1, word, sliceStart, newPos, sliceEnd, tryP, true, recordBalance, cont) {
						return true
					}
					m.varBound, m.varUsed = savedVars, savedUsed
					if m.steps > maxMatcherSteps {
						return false
					}
				}
			}
			return false
		case tOpSub:
			sub := op.sub
			maxSub := (sliceEnd - pos) - op.minAfter
			if sub.maxLen < maxSub {
				maxSub = sub.maxLen
			}
			savedVars, savedUsed := m.varBound, m.varUsed
			nextOpIdx := opIdx + 1
			for slen := sub.minLen; slen <= maxSub; slen++ {
				subEnd := pos + slen
				minP := pCnt
				if slen > 0 && m.punctPrefixCounts[subEnd-1] > minP {
					minP = m.punctPrefixCounts[subEnd-1]
				}
				maxP := m.punctPrefixCounts[subEnd]
				for endP := minP; endP <= maxP; endP++ {
					tryEndP := endP
					if sub.hasVars {
						stepCont := func() bool {
							return m.execTmplOpsRangeCont(atom, ops, nextOpIdx, word, sliceStart, subEnd, sliceEnd, tryEndP, true, recordBalance, cont)
						}
						if m.subMatchesWithEndPCnt(sub, word, pos, subEnd, pCnt, tryEndP, stepCont) {
							return true
						}
					} else if m.subMatchesWithEndPCnt(sub, word, pos, subEnd, pCnt, tryEndP, nil) &&
						m.execTmplOpsRangeCont(atom, ops, nextOpIdx, word, sliceStart, subEnd, sliceEnd, tryEndP, true, recordBalance, cont) {
						return true
					}
					m.varBound, m.varUsed = savedVars, savedUsed
					if m.steps > maxMatcherSteps {
						return false
					}
				}
			}
			return false
		}
	}
	if pos != sliceEnd || pCnt != m.punctPrefixCounts[sliceEnd] {
		return false
	}
	if atom.anag != nil {
		return m.solveAnagramCont(atom, word, sliceStart, sliceEnd, m.punctPrefixCounts[sliceEnd], true, true, recordBalance, cont)
	}
	if cont != nil {
		return cont()
	}
	return true
}

func (m *Matcher) solveAnagramCont(
	atom *compiledAtom,
	fullWord []byte,
	sliceStart, sliceEnd int,
	startPCnt int,
	rawActive bool,
	hasTmpl bool,
	recordBalance bool,
	cont func() bool,
) bool {
	anag := atom.anag
	word := fullWord[sliceStart:sliceEnd]
	L := len(word)
	maxOmit := 0
	if hasTmpl && anag.minLen > L {
		maxOmit = anag.minLen - L
	}

	var avail [256]int
	var bal [256]int
	for _, li := range word {
		avail[li]++
		bal[li]++
	}

	numTotal := atom.dict.NumTotalLetters()
	subs := anag.flatSubs
	acceptWithRem := func(remLeft int) bool {
		if !hasTmpl && !anag.hasStar {
			remAvail := 0
			for li := 0; li < numTotal; li++ {
				remAvail += avail[li]
			}
			if remAvail != anag.dots {
				return false
			}
		}
		if recordBalance {
			m.savedBalance = bal
			m.hasBalance = true
		}
		if cont != nil {
			if cont() {
				return true
			}
			if recordBalance {
				m.hasBalance = false
			}
			return false
		}
		return true
	}

	// Fast path when there are no embedded sub-patterns: reduce fixed letters upfront.
	if len(subs) == 0 {
		if !hasTmpl && !anag.hasStar && L != anag.minLen {
			return false
		}
		remOmit := maxOmit
		for _, req := range anag.fixed {
			bal[req.letterIdx] -= req.mult
			used := avail[req.letterIdx]
			if used > req.mult {
				used = req.mult
			}
			omitted := req.mult - used
			remOmit -= omitted
			if remOmit < 0 {
				return false
			}
			avail[req.letterIdx] -= used
		}
		if !anag.hasVars && len(anag.choices) == 0 {
			return acceptWithRem(remOmit)
		}
		return m.solveAnagramVarsAndChoices(atom, &avail, &bal, remOmit, recordBalance, acceptWithRem)
	}

	var claimed [tsd.MaxWordLengthLimit + 2]bool
	var punctClaimed [768]bool
	totalPunct := 0
	punctBase := 0
	if rawActive && !hasTmpl {
		punctBase = startPCnt
		totalPunct = m.punctPrefixCounts[sliceEnd] - startPCnt
	}

	reduceFixedAndFinish := func(extraSubLen int, pConsumed int) bool {
		if rawActive && !hasTmpl && pConsumed != totalPunct {
			return false
		}
		effMin := anag.minLen + extraSubLen
		if !hasTmpl && !anag.hasStar && L != effMin {
			return false
		}
		remAfterFixed := maxOmit
		for _, req := range anag.fixed {
			bal[req.letterIdx] -= req.mult
			used := avail[req.letterIdx]
			if used > req.mult {
				used = req.mult
			}
			omitted := req.mult - used
			remAfterFixed -= omitted
			if remAfterFixed < 0 {
				return false
			}
			avail[req.letterIdx] -= used
		}
		if !anag.hasVars && len(anag.choices) == 0 {
			return acceptWithRem(remAfterFixed)
		}
		return m.solveAnagramVarsAndChoices(atom, &avail, &bal, remAfterFixed, recordBalance, acceptWithRem)
	}

	var matchSubs func(sIdx int, extraSubLen int, pConsumed int) bool
	matchSubs = func(sIdx int, extraSubLen int, pConsumed int) bool {
		m.steps++
		if m.steps > maxMatcherSteps {
			return false
		}
		if sIdx >= len(subs) {
			savedAvail := avail
			savedBal := bal
			if reduceFixedAndFinish(extraSubLen, pConsumed) {
				return true
			}
			avail = savedAvail
			bal = savedBal
			return false
		}
		sub := subs[sIdx]
		maxSubL := L
		if sub.maxLen < maxSubL {
			maxSubL = sub.maxLen
		}
		savedVars := m.varBound
		savedUsed := m.varUsed

		for slen := sub.minLen; slen <= maxSubL; slen++ {
			nextExtra := extraSubLen + (slen - sub.minLen)
			for start := 0; start+slen <= L; start++ {
				canClaim := true
				for k := start; k < start+slen; k++ {
					if claimed[k] {
						canClaim = false
						break
					}
				}
				if !canClaim {
					continue
				}
				validAvail := true
				for k := start; k < start+slen; k++ {
					li := word[k]
					avail[li]--
					if avail[li] < 0 {
						validAvail = false
					}
				}
				if !validAvail {
					for k := start; k < start+slen; k++ {
						avail[word[k]]++
					}
					continue
				}

				absStart := sliceStart + start
				absEnd := absStart + slen

				trySubPlacement := func(subStartP, subEndP int) bool {
					for k := start; k < start+slen; k++ {
						claimed[k] = true
						bal[word[k]]--
					}
					nextPConsumed := pConsumed + (subEndP - subStartP)
					stepNext := func() bool {
						return matchSubs(sIdx+1, nextExtra, nextPConsumed)
					}
					var matched bool
					if !rawActive {
						if sub.hasVars {
							matched = m.matchSliceRangeCont(sub, fullWord, absStart, absEnd, 0, false, false, stepNext)
						} else {
							matched = m.matchSliceRangeCont(sub, fullWord, absStart, absEnd, 0, false, false, nil) && stepNext()
						}
					} else {
						if sub.hasVars {
							matched = m.subMatchesWithEndPCnt(sub, fullWord, absStart, absEnd, subStartP, subEndP, stepNext)
						} else {
							matched = m.subMatchesWithEndPCnt(sub, fullWord, absStart, absEnd, subStartP, subEndP, nil) && stepNext()
						}
					}
					for k := start; k < start+slen; k++ {
						claimed[k] = false
						bal[word[k]]++
					}
					return matched
				}

				if !rawActive {
					if trySubPlacement(0, 0) {
						return true
					}
				} else {
					minStartP := punctBase
					if absStart > sliceStart && m.punctPrefixCounts[absStart-1] > minStartP {
						minStartP = m.punctPrefixCounts[absStart-1]
					}
					maxStartP := m.punctPrefixCounts[absStart]
					maxEndP := m.punctPrefixCounts[absEnd]
					for sp := minStartP; sp <= maxStartP; sp++ {
						minEndP := sp
						if slen > 0 && m.punctPrefixCounts[absEnd-1] > minEndP {
							minEndP = m.punctPrefixCounts[absEnd-1]
						}
						for ep := minEndP; ep <= maxEndP; ep++ {
							pFree := true
							for pi := sp; pi < ep; pi++ {
								if punctClaimed[pi] {
									pFree = false
									break
								}
							}
							if !pFree {
								continue
							}
							for pi := sp; pi < ep; pi++ {
								punctClaimed[pi] = true
							}
							ok := trySubPlacement(sp, ep)
							for pi := sp; pi < ep; pi++ {
								punctClaimed[pi] = false
							}
							if ok {
								return true
							}
						}
					}
				}

				for k := start; k < start+slen; k++ {
					avail[word[k]]++
				}
				m.varBound = savedVars
				m.varUsed = savedUsed
				if m.steps > maxMatcherSteps {
					return false
				}
			}
		}
		return false
	}

	return matchSubs(0, 0, 0)
}

func (m *Matcher) solveAnagramVarsAndChoices(
	atom *compiledAtom,
	avail *[256]int,
	bal *[256]int,
	remOmit int,
	recordBalance bool,
	accept func(remLeft int) bool,
) bool {
	anag := atom.anag
	numLetters := atom.dict.NumUsedLetters()
	if m.allowTotalLetters {
		numLetters = atom.dict.NumTotalLetters()
	}
	choiceGroups := anag.choices

	var matchChoices func(cIdx int, copyIdx int, minLI int, rem int) bool
	matchChoices = func(cIdx int, copyIdx int, minLI int, rem int) bool {
		m.steps++
		if m.steps > maxMatcherSteps {
			return false
		}
		if cIdx >= len(choiceGroups) {
			return accept(rem)
		}
		cr := choiceGroups[cIdx]
		if copyIdx >= cr.mult {
			return matchChoices(cIdx+1, 0, 0, rem)
		}
		mask := cr.mask
		for li := minLI; li < numLetters; li++ {
			if avail[li] > 0 && mask.Contains(uint8(li)) {
				avail[li]--
				bal[li]--
				if matchChoices(cIdx, copyIdx+1, li, rem) {
					return true
				}
				avail[li]++
				bal[li]++
				if m.steps > maxMatcherSteps {
					return false
				}
			}
		}
		needOmit := cr.mult - copyIdx
		if rem >= needOmit {
			if matchChoices(cIdx+1, 0, 0, rem-needOmit) {
				return true
			}
		}
		return false
	}

	var matchVars func(vIdx int, rem int) bool
	matchVars = func(vIdx int, rem int) bool {
		m.steps++
		if m.steps > maxMatcherSteps {
			return false
		}
		for vIdx < 10 && anag.varMult[vIdx] == 0 {
			vIdx++
		}
		if vIdx >= 10 {
			return matchChoices(0, 0, 0, rem)
		}
		mult := anag.varMult[vIdx]
		if bound := m.varBound[vIdx]; bound >= 0 {
			li := int(bound)
			used := avail[li]
			if used > mult {
				used = mult
			}
			omitted := mult - used
			if rem < omitted {
				return false
			}
			avail[li] -= used
			bal[li] -= mult
			ok := matchVars(vIdx+1, rem-omitted)
			avail[li] += used
			bal[li] += mult
			return ok
		}

		// Unbound variable: try binding to each candidate letter index.
		// When used == 0 (the entire unbound variable is omitted), no letter in the word bound it,
		// so do not fabricate a negative balance for an arbitrary letter.
		triedZeroAvail := false
		for li := 0; li < numLetters; li++ {
			if m.cq.opts.ExclusiveVars && m.varUsed[li] {
				continue
			}
			used := avail[li]
			if used == 0 && !m.cq.opts.ExclusiveVars {
				if triedZeroAvail {
					continue
				}
				triedZeroAvail = true
			}
			if used > mult {
				used = mult
			}
			omitted := mult - used
			if rem < omitted {
				continue
			}
			balSub := mult
			if used == 0 {
				balSub = 0
			}
			m.varBound[vIdx] = int16(li)
			if m.cq.opts.ExclusiveVars {
				m.varUsed[li] = true
			}
			avail[li] -= used
			bal[li] -= balSub

			if matchVars(vIdx+1, rem-omitted) {
				return true
			}

			avail[li] += used
			bal[li] += balSub
			m.varBound[vIdx] = -1
			if m.cq.opts.ExclusiveVars {
				m.varUsed[li] = false
			}
			if m.steps > maxMatcherSteps {
				return false
			}
		}
		return false
	}

	return matchVars(0, remOmit)
}
