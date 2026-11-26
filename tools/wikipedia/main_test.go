package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"tisane/pkg/tsd"
)

func TestValidateURLAndEmail(t *testing.T) {
	validURLs := []string{"https://example.com", "http://example.org/contact"}
	for _, u := range validURLs {
		got, err := ValidateURL(u)
		if err != nil {
			t.Errorf("ValidateURL(%q) unexpected error: %v", u, err)
		}
		if got != u {
			t.Errorf("ValidateURL(%q) = %q, want %q", u, got, u)
		}
	}

	invalidURLs := []string{"", "ftp://example.com", "not-a-url", "https://", "https://nodot", "https://bad url.com"}
	for _, u := range invalidURLs {
		if _, err := ValidateURL(u); err == nil {
			t.Errorf("ValidateURL(%q) expected error, got nil", u)
		}
	}

	validEmails := []string{"user@example.com", "first.last+tag@sub.domain.org"}
	for _, e := range validEmails {
		got, err := ValidateEmail(e)
		if err != nil {
			t.Errorf("ValidateEmail(%q) unexpected error: %v", e, err)
		}
		if got != e {
			t.Errorf("ValidateEmail(%q) = %q, want %q", e, got, e)
		}
	}

	invalidEmails := []string{"", "notanemail", "user@nodot", "Name <user@example.com>", "bad email@example.com"}
	for _, e := range invalidEmails {
		if _, err := ValidateEmail(e); err == nil {
			t.Errorf("ValidateEmail(%q) expected error, got nil", e)
		}
	}
}

func TestPromptUserAgent(t *testing.T) {
	input := "invalid-url\nhttps://tisane.example.org\nbad-email\nuser@example.org\n"
	var out bytes.Buffer
	ua, err := PromptUserAgent(bufio.NewReader(strings.NewReader(input)), &out)
	if err != nil {
		t.Fatalf("PromptUserAgent failed: %v", err)
	}
	want := "TisaneDictionary/0.0 (https://tisane.example.org; user@example.org)"
	if ua != want {
		t.Errorf("got User-Agent %q, want %q", ua, want)
	}
}

func TestEnsureOutputFilename(t *testing.T) {
	cases := []struct {
		req, prefix, date string
		txtOnly           bool
		want              string
	}{
		{"", "enwiki", "20261116", false, "enwiki-20261116.tsd"},
		{"wiki.tsd", "enwiki", "20261116", false, "wiki-20261116.tsd"},
		{"out/my-20261116.tsd", "enwiki", "20261116", false, filepath.Join("out", "my-20261116.tsd")},
		{"out/custom", "enwiki", "20261116", false, filepath.Join("out", "custom-20261116.tsd")},
		{"", "enwiki", "20261116", true, "enwiki-20261116-titles.txt"},
		{"titles.txt", "enwiki", "20261116", true, "titles-20261116.txt"},
		{"out/my-20261116-list.txt", "enwiki", "20261116", true, filepath.Join("out", "my-20261116-list.txt")},
		{"out/custom", "enwiki", "20261116", true, filepath.Join("out", "custom-20261116.txt")},
	}
	for _, tc := range cases {
		got := EnsureOutputFilename(tc.req, tc.prefix, tc.date, tc.txtOnly)
		if got != tc.want {
			t.Errorf("EnsureOutputFilename(%q, %q, %q, %v) = %q, want %q", tc.req, tc.prefix, tc.date, tc.txtOnly, got, tc.want)
		}
	}
}

