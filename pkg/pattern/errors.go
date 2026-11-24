// Package pattern implements TEA/Tisane crossword & anagram pattern macro expansion,
// parsing, live validation, calibration-sorted compilation, and fast VM execution.
package pattern

import "fmt"

// StatusCode defines the 15 TEA-compatible pattern diagnostic codes (0..14),
// plus WarnIncompatibleLengths (15) when boolean length ranges are disjoint.
type StatusCode uint8

const (
	StatusValid               StatusCode = 0
	ErrEmpty                  StatusCode = 1
	ErrInvalidChoiceList      StatusCode = 2
	ErrTooManyPatterns        StatusCode = 3
	ErrTooLong                StatusCode = 4
	ErrUnclosedChoiceList     StatusCode = 5
	ErrMultipleSemicolons     StatusCode = 6
	ErrUnclosedSubpattern     StatusCode = 7
	ErrUnexpectedCloseParen   StatusCode = 8
	ErrUnexpectedCloseBracket StatusCode = 9
	ErrTooMuchPunctuation     StatusCode = 10
	ErrInvalidAnagramChar     StatusCode = 11
	ErrUnexpectedCaret        StatusCode = 12
	ErrTrailingBackslash      StatusCode = 13
	ErrEmptyChoiceList        StatusCode = 14
	WarnIncompatibleLengths   StatusCode = 15
)

// Message returns the canonical user-facing status/hint text for a StatusCode.
func (c StatusCode) Message() string {
	switch c {
	case StatusValid:
		return "Pattern is valid - press the return key to start searching"
	case ErrEmpty:
		return "Pattern is empty"
	case ErrInvalidChoiceList:
		return "Choice lists can only include letters, optionally preceded by ^"
	case ErrTooManyPatterns:
		return "Too many patterns or choice lists"
	case ErrTooLong:
		return "Pattern is too long to match anything"
	case ErrUnclosedChoiceList:
		return "Choice list started - use ] to complete it"
	case ErrMultipleSemicolons:
		return "Only one ; is allowed in a pattern"
	case ErrUnclosedSubpattern:
		return "Subpattern started - use ) to complete it"
	case ErrUnexpectedCloseParen:
		return "A ) can only appear at the end of a subpattern"
	case ErrUnexpectedCloseBracket:
		return "A ] can only appear at the end of a choice list"
	case ErrTooMuchPunctuation:
		return "Too many non-alphabetic characters in templates"
	case ErrInvalidAnagramChar:
		return "Only letters and match-alls allowed in anagrams"
	case ErrUnexpectedCaret:
		return "A ^ can only appear within a choice list"
	case ErrTrailingBackslash:
		return "Follow \\ by the character you want to match exactly"
	case ErrEmptyChoiceList:
		return "Choice lists must contain at least one letter"
	case WarnIncompatibleLengths:
		return "Patterns match incompatible lengths"
	default:
		return fmt.Sprintf("Unknown pattern status (%d)", c)
	}
}

// Span identifies a [Start, End) byte range in the original unexpanded query string.
type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// SyntaxError represents a pattern parse or compilation failure tied to a source Span.
type SyntaxError struct {
	Code StatusCode
	Span Span
	Msg  string
}

func (e *SyntaxError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return e.Code.Message()
}

// ValidationResult provides live feedback for GUI/REPL input bars.
type ValidationResult struct {
	Valid            bool       `json:"valid"`
	Code             StatusCode `json:"code"`
	Message          string     `json:"message"`
	Span             Span       `json:"span"`
	MinMatchedLength int        `json:"minMatchedLength"`
	MaxMatchedLength int        `json:"maxMatchedLength"`
	HasWarning       bool       `json:"hasWarning"`
}
