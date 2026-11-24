package pattern

// LogicOp identifies a binary boolean operator (& or |).
type LogicOp uint8

const (
	OpAnd LogicOp = iota + 1 // '&'
	OpOr                     // '|'
)

// Expr is a boolean pattern expression node.
// Note that '&' and '|' have equal precedence in TEA and associate left-to-right unless grouped.
type Expr interface {
	exprNode()
	ExprSpan() Span
}

// BinaryExpr represents (Left & Right) or (Left | Right).
type BinaryExpr struct {
	Op            LogicOp
	Left          Expr
	Right         Expr
	Span          Span
	SyntheticEnum bool // True when Parse conjoined a synthetic enumeration constraint on Right
}

func (*BinaryExpr) exprNode()        {}
func (e *BinaryExpr) ExprSpan() Span { return e.Span }

// NotExpr represents !Inner.
type NotExpr struct {
	Inner Expr
	Span  Span
}

func (*NotExpr) exprNode()        {}
func (e *NotExpr) ExprSpan() Span { return e.Span }

// AtomExpr wraps a single leaf AtomicPattern.
type AtomExpr struct {
	Atom *AtomicPattern
}

func (*AtomExpr) exprNode() {}
func (e *AtomExpr) ExprSpan() Span {
	if e == nil || e.Atom == nil {
		return Span{}
	}
	return e.Atom.Span
}

// AtomicPattern represents `[Template][;Anagram]` (where at least one of Template or Anagram is non-empty).
type AtomicPattern struct {
	Template []TemplateItem
	Anagram  *AnagramSpec // nil if no ';' section
	Span     Span
}

// ItemKind classifies items inside a Template or Anagram.
type ItemKind uint8

const (
	ItemLetter      ItemKind = iota + 1 // Single canonical letter (CP1252 raw byte)
	ItemExactLetter                     // '\c' case/accent-exact alphabetic byte
	ItemDot                             // '.' or '?' single-letter match-all
	ItemStar                            // '*' 0..N letter wildcard
	ItemChoice                          // [...] or [^...] choice list
	ItemVariable                        // '0'..'9' unification variable
	ItemPunct                           // Non-alphabetic literal punctuation byte in template
	ItemSubPattern                      // (...) embedded AtomicPattern
)

// ChoiceSpec represents `[letters]` or `[^letters]`.
type ChoiceSpec struct {
	Negated  bool
	RawBytes []byte // Raw CP1252 bytes inside [...]
	Span     Span
}

// TemplateItem is a single element of a Template sequence.
type TemplateItem struct {
	Kind   ItemKind
	Byte   byte           // Raw CP1252 byte for ItemLetter, ItemExactLetter, ItemPunct; 0..9 for ItemVariable
	Choice *ChoiceSpec    // Non-nil for ItemChoice
	Sub    *AtomicPattern // Non-nil for ItemSubPattern
	Span   Span
}

// AnagramSpec represents the `;anagram` portion of an AtomicPattern.
type AnagramSpec struct {
	Items []AnagramItem
	Span  Span
}

// AnagramItem is a single element of an Anagram multiset specification.
type AnagramItem struct {
	Kind   ItemKind       // ItemLetter, ItemDot, ItemStar, ItemChoice, ItemVariable, or ItemSubPattern
	Byte   byte           // Raw CP1252 byte for ItemLetter; 0..9 for ItemVariable
	Choice *ChoiceSpec    // Non-nil for ItemChoice
	Sub    *AtomicPattern // Non-nil for ItemSubPattern
	Span   Span
}

// ParsedQuery holds a parsed boolean pattern AST along with its source metadata.
type ParsedQuery struct {
	Raw      string
	Expanded string
	Root     Expr
}
