package tsd_test

import (
	"bytes"
	"strings"
	"testing"

	"tisane/pkg/tsd"
)

func TestExportSimpleAndMetadataRoundTrip(t *testing.T) {
	t.Run("tsbuild1_simple_and_metadata", func(t *testing.T) {
		src1 := strings.Join([]string{
			`| Example Word List 1`,
			`aardvark`,
			`abacus`,
			`abalone`,
			`abandon+ n.`,
			`abandon+ v.`,
			`Aberdeen+ \| City in Scotland`,
			`C\+`,
			`C\+\++ A programming language`,
			`Visual J\+\+`,
		}, "\n") + "\n"

		orig1Bytes, _, err := tsd.BuildBytes([]byte(src1), tsd.BuildOptions{
			MinLen:             1,
			MaxLen:             100,
			MaxFTSPercent:      10,
			SortAlphabetically: true,
			Description:        "Example Word List 1",
			Alphabet:           tsd.DefaultTSM(),
		})
		if err != nil {
			t.Fatalf("BuildBytes tsbuild1 fixture: %v", err)
		}

		d1, err := tsd.OpenBytes(orig1Bytes, "tsbuild1")
		if err != nil {
			t.Fatalf("OpenBytes tsbuild1: %v", err)
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
		if !bytes.Equal(rebuiltBytes, orig1Bytes) {
			t.Fatalf("rebuilt tsbuild1 from extracted metadata was not byte-identical (%d vs %d bytes)",
				len(rebuiltBytes), len(orig1Bytes))
		}
	})

	t.Run("tsbuild2_definitions_aliases_qualifiers_roundtrip", func(t *testing.T) {
		// Use higher MaxFTSPercent (50%) on small synthetic fixture so FTS occurrence lists are retained too.
		src2 := strings.Join([]string{
			`| Example Word List 2`,
			`undefined`,
			`defined|defined has a plain text definition`,
			`agonise:agonize|shared definition for spelling variants agonise and agonize`,
			`bear+ n.+ n.;bears|definition for bear annotated as noun with bears as an alias`,
			`tap++ 1 v.|first definition for tap with part of speech and sense number in qualifier`,
			`tap++ 2 v.|second definition for tap as verb`,
			`tap++ 3 n.|third definition for tap as noun`,
			`knife+ n.+ n.;knives+ n. pl.|knife definition with annotations for both headword and alias`,
			`fork++ n.;forks|fork definition with qualifier and an alias`,
			`;elusive| has a shareable definition not found by a Full Text Search`,
			`rich|{\rtf1 \b rich\b0  \i adj.\i0  has a definition in Rich Text Format}`,
			`aid|assistance and support given to someone`,
			`Crossword Man|creator of TEA at http://www.crosswordman.com`,
			`TEA Support|contact crosswordman for dictionary format help`,
			`colour:color|visual sensation produced by wavelengths of light`,
			`spoon|utensil with a shallow bowl`,
			`plate|flat dish from which food is served`,
		}, "\n") + "\n"

		buildOpts := tsd.BuildOptions{
			MinLen:             1,
			MaxLen:             100,
			MaxFTSPercent:      50,
			SortAlphabetically: true,
			Description:        "Example Word List 2",
			Alphabet:           tsd.DefaultTSM(),
		}
		orig2Bytes, origStats, err := tsd.BuildBytes([]byte(src2), buildOpts)
		if err != nil {
			t.Fatalf("BuildBytes tsbuild2 fixture: %v", err)
		}
		if origStats.Pass1Definitions != 1 || origStats.Pass2Definitions != 15 {
			t.Fatalf("unexpected initial fixture counts: %+v", origStats)
		}

		d2, err := tsd.OpenBytes(orig2Bytes, "tsbuild2")
		if err != nil {
			t.Fatalf("OpenBytes tsbuild2: %v", err)
		}
		defer d2.Close()

		var metaBuf bytes.Buffer
		metaStats, err := d2.Export(&metaBuf, tsd.DefaultExportOptions(tsd.ExportMetadata))
		if err != nil {
			t.Fatalf("Export metadata: %v", err)
		}
		if metaStats.DefinitionsExport != 16 {
			t.Fatalf("expected 16 exported definitions, got %d", metaStats.DefinitionsExport)
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
				t.Errorf("metadata missing expected line %q; got:\n%s", wantLine, metaText)
			}
		}

		rebuiltBytes, buildStats, err := tsd.BuildBytes(metaBuf.Bytes(), tsd.BuildOptions{
			MinLen:             1,
			MaxLen:             100,
			MaxFTSPercent:      50,
			SortAlphabetically: true,
			Description:        d2.Description(),
			Alphabet:           d2.AlphabetMap(),
		})
		if err != nil {
			t.Fatalf("BuildBytes from extracted metadata: %v", err)
		}
		if buildStats.Pass1Definitions != 1 || buildStats.Pass2Definitions != 15 {
			t.Fatalf("unexpected rebuilt definition counts: %+v", buildStats)
		}
		if !bytes.Equal(rebuiltBytes, orig2Bytes) {
			t.Fatalf("rebuilt TSD1 from extracted metadata was not byte-identical (%d vs %d bytes)",
				len(rebuiltBytes), len(orig2Bytes))
		}
	})
}
