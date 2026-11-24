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
	steps     int
	memoAtom  *compiledAtom
	failGen   uint32
	failStamp [maxMemoOps * memoStride]uint32

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
	L := len(entry.LetterIndices)
	if L <= 0 || L > tsd.MaxWordLengthLimit || !m.cq.compatLengths[L] {
		return false
	}
	m.hasBalance = false
	return m.evalNode(m.cq.root, entry)
}

// MatchCanonicalToken tests a tokenized plain/RTF word (canonical letter indices + optional raw bytes)
// against leaf idx of the CompiledQuery (used by Sequential Full Text Search).
func (m *Matcher) MatchLeafToken(leafIdx int, letterIndices []byte, rawWordBytes []byte) bool {
	leaf := m.cq.leaves[leafIdx]
	L := len(letterIndices)
	if L < leaf.minLen || L > leaf.maxLen {
		return false
	}
	entry := tsd.RawEntry{
		WordBytes:     rawWordBytes,
		LetterIndices: letterIndices,
	}
	return m.matchAtom(leaf, entry, false)
}

// MatchLeafEntry tests a dictionary/FTS stream entry against leaf idx (used by Indexed Full Text Search).
func (m *Matcher) MatchLeafEntry(leafIdx int, entry tsd.RawEntry) bool {
	leaf := m.cq.leaves[leafIdx]
	L := len(entry.LetterIndices)
	if L < leaf.minLen || L > leaf.maxLen {
		return false
	}
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

	// Pass 0: negative entries (-OMITTED)
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

	// Pass 1: positive entries (+ADDED)
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
	return string(out), balanceLetters
}

