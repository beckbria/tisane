package search_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"tisane/pkg/pattern"
	"tisane/pkg/search"
	"tisane/pkg/tsd"
)

func openRepoDict(t *testing.T, rel string) *tsd.Dictionary {
	t.Helper()
	p := filepath.Join("..", "..", "dictionaries", rel)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		t.Skipf("dictionary not present: %s", p)
	}
	d, err := tsd.Open(p)
	if err != nil {
		t.Fatalf("Open(%s): %v", rel, err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func buildSynthDict(t *testing.T, name, content string) *tsd.Dictionary {
	t.Helper()
	opts := tsd.DefaultBuildOptions()
	opts.Description = name
	opts.MaxFTSPercent = 100
	built, _, err := tsd.BuildBytes([]byte(content), opts)
	if err != nil {
		t.Fatalf("BuildBytes(%s): %v", name, err)
	}
	d, err := tsd.OpenBytes(built, name)
	if err != nil {
		t.Fatalf("OpenBytes(%s): %v", name, err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func matchWords(matches []search.Match) []string {
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.Word
	}
	return out
}

func TestMultiDictDedupeSortingFiltersEnumerationAndDefEnrichment(t *testing.T) {
	wordnet := openRepoDict(t, "English Wordnet 2025 Plus.tsd") // TSD1 (no definitions)
	defs := buildSynthDict(t, "defs", "cynic|one who sneers\n") // TSD1 (has definitions)

	eng := search.NewEngine()

	// 1. Cross-dictionary deduplication & HasDefinition enrichment on wordlist-only match!
	// WordNet contains both "Cynic" and "cynic" (2 intra-dict entries), while defs also contains "cynic".
	// DedupeNone yields 3 matches; DedupeSuppress preserves both WordNet entries (DictIndex 0) and suppresses defs's duplicate.
	reqNone := search.DefaultRequest("cynic", wordnet, defs)
	reqNone.DedupeMode = search.DedupeNone
	sumNone, err := eng.Search(context.Background(), reqNone)
	if err != nil {
		t.Fatalf("Search(cynic, DedupeNone): %v", err)
	}
	if len(sumNone.Matches) != 3 {
		t.Fatalf("expected 3 un-deduplicated matches for 'cynic', got %d (%+v)", len(sumNone.Matches), sumNone.Matches)
	}

	req := search.DefaultRequest("cynic", wordnet, defs)
	req.DedupeMode = search.DedupeSuppress
	sum, err := eng.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("Search(cynic): %v", err)
	}
	if got := matchWords(sum.Matches); !slices.Equal(got, []string{"Cynic", "cynic"}) {
		t.Fatalf("expected intra-dict [Cynic, cynic] preserved and cross-dict duplicate suppressed, got %v", got)
	}
	for _, m := range sum.Matches {
		if m.DictIndex != 0 || !m.HasDefinition {
			t.Fatalf("expected DictIndex=0 with HasDefinition=true, got %+v", m)
		}
	}

	// DedupeMergeSources records both sources [0, 1] without dropping WordNet's lowercase twin.
	req.DedupeMode = search.DedupeMergeSources
	sumMerge, err := eng.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(sumMerge.Matches) != 2 || !slices.Equal(sumMerge.Matches[0].AllDictIndices, []int{0, 1}) || !slices.Equal(sumMerge.Matches[1].AllDictIndices, []int{0, 1}) {
		t.Fatalf("expected 2 merged matches with AllDictIndices=[0, 1], got %+v", sumMerge.Matches)
	}

	// 2. Smart Balance Sort on sub-anagram rack play `.....*;aadmort`
	rackReq := search.DefaultRequest(".....*;aadmort", wordnet)
	rackReq.Sort = search.SortLenSmart
	rackSum, err := eng.Search(context.Background(), rackReq)
	if err != nil {
		t.Fatal(err)
	}
	if len(rackSum.Matches) == 0 || rackSum.Matches[0].Word != "matador" || rackSum.Matches[0].AnagramBalance != "" {
		t.Fatalf("expected exact 7-letter anagram 'matador' first under SortLenSmart, got %+v", rackSum.Matches)
	}
	// Verify balance lengths strictly non-decreasing across all results!
	prevBalChars := 0
	for _, m := range rackSum.Matches {
		balChars := 0
		for i := 0; i < len(m.AnagramBalance); i++ {
			if m.AnagramBalance[i] != '+' && m.AnagramBalance[i] != '-' {
				balChars++
			}
		}
		if balChars < prevBalChars {
			t.Fatalf("SortLenSmart out of order: %q (%s) after %d balance chars", m.Word, m.AnagramBalance, prevBalChars)
		}
		prevBalChars = balChars
	}

	// 3. Entry category filters (Lowercase, Capitalized, Punctuated, Phrase) & Enumeration
	customTSD, _, err := tsd.BuildBytes([]byte(strings.Join([]string{
		"apple",
		"Apple",
		"by-law",
		"don't ask",
		"ice cream",
	}, "\n")+"\n"), tsd.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	cd, err := tsd.OpenBytes(customTSD, "cats")
	if err != nil {
		t.Fatal(err)
	}
	defer cd.Close()

	catCases := []struct {
		mask search.EntryTypeMask
		want []string
	}{
		{search.EntryLowercase, []string{"apple"}},
		{search.EntryCapitalized, []string{"Apple"}},
		{search.EntryPunctuated, []string{"by-law"}},
		{search.EntryPhrase, []string{"don't ask", "ice cream"}},
	}
	for _, cc := range catCases {
		r := search.DefaultRequest("*", cd)
		r.EntryFilter = cc.mask
		s, err := eng.Search(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		if got := matchWords(s.Matches); !slices.Equal(got, cc.want) {
			t.Fatalf("mask 0x%X: got %v, want %v", cc.mask, got, cc.want)
		}
	}

	// Enumeration "4,3" matches "don't ask" (4 letters, apostrophe ignored, space, 3 letters)!
	enumParts, err := pattern.ParseEnumeration("4,3")
	if err != nil {
		t.Fatal(err)
	}
	enumReq := search.DefaultRequest("*", cd)
	enumReq.Enumeration = enumParts
	enumReq.IgnoreUnusedPunctuation = false // Even with /U:0, --enum ignores apostrophes!
	enumSum, err := eng.Search(context.Background(), enumReq)
	if err != nil {
		t.Fatal(err)
	}
	if got := matchWords(enumSum.Matches); !slices.Equal(got, []string{"don't ask"}) {
		t.Fatalf("Enumeration 4,3 = %v, want [don't ask]", got)
	}
}

func TestFullTextSearchIndexedVsSequentialParity(t *testing.T) {
	tsb2 := buildSynthDict(t, "fts-sample", strings.Join([]string{
		"tap|A 3-letter word",
		"word|A 4-letter entry enabling length-4 FTS index",
		"alias|A synonym for another word",
		"agonize|To suffer extreme pain",
		"agonise|British spelling of agonize",
		"colour:color|Visual hue sensation",
		"Crossword Man|Author of TEA",
		"TEA Support|{\\rtf1\\ansi Contact CrosswordMan for rtf help via mailto:support@crosswordman.com }",
		"Web Help|{\\rtf1\\ansi Visit our rtf web page over http }",
		"cynic|A blackguard whose faulty vision sees things as they are",
		"lexicon|A dictionary compiled by a Lexicographer",
		"glossary|A dictionary without a lexicographer note",
		"listen|To pay attention to sound",
		"silent|Making no sound",
		"quiet|listen or silent without music",
		"longbody|contains " + strings.Repeat("z", 105) + " overlong token",
	}, "\n")+"\n")

	eng := search.NewEngine()

	queries := []struct {
		dict       *tsd.Dictionary
		query      string
		wantIdxFTS bool
	}{
		{tsb2, "synonym*", false},            // Unbounded '*' exceeds indexed length buckets -> Sequential FTS fallback
		{tsb2, "agoni[sz]e", true},           // Bounded length 7 -> Indexed FTS!
		{tsb2, "crosswordman & !http", true}, // Exercises pure Indexed set_difference (A & !B)!
		{tsb2, "rtf & (web | mailto)", true},
		{tsb2, "dictionary & !lexicographer", true},
		{tsb2, "(;listen | ;silent) & !music", true},
		{tsb2, "visual", true},                       // Multi-headword Pass 2 record (colour:color, length 6 indexed)
		{tsb2, `dictionary & !\Lexicographer`, true}, // Hybrid indexed LHS + case-exact sequential filter!
		{tsb2, strings.Repeat("z", 100), false},      // 105-letter body token must be ignored, not truncated to 100!
	}

	for _, tc := range queries {
		t.Run(tc.dict.Name()+"/"+tc.query, func(t *testing.T) {
			idxReq := search.DefaultRequest(tc.query, tc.dict)
			idxReq.Mode = search.ModeFullText
			idxReq.DedupeMode = search.DedupeSuppress
			idxSum, err := eng.Search(context.Background(), idxReq)
			if err != nil {
				t.Fatalf("Indexed FTS(%q): %v", tc.query, err)
			}
			if tc.wantIdxFTS && !idxSum.UsedIndexedFTS {
				t.Errorf("expected UsedIndexedFTS=true for %q", tc.query)
			}

			seqReq := idxReq
			seqReq.ForceSequentialFTS = true
			seqSum, err := eng.Search(context.Background(), seqReq)
			if err != nil {
				t.Fatalf("Sequential FTS(%q): %v", tc.query, err)
			}

			gotIdx := matchWords(idxSum.Matches)
			gotSeq := matchWords(seqSum.Matches)
			if !slices.Equal(gotIdx, gotSeq) {
				t.Fatalf("Indexed vs Sequential FTS mismatch on %q:\n  indexed (%d): %v\n  seq     (%d): %v",
					tc.query, len(gotIdx), gotIdx, len(gotSeq), gotSeq)
			}
			if tc.dict == tsb2 && tc.query == "crosswordman & !http" {
				if !slices.Equal(gotIdx, []string{"Crossword Man", "TEA Support"}) {
					t.Fatalf("expected [Crossword Man, TEA Support], got %v", gotIdx)
				}
			}
			if tc.dict == tsb2 && tc.query == "visual" {
				if !slices.Equal(gotIdx, []string{"colour", "color"}) {
					t.Fatalf("expected multi-headword [colour, color], got %v", gotIdx)
				}
			}
			if tc.query == strings.Repeat("z", 100) && len(gotSeq) != 0 {
				t.Fatalf("expected >100-letter body token to be skipped, got %v", gotSeq)
			}
		})
	}
}

func TestIntraDictTwinsMentionOnlyHasDefinitionAndSortModes(t *testing.T) {
	d1 := buildSynthDict(t, "d1", strings.Join([]string{
		"March",
		"march|walk in step",
		"unlinked",
		"other|mentions unlinked in body",
	}, "\n")+"\n")
	d2 := buildSynthDict(t, "d2", "march|duplicate in lower-priority dict\nbeta\nalpha\n")

	eng := search.NewEngine()

	// 1. DedupeSuppress preserves intra-dict "March" + "march" in d1 while suppressing d2's "march"!
	req := search.DefaultRequest("march", d1, d2)
	req.DedupeMode = search.DedupeSuppress
	sum, err := eng.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got := matchWords(sum.Matches); !slices.Equal(got, []string{"March", "march"}) {
		t.Fatalf("DedupeSuppress on 'march' = %v, want [March, march]", got)
	}

	// 2. Mention-only word "unlinked" must report HasDefinition == false!
	uReq := search.DefaultRequest("unlinked", d1)
	uSum, err := eng.Search(context.Background(), uReq)
	if err != nil {
		t.Fatal(err)
	}
	if len(uSum.Matches) != 1 || uSum.Matches[0].HasDefinition {
		t.Fatalf("expected HasDefinition=false on mention-only word 'unlinked', got %+v", uSum.Matches)
	}

	// 3. EntryNone rejects all matches cleanly instead of defaulting to EntryAll
	noneMask, err := search.ParseEntryFilter("none")
	if err != nil {
		t.Fatal(err)
	}
	noneReq := search.DefaultRequest("*", d1)
	noneReq.EntryFilter = noneMask
	noneSum, err := eng.Search(context.Background(), noneReq)
	if err != nil {
		t.Fatal(err)
	}
	if len(noneSum.Matches) != 0 {
		t.Fatalf("expected 0 matches with EntryNone, got %v", matchWords(noneSum.Matches))
	}

	// 4. SortMatches modes including external SortLenSmart balanceLen backfill
	ext := []search.Match{
		{Word: "zebra", Length: 5, AnagramBalance: "+AB"},
		{Word: "ant", Length: 3, AnagramBalance: ""},
		{Word: "cat", Length: 3, AnagramBalance: "-D"},
	}
	search.SortMatches(ext, search.SortLenSmart)
	if got := matchWords(ext); !slices.Equal(got, []string{"ant", "cat", "zebra"}) {
		t.Fatalf("SortLenSmart external = %v, want [ant, cat, zebra]", got)
	}
	search.SortMatches(ext, search.SortAlphaDesc)
	if got := matchWords(ext); !slices.Equal(got, []string{"zebra", "cat", "ant"}) {
		t.Fatalf("SortAlphaDesc = %v, want [zebra, cat, ant]", got)
	}
	search.SortMatches(ext, search.SortLenDesc)
	if got := matchWords(ext); !slices.Equal(got, []string{"zebra", "ant", "cat"}) {
		t.Fatalf("SortLenDesc = %v, want [zebra, ant, cat]", got)
	}
}

func TestSearchContextCancellation(t *testing.T) {
	wordnet := openRepoDict(t, "English Wordnet 2025 Plus.tsd")
	eng := search.NewEngine()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := search.DefaultRequest("*", wordnet)

	batches := 0
	for _, err := range eng.SearchStream(ctx, req) {
		if err != nil {
			if err == context.Canceled {
				break
			}
			t.Fatalf("unexpected stream error: %v", err)
		}
		batches++
		if batches == 2 {
			cancel()
		}
	}
	if batches < 2 {
		t.Fatalf("expected at least 2 batches before cancel, got %d", batches)
	}
}
