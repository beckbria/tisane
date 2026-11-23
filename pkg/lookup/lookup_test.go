package lookup_test

import (
	"context"
	"strings"
	"testing"

	"tisane/pkg/lookup"
	"tisane/pkg/tsd"
)

func TestRTFConversionAndXSSSanitization(t *testing.T) {
	rtf := []byte(`{\rtf1\ansi{\fonttbl{\f0\fmodern Courier;}}\sb180\li180\ri360 {\b A'ali Pasha}\par\li360 An {\i eminent} statesman \emdash  <script>alert(1)</script> see http://www.crosswordman.com/ and \uc1\u8226? item.\par }`)

	htmlOut := lookup.DefinitionToHTML(rtf)
	if strings.Contains(htmlOut, "Courier") {
		t.Fatalf("fonttbl leaked into HTML: %s", htmlOut)
	}
	if strings.Contains(htmlOut, "<script>") {
		t.Fatalf("unescaped <script> in HTML: %s", htmlOut)
	}
	for _, want := range []string{
		`<p class="tsd-par"><strong>A&#39;ali Pasha</strong></p>`,
		`<p class="tsd-par tsd-indent">`,
		`<em>eminent</em>`,
		`—`,
		`• item.`,
		`<a href="http://www.crosswordman.com/" target="_blank" rel="noopener noreferrer">http://www.crosswordman.com/</a>`,
	} {
		if !strings.Contains(htmlOut, want) {
			t.Errorf("HTML output missing %q\ngot: %s", want, htmlOut)
		}
	}

	textOut := lookup.DefinitionToText(rtf)
	if strings.Contains(textOut, "Courier") || !strings.Contains(textOut, "A'ali Pasha\n  An eminent statesman —") {
		t.Fatalf("unexpected plain-text output:\n%s", textOut)
	}
}

func TestLookupAndWebURLs(t *testing.T) {
	src := strings.Join([]string{
		"tap|First sense of tap",
		"tap|Second sense of tap",
		"tap|Third sense of tap",
		"evasive|Not direct",
		";elusive|see evasive",
		"Crossword Man|Author of TEA",
		`TEA Support|{\rtf1\ansi Contact CrosswordMan at mailto:support@crosswordman.com }`,
	}, "\n") + "\n"

	opts := tsd.DefaultBuildOptions()
	opts.Description = "Lookup Test"
	built, _, err := tsd.BuildBytes([]byte(src), opts)
	if err != nil {
		t.Fatalf("BuildBytes: %v", err)
	}
	d2, err := tsd.OpenBytes(built, "lookup-test")
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()

	// 1. Multi-sense headword "tap" (3 definitions)
	res, err := lookup.Lookup(context.Background(), "tap", []*tsd.Dictionary{d2}, lookup.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Entries) != 3 {
		t.Fatalf("Lookup(tap): got %d entries, want 3", len(res.Entries))
	}

	// 2. Pass 1 alias-only stub ";elusive"
	resAlias, err := lookup.Lookup(context.Background(), "elusive", []*tsd.Dictionary{d2}, lookup.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resAlias.Entries) != 1 || !resAlias.Entries[0].IsAliasRef {
		t.Fatalf("Lookup(elusive): %+v", resAlias.Entries)
	}

	// 3. "Crossword Man" with IncludeMentions=true (1 direct + 1 FTS mention in "TEA Support")
	resCM, err := lookup.Lookup(context.Background(), "crossword man", []*tsd.Dictionary{d2}, lookup.Options{
		CaseInsensitiveFallback: true,
		IncludeMentions:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resCM.Entries) != 2 || resCM.Entries[0].IsMention || !resCM.Entries[1].IsMention {
		t.Fatalf("Lookup(crossword man) entries = %+v", resCM.Entries)
	}
	if !strings.Contains(resCM.Entries[1].HTML, `href="mailto:support@crosswordman.com"`) {
		t.Fatalf("expected mailto link in TEA Support HTML, got: %s", resCM.Entries[1].HTML)
	}

	// 4. Web fallback URL escaping for phrases & special characters
	links := lookup.BuildWebLinks("ice cream & cake", nil)
	if len(links) < 4 {
		t.Fatalf("expected default web links, got %v", links)
	}
	if links[0].URL != "https://www.onelook.com/?w=ice+cream+%26+cake" {
		t.Fatalf("unexpected OneLook URL: %s", links[0].URL)
	}
	if links[1].URL != "https://en.wiktionary.org/wiki/ice_cream_%26_cake" {
		t.Fatalf("unexpected Wiktionary URL: %s", links[1].URL)
	}
}
