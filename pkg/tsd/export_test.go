package tsd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tisane/pkg/tsd"
)

func TestExportSimpleAndMetadataRoundTrip(t *testing.T) {
	const binDir = "/home/brian/tea/binary"
	if _, err := os.Stat(binDir); os.IsNotExist(err) {
		t.Skip("binary corpus not available")
	}

	t.Run("tsbuild1_simple_and_metadata", func(t *testing.T) {
		d1, err := tsd.Open(filepath.Join(binDir, "tsbuild1.tsd"))
		if err != nil {
			t.Fatalf("Open tsbuild1.tsd: %v", err)
		}
		defer d1.Close()

		// 1. Simple mode: one word per line, deduplicated ("abandon" appears once, annotations stripped)
		var simpleBuf bytes.Buffer
		simpleStats, err := d1.Export(&simpleBuf, tsd.DefaultExportOptions(tsd.ExportSimple))
		if err != nil {
			t.Fatalf("Export simple: %v", err)
		}
		simpleLines := strings.Split(strings.TrimSpace(simpleBuf.String()), "\n")
		expectedSimple := []string{
			"aardvark",
			"abacus",
			"abalone",
			"abandon",
			"Aberdeen",
			"C+",
			"C++",
			"Visual J++",
		}
		if len(simpleLines) != len(expectedSimple) || simpleStats.WordsExported != len(expectedSimple) {
			t.Fatalf("expected %d simple words, got %d (%v)", len(expectedSimple), len(simpleLines), simpleLines)
		}
		for i := range expectedSimple {
			if simpleLines[i] != expectedSimple[i] {
				t.Errorf("simple line %d: got %q, want %q", i, simpleLines[i], expectedSimple[i])
			}
		}

		// 2. Metadata mode: preserves escaped '+' and '|' and annotations, and round-trips through BuildBytes!
		var metaBuf bytes.Buffer
		_, err = d1.Export(&metaBuf, tsd.DefaultExportOptions(tsd.ExportMetadata))
		if err != nil {
			t.Fatalf("Export metadata: %v", err)
		}
		metaText := metaBuf.String()
		for _, wantLine := range []string{
			`abandon+ n.`,
			`abandon+ v.`,
			`Aberdeen+ \| City in Scotland`,
			`C\+\++ A programming language`,
			`Visual J\+\+`,
		} {
			if !strings.Contains(metaText, wantLine+"\n") {
				t.Errorf("metadata output missing expected line %q; got:\n%s", wantLine, metaText)
			}
		}

		// Compile extracted metadata back into a .tsd and verify identical word & annotation set!
		rebuiltBytes, stats, err := tsd.BuildBytes(metaBuf.Bytes(), tsd.BuildOptions{
			MinLen:             1,
			MaxLen:             100,
			MaxFTSPercent:      10,
			SortAlphabetically: true,
			Description:        d1.Description(),
			Alphabet:           d1.AlphabetMap(),
		})
		if err != nil {
			t.Fatalf("BuildBytes from extracted tsbuild1 metadata: %v", err)
		}
		if uint64(stats.UniqueWords) != d1.TotalWords() {
			t.Fatalf("rebuilt UniqueWords=%d, want %d", stats.UniqueWords, d1.TotalWords())
		}
		d1Rebuilt, err := tsd.OpenBytes(rebuiltBytes, "tsbuild1_rebuilt")
		if err != nil {
			t.Fatalf("OpenBytes: %v", err)
		}
		if !bytes.Equal(rebuiltBytes, mustReadFile(t, filepath.Join(binDir, "tsbuild1.tsd"))) {
			// Verify every length bucket entry matches identically
			for L := 1; L <= d1.MaxWordLength(); L++ {
				var orig, reb []tsd.WordEntry
				for w, err := range d1.ScanWords(L) {
					if err != nil {
						t.Fatal(err)
					}
					orig = append(orig, w)
				}
				for w, err := range d1Rebuilt.ScanWords(L) {
					if err != nil {
						t.Fatal(err)
					}
					reb = append(reb, w)
				}
				if len(orig) != len(reb) {
					t.Fatalf("L=%d count mismatch: %d vs %d", L, len(orig), len(reb))
				}
				for i := range orig {
					if orig[i].Word != reb[i].Word || orig[i].Annotation != reb[i].Annotation {
						t.Fatalf("L=%d[%d] mismatch: %+v vs %+v", L, i, orig[i], reb[i])
					}
				}
			}
		}
	})

	t.Run("tsbuild2_definitions_aliases_qualifiers_roundtrip", func(t *testing.T) {
		d2, err := tsd.Open(filepath.Join(binDir, "tsbuild2.tsd"))
		if err != nil {
			t.Fatalf("Open tsbuild2.tsd: %v", err)
		}
		defer d2.Close()

		var metaBuf bytes.Buffer
		metaStats, err := d2.Export(&metaBuf, tsd.DefaultExportOptions(tsd.ExportMetadata))
		if err != nil {
			t.Fatalf("Export metadata: %v", err)
		}
		if metaStats.DefinitionsExport != 16 {
			t.Fatalf("expected 16 exported definitions from tsbuild2.tsd, got %d", metaStats.DefinitionsExport)
		}

		metaText := metaBuf.String()
		for _, wantLine := range []string{
			`undefined`,
			`defined|defined has a plain text definition`,
			`agonise:agonize|shared definition for spelling variants agonise and agonize`,
			`bear+ n.+ n.;bears|definition for bear annotated as noun with bears as an alias`,
			`tap++ 1 v.|first definition for tap with part of speech and sense number in qualifier`,
			`knife+ n.+ n.;knives+ n. pl.|knife definition with annotations for both headword and alias`,
			`fork++ n.;forks|fork definition with qualifier and an alias`,
			`;elusive| has a shareable definition not found by a Full Text Search`,
			`rich|{\rtf1 \b rich\b0  \i adj.\i0  has a definition in Rich Text Format}`,
		} {
			if !strings.Contains(metaText, wantLine+"\n") {
				t.Errorf("tsbuild2 metadata missing expected line %q; got:\n%s", wantLine, metaText)
			}
		}

		// Rebuild a TSD1 binary from the extracted metadata and verify byte-identical match with tsbuild2.tsd!
		rebuiltBytes, buildStats, err := tsd.BuildBytes(metaBuf.Bytes(), tsd.BuildOptions{
			MinLen:             1,
			MaxLen:             100,
			MaxFTSPercent:      10,
			SortAlphabetically: true,
			Description:        d2.Description(),
			Alphabet:           d2.AlphabetMap(),
		})
		if err != nil {
			t.Fatalf("BuildBytes from extracted tsbuild2 metadata: %v", err)
		}
		if buildStats.Pass1Definitions != 1 || buildStats.Pass2Definitions != 15 {
			t.Fatalf("unexpected rebuilt definition counts: %+v", buildStats)
		}
		origBytes := mustReadFile(t, filepath.Join(binDir, "tsbuild2.tsd"))
		if !bytes.Equal(rebuiltBytes, origBytes) {
			t.Fatalf("rebuilt tsbuild2.tsd from extracted metadata was not byte-identical (%d vs %d bytes)",
				len(rebuiltBytes), len(origBytes))
		}
	})
}

func mustReadFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