func (m *Matcher) evalNode(node compiledNode, entry tsd.RawEntry) bool {
	switch v := node.(type) {
	case *compiledBinary:
		if v.op == OpAnd {
			// Short-circuit AND
			return m.evalNode(v.left, entry) && m.evalNode(v.right, entry)
		}
		// Short-circuit OR
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
	for letterPos <= L {
		m.punctPrefixCounts[letterPos] = pCnt
		letterPos++
	}
	return true
}

func (m *Matcher) matchAtom(atom *compiledAtom, entry tsd.RawEntry, recordBalance bool) bool {
	if atom.universal {
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
		if atom.anag.impossible {
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
		if atom.anag.impossible {
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
	return m.matchSliceRange(atom, word, 0, L, 0, atom.needsRawPrep, recordBalance)
}

func (m *Matcher) subMatchesWithEndPCnt(sub *compiledAtom, word []byte, subStart, subEnd, startP, targetEndP int) bool {
	// Temporarily adjust punctPrefixCounts[subEnd] to targetEndP while matching sub, then restore!
	orig := m.punctPrefixCounts[subEnd]
	m.punctPrefixCounts[subEnd] = targetEndP
	ok := m.matchSliceRange(sub, word, subStart, subEnd, startP, true, false)
	m.punctPrefixCounts[subEnd] = orig
	return ok
}

func (m *Matcher) matchSliceRange(
	atom *compiledAtom,
	word []byte,
	sliceStart, sliceEnd int,
	startPCnt int,
	rawActive bool,
	recordBalance bool,
) bool {
	m.steps++
	if m.steps > maxMatcherSteps {
		return false
	}
	subWord := word[sliceStart:sliceEnd]
	if !rawActive {
		if atom.universal && !recordBalance {
			return true
		}
		if !atom.hasTemplate {
			return m.solveAnagram(atom, subWord, false, recordBalance)
		}
		return m.execTmplSubNoRaw(atom, atom.tmplOps, 0, subWord, 0, recordBalance)
	}
	// When rawActive is true and sliceStart > 0, shift filteredRaw/punctPrefixCounts or pass sliceStart:
	if !atom.hasTemplate {
		if startPCnt != m.punctPrefixCounts[sliceEnd] {
			return false
		}
		return m.solveAnagram(atom, subWord, false, recordBalance)
	}
	return m.execTmplOpsRange(atom, atom.tmplOps, 0, word, sliceStart, sliceStart, sliceEnd, startPCnt, true, recordBalance)
}

func (m *Matcher) execTmplSubNoRaw(
	atom *compiledAtom,
	ops []compiledTmplOp,
	opIdx int,
	word []byte,
	pos int,
	recordBalance bool,
) bool {
	m.steps++
	if m.steps > maxMatcherSteps {
		return false
	}
	L := len(word)
	useMemo := atom == m.memoAtom && opIdx < maxMemoOps && pos <= tsd.MaxWordLengthLimit+1
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
	m.steps++
	if m.steps > maxMatcherSteps {
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
			if m.execTmplSubNoRaw(atom, ops, opIdx+1, word, pos+1, recordBalance) {
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
				if m.execTmplSubNoRaw(atom, ops, opIdx+1, word, pos+k, recordBalance) {
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
			for slen := sub.minLen; slen <= maxSub; slen++ {
				if m.matchSliceRange(sub, word, pos, pos+slen, 0, false, false) &&
					m.execTmplSubNoRaw(atom, ops, opIdx+1, word, pos+slen, recordBalance) {
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
		if m.solveAnagram(atom, word, true, recordBalance) {
			return true
		}
		return fail()
	}
	return true
}

func (m *Matcher) execTmplOpsRange(
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
			if m.execTmplOpsRange(atom, ops, opIdx+1, word, sliceStart, pos+1, sliceEnd, pCnt, true, recordBalance) {
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
				if m.execTmplOpsRange(atom, ops, opIdx+1, word, sliceStart, newPos, sliceEnd, tryP, true, recordBalance) {
					return true
				}
				m.varBound, m.varUsed = savedVars, savedUsed
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
					if m.execTmplOpsRange(atom, ops, opIdx+1, word, sliceStart, newPos, sliceEnd, tryP, true, recordBalance) {
						return true
					}
					m.varBound, m.varUsed = savedVars, savedUsed
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
			for slen := sub.minLen; slen <= maxSub; slen++ {
				subEnd := pos + slen
				minP := pCnt
				if slen > 0 && m.punctPrefixCounts[subEnd-1] > minP {
					minP = m.punctPrefixCounts[subEnd-1]
				}
				maxP := m.punctPrefixCounts[subEnd]
				for endP := minP; endP <= maxP; endP++ {
					if m.subMatchesWithEndPCnt(sub, word, pos, subEnd, pCnt, endP) &&
						m.execTmplOpsRange(atom, ops, opIdx+1, word, sliceStart, subEnd, sliceEnd, endP, true, recordBalance) {
						return true
					}
					m.varBound, m.varUsed = savedVars, savedUsed
				}
			}
			return false
		}
	}
	if pos != sliceEnd || pCnt != m.punctPrefixCounts[sliceEnd] {
		return false
	}
	if atom.anag != nil {
		return m.solveAnagram(atom, word[sliceStart:sliceEnd], true, recordBalance)
	}
	return true
}

func (m *Matcher) solveAnagram(atom *compiledAtom, word []byte, hasTmpl bool, recordBalance bool) bool {
	anag := atom.anag
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

	// Fast deterministic reduction of fixed anagram letters before any backtracking!
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

	// Flatten embedded sub-patterns and choice lists if present.
	hasVars := false
	for _, mult := range anag.varMult {
		if mult > 0 {
			hasVars = true
			break
		}
	}
	if !hasVars && len(anag.choices) == 0 && len(anag.subs) == 0 {
		if recordBalance {
			m.savedBalance = bal
			m.hasBalance = true
		}
		return true
	}

	var claimed [tsd.MaxWordLengthLimit + 2]bool
	var subs []*compiledAtom
	for _, sr := range anag.subs {
		for k := 0; k < sr.mult; k++ {
			subs = append(subs, sr.sub)
		}
	}
	numUsed := atom.dict.NumUsedLetters()
	choiceGroups := anag.choices

	var matchChoices func(cIdx int, copyIdx int, minLI int, rem int) bool
	matchChoices = func(cIdx int, copyIdx int, minLI int, rem int) bool {
		m.steps++
		if m.steps > maxMatcherSteps {
			return false
		}
		if cIdx >= len(choiceGroups) {
			if recordBalance {
				m.savedBalance = bal
				m.hasBalance = true
			}
			return true
		}
		cr := choiceGroups[cIdx]
		if copyIdx >= cr.mult {
			return matchChoices(cIdx+1, 0, 0, rem)
		}
		mask := cr.mask
		for li := minLI; li < numUsed; li++ {
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

		// Try binding unbound variable vIdx to each candidate letter index.
		triedZeroAvail := false
		for li := 0; li < numUsed; li++ {
			if m.cq.opts.ExclusiveVars && m.varUsed[li] {
				continue
			}
			used := avail[li]
			if used == 0 && !recordBalance && !m.cq.opts.ExclusiveVars {
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
			m.varBound[vIdx] = int16(li)
			if m.cq.opts.ExclusiveVars {
				m.varUsed[li] = true
			}
			avail[li] -= used
			bal[li] -= mult

			if matchVars(vIdx+1, rem-omitted) {
				return true
			}

			avail[li] += used
			bal[li] += mult
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

	var matchSubs func(sIdx int, rem int) bool
	matchSubs = func(sIdx int, rem int) bool {
		m.steps++
		if m.steps > maxMatcherSteps {
			return false
		}
		if sIdx >= len(subs) {
			return matchVars(0, rem)
		}
		sub := subs[sIdx]
		maxSubL := L
		if sub.maxLen < maxSubL {
			maxSubL = sub.maxLen
		}
		savedVars := m.varBound
		savedUsed := m.varUsed

		for slen := sub.minLen; slen <= maxSubL; slen++ {
			for start := 0; start+slen <= L; start++ {
				// Check slice [start, start+slen) is unclaimed and its letters are still available in avail[].
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
				// Temporarily consume slice letters from avail[] and check non-negativity.
				validAvail := true
				for k := start; k < start+slen; k++ {
					li := word[k]
					avail[li]--
					if avail[li] < 0 {
						validAvail = false
					}
				}
				if validAvail && m.matchSliceRange(sub, word, start, start+slen, 0, false, false) {
					for k := start; k < start+slen; k++ {
						claimed[k] = true
						bal[word[k]]--
					}
					if matchSubs(sIdx+1, rem) {
						return true
					}
					for k := start; k < start+slen; k++ {
						claimed[k] = false
						bal[word[k]]++
					}
				}
				for k := start; k < start+slen; k++ {
					avail[word[k]]++
				}
				m.varBound = savedVars
				m.varUsed = savedUsed
			}
		}

		// Try omitting the embedded sub-pattern if omission budget permits.
		if rem >= sub.minLen && sub.minLen > 0 {
			if matchSubs(sIdx+1, rem-sub.minLen) {
				return true
			}
		}
		return false
	}

	return matchSubs(0, remOmit)
}
