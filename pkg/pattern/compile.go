package pattern

import (
	"errors"
	"sort"

	"tisane/pkg/tsd"
)

const unboundedMaxLen = 15000

// LetterBitmask represents a set of canonical letter indices (0..255) in 4 uint64 words.
type LetterBitmask [4]uint64

func (m *LetterBitmask) Set(li uint8) {
	m[li>>6] |= 1 << (li & 63)
}

func (m *LetterBitmask) Clear(li uint8) {
	m[li>>6] &^= 1 << (li & 63)
}

func (m LetterBitmask) Contains(li uint8) bool {
	return (m[li>>6]>>(li&63))&1 != 0
}

func (m LetterBitmask) IsEmpty() bool {
	return (m[0] | m[1] | m[2] | m[3]) == 0
}

// CompileOptions controls dictionary-bound pattern compilation.
type CompileOptions struct {
	MinLength               int  // Shortest word length to match (1..100, default 1)
	MaxLength               int  // Longest word length to match (1..100, default 100)
	ExclusiveVars           bool // Require variables 0..9 to bind distinct canonical letters
	IgnoreUnusedPunctuation bool // Ignore dictionary punctuation bytes not mentioned in template
	ComputeBalances         bool // Compute -OMITTED+ADDED anagram balance strings on match
}

// DefaultCompileOptions returns standard Tisane compilation defaults.
func DefaultCompileOptions() CompileOptions {
	return CompileOptions{
		MinLength:               1,
		MaxLength:               tsd.MaxWordLengthLimit,
		IgnoreUnusedPunctuation: true,
		ComputeBalances:         true,
	}
}

type fastKind uint8

const (
	fastGeneral fastKind = iota
	fastFixedLetters
	fastFixedMasks
	fastDistinctAnagram
	fastMultiAnagram
)

type fixedLetterCheck struct {
	pos       int
	letterIdx uint8
	score     int
}

type fixedMaskCheck struct {
	pos   int
	mask  LetterBitmask
	score int
}

type anagFixedReq struct {
	letterIdx uint8
	mult      int
}

type anagChoiceReq struct {
	mask LetterBitmask
	mult int
}

type anagSubReq struct {
	sub  *compiledAtom
	mult int
}

type tmplOpKind uint8

const (
	tOpDot tmplOpKind = iota + 1
	tOpLetter
	tOpExactLetter
	tOpChoice
	tOpVar
	tOpPunct
	tOpStar
	tOpTailStar
	tOpSub
)

type compiledTmplOp struct {
	kind      tmplOpKind
	letterIdx int16         // >=0 for tOpLetter/tOpExactLetter, -1 if unmatchable letter
	rawByte   byte          // raw CP1252 byte for tOpExactLetter, tOpPunct, or var 0..9 for tOpVar
	mask      LetterBitmask // for tOpChoice
	sub       *compiledAtom // for tOpSub
	minAfter  int           // minimum canonical letters required by all subsequent ops in this template
}

type compiledAnagram struct {
	fixed       []anagFixedReq
	distinctLIs []uint8 // populated when all fixed have mult == 1
	varMult     [10]int
	choices     []anagChoiceReq
	subs        []anagSubReq
	flatSubs    []*compiledAtom
	hasVars     bool
	dots        int
	hasStar     bool
	specCount   int
	minLen      int
	maxExtra    int  // additional letters allowed by variable-length embedded sub-patterns
	impossible  bool // true if a required letter does not exist in dict's used letters and cannot be omitted
}

type compiledAtom struct {
	dict *tsd.Dictionary
	opts CompileOptions

	hasTemplate bool
	hasVars     bool
	tmplMinLen  int
	tmplMaxLen  int
	tmplOps     []compiledTmplOp

	needsRawPrep bool
	punctMode    [256]uint8 // 0x00=ignored, 0xFE=letter, 0xFF=active punctuation

	anag *compiledAnagram

	minLen    int
	maxLen    int
	universal bool // true if matches every word in [minLen..maxLen] unconditionally

	fast        fastKind
	fixedLetChk []fixedLetterCheck
	fixedMskChk []fixedMaskCheck
}

type compiledNode interface {
	canBeTrueAt(L int) bool
	canBeFalseAt(L int) bool
}

