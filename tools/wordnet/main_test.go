package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"tisane/pkg/tsd"
)

func TestDeriveOutputFilename(t *testing.T) {
	cases := []struct {
		input, requested string
		txtOnly          bool
		want             string
	}{
		{"english-wordnet-2025-plus.xml.gz", "", false, "english-wordnet-2025-plus.tsd"},
		{"/tmp/english-wordnet-2025-plus.xml.gz", "", false, "english-wordnet-2025-plus.tsd"},
		{"english-wordnet-2025.xml.gz", "", false, "english-wordnet-2025.tsd"},
		{"english-wordnet-2024.xml", "", false, "english-wordnet-2024.tsd"},
		{"english-wordnet-2025-plus.xml.gz", "custom.tsd", false, "custom.tsd"},
		{"english-wordnet-2025-plus.xml.gz", "out/custom", false, filepath.Join("out", "custom.tsd")},
		{"english-wordnet-2025-plus.xml.gz", "", true, "english-wordnet-2025-plus.txt"},
		{"english-wordnet-2025-plus.xml.gz", "custom.txt", true, "custom.txt"},
		{"english-wordnet-2025-plus.xml.gz", "out/custom", true, filepath.Join("out", "custom.txt")},
	}
	for _, tc := range cases {
		got := DeriveOutputFilename(tc.input, tc.requested, tc.txtOnly)
		if got != tc.want {
			t.Errorf("DeriveOutputFilename(%q, %q, %v) = %q, want %q", tc.input, tc.requested, tc.txtOnly, got, tc.want)
		}
	}
}

func TestSelectLatestWordnetDump(t *testing.T) {
	links := []string{
		"/downloads/english-wordnet-2023.xml.gz",
		"/downloads/english-wordnet-2024.xml.gz",
		"/downloads/english-wordnet-2025.zip",
		"/downloads/english-wordnet-2025.xml.gz",
		"/downloads/english-wordnet-2025.ttl.gz",
		"/downloads/english-wordnet-2025-plus.zip",
		"/downloads/english-wordnet-2025-plus.xml.gz",
		"/downloads/english-wordnet-2025-plus.ttl.gz",
	}

	cand, err := SelectLatestWordnetDump("https://en-word.net/downloads", links)
	if err != nil {
		t.Fatalf("SelectLatestWordnetDump error: %v", err)
	}
	if cand.URL != "https://en-word.net/downloads/english-wordnet-2025-plus.xml.gz" {
		t.Errorf("got URL %q, want https://en-word.net/downloads/english-wordnet-2025-plus.xml.gz", cand.URL)
	}
	if cand.Filename != "english-wordnet-2025-plus.xml.gz" {
		t.Errorf("got Filename %q, want english-wordnet-2025-plus.xml.gz", cand.Filename)
	}
	if cand.Year != 2025 || !cand.IsPlus || !cand.IsGzip {
		t.Errorf("unexpected candidate metadata: %+v", cand)
	}

	// Verify newer standard year beats older plus year
	linksNewerStd := append(links, "/downloads/english-wordnet-2026.xml.gz")
	cand2, err := SelectLatestWordnetDump("https://en-word.net/downloads", linksNewerStd)
	if err != nil {
		t.Fatalf("SelectLatestWordnetDump error: %v", err)
	}
	if cand2.Filename != "english-wordnet-2026.xml.gz" {
		t.Errorf("got Filename %q, want english-wordnet-2026.xml.gz", cand2.Filename)
	}
}