func TestExtractTitlesFromFile(t *testing.T) {
	tmp := t.TempDir()

	// 1. Gzipped pagetitles
	gzPath := filepath.Join(tmp, "enwiki-20261116-all-titles-in-ns-0.gz")
	writeGzipFile(t, gzPath, "page_title\nAlbert_Einstein\nCrossword_puzzle\n\nTea_kettle\n")

	var buf bytes.Buffer
	bw := bufio.NewWriter(&buf)
	n, err := ExtractTitlesFromFile(gzPath, bw)
	if err != nil {
		t.Fatalf("ExtractTitlesFromFile(gz) error: %v", err)
	}
	bw.Flush()
	if n != 3 {
		t.Errorf("got %d titles, want 3", n)
	}
	want := "Albert Einstein\nCrossword puzzle\nTea kettle\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}

	// 2. MediaWiki XML
	xmlPath := filepath.Join(tmp, "sample.xml")
	xmlData := `<mediawiki>
  <page><title>Main Page</title><ns>0</ns><id>1</id></page>
  <page><title>Talk:Main Page</title><ns>1</ns><id>2</id></page>
  <page><title>Alan Turing</title><ns>0</ns><id>3</id></page>
  <page><title>Category:Science</title><ns>14</ns><id>4</id></page>
</mediawiki>`
	if err := os.WriteFile(xmlPath, []byte(xmlData), 0o644); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	bw = bufio.NewWriter(&buf)
	n, err = ExtractTitlesFromFile(xmlPath, bw)
	if err != nil {
		t.Fatalf("ExtractTitlesFromFile(xml) error: %v", err)
	}
	bw.Flush()
	if n != 2 {
		t.Errorf("got %d XML titles, want 2", n)
	}
	if buf.String() != "Main Page\nAlan Turing\n" {
		t.Errorf("got XML output %q", buf.String())
	}
}