type compiledBinary struct {
	op          LogicOp
	left, right compiledNode
}

func (b *compiledBinary) canBeTrueAt(L int) bool {
	if b.op == OpAnd {
		return b.left.canBeTrueAt(L) && b.right.canBeTrueAt(L)
	}
	return b.left.canBeTrueAt(L) || b.right.canBeTrueAt(L)
}

func (b *compiledBinary) canBeFalseAt(L int) bool {
	if b.op == OpAnd {
		return b.left.canBeFalseAt(L) || b.right.canBeFalseAt(L)
	}
	return b.left.canBeFalseAt(L) && b.right.canBeFalseAt(L)
}

type compiledNot struct {
	inner compiledNode
}

func (n *compiledNot) canBeTrueAt(L int) bool  { return n.inner.canBeFalseAt(L) }
func (n *compiledNot) canBeFalseAt(L int) bool { return n.inner.canBeTrueAt(L) }

type compiledLeaf struct {
	idx  int
	atom *compiledAtom
}

func (l *compiledLeaf) canBeTrueAt(L int) bool {
	if L < l.atom.minLen || L > l.atom.maxLen {
		return false
	}
	if l.atom.anag != nil && l.atom.anag.impossible {
		maxOmit := 0
		if l.atom.hasTemplate && l.atom.anag.minLen > L {
			maxOmit = l.atom.anag.minLen - L
		}
		if maxOmit == 0 {
			return false
		}
	}
	return true
}

func (l *compiledLeaf) canBeFalseAt(L int) bool {
	if L < l.atom.minLen || L > l.atom.maxLen {
		return true
	}
	return !l.atom.universal
}

// CompiledQuery is a dictionary-bound executable matcher for a ParsedQuery.
// CompiledQuery is immutable; use NewMatcher() per goroutine for zero-allocation matching.
type CompiledQuery struct {
	dict          *tsd.Dictionary
	opts          CompileOptions
	root          compiledNode
	leaves        []*compiledAtom
	singleAnag    *compiledAtom // non-nil when query is a single positive anagram/hybrid leaf
	compatLengths [tsd.MaxWordLengthLimit + 1]bool
	minCompatLen  int
	maxCompatLen  int
}

// Validate parses and validates raw against opts and zero or more target dictionaries,
// returning live guidance suitable for GUI/REPL display.
func Validate(raw string, parseOpts ParseOptions, compOpts CompileOptions, dicts ...*tsd.Dictionary) ValidationResult {
	pq, err := Parse(raw, parseOpts)
	if err != nil {
		var se *SyntaxError
		if errors.As(err, &se) {
			return ValidationResult{
				Valid:   false,
				Code:    se.Code,
				Message: se.Error(),
				Span:    se.Span,
			}
		}
		return ValidationResult{
			Valid:   false,
			Code:    ErrEmpty,
			Message: err.Error(),
		}
	}

	if compOpts.MinLength <= 0 {
		compOpts.MinLength = 1
	}
	if compOpts.MaxLength <= 0 || compOpts.MaxLength > tsd.MaxWordLengthLimit {
		compOpts.MaxLength = tsd.MaxWordLengthLimit
	}
	if compOpts.MinLength > compOpts.MaxLength {
		return ValidationResult{
			Valid:      true,
			Code:       WarnIncompatibleLengths,
			Message:    WarnIncompatibleLengths.Message(),
			HasWarning: true,
		}
	}

	if len(dicts) == 0 {
		minL, maxL := estimateASTLengths(pq.Root, compOpts.MinLength, compOpts.MaxLength)
		if minL == 0 {
			return ValidationResult{
				Valid:      true,
				Code:       WarnIncompatibleLengths,
				Message:    WarnIncompatibleLengths.Message(),
				Span:       pq.Root.ExprSpan(),
				HasWarning: true,
			}
		}
		return ValidationResult{
			Valid:            true,
			Code:             StatusValid,
			Message:          StatusValid.Message(),
			MinMatchedLength: minL,
			MaxMatchedLength: maxL,
		}
	}

	globalMin := 0
	globalMax := 0
	for _, d := range dicts {
		if d == nil {
			continue
		}
		cq, cerr := Compile(pq, d, compOpts)
		if cerr != nil {
			var se *SyntaxError
			if errors.As(cerr, &se) {
				return ValidationResult{
					Valid:   false,
					Code:    se.Code,
					Message: se.Error(),
					Span:    se.Span,
				}
			}
			return ValidationResult{Valid: false, Code: ErrInvalidChoiceList, Message: cerr.Error()}
		}
		if cq.minCompatLen > 0 {
			if globalMin == 0 || cq.minCompatLen < globalMin {
				globalMin = cq.minCompatLen
			}
			if cq.maxCompatLen > globalMax {
				globalMax = cq.maxCompatLen
			}
		}
	}

	if globalMin == 0 {
		return ValidationResult{
			Valid:      true,
			Code:       WarnIncompatibleLengths,
			Message:    WarnIncompatibleLengths.Message(),
			Span:       pq.Root.ExprSpan(),
			HasWarning: true,
		}
	}
	return ValidationResult{
		Valid:            true,
		Code:             StatusValid,
		Message:          StatusValid.Message(),
		MinMatchedLength: globalMin,
		MaxMatchedLength: globalMax,
	}
}

