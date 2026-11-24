package pattern

import (
	"unicode/utf8"

	"tisane/pkg/tsd"
)

const (
	maxLogicDepth         = 32
	maxBooleanSegments    = 128
	maxPoolConstruct      = 256
	maxTemplateMin        = 128
	maxTemplatePunct      = 644
	maxAnagramItems       = 512
	maxExpandedQueryBytes = 16384
)

// ParseOptions configures macro expansion and default alphabet character classification during parsing.
type ParseOptions struct {
	AtMacro     string            // Replacement for '@'; note: use DefaultParseOptions() for "[aeiou]"
	HashMacro   string            // Replacement for '#'; note: use DefaultParseOptions() for "[^aeiou]"
	Alphabet    tsd.TSM           // Used for syntactic choice-list / anagram letter checks (DefaultTSM if zero)
	Enumeration []EnumerationPart // Optional crossword enumeration constraint conjoined onto the AST
}

// DefaultParseOptions returns standard TEA macro and alphabet settings.
func DefaultParseOptions() ParseOptions {
	return ParseOptions{
		AtMacro:   DefaultAtMacro,
		HashMacro: DefaultHashMacro,
		Alphabet:  tsd.DefaultTSM(),
	}
}

type mappedBuf struct {
	bytes []byte
	spans []Span
}

func (m *mappedBuf) spanAt(i int) Span {
	if len(m.spans) == 0 {
		return Span{}
	}
	if i < 0 {
		return m.spans[0]
	}
	if i >= len(m.spans) {
		last := m.spans[len(m.spans)-1]
		return Span{Start: last.End, End: last.End}
	}
	return m.spans[i]
}

func (m *mappedBuf) rangeSpan(start, end int) Span {
	if len(m.spans) == 0 || start >= len(m.spans) {
		return Span{}
	}
	if start < 0 {
		start = 0
	}
	if end <= start {
		return m.spans[start]
	}
	if end > len(m.spans) {
		end = len(m.spans)
	}
	return Span{
		Start: m.spans[start].Start,
		End:   m.spans[end-1].End,
	}
}

func (m *mappedBuf) slice(start, end int) mappedBuf {
	return mappedBuf{
		bytes: m.bytes[start:end],
		spans: m.spans[start:end],
	}
}

// utf8ExpandedToCP1252 converts the UTF-8 expanded query into CP1252 bytes while preserving
// byte-accurate spans into the unexpanded raw query. Unrepresentable Unicode runes map to 0x00
// (which is never alphabetic and never matches any .tsd byte).
func utf8ExpandedToCP1252(expanded string, expSpans []Span) mappedBuf {
	out := mappedBuf{
		bytes: make([]byte, 0, len(expanded)),
		spans: make([]Span, 0, len(expanded)),
	}
	for i := 0; i < len(expanded); {
		b := expanded[i]
		if b < 0x80 {
			out.bytes = append(out.bytes, b)
			out.spans = append(out.spans, expSpans[i])
			i++
			continue
		}
		r, width := utf8.DecodeRuneInString(expanded[i:])
		sp := Span{
			Start: expSpans[i].Start,
			End:   expSpans[i+width-1].End,
		}
		if cp, ok := tsd.EncodeCP1252Rune(r); ok && cp != 0 {
			out.bytes = append(out.bytes, cp)
		} else {
			out.bytes = append(out.bytes, 0x00)
		}
		out.spans = append(out.spans, sp)
		i += width
	}
	return out
}

func trimUnescapedSpaces(m mappedBuf) mappedBuf {
	n := len(m.bytes)
	start := 0
	for start < n && m.bytes[start] == ' ' {
		start++
	}
	if start >= n {
		return m.slice(n, n)
	}

	// Determine whether each byte is escaped by a preceding unescaped backslash.
	escaped := make([]bool, n)
	for i := start; i < n; i++ {
		if m.bytes[i] == '\\' && !escaped[i] && i+1 < n {
			escaped[i+1] = true
		}
	}
	end := n
	for end > start && m.bytes[end-1] == ' ' && !escaped[end-1] {
		end--
	}
	return m.slice(start, end)
}

