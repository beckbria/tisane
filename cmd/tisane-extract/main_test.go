package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tisane/pkg/tsd"
)

func buildExtractFixtures(t *testing.T) (devilsPath string, tsb2Path string) {
	t.Helper()
	dir := t.TempDir()

	devilsSrc := strings.Join([]string{
		`abase+ v.|{\rtf1 To humble or degrade.}`,
		`abase+ n.|Obsolete noun form.`,
		`cynic+ n.|{\rtf1 A blackguard whose faulty vision sees things as they are.}`,
		`ebony+ n.|A hard dark wood.`,
		`zealot+ n.|One who is overly fond of his own opinion.`,
	}, "\n") + "\n"
	devilsPath = filepath.Join(dir, "The Devil's Dictionary.tsd")
	devilsBytes, _, err := tsd.BuildBytes([]byte(devilsSrc), tsd.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(devilsPath, devilsBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	tsb2Src := strings.Join([]string{
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
	tsb2Opts := tsd.DefaultBuildOptions()
	tsb2Opts.MaxFTSPercent = 50
	tsb2Opts.Description = "Example Word List 2"
	tsb2Bytes, _, err := tsd.BuildBytes([]byte(tsb2Src), tsb2Opts)
	if err != nil {
		t.Fatal(err)
	}
	tsb2Path = filepath.Join(dir, "tsbuild2.tsd")
	if err := os.WriteFile(tsb2Path, tsb2Bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	return devilsPath, tsb2Path
}

func TestTisaneExtractCLI(t *testing.T) {
	devilsPath, tsb2Path := buildExtractFixtures(t)

	t.Run("simple_stdout_discards_definitions_and_annotations", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		code := run([]string{devilsPath}, &out, &errBuf)
		if code != 0 {
			t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
		}
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		if len(lines) != 4 {
			t.Fatalf("expected 4 deduplicated words (abase, cynic, ebony, zealot), got %d (%v)", len(lines), lines)
		}
		for i, ln := range lines {
			if strings.Contains(ln, "|") || strings.Contains(ln, "+") || strings.Contains(ln, `{\rtf1`) {
				t.Fatalf("line %d leaked definition/metadata in simple mode: %q", i, ln)
			}
		}
	})

	t.Run("metadata_file_roundtrips_with_tisane_build_and_saves_tsm", func(t *testing.T) {
		tmpDir := t.TempDir()
		extractedTxt := filepath.Join(tmpDir, "tsbuild2-extracted.txt")
		extractedMap := filepath.Join(tmpDir, "tsbuild2.tsm")
		rebuiltTSD := filepath.Join(tmpDir, "tsbuild2-rebuilt.tsd")

		var out, errBuf bytes.Buffer
		code := run([]string{
			"--metadata",
			"--save-map", extractedMap,
			tsb2Path,
			extractedTxt,
		}, &out, &errBuf)
		if code != 0 {
			t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
		}
		if !strings.Contains(errBuf.String(), "Definitions: 16") {
			t.Fatalf("expected stderr summary with 16 definitions, got: %s", errBuf.String())
		}

		tsm, err := tsd.LoadTSM(extractedMap)
		if err != nil {
			t.Fatalf("LoadTSM: %v", err)
		}
		_, err = tsd.BuildFile(extractedTxt, rebuiltTSD, tsd.BuildOptions{
			MinLen:             1,
			MaxLen:             100,
			MaxFTSPercent:      50,
			SortAlphabetically: true,
			Description:        "Example Word List 2",
			Alphabet:           tsm,
		})
		if err != nil {
			t.Fatalf("BuildFile from extracted metadata failed: %v", err)
		}

		dRebuilt, err := tsd.Open(rebuiltTSD)
		if err != nil {
			t.Fatalf("Open rebuilt .tsd: %v", err)
		}
		defer dRebuilt.Close()

		dOrig, err := tsd.Open(tsb2Path)
		if err != nil {
			t.Fatalf("Open orig .tsd: %v", err)
		}
		defer dOrig.Close()

		if dRebuilt.TotalWords() != dOrig.TotalWords() || !dRebuilt.HasDefinitions() || !dRebuilt.HasFTSIndex() {
			t.Fatalf("rebuilt dictionary capabilities mismatch: words=%d (want %d)",
				dRebuilt.TotalWords(), dOrig.TotalWords())
		}
	})

	t.Run("metadata_no_defs_keeps_annotations_only", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		code := run([]string{"--metadata", "--no-defs", "--no-header", tsb2Path}, &out, &errBuf)
		if code != 0 {
			t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
		}
		txt := out.String()
		if !strings.Contains(txt, "knives+ n. pl.\n") {
			t.Fatalf("expected 'knives+ n. pl.' in --metadata --no-defs output, got:\n%s", txt)
		}
		if strings.Contains(txt, "|") || strings.Contains(txt, `{\rtf1`) {
			t.Fatalf("expected no definitions in --no-defs output, got:\n%s", txt)
		}
	})
}

func TestTisaneExtractValidationAndCollisionGuards(t *testing.T) {
	dir := t.TempDir()
	tsdBytes, _, err := tsd.BuildBytes([]byte("alpha\nbeta\n"), tsd.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	dictPath := filepath.Join(dir, "sample.tsd")
	if err := os.WriteFile(dictPath, tsdBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errBuf bytes.Buffer
	if code := run([]string{dictPath, "-q"}, &out, &errBuf); code != 2 {
		t.Fatalf("expected exit 2 for misplaced -q flag, got %d", code)
	}
	if code := run([]string{"--min-len", "15", "--max-len", "3", dictPath}, &out, &errBuf); code != 2 {
		t.Fatalf("expected exit 2 for min-len > max-len, got %d", code)
	}
	if code := run([]string{dictPath, dictPath}, &out, &errBuf); code != 2 {
		t.Fatalf("expected exit 2 for output == input collision, got %d", code)
	}
	sameTarget := filepath.Join(dir, "same.out")
	if code := run([]string{"--save-map", sameTarget, dictPath, sameTarget}, &out, &errBuf); code != 2 {
		t.Fatalf("expected exit 2 for output == save-map collision, got %d", code)
	}
}