func estimateAtomBounds(a *AtomicPattern) (minL, maxL int, univ bool) {
	hasTmpl := len(a.Template) > 0
	tMin, tMax := 0, 0
	allWild := true
	if hasTmpl {
		for _, it := range a.Template {
			switch it.Kind {
			case ItemDot:
				tMin++
				if tMax < unboundedMaxLen {
					tMax++
				}
			case ItemLetter, ItemExactLetter, ItemChoice, ItemVariable:
				allWild = false
				tMin++
				if tMax < unboundedMaxLen {
					tMax++
				}
			case ItemPunct:
				allWild = false
			case ItemStar:
				tMax = unboundedMaxLen
			case ItemSubPattern:
				sMin, sMax, sUniv := estimateAtomBounds(it.Sub)
				if !sUniv {
					allWild = false
				}
				tMin += sMin
				if tMax >= unboundedMaxLen || sMax >= unboundedMaxLen {
					tMax = unboundedMaxLen
				} else {
					tMax += sMax
				}
			}
		}
	}

	if a.Anagram == nil {
		return tMin, tMax, hasTmpl && allWild
	}

	aMin := 0
	aSpec := 0
	aDots := 0
	aMaxExtra := 0
	aHasStar := false
	aHasSubs := false
	for _, it := range a.Anagram.Items {
		switch it.Kind {
		case ItemLetter, ItemChoice, ItemVariable:
			aMin++
			aSpec++
		case ItemDot:
			aMin++
			aDots++
		case ItemStar:
			aHasStar = true
		case ItemSubPattern:
			aHasSubs = true
			sMin, sMax, _ := estimateAtomBounds(it.Sub)
			aMin += sMin
			aSpec += sMin
			if aMaxExtra >= unboundedMaxLen || sMax >= unboundedMaxLen {
				aMaxExtra = unboundedMaxLen
			} else {
				aMaxExtra += sMax - sMin
			}
		}
	}
	anagMax := unboundedMaxLen
	if !aHasStar && aMaxExtra < unboundedMaxLen {
		anagMax = min(aMin+aMaxExtra, unboundedMaxLen)
	}
	if !hasTmpl {
		univ := !aHasSubs && ((aHasStar && aSpec == 0) || (!aHasStar && aSpec == 0 && aDots > 0))
		return aMin, anagMax, univ
	}
	minOut, maxOut := tMin, tMax
	if tMax > tMin && anagMax < unboundedMaxLen && tMin < anagMax && anagMax < maxOut {
		maxOut = anagMax
	}
	return minOut, maxOut, false
}