type exprTokenKind uint8

const (
	tokLParen exprTokenKind = iota + 1
	tokRParen
	tokNot
	tokAnd
	tokOr
	tokAtom
)

type exprToken struct {
	kind exprTokenKind
	atom *AtomicPattern
	span Span
}

// Parse expands macros, parses logical operators and sub-patterns, validates syntax against opts.Alphabet,
// and returns an inspectable ParsedQuery AST (or *SyntaxError on failure).
func Parse(raw string, opts ParseOptions) (*ParsedQuery, error) {
	tsm := opts.Alphabet
	if tsm == (tsd.TSM{}) {
		tsm = tsd.DefaultTSM()
	}

	if len(raw) > maxExpandedQueryBytes {
		return nil, &SyntaxError{Code: ErrTooLong, Span: Span{Start: 0, End: len(raw)}}
	}
	expanded, expSpans, overSpan, ok := expandMacrosBounded(raw, opts.AtMacro, opts.HashMacro, maxExpandedQueryBytes)
	if !ok {
		return nil, &SyntaxError{Code: ErrTooLong, Span: overSpan}
	}
	mb := utf8ExpandedToCP1252(expanded, expSpans)
	trimmed := trimUnescapedSpaces(mb)
	if len(trimmed.bytes) == 0 {
		return nil, &SyntaxError{Code: ErrEmpty, Span: Span{Start: 0, End: len(raw)}}
	}

	// Split on top-level unescaped '&' and '|' (outside '[...]').
	type rawSeg struct {
		buf mappedBuf
	}
	var segs []rawSeg
	var ops []exprToken

	segStart := 0
	inBracket := false
	for i := 0; i < len(trimmed.bytes); i++ {
		c := trimmed.bytes[i]
		if !inBracket && c == '\\' && i+1 < len(trimmed.bytes) {
			i++
			continue
		}
		if c == '[' && !inBracket {
			inBracket = true
			continue
		}
		if c == ']' && inBracket {
			inBracket = false
			continue
		}
		if !inBracket && (c == '&' || c == '|') {
			if len(ops) >= maxBooleanSegments {
				return nil, &SyntaxError{Code: ErrTooManyPatterns, Span: trimmed.spanAt(i)}
			}
			segs = append(segs, rawSeg{buf: trimmed.slice(segStart, i)})
			tk := tokAnd
			if c == '|' {
				tk = tokOr
			}
			ops = append(ops, exprToken{kind: tk, span: trimmed.spanAt(i)})
			segStart = i + 1
		}
	}
	segs = append(segs, rawSeg{buf: trimmed.slice(segStart, len(trimmed.bytes))})

	var tokens []exprToken
	for idx, rs := range segs {
		if idx > 0 {
			tokens = append(tokens, ops[idx-1])
		}
		seg := trimUnescapedSpaces(rs.buf)
		if len(seg.bytes) == 0 {
			sp := Span{Start: len(raw), End: len(raw)}
			if idx > 0 {
				sp = ops[idx-1].span
			} else if idx < len(ops) {
				sp = ops[idx].span
			}
			return nil, &SyntaxError{Code: ErrEmpty, Span: sp}
		}

		segTokens, err := tokenizeSegment(seg, &tsm)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, segTokens...)
	}

	root, err := buildBooleanAST(tokens)
	if err != nil {
		return nil, err
	}

	if len(opts.Enumeration) > 0 {
		enumTmpl := EnumerationTemplate(opts.Enumeration)
		enumParsed, err := Parse(enumTmpl, ParseOptions{Alphabet: tsm})
		if err != nil {
			return nil, err
		}
		root = &BinaryExpr{
			Op:            OpAnd,
			Left:          root,
			Right:         enumParsed.Root,
			Span:          root.ExprSpan(),
			SyntheticEnum: true,
		}
	}

	return &ParsedQuery{
		Raw:      raw,
		Expanded: expanded,
		Root:     root,
	}, nil
}

