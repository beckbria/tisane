package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tisane/pkg/tsd"
)

const repoBinDir = "/home/brian/tea/binary"

func TestTisaneExtractCLI(t *testing.T) {
	if _, err := os.Stat(repoBinDir); os.IsNotExist(err) {
		t.Skip("binary corpus not available")
	}

	devilsPath := filepath.Join(repoBinDir, "Dictionaries", "The Devil's Dictionary.tsd")
	tsb2Path := filepath.Join(repoBinDir, "tsbuild2.tsd")

	t.Run("simple_stdout_discards_definitions_and_annotations", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		code := run([]string{devilsPath}, &out, &errBuf)
		if code != 0 {
			t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
		}
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		if len(lines) != 995 {
			t.Fatalf("expected 995 unique words from The Devil's Dictionary, got %d", len(lines))
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
			MaxFTSPercent:      10,
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