func estimateASTLengths(root Expr, minBound, maxBound int) (minMatched, maxMatched int) {
	var build func(e Expr) compiledNode
	build = func(e Expr) compiledNode {
		switch v := e.(type) {
		case *BinaryExpr:
			return &compiledBinary{op: v.Op, left: build(v.Left), right: build(v.Right)}
		case *NotExpr:
			return &compiledNot{inner: build(v.Inner)}
		case *AtomExpr:
			mn, mx, univ := estimateAtomBounds(v.Atom)
			return &compiledLeaf{atom: &compiledAtom{
				hasTemplate: len(v.Atom.Template) > 0,
				minLen:      mn,
				maxLen:      mx,
				universal:   univ,
			}}
		}
		return nil
	}
	node := build(root)
	for L := minBound; L <= maxBound; L++ {
		if node.canBeTrueAt(L) {
			if minMatched == 0 {
				minMatched = L
			}
			maxMatched = L
		}
	}
	return minMatched, maxMatched
}

// Compile binds a ParsedQuery to a specific *tsd.Dictionary and CompileOptions.
func Compile(pq *ParsedQuery, dict *tsd.Dictionary, opts CompileOptions) (*CompiledQuery, error) {
	if pq == nil || pq.Root == nil || dict == nil {
		return nil, &SyntaxError{Code: ErrEmpty}
	}
	if opts.MinLength <= 0 {
		opts.MinLength = 1
	}
	if opts.MaxLength <= 0 || opts.MaxLength > tsd.MaxWordLengthLimit {
		opts.MaxLength = tsd.MaxWordLengthLimit
	}

	cq := &CompiledQuery{
		dict: dict,
		opts: opts,
	}

	// Note: if Parse injected a synthetic right-hand enumeration branch, recognize it so
	// enumeration punctuation always ignores unused punctuation (allowing apostrophes in "4,3" -> "don't ask").
	var compileExpr func(e Expr, isEnumBranch bool) (compiledNode, error)
	compileExpr = func(e Expr, isEnumBranch bool) (compiledNode, error) {
		switch v := e.(type) {
		case *BinaryExpr:
			left, err := compileExpr(v.Left, false)
			if err != nil {
				return nil, err
			}
			right, err := compileExpr(v.Right, isEnumBranch)
			if err != nil {
				return nil, err
			}
			return &compiledBinary{op: v.Op, left: left, right: right}, nil
		case *NotExpr:
			inner, err := compileExpr(v.Inner, false)
			if err != nil {
				return nil, err
			}
			return &compiledNot{inner: inner}, nil
		case *AtomExpr:
			atomOpts := opts
			if isEnumBranch {
				atomOpts.IgnoreUnusedPunctuation = true
			}
			ca, err := compileAtomic(v.Atom, dict, atomOpts, true)
			if err != nil {
				return nil, err
			}
			idx := len(cq.leaves)
			cq.leaves = append(cq.leaves, ca)
			return &compiledLeaf{idx: idx, atom: ca}, nil
		default:
			return nil, &SyntaxError{Code: ErrEmpty}
		}
	}

	// Detect if top-level root has a synthetic enumeration RHS injected by Parse.
	var err error
	if bin, ok := pq.Root.(*BinaryExpr); ok && bin.Op == OpAnd && bin.SyntheticEnum {
		leftNode, lerr := compileExpr(bin.Left, false)
		if lerr != nil {
			return nil, lerr
		}
		rightNode, rerr := compileExpr(bin.Right, true)
		if rerr != nil {
			return nil, rerr
		}
		cq.root = &compiledBinary{op: OpAnd, left: leftNode, right: rightNode}
		// Preserve single-anagram balance display when user query was a single anagram leaf + --enum!
		if len(cq.leaves) == 2 && cq.leaves[0].anag != nil {
			if _, isAtom := bin.Left.(*AtomExpr); isAtom {
				cq.singleAnag = cq.leaves[0]
			}
		}
	} else {
		cq.root, err = compileExpr(pq.Root, false)
		if err != nil {
			return nil, err
		}
		if len(cq.leaves) == 1 && cq.leaves[0].anag != nil {
			if _, isAtom := pq.Root.(*AtomExpr); isAtom {
				cq.singleAnag = cq.leaves[0]
			}
		}
	}

	if opts.MinLength <= opts.MaxLength {
		for L := opts.MinLength; L <= opts.MaxLength; L++ {
			if cq.root.canBeTrueAt(L) {
				cq.compatLengths[L] = true
				if cq.minCompatLen == 0 {
					cq.minCompatLen = L
				}
				cq.maxCompatLen = L
			}
		}
	}

	return cq, nil
}