func TestExtractWordsFromFile(t *testing.T) {
	sampleXML := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE LexicalResource SYSTEM "http://globalwordnet.github.io/schemas/WN-LMF-1.3.dtd">
<LexicalResource xmlns:dc="https://globalwordnet.github.io/schemas/dc/">
  <Lexicon id="oewn" label="Open English Wordnet" language="en" version="2025+">
    <LexicalEntry id="oewn--ap-hood-n">
      <Lemma writtenForm="&apos;hood" partOfSpeech="n"/>
      <Sense id="oewn--apos-hood__1.14.01.." synset="oewn-08242255-n"/>
    </LexicalEntry>
    <LexicalEntry id="oewn-aardwolf-n">
      <Lemma writtenForm="aardwolf" partOfSpeech="n"/>
      <Form writtenForm="aardwolves"/>
      <Sense id="oewn-aardwolf__1.05.00.." synset="oewn-02120828-n"/>
    </LexicalEntry>
    <LexicalEntry id="oewn-run-n">
      <Lemma writtenForm="run" partOfSpeech="n"/>
    </LexicalEntry>
    <LexicalEntry id="oewn-run-v">
      <Lemma writtenForm="run" partOfSpeech="v"/>
      <Form writtenForm="running"/>
    </LexicalEntry>
  </Lexicon>
</LexicalResource>`

	tmp := t.TempDir()
	gzPath := filepath.Join(tmp, "english-wordnet-2025-plus.xml.gz")
	writeGzipFile(t, gzPath, sampleXML)

	var buf bytes.Buffer
	bw := bufio.NewWriter(&buf)
	n, err := ExtractWordsFromFile(gzPath, bw)
	if err != nil {
		t.Fatalf("ExtractWordsFromFile error: %v", err)
	}
	bw.Flush()

	if n != 5 {
		t.Errorf("got %d words, want 5", n)
	}
	want := "'hood\naardwolf\naardwolves\nrun\nrunning\n"
	if buf.String() != want {
		t.Errorf("got:\n%q\nwant:\n%q", buf.String(), want)
	}
}

func TestRemoteDownloadAndBuildTSD(t *testing.T) {
	sampleXML := `<?xml version="1.0" encoding="UTF-8"?>
<LexicalResource>
  <Lexicon id="oewn">
    <LexicalEntry id="oewn-puzzle-n">
      <Lemma writtenForm="puzzle" partOfSpeech="n"/>
    </LexicalEntry>
    <LexicalEntry id="oewn-tisane-n">
      <Lemma writtenForm="tisane" partOfSpeech="n"/>
    </LexicalEntry>
    <LexicalEntry id="oewn-cafe-n">
      <Lemma writtenForm="café" partOfSpeech="n"/>
    </LexicalEntry>
  </Lexicon>
</LexicalResource>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/downloads":
			fmt.Fprint(w, `<a href="/downloads/english-wordnet-2024.xml.gz">2024</a>
<a href="/downloads/english-wordnet-2025.xml.gz">2025</a>
<a href="/downloads/english-wordnet-2025-plus.xml.gz">2025 plus</a>`)
		case "/downloads/english-wordnet-2025-plus.xml.gz":
			var gzBuf bytes.Buffer
			zw := gzip.NewWriter(&gzBuf)
			_, _ = zw.Write([]byte(sampleXML))
			_ = zw.Close()
			w.Write(gzBuf.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tmp := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	// 1. Default mode: downloads, extracts, and builds .tsd directly.
	var stdout, stderr bytes.Buffer
	code := run([]string{"--keep-txt", "-u", srv.URL + "/downloads"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run failed (%d): %s", code, stderr.String())
	}

	outTSD := filepath.Join(tmp, "english-wordnet-2025-plus.tsd")
	d, err := tsd.Open(outTSD)
	if err != nil {
		t.Fatalf("tsd.Open(%s) failed: %v", outTSD, err)
	}
	defer d.Close()

	if d.TotalWords() != 3 {
		t.Errorf("got %d words in .tsd, want 3", d.TotalWords())
	}
	wantDesc := "English WordNet 2025\nUsed under the terms of the CC-BY 4.0 license https://creativecommons.org/licenses/by/4.0/"
	if d.Description() != wantDesc {
		t.Errorf("unexpected description:\ngot:  %q\nwant: %q", d.Description(), wantDesc)
	}

	// Verify --keep-txt preserved the .txt file.
	outTxt := filepath.Join(tmp, "english-wordnet-2025-plus.txt")
	data, err := os.ReadFile(outTxt)
	if err != nil {
		t.Fatalf("failed reading kept output %s: %v", outTxt, err)
	}
	if string(data) != "puzzle\ntisane\ncafé\n" {
		t.Errorf("got kept txt %q, want %q", string(data), "puzzle\ntisane\ncafé\n")
	}

	// 2. --txt-only with --convert ascii-lower
	stdout.Reset()
	stderr.Reset()
	convTxt := filepath.Join(tmp, "converted.txt")
	code = run([]string{"--txt-only", "--convert", "ascii-lower", "-o", convTxt, "-u", srv.URL + "/downloads"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run --txt-only failed (%d): %s", code, stderr.String())
	}
	convData, err := os.ReadFile(convTxt)
	if err != nil {
		t.Fatalf("failed reading converted txt %s: %v", convTxt, err)
	}
	if string(convData) != "cafe\npuzzle\ntisane\n" {
		t.Errorf("got converted txt %q, want %q", string(convData), "cafe\npuzzle\ntisane\n")
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