func TestResolveDumpURLsAndConfirmation(t *testing.T) {
	var dumpDownloaded atomic.Int32
	var seenUA atomic.Value

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenUA.Store(r.Header.Get("User-Agent"))
		switch r.URL.Path {
		case "/other/pagetitles/":
			fmt.Fprint(w, `<pre><a href="20261115/">20261115/</a><a href="20261116/">20261116/</a></pre>`)
		case "/other/pagetitles/20261116/":
			fmt.Fprint(w, `<pre>
<a href="enwikibooks-20261116-all-titles-in-ns-0.gz">enwikibooks</a>
<a href="enwiki-20261116-all-titles-in-ns-0.gz">enwiki</a>
<a href="enwiki-20261116-pages-meta-history.xml.bz2">history</a>
</pre>`)
		case "/other/pagetitles/20261116/enwiki-20261116-all-titles-in-ns-0.gz":
			dumpDownloaded.Add(1)
			var gzBuf bytes.Buffer
			zw := gzip.NewWriter(&gzBuf)
			_, _ = zw.Write([]byte("page_title\nTest_Title\n"))
			_ = zw.Close()
			w.Write(gzBuf.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Test rejecting full history URL
	if !IsHistoryDumpURL("https://dumps.wikimedia.org/enwiki/latest/enwiki-latest-pages-meta-history.xml.bz2") {
		t.Fatal("expected IsHistoryDumpURL to detect meta-history")
	}

	// Test declining confirmation prevents download
	stdin := strings.NewReader("https://example.org\nuser@example.org\nn\n")
	var stdout, stderr bytes.Buffer
	code := run([]string{"-u", srv.URL + "/other/pagetitles/"}, stdin, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0 on cancel, got %d (stderr: %s)", code, stderr.String())
	}
	if dumpDownloaded.Load() != 0 {
		t.Fatalf("dump should NOT be downloaded when user declines confirmation")
	}
	if gotUA, _ := seenUA.Load().(string); gotUA != "TisaneDictionary/0.0 (https://example.org; user@example.org)" {
		t.Errorf("unexpected User-Agent: %q", gotUA)
	}
}

func TestShardedPipelineMaxTwoFilesAndSingleStream(t *testing.T) {
	var activeDownloads atomic.Int32
	var maxDownloads atomic.Int32
	var maxFilesOnDisk atomic.Int32

	workDir := t.TempDir()

	countFiles := func() int32 {
		entries, err := os.ReadDir(workDir)
		if err != nil {
			return 0
		}
		return int32(len(entries))
	}

	updateMaxFiles := func() {
		cur := countFiles()
		for {
			prev := maxFilesOnDisk.Load()
			if cur <= prev || maxFilesOnDisk.CompareAndSwap(prev, cur) {
				break
			}
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := activeDownloads.Add(1)
		defer activeDownloads.Add(-1)
		for {
			prev := maxDownloads.Load()
			if cur <= prev || maxDownloads.CompareAndSwap(prev, cur) {
				break
			}
		}
		updateMaxFiles()

		var gzBuf bytes.Buffer
		zw := gzip.NewWriter(&gzBuf)
		_, _ = fmt.Fprintf(zw, "page_title\nTitle_From_%s\n", strings.TrimPrefix(r.URL.Path, "/"))
		_ = zw.Close()
		w.Write(gzBuf.Bytes())
	}))
	defer srv.Close()

	urls := []string{
		srv.URL + "/shard1.gz",
		srv.URL + "/shard2.gz",
		srv.URL + "/shard3.gz",
		srv.URL + "/shard4.gz",
	}

	client := NewWikimediaClient("TisaneDictionary/0.0 (https://example.org; user@example.org)")
	var out bytes.Buffer
	bw := bufio.NewWriter(&out)

	// Slow down processing so shard2 finishes downloading before shard1 finishes processing.
	// Shard3 MUST wait until shard1 is processed and deleted.
	n, err := runDownloadPipelineWithHook(context.Background(), client, urls, workDir, bw, false, nil, func(_ string) {
		updateMaxFiles()
		time.Sleep(40 * time.Millisecond)
		updateMaxFiles()
	})
	if err != nil {
		t.Fatalf("pipeline failed: %v", err)
	}
	bw.Flush()

	if n != 4 {
		t.Errorf("got %d titles, want 4", n)
	}
	if maxDownloads.Load() != 1 {
		t.Errorf("max concurrent downloads = %d, want 1", maxDownloads.Load())
	}
	if maxFilesOnDisk.Load() > 2 {
		t.Errorf("max files on disk = %d, want <= 2", maxFilesOnDisk.Load())
	}
	if rem := countFiles(); rem != 0 {
		t.Errorf("expected all processed shards to be deleted, found %d remaining", rem)
	}
}

func TestEndToEndMergeAndBuildTSD(t *testing.T) {
	tmp := t.TempDir()
	dumpPath := filepath.Join(tmp, "enwiki-20261116-all-titles-in-ns-0.gz")
	// Includes duplicates ("West Hall" and "Westhall" collapse under ascii-lower, and exact duplicate "Crossword").
	writeGzipFile(t, dumpPath, "page_title\nCrossword\nCrossword\nAnagrams\nTisane\nWest_Hall\nWesthall\nAlbert_Einstein\n")

	outTSD := filepath.Join(tmp, "wiki.tsd")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--convert", "ascii-lower", "--keep-txt", "-o", outTSD, dumpPath}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("tisane-wiki failed (%d): %s", code, stderr.String())
	}

	expectedTSD := filepath.Join(tmp, "wiki-20261116.tsd")
	d, err := tsd.Open(expectedTSD)
	if err != nil {
		t.Fatalf("tsd.Open(%s) failed: %v", expectedTSD, err)
	}
	defer d.Close()

	// Unique after ascii-lower deduplication: crossword, anagrams, tisane, westhall, alberteinstein = 5
	if d.TotalWords() != 5 {
		t.Errorf("got %d words in .tsd, want 5", d.TotalWords())
	}
	if !strings.Contains(d.Description(), "enwiki 20261116") {
		t.Errorf("unexpected description: %q", d.Description())
	}

	// Verify --keep-txt preserved the intermediate files.
	if _, err := os.Stat(filepath.Join(tmp, "wiki-20261116-titles.txt")); err != nil {
		t.Errorf("expected kept raw titles file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "wiki-20261116-titles-deduped.txt")); err != nil {
		t.Errorf("expected kept deduped titles file: %v", err)
	}
}

func TestTitleTier(t *testing.T) {
	cases := []struct {
		title string
		want  int
	}{
		{"Albert Einstein", 1},
		{"What's Up, Doc?! - Vol./Pt.", 1},
		{"AC/DC", 1},
		{"O'Brien", 1},
		{"Foo (bar)", 2},
		{"A&B", 2},
		{"C++", 2},
		{"Title: Subtitle", 2},
		{"Café", 2},
		{"1984", 3},
		{"Apollo 11", 3},
		{"Foo (1999 film)", 3},
		{"Café 2000", 3},
		{"$100", 3},
	}
	for _, tc := range cases {
		if got := TitleTier(tc.title); got != tc.want {
			t.Errorf("TitleTier(%q) = %d, want %d", tc.title, got, tc.want)
		}
	}
}

func TestTieredSortingOrder(t *testing.T) {
	tmp := t.TempDir()

	// 1. Single pagetitles file with mixed tiers
	gzPath := filepath.Join(tmp, "enwiki-20261116-all-titles-in-ns-0.gz")
	writeGzipFile(t, gzPath, strings.Join([]string{
		"page_title",
		"1984",
		"Foo_(bar)",
		"Albert_Einstein",
		"Apollo_11",
		"Café",
		"What's_Up,_Doc?!_-_Vol./Pt.",
		"Foo_(1999_film)",
		"A&B",
		"Tea_kettle",
	}, "\n")+"\n")

	var buf bytes.Buffer
	bw := bufio.NewWriter(&buf)
	n, err := ExtractTitlesFromFile(gzPath, bw)
	if err != nil {
		t.Fatalf("ExtractTitlesFromFile error: %v", err)
	}
	bw.Flush()
	if n != 9 {
		t.Fatalf("got %d titles, want 9", n)
	}
	want := strings.Join([]string{
		"Albert Einstein",
		"What's Up, Doc?! - Vol./Pt.",
		"Tea kettle",
		"Foo (bar)",
		"Café",
		"A&B",
		"1984",
		"Apollo 11",
		"Foo (1999 film)",
	}, "\n") + "\n"
	if buf.String() != want {
		t.Errorf("sorted pagetitles mismatch:\ngot:\n%s\nwant:\n%s", buf.String(), want)
	}

	// 2. MediaWiki XML with mixed tiers
	xmlPath := filepath.Join(tmp, "mixed.xml")
	xmlData := `<mediawiki>
  <page><title>2001: A Space Odyssey</title><ns>0</ns><id>1</id></page>
  <page><title>Python (programming language)</title><ns>0</ns><id>2</id></page>
  <page><title>Alan Turing</title><ns>0</ns><id>3</id></page>
</mediawiki>`
	if err := os.WriteFile(xmlPath, []byte(xmlData), 0o644); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	bw = bufio.NewWriter(&buf)
	n, err = ExtractTitlesFromFile(xmlPath, bw)
	if err != nil {
		t.Fatalf("ExtractTitlesFromFile(xml) error: %v", err)
	}
	bw.Flush()
	if n != 3 {
		t.Fatalf("got %d XML titles, want 3", n)
	}
	wantXML := "Alan Turing\nPython (programming language)\n2001: A Space Odyssey\n"
	if buf.String() != wantXML {
		t.Errorf("sorted XML mismatch:\ngot:\n%s\nwant:\n%s", buf.String(), wantXML)
	}

	// 3. Multiple local files sort globally across files (--txt-only --no-merge preserves exact tiered order)
	part1 := filepath.Join(tmp, "enwiki-20261116-part1.gz")
	part2 := filepath.Join(tmp, "enwiki-20261116-part2.gz")
	writeGzipFile(t, part1, "page_title\n1984\nFoo_(bar)\nFirst_Basic\n")
	writeGzipFile(t, part2, "page_title\nApollo_11\nSecond_Basic\nBaz_(qux)\n")

	outTxt := filepath.Join(tmp, "combined.txt")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--txt-only", "--no-merge", "-o", outTxt, part1, part2}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run failed (%d): %s", code, stderr.String())
	}
	gotBytes, err := os.ReadFile(filepath.Join(tmp, "combined-20261116.txt"))
	if err != nil {
		t.Fatalf("failed reading combined output: %v", err)
	}
	wantCombined := "First Basic\nSecond Basic\nFoo (bar)\nBaz (qux)\n1984\nApollo 11\n"
	if string(gotBytes) != wantCombined {
		t.Errorf("multi-file sorted output mismatch:\ngot:\n%s\nwant:\n%s", string(gotBytes), wantCombined)
	}
}

func writeGzipFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := gzip.NewWriter(f)
	if _, err := zw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