// CanMatchLength reports whether any word of canonical length L can satisfy the compiled query.
func (cq *CompiledQuery) CanMatchLength(L int) bool {
	if L <= 0 || L > tsd.MaxWordLengthLimit {
		return false
	}
	return cq.compatLengths[L]
}

// MinCompatLength returns the smallest compatible word length in [MinLength..MaxLength] (0 if none).
func (cq *CompiledQuery) MinCompatLength() int { return cq.minCompatLen }

// MaxCompatLength returns the largest compatible word length in [MinLength..MaxLength] (0 if none).
func (cq *CompiledQuery) MaxCompatLength() int { return cq.maxCompatLen }

// NumLeaves returns the number of atomic leaf matchers in the query.
func (cq *CompiledQuery) NumLeaves() int { return len(cq.leaves) }

// LeafNeedsRawBytes reports whether leaf idx inspects raw unfolded bytes or punctuation.
func (cq *CompiledQuery) LeafNeedsRawBytes(idx int) bool {
	return cq.leaves[idx].needsRawPrep
}

// LeafBounds returns the [minLen, maxLen] canonical length range matched by leaf idx.
func (cq *CompiledQuery) LeafBounds(idx int) (minLen, maxLen int) {
	leaf := cq.leaves[idx]
	return leaf.minLen, leaf.maxLen
}

// AST returns the boolean root structure for FTS planners via LeafIndex / EvaluateLeafFlags.
func (cq *CompiledQuery) EvaluateLeafFlags(matched []bool) bool {
	var eval func(n compiledNode) bool
	eval = func(n compiledNode) bool {
		switch v := n.(type) {
		case *compiledBinary:
			if v.op == OpAnd {
				return eval(v.left) && eval(v.right)
			}
			return eval(v.left) || eval(v.right)
		case *compiledNot:
			return !eval(v.inner)
		case *compiledLeaf:
			return matched[v.idx]
		}
		return false
	}
	return eval(cq.root)
}

func compileChoiceMask(ch *ChoiceSpec, dict *tsd.Dictionary) (LetterBitmask, error) {
	var mask LetterBitmask
	numTotal := dict.NumTotalLetters()
	if ch.Negated {
		for li := 0; li < numTotal; li++ {
			mask.Set(uint8(li))
		}
	}
	for _, b := range ch.RawBytes {
		li := dict.LetterIndex(b)
		if li < 0 {
			return LetterBitmask{}, &SyntaxError{Code: ErrInvalidChoiceList, Span: ch.Span}
		}
		if ch.Negated {
			mask.Clear(uint8(li))
		} else {
			mask.Set(uint8(li))
		}
	}
	if mask.IsEmpty() {
		return LetterBitmask{}, &SyntaxError{Code: ErrEmptyChoiceList, Span: ch.Span}
	}
	return mask, nil
}

func collectActivePunct(a *AtomicPattern, dict *tsd.Dictionary, punctUsed *[256]bool, hasAnyPunct *bool, hasExactAlpha *bool) {
	for _, it := range a.Template {
		switch it.Kind {
		case ItemLetter:
			if !dict.IsLetter(it.Byte) {
				punctUsed[it.Byte] = true
				*hasAnyPunct = true
			}
		case ItemExactLetter:
			if !dict.IsLetter(it.Byte) {
				punctUsed[it.Byte] = true
				*hasAnyPunct = true
			} else {
				*hasExactAlpha = true
			}
		case ItemPunct:
			if !dict.IsLetter(it.Byte) {
				punctUsed[it.Byte] = true
				*hasAnyPunct = true
			}
		case ItemSubPattern:
			collectActivePunct(it.Sub, dict, punctUsed, hasAnyPunct, hasExactAlpha)
		}
	}
	if a.Anagram != nil {
		for _, it := range a.Anagram.Items {
			if it.Kind == ItemSubPattern {
				collectActivePunct(it.Sub, dict, punctUsed, hasAnyPunct, hasExactAlpha)
			}
		}
	}
}

