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
	p := filepath.Join("..", "..", "binary", rel)
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

func matchWords(matches []search.Match) []string {
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.Word
	}
	return out
}

func TestMultiDictDedupeSortingFiltersEnumerationAndDefEnrichment(t *testing.T) {
	core := openRepoDict(t, filepath.Join("Dictionaries", "Core English.tsd"))            // TSD0 (no definitions)
	devil := openRepoDict(t, filepath.Join("Dictionaries", "The Devil's Dictionary.tsd")) // TSD1 (has definitions)

	eng := search.NewEngine()

	// 1. Cross-dictionary deduplication & HasDefinition enrichment on TSD0 match!
	req := search.DefaultRequest("cynic", core, devil)
	req.DedupeMode = search.DedupeSuppress
	sum, err := eng.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("Search(cynic): %v", err)
	}
	if len(sum.Matches) != 1 {
		t.Fatalf("expected 1 deduplicated match for 'cynic', got %d (%+v)", len(sum.Matches), sum.Matches)
	}
	// Won by Core English (DictIndex 0, TSD0), enriched with HasDefinition=true from Devil's Dictionary!
	if sum.Matches[0].DictIndex != 0 || !sum.Matches[0].HasDefinition {
		t.Fatalf("expected DictIndex=0 with HasDefinition=true, got %+v", sum.Matches[0])
	}

	// DedupeMergeSources records both sources [0, 1].
	req.DedupeMode = search.DedupeMergeSources
	sumMerge, err := eng.Search(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(sumMerge.Matches) != 1 || !slices.Equal(sumMerge.Matches[0].AllDictIndices, []int{0, 1}) {
		t.Fatalf("expected AllDictIndices=[0, 1], got %+v", sumMerge.Matches)
	}

	// 2. Smart Balance Sort on sub-anagram rack play `.....*;aadmort`
	rackReq := search.DefaultRequest(".....*;aadmort", core)
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
	tsb2 := openRepoDict(t, "tsbuild2.tsd")
	devil := openRepoDict(t, filepath.Join("Dictionaries", "The Devil's Dictionary.tsd"))
	grose := openRepoDict(t, filepath.Join("Dictionaries", "1811 Grose's Dictionary.tsd"))

	eng := search.NewEngine()

	queries := []struct {
		dict       *tsd.Dictionary
		query      string
		wantIdxFTS bool
	}{
		{tsb2, "synonym*", false},            // Unbounded '*' exceeds indexed length buckets -> Sequential FTS fallback
		{tsb2, "agoni[sz]e", true},           // Bounded length 7 (WordCount(7)==5 > 0) -> Indexed FTS!
		{tsb2, "crosswordman & !http", true}, // Exercises Indexed set_difference (A & !B)!
		{tsb2, "rtf & (web | mailto)", true},
		{devil, "dictionary & !lexicographer", true},
		{devil, "(;listen | ;silent) & !music", true},
		{devil, `dictionary & !\Lexicographer`, false}, // Hybrid indexed + case-exact sequential filter
		{grose, "mistress & !brothel", true},
	}

	for _, tc := range queries {
		t.Run(tc.dict.Name()+"/"+tc.query, func(t *testing.T) {
			idxReq := search.DefaultRequest(tc.query, tc.dict)
			idxReq.Mode = search.ModeFullText
			idxReq.DedupeMode = search.DedupeNone
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
				if !slices.Equal(gotIdx, []string{"TEA Support"}) {
					t.Fatalf("expected [TEA Support], got %v", gotIdx)
				}
			}
		})
	}
}

func TestSearchContextCancellation(t *testing.T) {
	unedited := openRepoDict(t, filepath.Join("Dictionaries", "Unedited English.tsd"))
	eng := search.NewEngine()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := search.DefaultRequest("*", unedited)

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