// tokenizeSegment disambiguates outer logical '(', ')', and '!' from inner AtomicPattern sub-patterns.
func tokenizeSegment(seg mappedBuf, tsm *tsd.TSM) ([]exprToken, error) {
	b := seg.bytes
	n := len(b)

	// Count total unescaped '(' and ')' outside '[...]', and track '(' count at leading '!'.
	openCnt := 0
	closeCnt := 0
	openAtBang := 0
	inLeadingPrefix := true
	inBracket := false

	for i := 0; i < n; i++ {
		c := b[i]
		if !inBracket && c == '\\' && i+1 < n {
			inLeadingPrefix = false
			i++
			continue
		}
		if c == '[' && !inBracket {
			inBracket = true
			inLeadingPrefix = false
			continue
		}
		if c == ']' && inBracket {
			inBracket = false
			continue
		}
		if inBracket {
			continue
		}
		switch c {
		case '(':
			openCnt++
		case ')':
			closeCnt++
			inLeadingPrefix = false
		case '!':
			if inLeadingPrefix {
				openAtBang = openCnt
			}
		case ' ':
			// Space stays in leading prefix only if still among '(' / '!'.
		default:
			inLeadingPrefix = false
		}
	}

	common := openCnt
	if closeCnt < common {
		common = closeCnt
	}
	if openCnt-common < openAtBang {
		common = openCnt - openAtBang
	}
	needOpen := openCnt - common
	needClose := closeCnt - common

	var prefixTokens []exprToken
	left := 0
	for left < n {
		c := b[left]
		if c == ' ' && len(prefixTokens) > 0 {
			left++
			continue
		}
		if c == '!' {
			prefixTokens = append(prefixTokens, exprToken{kind: tokNot, span: seg.spanAt(left)})
			left++
			continue
		}
		if c == '(' && needOpen > 0 {
			prefixTokens = append(prefixTokens, exprToken{kind: tokLParen, span: seg.spanAt(left)})
			needOpen--
			left++
			continue
		}
		break
	}
	// If needOpen > 0 remains, an unclosed '(' appeared after atom characters (e.g. "a(b | c)"),
	// so leave it in the atom slice where parseAtomic will report ErrUnclosedSubpattern at its exact span.

	var suffixTokens []exprToken
	right := n
	if needClose > 0 {
		// Escaped check so "\)" at end is not mistaken for logical ')'.
		escaped := make([]bool, n)
		for i := left; i < n; i++ {
			if b[i] == '\\' && !escaped[i] && i+1 < n {
				escaped[i+1] = true
			}
		}
		for right > left && needClose > 0 {
			if b[right-1] == ' ' && !escaped[right-1] && len(suffixTokens) > 0 {
				right--
				continue
			}
			if b[right-1] == ')' && !escaped[right-1] {
				suffixTokens = append(suffixTokens, exprToken{kind: tokRParen, span: seg.spanAt(right - 1)})
				needClose--
				right--
				continue
			}
			break
		}
	}
	// Reverse suffixTokens so close-parens maintain source order.
	for i, j := 0, len(suffixTokens)-1; i < j; i, j = i+1, j-1 {
		suffixTokens[i], suffixTokens[j] = suffixTokens[j], suffixTokens[i]
	}

	atomBuf := trimUnescapedSpaces(seg.slice(left, right))
	if len(atomBuf.bytes) == 0 {
		return nil, &SyntaxError{Code: ErrEmpty, Span: seg.rangeSpan(0, n)}
	}

	poolCounter := 0
	atom, err := parseAtomic(atomBuf, tsm, &poolCounter)
	if err != nil {
		return nil, err
	}

	out := make([]exprToken, 0, len(prefixTokens)+1+len(suffixTokens))
	out = append(out, prefixTokens...)
	out = append(out, exprToken{kind: tokAtom, atom: atom, span: atom.Span})
	out = append(out, suffixTokens...)
	return out, nil
}