func compileAtomic(a *AtomicPattern, dict *tsd.Dictionary, opts CompileOptions, isRootAtom bool) (*compiledAtom, error) {
	ca := &compiledAtom{
		dict:        dict,
		opts:        opts,
		hasTemplate: len(a.Template) > 0,
	}

	var punctUsed [256]bool
	hasAnyPunct := false
	hasExactAlpha := false
	collectActivePunct(a, dict, &punctUsed, &hasAnyPunct, &hasExactAlpha)
	if hasAnyPunct || hasExactAlpha {
		ca.needsRawPrep = true
		if isRootAtom {
			for b := 0; b < 256; b++ {
				if dict.IsLetter(byte(b)) {
					ca.punctMode[b] = 0xFE
				} else if hasAnyPunct {
					if !opts.IgnoreUnusedPunctuation || punctUsed[b] {
						ca.punctMode[b] = 0xFF
					}
				}
			}
		}
	}

	hasVars := false
	hasSubs := false
	allWild := true

	if ca.hasTemplate {
		tMin := 0
		tMax := 0
		ops := make([]compiledTmplOp, 0, len(a.Template))

		for _, it := range a.Template {
			switch it.Kind {
			case ItemDot:
				ops = append(ops, compiledTmplOp{kind: tOpDot})
				tMin++
				if tMax < unboundedMaxLen {
					tMax++
				}
			case ItemLetter:
				allWild = false
				li := dict.LetterIndex(it.Byte)
				if li < 0 {
					ops = append(ops, compiledTmplOp{kind: tOpPunct, rawByte: it.Byte})
				} else {
					ops = append(ops, compiledTmplOp{kind: tOpLetter, letterIdx: li, rawByte: it.Byte})
					tMin++
					if tMax < unboundedMaxLen {
						tMax++
					}
				}
			case ItemExactLetter:
				allWild = false
				li := dict.LetterIndex(it.Byte)
				if li < 0 {
					ops = append(ops, compiledTmplOp{kind: tOpPunct, rawByte: it.Byte})
				} else {
					ops = append(ops, compiledTmplOp{kind: tOpExactLetter, letterIdx: li, rawByte: it.Byte})
					tMin++
					if tMax < unboundedMaxLen {
						tMax++
					}
				}
			case ItemChoice:
				allWild = false
				mask, err := compileChoiceMask(it.Choice, dict)
				if err != nil {
					return nil, err
				}
				ops = append(ops, compiledTmplOp{kind: tOpChoice, mask: mask})
				tMin++
				if tMax < unboundedMaxLen {
					tMax++
				}
			case ItemVariable:
				allWild = false
				hasVars = true
				ops = append(ops, compiledTmplOp{kind: tOpVar, rawByte: it.Byte})
				tMin++
				if tMax < unboundedMaxLen {
					tMax++
				}
			case ItemPunct:
				allWild = false
				if dict.IsLetter(it.Byte) {
					// If custom dictionary .TSM considers this byte a letter (e.g. ANSIAll.TSM) and it wasn't escaped:
					li := dict.LetterIndex(it.Byte)
					ops = append(ops, compiledTmplOp{kind: tOpLetter, letterIdx: li, rawByte: it.Byte})
					tMin++
					if tMax < unboundedMaxLen {
						tMax++
					}
				} else {
					ops = append(ops, compiledTmplOp{kind: tOpPunct, rawByte: it.Byte})
				}
			case ItemStar:
				// Coalesce adjacent '*' wildcards cleanly.
				if len(ops) > 0 && ops[len(ops)-1].kind == tOpStar {
					continue
				}
				ops = append(ops, compiledTmplOp{kind: tOpStar})
				tMax = unboundedMaxLen
			case ItemSubPattern:
				hasSubs = true
				subCA, err := compileAtomic(it.Sub, dict, opts, false)
				if err != nil {
					return nil, err
				}
				if subCA.hasVars {
					hasVars = true
				}
				if !subCA.universal {
					allWild = false
				}
				ops = append(ops, compiledTmplOp{kind: tOpSub, sub: subCA})
				tMin += subCA.minLen
				if tMax >= unboundedMaxLen || subCA.maxLen >= unboundedMaxLen {
					tMax = unboundedMaxLen
				} else {
					tMax += subCA.maxLen
				}
			}
		}

		// Compute minAfter from right to left and upgrade the final '*' (if suffix is fixed-length!) to tOpTailStar.
		remMin := 0
		suffixFixed := true
		for i := len(ops) - 1; i >= 0; i-- {
			ops[i].minAfter = remMin
			switch ops[i].kind {
			case tOpDot, tOpLetter, tOpExactLetter, tOpChoice, tOpVar:
				remMin++
			case tOpStar:
				if suffixFixed {
					ops[i].kind = tOpTailStar
				}
				suffixFixed = false
			case tOpSub:
				remMin += ops[i].sub.minLen
				if ops[i].sub.minLen != ops[i].sub.maxLen {
					suffixFixed = false
				}
			}
		}

		ca.tmplMinLen = tMin
		ca.tmplMaxLen = tMax
		ca.tmplOps = ops
	}

	if a.Anagram != nil {
		anag, err := compileAnagramSpec(a.Anagram, dict, opts)
		if err != nil {
			return nil, err
		}
		ca.anag = anag
		if anag.hasVars {
			hasVars = true
		}
		if len(anag.subs) > 0 {
			hasSubs = true
		}
	}
	ca.hasVars = hasVars

	// Compute overall [minLen, maxLen]: anagram-only uses anagram bounds, template-only uses
	// template bounds, and hybrid patterns start with template bounds and cap maxLen at the
	// anagram maximum when the template has variable length and tmplMinLen < anagMax.
	if !ca.hasTemplate {
		ca.minLen = ca.anag.minLen
		if ca.anag.hasStar || ca.anag.maxExtra >= unboundedMaxLen {
			ca.maxLen = unboundedMaxLen
		} else {
			ca.maxLen = min(ca.anag.minLen+ca.anag.maxExtra, unboundedMaxLen)
		}
		ca.universal = !ca.needsRawPrep && len(ca.anag.subs) == 0 &&
			((ca.anag.hasStar && ca.anag.specCount == 0) || (!ca.anag.hasStar && ca.anag.specCount == 0 && ca.anag.dots > 0))
	} else if ca.anag == nil {
		ca.minLen = ca.tmplMinLen
		ca.maxLen = ca.tmplMaxLen
		ca.universal = allWild && !ca.needsRawPrep
	} else {
		ca.minLen = ca.tmplMinLen
		ca.maxLen = ca.tmplMaxLen
		anagMax := unboundedMaxLen
		if !ca.anag.hasStar && ca.anag.maxExtra < unboundedMaxLen {
			anagMax = min(ca.anag.minLen+ca.anag.maxExtra, unboundedMaxLen)
		}
		if ca.tmplMaxLen > ca.tmplMinLen && anagMax < unboundedMaxLen && ca.tmplMinLen < anagMax {
			if anagMax < ca.maxLen {
				ca.maxLen = anagMax
			}
		}
		ca.universal = false
	}

	if ca.minLen > maxTemplateMin {
		return nil, &SyntaxError{Code: ErrTooLong, Span: a.Span}
	}

	// Select specialized fast-path matcher when eligible.
	if isRootAtom && !ca.needsRawPrep && !hasVars && !hasSubs {
		if ca.hasTemplate && ca.anag == nil && ca.minLen == ca.maxLen && ca.minLen <= tsd.MaxWordLengthLimit {
			L := ca.minLen
			onlyLetters := true
			for _, op := range ca.tmplOps {
				if op.kind == tOpChoice {
					onlyLetters = false
				} else if op.kind != tOpDot && op.kind != tOpLetter {
					onlyLetters = false
				}
			}
			if onlyLetters {
				checks := make([]fixedLetterCheck, 0, len(ca.tmplOps))
				for pos, op := range ca.tmplOps {
					if op.kind == tOpLetter {
						checks = append(checks, fixedLetterCheck{
							pos:       pos,
							letterIdx: uint8(op.letterIdx),
							score:     int(dict.Calibration(L, pos, int(op.letterIdx))),
						})
					}
				}
				sort.Slice(checks, func(i, j int) bool {
					if checks[i].score != checks[j].score {
						return checks[i].score < checks[j].score
					}
					return checks[i].pos < checks[j].pos
				})
				ca.fast = fastFixedLetters
				ca.fixedLetChk = checks
			} else {
				checks := make([]fixedMaskCheck, 0, len(ca.tmplOps))
				numUsed := dict.NumUsedLetters()
				for pos, op := range ca.tmplOps {
					switch op.kind {
					case tOpLetter:
						var m LetterBitmask
						m.Set(uint8(op.letterIdx))
						checks = append(checks, fixedMaskCheck{
							pos:   pos,
							mask:  m,
							score: int(dict.Calibration(L, pos, int(op.letterIdx))),
						})
					case tOpChoice:
						// Score choice list by summing approximate relative weights so narrow/rare sets run first.
						score := 0
						matchedAny := false
						for li := 0; li < numUsed; li++ {
							if op.mask.Contains(uint8(li)) {
								cal := int(dict.Calibration(L, pos, li))
								if cal > -128 {
									matchedAny = true
									score += (cal + 129)
								}
							}
						}
						if !matchedAny {
							score = -1000
						}
						checks = append(checks, fixedMaskCheck{
							pos:   pos,
							mask:  op.mask,
							score: score,
						})
					}
				}
				sort.Slice(checks, func(i, j int) bool {
					if checks[i].score != checks[j].score {
						return checks[i].score < checks[j].score
					}
					return checks[i].pos < checks[j].pos
				})
				ca.fast = fastFixedMasks
				ca.fixedMskChk = checks
			}
		} else if !ca.hasTemplate && ca.anag != nil && len(ca.anag.choices) == 0 {
			if len(ca.anag.distinctLIs) == len(ca.anag.fixed) {
				ca.fast = fastDistinctAnagram
			} else {
				ca.fast = fastMultiAnagram
			}
		}
	}

	return ca, nil
}

