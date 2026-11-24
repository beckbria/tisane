package pattern_test

import (
	"slices"
	"strings"
	"testing"

	"tisane/pkg/pattern"
	"tisane/pkg/tsd"
)

func buildTestDict(t *testing.T, lines ...string) *tsd.Dictionary {
	t.Helper()
	opts := tsd.DefaultBuildOptions()
	opts.SortAlphabetically = false
	raw := strings.Join(lines, "\n") + "\n"
	data, _, err := tsd.BuildBytes([]byte(raw), opts)
	if err != nil {
		t.Fatalf("BuildBytes: %v", err)
	}
	d, err := tsd.OpenBytes(data, "test")
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func evalQuery(t *testing.T, d *tsd.Dictionary, query string, opts pattern.CompileOptions) []string {
	t.Helper()
	pq, err := pattern.Parse(query, pattern.DefaultParseOptions())
	if err != nil {
		t.Fatalf("Parse(%q): %v", query, err)
	}
	cq, err := pattern.Compile(pq, d, opts)
	if err != nil {
		t.Fatalf("Compile(%q): %v", query, err)
	}
	m := cq.NewMatcher()
	var out []string
	for L := 1; L <= d.MaxWordLength(); L++ {
		if !cq.CanMatchLength(L) {
			continue
		}
		for raw, err := range d.ScanRawWords(L) {
			if err != nil {
				t.Fatal(err)
			}
			if m.MatchEntry(raw) {
				w := tsd.DecodeCP1252(raw.WordBytes)
				if opts.ComputeBalances {
					if bal, _ := m.FormatBalance(); bal != "" {
						w += " " + bal
					}
				}
				out = append(out, w)
			}
		}
	}
	return out
}

func TestAll15ValidationCodesAndSpans(t *testing.T) {
	pOpts := pattern.DefaultParseOptions()
	cOpts := pattern.DefaultCompileOptions()

	longDots := strings.Repeat(".", 129)
	manyPunct := "a" + strings.Repeat("-", 645)
	var manyChoices strings.Builder
	for i := 0; i < 257; i++ {
		manyChoices.WriteString("[ab]")
	}

	cases := []struct {
		name     string
		query    string
		wantCode pattern.StatusCode
		wantWarn bool
	}{
		{"valid", "c.t", pattern.StatusValid, false},
		{"err1_empty", "   ", pattern.ErrEmpty, false},
		{"err1_empty_rhs", "cat & ", pattern.ErrEmpty, false},
		{"err1_empty_anag", "cat;", pattern.ErrEmpty, false},
		{"err2_bad_choice_digit", "[a1b]", pattern.ErrInvalidChoiceList, false},
		{"err2_macro_inside_choice", "[@y]", pattern.ErrInvalidChoiceList, false},
		{"err3_too_many_constructs", manyChoices.String(), pattern.ErrTooManyPatterns, false},
		{"err4_too_long", longDots, pattern.ErrTooLong, false},
		{"err5_unclosed_choice", "[abc", pattern.ErrUnclosedChoiceList, false},
		{"err6_two_semicolons", "a;b;c", pattern.ErrMultipleSemicolons, false},
		{"err7_unclosed_sub", "(abc", pattern.ErrUnclosedSubpattern, false},
		{"err7_unclosed_logic", "(a* | b*", pattern.ErrUnclosedSubpattern, false},
		{"err8_unexpected_rparen", "abc)", pattern.ErrUnexpectedCloseParen, false},
		{"err9_unexpected_rbracket", "abc]", pattern.ErrUnexpectedCloseBracket, false},
		{"err10_too_much_punct", manyPunct, pattern.ErrTooMuchPunctuation, false},
		{"err11_invalid_anag_char", ";ab-c", pattern.ErrInvalidAnagramChar, false},
		{"err11_backslash_in_anag", `;ab\c`, pattern.ErrInvalidAnagramChar, false},
		{"err12_caret_outside_choice", "^abc", pattern.ErrUnexpectedCaret, false},
		{"err13_trailing_backslash", `abc\`, pattern.ErrTrailingBackslash, false},
		{"err14_empty_choice", "[]", pattern.ErrEmptyChoiceList, false},
		{"err14_empty_neg_choice", "[^]", pattern.ErrEmptyChoiceList, false},
		{"warn15_disjoint_lengths", "... & ....", pattern.WarnIncompatibleLengths, true},
		{"warn15_universal_contradiction", "... & !...", pattern.WarnIncompatibleLengths, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := pattern.Validate(tc.query, pOpts, cOpts)
			if res.Code != tc.wantCode {
				t.Fatalf("Validate(%q): Code=%d (%s), want %d (%s)",
					tc.query, res.Code, res.Message, tc.wantCode, tc.wantCode.Message())
			}
			if res.HasWarning != tc.wantWarn {
				t.Fatalf("Validate(%q): HasWarning=%v, want %v", tc.query, res.HasWarning, tc.wantWarn)
			}
		})
	}

	// Verify macro error span points back to '@' at byte [1..2] in "[@y]".
	res := pattern.Validate("[@y]", pOpts, cOpts)
	if res.Span.Start != 1 || res.Span.End != 2 {
		t.Fatalf("expected error span [1,2] for [@y], got %+v", res.Span)
	}

	// Verify negation length pre-check fix: ".. & !*ing" MUST allow length 2!
	negRes := pattern.Validate(".. & !*ing", pOpts, cOpts)
	if !negRes.Valid || negRes.HasWarning || negRes.MinMatchedLength != 2 || negRes.MaxMatchedLength != 2 {
		t.Fatalf("unexpected validation for '.. & !*ing': %+v", negRes)
	}
}

func TestEnumerationParsing(t *testing.T) {
	parts, err := pattern.ParseEnumeration("(4, 2-2)")
	if err != nil {
		t.Fatalf("ParseEnumeration: %v", err)
	}
	if got := pattern.EnumerationTemplate(parts); got != ".... ..-.." {
		t.Fatalf("EnumerationTemplate = %q, want %q", got, ".... ..-..")
	}
	if tot := pattern.TotalEnumerationLength(parts); tot != 8 {
		t.Fatalf("TotalEnumerationLength = %d, want 8", tot)
	}
}

func TestTemplatesAnagramsVariablesPunctuationAndSubpatterns(t *testing.T) {
	d := buildTestDict(t,
		"cat", "cot", "cut", "bat",
		"enlist", "listen", "silent", "tinsel",
		"escalator",
		"aorta", "aroma", "drama", "matador",
		"hitter", "thicket", "hittery",
		"peep", "deed", "noon", "radar", "papa", "data",
		"Manchester", "manchester", "café", "cafe",
		"bye-laws", "son-in-law", "son-in law", "cul-de-sac",
		"orangeade", "apostle", "deadbeat", "readable", "compost",
		"ax", "ox", "baking",
	)

	opts := pattern.DefaultCompileOptions()

	// 1. Fixed template & choice list
	if got := evalQuery(t, d, "[bc]at", opts); !slices.Equal(got, []string{"cat", "bat"}) {
		t.Fatalf("[bc]at = %v", got)
	}

	// 2. Pure anagram
	if got := evalQuery(t, d, ";listen", opts); !slices.Equal(got, []string{"enlist", "listen", "silent", "tinsel"}) {
		t.Fatalf(";listen = %v", got)
	}

	// 3. Hybrid super-anagram with +ADDED balance
	if got := evalQuery(t, d, "e........;cats", opts); !slices.Equal(got, []string{"escalator +AELOR"}) {
		t.Fatalf("e........;cats = %v", got)
	}

	// 4. Hybrid sub-anagrams (rack plays) with -OMITTED balance
	wantRack := []string{"aorta -DM", "aroma -DT", "drama -OT", "matador"}
	if got := evalQuery(t, d, ".....*;aadmort", opts); !slices.Equal(got, wantRack) {
		t.Fatalf(".....*;aadmort = %v, want %v", got, wantRack)
	}

	// 5. Hybrid blank + omission budget ("hittery" MUST be rejected at L=7 because only 1 blank '.')
	wantBlank := []string{"hitter -K+R", "thicket +C"}
	if got := evalQuery(t, d, "......*;ehiktt.", opts); !slices.Equal(got, wantBlank) {
		t.Fatalf("......*;ehiktt. = %v, want %v", got, wantBlank)
	}

	// 6. Letter variables: non-exclusive vs exclusive (on "1232": "papa" has 1='p' == 3='p', "data" has 1='d' != 3='t')
	optsNonEx := opts
	optsNonEx.ExclusiveVars = false
	if got := evalQuery(t, d, "1232", optsNonEx); !slices.Equal(got, []string{"papa", "data"}) {
		t.Fatalf("non-exclusive 1232 = %v", got)
	}
	optsEx := opts
	optsEx.ExclusiveVars = true
	if got := evalQuery(t, d, "1232", optsEx); !slices.Equal(got, []string{"data"}) {
		t.Fatalf("exclusive 1232 = %v", got)
	}

	// 7. Case/accent-exact backslash escapes (\M, \é)
	if got := evalQuery(t, d, `\Man*`, opts); !slices.Equal(got, []string{"Manchester"}) {
		t.Fatalf(`\Man* = %v`, got)
	}
	if got := evalQuery(t, d, `caf\é`, opts); !slices.Equal(got, []string{"café"}) {
		t.Fatalf(`caf\é = %v`, got)
	}

	// 8. IgnoreUnusedPunctuation ON vs OFF
	optsIgnOn := opts
	optsIgnOn.IgnoreUnusedPunctuation = true
	// "...-....." (3 letters, '-', 5 letters): matches "son-in law" (space ignored), rejects "cul-de-sac" (2nd '-' active!)
	if got := evalQuery(t, d, "...-.....", optsIgnOn); !slices.Equal(got, []string{"son-in law"}) {
		t.Fatalf("IgnoreUnusedPunct=ON '...-.....' = %v", got)
	}
	// "*-law" with IgnoreUnusedPunct=ON: '*' consumes first '-' in "son-in-law"!
	if got := evalQuery(t, d, "*-law", optsIgnOn); !slices.Equal(got, []string{"son-in-law"}) {
		t.Fatalf("IgnoreUnusedPunct=ON '*-law' = %v", got)
	}

	optsIgnOff := opts
	optsIgnOff.IgnoreUnusedPunctuation = false
	if got := evalQuery(t, d, "...-.....", optsIgnOff); len(got) != 0 {
		t.Fatalf("IgnoreUnusedPunct=OFF '...-.....' expected [], got %v", got)
	}
	if got := evalQuery(t, d, "...-....", optsIgnOff); !slices.Equal(got, []string{"bye-laws"}) {
		t.Fatalf("IgnoreUnusedPunct=OFF '...-....' = %v", got)
	}

	// 9. Sub-patterns in templates and inside anagrams
	if got := evalQuery(t, d, "(;orange)*", opts); !slices.Equal(got, []string{"orangeade"}) {
		t.Fatalf("(;orange)* = %v", got)
	}
	if got := evalQuery(t, d, "*(;post)*", opts); !slices.Equal(got, []string{"apostle", "compost"}) {
		t.Fatalf("*(;post)* = %v", got)
	}
	if got := evalQuery(t, d, ";reble(ada)", opts); !slices.Equal(got, []string{"readable"}) {
		t.Fatalf(";reble(ada) = %v", got)
	}
	if got := evalQuery(t, d, ";deeat(adb)", opts); !slices.Equal(got, []string{"deadbeat"}) {
		t.Fatalf(";deeat(adb) = %v", got)
	}

	// 10. Negated length-2 query (".. & !*ing") and left-to-right boolean grouping
	if got := evalQuery(t, d, ".. & !*ing", opts); !slices.Equal(got, []string{"ax", "ox"}) {
		t.Fatalf(".. & !*ing = %v", got)
	}
	if got := evalQuery(t, d, "(c* | b*) & *t", opts); !slices.Equal(got, []string{"cat", "cot", "cut", "bat", "compost"}) {
		t.Fatalf("(c* | b*) & *t = %v", got)
	}
}

func TestParserAndVMAdversarialBounds(t *testing.T) {
	d := buildTestDict(t,
		strings.Repeat("a", 80),
		"abcdefghijklmnopqrst",
	)
	pOpts := pattern.DefaultParseOptions()
	cOpts := pattern.DefaultCompileOptions()

	t.Run("deep_nested_subpattern_parens_is_linear", func(t *testing.T) {
		q := strings.Repeat("(", 60) + "a*" + strings.Repeat(")", 60)
		res := pattern.Validate(q, pOpts, cOpts, d)
		if !res.Valid {
			t.Fatalf("expected 60 nested subpattern parens to validate quickly, got %+v", res)
		}
	})

	t.Run("excessive_not_chain_rejected_cleanly", func(t *testing.T) {
		q := strings.Repeat("!", 5000) + "cat"
		res := pattern.Validate(q, pOpts, cOpts, d)
		if res.Valid || res.Code != pattern.ErrTooManyPatterns {
			t.Fatalf("expected ErrTooManyPatterns for deep ! chain, got %+v", res)
		}
	})

	t.Run("catastrophic_star_and_nested_sub_backtracking_memoized", func(t *testing.T) {
		for _, q := range []string{
			"*a*a*a*a*a*a*a*a*a*a*z",
			"((((((((*)*)*)*)*)*)*)*)z",
		} {
			if got := evalQuery(t, d, q, cOpts); len(got) != 0 {
				t.Fatalf("unexpected match for %q: %v", q, got)
			}
		}
	})

	t.Run("duplicate_anagram_choices_avoid_factorial_blowup", func(t *testing.T) {
		q := ";" + strings.Repeat("[abcdefghijklmnopqrs]", 19) + "[xy]"
		if got := evalQuery(t, d, q, cOpts); len(got) != 0 {
			t.Fatalf("unexpected match for %q: %v", q, got)
		}
	})
}
