package tsd_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tisane/pkg/tsd"
)

func TestBuilderUTF8UnsortedDedupAndFTSCutoff(t *testing.T) {
	var src strings.Builder
	src.WriteString("| UTF-8 custom dictionary test\n")
	src.WriteString("zèbre\tn. striped equine\n")
	src.WriteString("café+ n.+ 1 n.|A coffeehouse serving “espresso” — hot!\n")
	src.WriteString("œuvre|A work of art; see also zèbre\n")
	src.WriteString("dictionary|A book of words and definitions\n")

	// Add 260 alias-only lines to exercise Pass 1 256-entry deduplication & eviction,
	// and >105 Pass 2 definitions sharing the token "commonword" to trigger FTS cutoff.
	for i := 0; i < 130; i++ {
		fmt.Fprintf(&src, ";aliasdup%d|Shared cross-reference stub\n", i)
	}
	for i := 0; i < 265; i++ {
		fmt.Fprintf(&src, ";aliasuniq%d|Unique cross-reference stub %d\n", i, i)
	}
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&src, "entry%d|Definition %d mentioning commonword repeatedly\n", i, i)
	}

	opts := tsd.DefaultBuildOptions()
	opts.SortAlphabetically = false
	opts.TabSeparatedAnnotations = true
	opts.MaxFTSPercent = 5
	opts.Description = "Café — Tisane Test"

	built, stats, err := tsd.BuildBytes([]byte(src.String()), opts)
	if err != nil {
		t.Fatalf("BuildBytes: %v", err)
	}
	// 130 identical stubs collapse into 1 Pass 1 record + 265 unique stubs = 266 Pass 1 records.
	if stats.Pass1Definitions != 266 {
		t.Fatalf("Pass1Definitions = %d, want 266", stats.Pass1Definitions)
	}

	d, err := tsd.OpenBytes(built, "custom")
	if err != nil {
		t.Fatalf("OpenBytes: %v", err)
	}
	defer d.Close()

	if d.Description() != "Café — Tisane Test" {
		t.Fatalf("Description() = %q", d.Description())
	}

	// Verify unsorted order on length 5: "zèbre" appeared before "œuvre" (OEUVRE = 5 letters in Default.TSM since Œ is 1 canonical byte 0x8C!).
	var words5 []tsd.WordEntry
	for w, err := range d.ScanWords(5) {
		if err != nil {
			t.Fatal(err)
		}
		words5 = append(words5, w)
	}
	if len(words5) < 2 || words5[0].Word != "zèbre" || words5[0].Annotation != "n. striped equine" || words5[1].Word != "œuvre" {
		t.Fatalf("unexpected L=5 unsorted entries: %+v", words5)
	}

	// Verify "commonword" (10 letters) triggered FTSOverflowSentinel in Sub-stream 2.
	foundCommon := false
	for fe, err := range d.ScanFTSOnlyWords(10) {
		if err != nil {
			t.Fatal(err)
		}
		if string(fe.Canonical) == "COMMONWORD" {
			foundCommon = true
			if fe.OccListOffset != tsd.FTSOverflowSentinel {
				t.Fatalf("expected FTSOverflowSentinel for COMMONWORD, got 0x%X", fe.OccListOffset)
			}
			if _, err := d.ReadFTSOccurrences(fe.OccListOffset); err != tsd.ErrFTSOverflow {
				t.Fatalf("expected ErrFTSOverflow, got %v", err)
			}
		}
	}
	if !foundCommon {
		t.Fatalf("COMMONWORD not found in L=10 FTS-only stream")
	}
}

func TestBuilderEmptyInput(t *testing.T) {
	built, stats, err := tsd.BuildBytes([]byte("| comment only\n"), tsd.DefaultBuildOptions())
	if err != nil {
		t.Fatalf("BuildBytes(empty): %v", err)
	}
	if stats.UniqueWords != 0 || stats.MaxWordLength != 0 {
		t.Fatalf("unexpected stats for empty dictionary: %+v", stats)
	}
	d, err := tsd.OpenBytes(built, "empty")
	if err != nil {
		t.Fatalf("OpenBytes(empty): %v", err)
	}
	defer d.Close()
	if d.TotalWords() != 0 || d.MaxWordLength() != 0 || d.HasDefinitions() {
		t.Fatalf("unexpected empty dictionary properties")
	}
}

func TestBuildFileRejectsDirectoryAndOversizedInput(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.tsd")

	if _, err := tsd.BuildFile(dir, outPath, tsd.DefaultBuildOptions()); err == nil {
		t.Fatal("expected BuildFile to reject directory input")
	}

	sparse := filepath.Join(dir, "huge.txt")
	f, err := os.Create(sparse)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = f.Close()
	if err := os.Truncate(sparse, tsd.MaxBuildInputBytes+1); err != nil {
		t.Skipf("os.Truncate sparse file unsupported: %v", err)
	}
	if _, err := tsd.BuildFile(sparse, outPath, tsd.DefaultBuildOptions()); err == nil || !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("expected BuildFile size-limit error, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := tsd.BuildBytesContext(ctx, []byte("alpha\nbeta\n"), tsd.DefaultBuildOptions()); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from BuildBytesContext, got %v", err)
	}
}