func buildBooleanAST(tokens []exprToken) (Expr, error) {
	pos := 0
	n := len(tokens)

	var parseExpr func(depth int) (Expr, error)
	var parseUnary func(depth int) (Expr, error)

	parseUnary = func(depth int) (Expr, error) {
		if pos >= n {
			return nil, &SyntaxError{Code: ErrEmpty}
		}
		tk := tokens[pos]
		switch tk.kind {
		case tokNot:
			if depth+1 > maxLogicDepth {
				return nil, &SyntaxError{Code: ErrTooManyPatterns, Span: tk.span}
			}
			pos++
			inner, err := parseUnary(depth + 1)
			if err != nil {
				return nil, err
			}
			return &NotExpr{
				Inner: inner,
				Span:  Span{Start: tk.span.Start, End: inner.ExprSpan().End},
			}, nil
		case tokLParen:
			if depth+1 > maxLogicDepth {
				return nil, &SyntaxError{Code: ErrTooManyPatterns, Span: tk.span}
			}
			openSpan := tk.span
			pos++
			inner, err := parseExpr(depth + 1)
			if err != nil {
				return nil, err
			}
			if pos >= n || tokens[pos].kind != tokRParen {
				return nil, &SyntaxError{Code: ErrUnclosedSubpattern, Span: openSpan}
			}
			pos++
			return inner, nil
		case tokAtom:
			pos++
			return &AtomExpr{Atom: tk.atom}, nil
		case tokRParen:
			return nil, &SyntaxError{Code: ErrUnexpectedCloseParen, Span: tk.span}
		default:
			return nil, &SyntaxError{Code: ErrEmpty, Span: tk.span}
		}
	}

	// '&' and '|' have equal precedence and associate left-to-right.
	parseExpr = func(depth int) (Expr, error) {
		left, err := parseUnary(depth)
		if err != nil {
			return nil, err
		}
		for pos < n && (tokens[pos].kind == tokAnd || tokens[pos].kind == tokOr) {
			opTok := tokens[pos]
			pos++
			right, err := parseUnary(depth)
			if err != nil {
				return nil, err
			}
			op := OpAnd
			if opTok.kind == tokOr {
				op = OpOr
			}
			left = &BinaryExpr{
				Op:    op,
				Left:  left,
				Right: right,
				Span:  Span{Start: left.ExprSpan().Start, End: right.ExprSpan().End},
			}
		}
		return left, nil
	}

	root, err := parseExpr(0)
	if err != nil {
		return nil, err
	}
	if pos < n {
		if tokens[pos].kind == tokRParen {
			return nil, &SyntaxError{Code: ErrUnexpectedCloseParen, Span: tokens[pos].span}
		}
		return nil, &SyntaxError{Code: ErrEmpty, Span: tokens[pos].span}
	}
	return root, nil
}

func parseChoiceList(m mappedBuf, start int, tsm *tsd.TSM, poolCounter *int) (*ChoiceSpec, int, error) {
	*poolCounter++
	if *poolCounter > maxPoolConstruct {
		return nil, 0, &SyntaxError{Code: ErrTooManyPatterns, Span: m.spanAt(start)}
	}

	b := m.bytes
	n := len(b)
	i := start + 1
	if i >= n {
		return nil, 0, &SyntaxError{Code: ErrUnclosedChoiceList, Span: m.spanAt(start)}
	}

	negated := false
	if b[i] == '^' {
		negated = true
		i++
		if i >= n {
			return nil, 0, &SyntaxError{Code: ErrUnclosedChoiceList, Span: m.rangeSpan(start, n)}
		}
	}

	if b[i] == ']' {
		return nil, 0, &SyntaxError{Code: ErrEmptyChoiceList, Span: m.rangeSpan(start, i+1)}
	}

	var letters []byte
	for i < n {
		c := b[i]
		if c == ']' {
			if len(letters) == 0 {
				return nil, 0, &SyntaxError{Code: ErrEmptyChoiceList, Span: m.rangeSpan(start, i+1)}
			}
			return &ChoiceSpec{
				Negated:  negated,
				RawBytes: letters,
				Span:     m.rangeSpan(start, i+1),
			}, i + 1, nil
		}
		// A trailing ')' at end of input inside an unclosed '[' reports ErrUnclosedChoiceList
		// rather than ErrInvalidChoiceList.
		if c == ')' && i+1 == n {
			return nil, 0, &SyntaxError{Code: ErrUnclosedChoiceList, Span: m.rangeSpan(start, n)}
		}
		// Digits 0..9, '[', '^', '\', match-alls, spaces, and non-letters are illegal inside [...].
		if (c >= '0' && c <= '9') || c == '[' || c == '^' || c == '\\' ||
			c == '.' || c == '?' || c == '*' || c == ';' || c == '(' || c == ')' ||
			!tsm.IsAlpha(c) {
			return nil, 0, &SyntaxError{Code: ErrInvalidChoiceList, Span: m.spanAt(i)}
		}
		letters = append(letters, c)
		i++
	}
	return nil, 0, &SyntaxError{Code: ErrUnclosedChoiceList, Span: m.rangeSpan(start, n)}
}