func compileAnagramSpec(spec *AnagramSpec, dict *tsd.Dictionary, opts CompileOptions) (*compiledAnagram, error) {
	ca := &compiledAnagram{}
	var fixedCounts [256]int
	var fixedOrder []uint8

	for _, it := range spec.Items {
		switch it.Kind {
		case ItemLetter:
			li := dict.LetterIndex(it.Byte)
			if li < 0 {
				return nil, &SyntaxError{Code: ErrInvalidAnagramChar, Span: it.Span}
			}
			uli := uint8(li)
			if fixedCounts[uli] == 0 {
				fixedOrder = append(fixedOrder, uli)
			}
			fixedCounts[uli]++
			ca.specCount++
			if int(li) >= dict.NumUsedLetters() {
				ca.impossible = true
			}
		case ItemDot:
			ca.dots++
		case ItemStar:
			ca.hasStar = true
		case ItemVariable:
			ca.varMult[it.Byte]++
			ca.hasVars = true
			ca.specCount++
		case ItemChoice:
			mask, err := compileChoiceMask(it.Choice, dict)
			if err != nil {
				return nil, err
			}
			merged := false
			for i := range ca.choices {
				if ca.choices[i].mask == mask {
					ca.choices[i].mult++
					merged = true
					break
				}
			}
			if !merged {
				ca.choices = append(ca.choices, anagChoiceReq{mask: mask, mult: 1})
			}
			ca.specCount++
		case ItemSubPattern:
			subCA, err := compileAtomic(it.Sub, dict, opts, false)
			if err != nil {
				return nil, err
			}
			if subCA.hasVars {
				ca.hasVars = true
			}
			ca.subs = append(ca.subs, anagSubReq{sub: subCA, mult: 1})
			ca.flatSubs = append(ca.flatSubs, subCA)
			ca.specCount += subCA.minLen
			if ca.maxExtra >= unboundedMaxLen || subCA.maxLen >= unboundedMaxLen {
				ca.maxExtra = unboundedMaxLen
			} else {
				ca.maxExtra += subCA.maxLen - subCA.minLen
			}
		}
	}

	allSingle := true
	for _, li := range fixedOrder {
		mult := fixedCounts[li]
		ca.fixed = append(ca.fixed, anagFixedReq{letterIdx: li, mult: mult})
		if mult != 1 {
			allSingle = false
		}
	}
	if allSingle {
		ca.distinctLIs = fixedOrder
	}
	ca.minLen = ca.specCount + ca.dots
	return ca, nil
}
