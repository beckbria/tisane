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

func TestRTFSafetyLinkifierSpeedSubstream2AndURLValidation(t *testing.T) {
	// 1. \bin signed int overflow must not panic, malformed \' must not eat '}', and UTF-16 surrogates combine
	_ = lookup.DefinitionToHTML([]byte(`{\rtf1\ansi{\*\pict\bin9223372036854775807 abc}}`))
	malformed := []byte(`{\rtf1\ansi{\b bold\'}\par\pard Header\par\li360 Indented \uc1\u-10179?\u-9032?\par }`)
	htmlOut := lookup.DefinitionToHTML(malformed)
	if !strings.Contains(htmlOut, "<strong>bold</strong>") {
		t.Fatalf("malformed hex escape ate closing brace: %s", htmlOut)
	}
	if !strings.Contains(htmlOut, `<p class="tsd-par">Header</p><p class="tsd-par tsd-indent">Indented`) {
		t.Fatalf("expected \\pard + \\li360 indentation preserved: %s", htmlOut)
	}
	if !strings.Contains(htmlOut, "💸") {
		t.Fatalf("expected UTF-16 surrogate pair \\u-10179\\u-9032 (U+1F4B8) to decode to 💸, got: %s", htmlOut)
	}

	// 2. O(N) linkifier handles 100k trailing ')' and bare schemes instantaneously
	hugeParens := "see http://example.com/path" + strings.Repeat(")", 100_000) + " and mailto: and http://"
	linked := lookup.DefinitionToHTML([]byte(hugeParens))
	if !strings.Contains(linked, `href="http://example.com/path"`) {
		t.Fatalf("unexpected linkified URL in huge-paren input")
	}
	if strings.Contains(linked, `href="mailto:"`) || strings.Contains(linked, `href="http://"`) || !strings.Contains(linked, "mailto:") {
		t.Fatalf("bare scheme mishandled: %s", linked[len(linked)-120:])
	}

	// 3. BuildWebURL / BuildWebLinks reject unsafe schemes and userinfo spoofing
	for _, bad := range []struct{ tmpl, word string }{
		{"javascript:alert({entry})", "1"},
		{"file:///etc/{entry}", "passwd"},
		{"{entry}", "javascript:alert(1)"},
		{"https://{entry}", "user@evil.com"},
	} {
		if got := lookup.BuildWebURL(bad.tmpl, bad.word); got != "" {
			t.Fatalf("BuildWebURL(%q, %q) = %q, want empty", bad.tmpl, bad.word, got)
		}
	}
	filtered := lookup.BuildWebLinks("cat", []lookup.WebProvider{
		{ID: "bad", Name: "Bad", URLTemplate: "javascript:{entry}"},
		{ID: "ok", Name: "OK", URLTemplate: "https://example.com/{entry}?q={entry}"},
	})
	if len(filtered) != 1 || filtered[0].URL != "https://example.com/cat?q=cat" {
		t.Fatalf("BuildWebLinks filtered = %+v", filtered)
	}

	// 4. Sub-stream 2 FTS-only mention lookup & exact-mention vs case-insensitive-direct fallback
	src := strings.Join([]string{
		"alpha|mentions zephyr inside body",
		"March",
		"march|to walk in step",
		"other|mentions March in spring",
	}, "\n") + "\n"
	raw, _, err := tsd.BuildBytes([]byte(src), tsd.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	d, err := tsd.OpenBytes(raw, "sub2")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	// "zephyr" (len 6, same length bucket as "other"=5? No, let's ensure bucket 6 exists!)
	src2 := src + "abcdef\n"
	raw2, _, err := tsd.BuildBytes([]byte(src2), tsd.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	d2, err := tsd.OpenBytes(raw2, "sub2b")
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()

	zephyrRes, err := lookup.Lookup(context.Background(), "zephyr", []*tsd.Dictionary{d2}, lookup.Options{
		CaseInsensitiveFallback: true,
		IncludeMentions:         true,
	})
	if err != nil {
		t.Fatalf("Lookup(zephyr): %v", err)
	}
	if len(zephyrRes.Entries) != 1 || !zephyrRes.Entries[0].IsMention {
		t.Fatalf("expected 1 Sub-stream 2 mention for 'zephyr', got %+v", zephyrRes.Entries)
	}

	// "March" has only an FTS mention while "march" has a direct definition;
	// Lookup("March") with IncludeMentions=false must fall back to "march"'s direct definition!
	marchRes, err := lookup.Lookup(context.Background(), "March", []*tsd.Dictionary{d2}, lookup.Options{
		CaseInsensitiveFallback: true,
		IncludeMentions:         false,
	})
	if err != nil {
		t.Fatalf("Lookup(March): %v", err)
	}
	if len(marchRes.Entries) != 1 || marchRes.Entries[0].IsMention || !strings.Contains(marchRes.Entries[0].Text, "walk in step") {
		t.Fatalf("expected fallback to direct definition of 'march', got %+v", marchRes.Entries)
	}
}

func TestBuildWebURLHardening(t *testing.T) {
	badTemplates := []string{
		"https://{entry}/wiki",
		"https://{entry}.example.com/search",
		"https://{entry}",
		"https://user:pass@example.com/?q={entry}",
		"https://example.com/?q={entry}&bad=\"quote\"",
		"https://example.com/?q={entry}\u202E",
	}
	for _, tmpl := range badTemplates {
		if got := lookup.BuildWebURL(tmpl, "127.0.0.1:8080"); got != "" {
			t.Fatalf("BuildWebURL(%q) = %q, want empty", tmpl, got)
		}
	}

	got := lookup.BuildWebURL("https://en.wiktionary.org/wiki/{entry}", `a"b c:d`)
	if got == "" || strings.ContainsAny(got, `" `) {
		t.Fatalf("unexpected escaped URL %q", got)
	}
}

func TestRTFLinkifierStopsAtNBSPAndTypographicPunctuation(t *testing.T) {
	rtf := []byte(`{\rtf1 Visit \ldblquote https://example.org/entry\rdblquote\~next or https://example.org/dash\emdash see also 'https://example.org/apos\rquote.}`)
	htmlOut := lookup.DefinitionToHTML(rtf)
	for _, wantHref := range []string{
		`href="https://example.org/entry"`,
		`href="https://example.org/dash"`,
		`href="https://example.org/apos"`,
	} {
		if !strings.Contains(htmlOut, wantHref) {
			t.Fatalf("expected %s in rendered RTF HTML, got: %s", wantHref, htmlOut)
		}
	}
}