func findMatchingParen(m mappedBuf, openIdx int) (int, error) {
	b := m.bytes
	n := len(b)
	depth := 1
	inBracket := false

	for i := openIdx + 1; i < n; i++ {
		c := b[i]
		if !inBracket && c == '\\' {
			if i+1 >= n {
				return -1, &SyntaxError{Code: ErrTrailingBackslash, Span: m.spanAt(i)}
			}
			i++
			continue
		}
		if c == '[' && !inBracket {
			inBracket = true
			continue
		}
		if c == ']' && inBracket {
			inBracket = false
			continue
		}
		if inBracket {
			continue
		}
		if c == '(' {
			depth++
		} else if c == ')' {
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	if inBracket {
		return -1, &SyntaxError{Code: ErrUnclosedChoiceList, Span: m.rangeSpan(openIdx, n)}
	}
	return -1, &SyntaxError{Code: ErrUnclosedSubpattern, Span: m.spanAt(openIdx)}
}

func parseAtomic(m mappedBuf, tsm *tsd.TSM, poolCounter *int) (*AtomicPattern, error) {
	b := m.bytes
	n := len(b)
	if n == 0 {
		return nil, &SyntaxError{Code: ErrEmpty, Span: m.rangeSpan(0, 0)}
	}

	// Scan for unescaped ';' at subpattern depth 0 (outside '[...]').
	// Note: if brackets/parens are malformed before ';', let the item parser report the exact first error.
	semiIdx := -1
	depth := 0
	inBracket := false
	for i := 0; i < n; i++ {
		c := b[i]
		if !inBracket && c == '\\' {
			if i+1 < n {
				i++
			}
			continue
		}
		if c == '[' && !inBracket {
			inBracket = true
			continue
		}
		if c == ']' && inBracket {
			inBracket = false
			continue
		}
		if inBracket {
			continue
		}
		switch c {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ';':
			if depth == 0 {
				if semiIdx >= 0 {
					return nil, &SyntaxError{Code: ErrMultipleSemicolons, Span: m.spanAt(i)}
				}
				semiIdx = i
			}
		}
	}

	var tmplBuf mappedBuf
	var hasAnagram bool
	var anagBuf mappedBuf
	if semiIdx >= 0 {
		tmplBuf = m.slice(0, semiIdx)
		hasAnagram = true
		anagBuf = m.slice(semiIdx+1, n)
	} else {
		tmplBuf = m
	}

	var tmplItems []TemplateItem
	if len(tmplBuf.bytes) > 0 {
		var err error
		tmplItems, err = parseTemplateItems(tmplBuf, tsm, poolCounter)
		if err != nil {
			return nil, err
		}
	}

	var anagSpec *AnagramSpec
	if hasAnagram {
		items, err := parseAnagramItems(anagBuf, m.spanAt(semiIdx), tsm, poolCounter)
		if err != nil {
			return nil, err
		}
		anagSpec = &AnagramSpec{
			Items: items,
			Span:  anagBuf.rangeSpan(0, len(anagBuf.bytes)),
		}
	}

	if len(tmplItems) == 0 && anagSpec == nil {
		return nil, &SyntaxError{Code: ErrEmpty, Span: m.rangeSpan(0, n)}
	}

	// Validate minimum matched length <= 128 and punctuation count <= 644 in a single linear pass.
	tMin, punctCnt := templateMinAndPunct(tmplItems)
	if tMin > maxTemplateMin {
		return nil, &SyntaxError{Code: ErrTooLong, Span: m.rangeSpan(0, n)}
	}
	if anagSpec != nil {
		aMin, aPunct := anagramMinAndPunct(anagSpec.Items)
		punctCnt += aPunct
		if len(tmplItems) == 0 && aMin > maxTemplateMin {
			return nil, &SyntaxError{Code: ErrTooLong, Span: anagSpec.Span}
		}
	}
	if punctCnt > maxTemplatePunct {
		return nil, &SyntaxError{Code: ErrTooMuchPunctuation, Span: m.rangeSpan(0, n)}
	}

	return &AtomicPattern{
		Template: tmplItems,
		Anagram:  anagSpec,
		Span:     m.rangeSpan(0, n),
	}, nil
}

func parseTemplateItems(m mappedBuf, tsm *tsd.TSM, poolCounter *int) ([]TemplateItem, error) {
	b := m.bytes
	n := len(b)
	items := make([]TemplateItem, 0, min(n, 64))

	for i := 0; i < n; {
		c := b[i]
		sp := m.spanAt(i)
		switch {
		case c == ']':
			return nil, &SyntaxError{Code: ErrUnexpectedCloseBracket, Span: sp}
		case c == '^':
			return nil, &SyntaxError{Code: ErrUnexpectedCaret, Span: sp}
		case c == ')':
			return nil, &SyntaxError{Code: ErrUnexpectedCloseParen, Span: sp}
		case c == '[':
			ch, nextI, err := parseChoiceList(m, i, tsm, poolCounter)
			if err != nil {
				return nil, err
			}
			items = append(items, TemplateItem{Kind: ItemChoice, Choice: ch, Span: ch.Span})
			i = nextI
		case c == '(':
			*poolCounter++
			if *poolCounter > maxPoolConstruct {
				return nil, &SyntaxError{Code: ErrTooManyPatterns, Span: sp}
			}
			closeIdx, err := findMatchingParen(m, i)
			if err != nil {
				return nil, err
			}
			inner := m.slice(i+1, closeIdx)
			if len(inner.bytes) == 0 {
				return nil, &SyntaxError{Code: ErrEmpty, Span: m.rangeSpan(i, closeIdx+1)}
			}
			sub, err := parseAtomic(inner, tsm, poolCounter)
			if err != nil {
				return nil, err
			}
			sub.Span = m.rangeSpan(i, closeIdx+1)
			items = append(items, TemplateItem{Kind: ItemSubPattern, Sub: sub, Span: sub.Span})
			i = closeIdx + 1
		case c == '\\':
			if i+1 >= n {
				return nil, &SyntaxError{Code: ErrTrailingBackslash, Span: sp}
			}
			nb := b[i+1]
			escSpan := m.rangeSpan(i, i+2)
			if tsm.IsAlpha(nb) {
				items = append(items, TemplateItem{Kind: ItemExactLetter, Byte: nb, Span: escSpan})
			} else {
				items = append(items, TemplateItem{Kind: ItemPunct, Byte: nb, Span: escSpan})
			}
			i += 2
		case c == '.' || c == '?':
			items = append(items, TemplateItem{Kind: ItemDot, Span: sp})
			i++
		case c == '*':
			items = append(items, TemplateItem{Kind: ItemStar, Span: sp})
			i++
		case c >= '0' && c <= '9':
			items = append(items, TemplateItem{Kind: ItemVariable, Byte: c - '0', Span: sp})
			i++
		default:
			if tsm.IsAlpha(c) {
				items = append(items, TemplateItem{Kind: ItemLetter, Byte: c, Span: sp})
			} else {
				items = append(items, TemplateItem{Kind: ItemPunct, Byte: c, Span: sp})
			}
			i++
		}
	}
	return items, nil
}

func parseAnagramItems(m mappedBuf, semiSpan Span, tsm *tsd.TSM, poolCounter *int) ([]AnagramItem, error) {
	b := m.bytes
	n := len(b)
	var items []AnagramItem

	for i := 0; i < n; {
		c := b[i]
		sp := m.spanAt(i)
		if c != ' ' && len(items) >= maxAnagramItems {
			return nil, &SyntaxError{Code: ErrTooLong, Span: sp}
		}
		switch {
		case c == ' ':
			i++
		case c == ']':
			return nil, &SyntaxError{Code: ErrUnexpectedCloseBracket, Span: sp}
		case c == '^':
			return nil, &SyntaxError{Code: ErrUnexpectedCaret, Span: sp}
		case c == ')':
			return nil, &SyntaxError{Code: ErrUnexpectedCloseParen, Span: sp}
		case c == '[':
			ch, nextI, err := parseChoiceList(m, i, tsm, poolCounter)
			if err != nil {
				return nil, err
			}
			items = append(items, AnagramItem{Kind: ItemChoice, Choice: ch, Span: ch.Span})
			i = nextI
		case c == '(':
			*poolCounter++
			if *poolCounter > maxPoolConstruct {
				return nil, &SyntaxError{Code: ErrTooManyPatterns, Span: sp}
			}
			closeIdx, err := findMatchingParen(m, i)
			if err != nil {
				return nil, err
			}
			inner := m.slice(i+1, closeIdx)
			if len(inner.bytes) == 0 {
				return nil, &SyntaxError{Code: ErrEmpty, Span: m.rangeSpan(i, closeIdx+1)}
			}
			sub, err := parseAtomic(inner, tsm, poolCounter)
			if err != nil {
				return nil, err
			}
			sub.Span = m.rangeSpan(i, closeIdx+1)
			items = append(items, AnagramItem{Kind: ItemSubPattern, Sub: sub, Span: sub.Span})
			i = closeIdx + 1
		case c == '.' || c == '?':
			items = append(items, AnagramItem{Kind: ItemDot, Span: sp})
			i++
		case c == '*':
			items = append(items, AnagramItem{Kind: ItemStar, Span: sp})
			i++
		case c >= '0' && c <= '9':
			items = append(items, AnagramItem{Kind: ItemVariable, Byte: c - '0', Span: sp})
			i++
		default:
			if tsm.IsAlpha(c) {
				items = append(items, AnagramItem{Kind: ItemLetter, Byte: c, Span: sp})
				i++
			} else {
				return nil, &SyntaxError{Code: ErrInvalidAnagramChar, Span: sp}
			}
		}
	}

	if len(items) == 0 {
		errSpan := semiSpan
		if n > 0 {
			errSpan = m.rangeSpan(0, n)
		}
		return nil, &SyntaxError{Code: ErrEmpty, Span: errSpan}
	}
	return items, nil
}

func atomicMinAndPunct(a *AtomicPattern) (minLen int, punctCnt int) {
	tMin, tPunct := templateMinAndPunct(a.Template)
	var aMin, aPunct int
	if a.Anagram != nil {
		aMin, aPunct = anagramMinAndPunct(a.Anagram.Items)
	}
	if len(a.Template) > 0 {
		return tMin, tPunct + aPunct
	}
	return aMin, tPunct + aPunct
}

func templateMinAndPunct(items []TemplateItem) (minLen int, punctCnt int) {
	for _, it := range items {
		switch it.Kind {
		case ItemLetter, ItemDot, ItemChoice, ItemVariable:
			minLen++
		case ItemExactLetter:
			minLen++
			punctCnt++
		case ItemPunct:
			punctCnt++
		case ItemSubPattern:
			subMin, subPunct := atomicMinAndPunct(it.Sub)
			minLen += subMin
			punctCnt += subPunct
		}
	}
	return minLen, punctCnt
}

func anagramMinAndPunct(items []AnagramItem) (minLen int, punctCnt int) {
	for _, it := range items {
		switch it.Kind {
		case ItemLetter, ItemDot, ItemChoice, ItemVariable:
			minLen++
		case ItemSubPattern:
			subMin, subPunct := atomicMinAndPunct(it.Sub)
			minLen += subMin
			punctCnt += subPunct
		}
	}
	return minLen, punctCnt
}
