package wordlist

import (
	"os"
	"path/filepath"
	"testing"

	"tisane/pkg/tsd"
)

func TestConvertEntry(t *testing.T) {
	cases := []struct {
		name string
		in   string
		conv Conversion
		want string
	}{
		{"none preserves accents and punctuation", "Café-au-lait!", ConvertNone, "Café-au-lait!"},
		{"ascii transliterates accents", "Café-au-lait!", ConvertASCII, "Cafe-au-lait!"},
		{"ascii transliterates ligatures", "Æon œuf Straße", ConvertASCII, "AEon oeuf Strasse"},
		{"ascii transliterates smart quotes and dashes", "O’Brien–Smith…", ConvertASCII, "O'Brien-Smith..."},
		{"ascii strips NFD combining marks", "Cafe\u0301", ConvertASCII, "Cafe"},
		{"ascii drops unmappable non-latin", "hello 世界!", ConvertASCII, "hello !"},
		{"ascii-letters keeps only ASCII letters preserving case", "Café-au-lait 123!", ConvertASCIILetters, "Cafeaulait"},
		{"ascii-letters handles O'Brien", "O’Brien", ConvertASCIILetters, "OBrien"},
		{"ascii-lower lowercases and keeps letters only", "Café-au-lait 123!", ConvertASCIILower, "cafeaulait"},
		{"ascii-lower handles ligatures", "Æon Straße", ConvertASCIILower, "aeonstrasse"},
		{"ascii-upper uppercases and keeps letters only", "Café-au-lait 123!", ConvertASCIIUpper, "CAFEAULAIT"},
		{"ascii-upper handles ligatures", "œuf ß", ConvertASCIIUpper, "OEUFSS"},
		{"ascii-letters drops digits-only entry", "12345---", ConvertASCIILetters, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ConvertEntry(tc.in, tc.conv)
			if got != tc.want {
				t.Fatalf("ConvertEntry(%q, %q) = %q, want %q", tc.in, tc.conv, got, tc.want)
			}
		})
	}
}

func TestMergeBytesOperationsAndConversionOrder(t *testing.T) {
	// Primary has accented/mixed-case/punctuated entries; secondary has plain lowercase equivalents.
	primary := []byte("Café\r\nO'Brien\r\nalpha\r\n12345\r\n")
	secondary := []byte("cafe\nobrien\nbeta\n")

	t.Run("OR with none keeps distinct unconverted entries", func(t *testing.T) {
		out, stats, err := MergeBytes(primary, secondary, MergeOptions{
			Operation:  OpOR,
			Conversion: ConvertNone,
		})
		if err != nil {
			t.Fatalf("MergeBytes: %v", err)
		}
		want := "12345\nCafé\nO'Brien\nalpha\nbeta\ncafe\nobrien\n"
		if string(out) != want {
			t.Fatalf("got:\n%s\nwant:\n%s", string(out), want)
		}
		if stats.PrimaryLines != 4 || stats.SecondaryLines != 3 || stats.OutputEntries != 7 {
			t.Fatalf("unexpected stats: %+v", stats)
		}
	})

	t.Run("AND with none finds no overlap", func(t *testing.T) {
		out, stats, err := MergeBytes(primary, secondary, MergeOptions{
			Operation:  OpAND,
			Conversion: ConvertNone,
		})
		if err != nil {
			t.Fatalf("MergeBytes: %v", err)
		}
		if len(out) != 0 || stats.OutputEntries != 0 {
			t.Fatalf("expected empty output, got %q (%+v)", string(out), stats)
		}
	})

	t.Run("AND with ascii-lower matches after conversion", func(t *testing.T) {
		out, stats, err := MergeBytes(primary, secondary, MergeOptions{
			Operation:  OpAND,
			Conversion: ConvertASCIILower,
		})
		if err != nil {
			t.Fatalf("MergeBytes: %v", err)
		}
		want := "cafe\nobrien\n"
		if string(out) != want {
			t.Fatalf("got:\n%s\nwant:\n%s", string(out), want)
		}
		// 12345 converts to empty string and is excluded from PrimaryEntries.
		if stats.PrimaryEntries != 3 || stats.SecondaryEntries != 3 || stats.OutputEntries != 2 {
			t.Fatalf("unexpected stats: %+v", stats)
		}
	})

	t.Run("OR with ascii-upper deduplicates after conversion", func(t *testing.T) {
		out, stats, err := MergeBytes(primary, secondary, MergeOptions{
			Operation:  OpOR,
			Conversion: ConvertASCIIUpper,
		})
		if err != nil {
			t.Fatalf("MergeBytes: %v", err)
		}
		want := "ALPHA\nBETA\nCAFE\nOBRIEN\n"
		if string(out) != want {
			t.Fatalf("got:\n%s\nwant:\n%s", string(out), want)
		}
		if stats.OutputEntries != 4 {
			t.Fatalf("unexpected stats: %+v", stats)
		}
	})

	t.Run("EXCLUDE with ascii-lower removes converted secondary matches", func(t *testing.T) {
		out, stats, err := MergeBytes(primary, secondary, MergeOptions{
			Operation:  OpEXCLUDE,
			Conversion: ConvertASCIILower,
		})
		if err != nil {
			t.Fatalf("MergeBytes: %v", err)
		}
		want := "alpha\n"
		if string(out) != want {
			t.Fatalf("got:\n%s\nwant:\n%s", string(out), want)
		}
		if stats.OutputEntries != 1 {
			t.Fatalf("unexpected stats: %+v", stats)
		}
	})

	t.Run("EXCLUDE with ascii-letters preserves case so Café != cafe", func(t *testing.T) {
		out, _, err := MergeBytes(primary, secondary, MergeOptions{
			Operation:  OpEXCLUDE,
			Conversion: ConvertASCIILetters,
		})
		if err != nil {
			t.Fatalf("MergeBytes: %v", err)
		}
		want := "Cafe\nOBrien\nalpha\n"
		if string(out) != want {
			t.Fatalf("got:\n%s\nwant:\n%s", string(out), want)
		}
	})
}

func TestMergeBytesEncoding(t *testing.T) {
	// Windows-1252 bytes for "café\n" (0xE9 = é)
	cp1252Primary := []byte{'c', 'a', 'f', 0xE9, '\n'}
	// UTF-8 with BOM for "CAFE\n"
	utf8BOMSecondary := append([]byte{0xEF, 0xBB, 0xBF}, []byte("CAFE\n")...)

	out, _, err := MergeBytes(cp1252Primary, utf8BOMSecondary, MergeOptions{
		Operation:  OpAND,
		Conversion: ConvertASCIILower,
		Encoding:   tsd.EncodingAuto,
	})
	if err != nil {
		t.Fatalf("MergeBytes: %v", err)
	}
	if string(out) != "cafe\n" {
		t.Fatalf("got %q, want %q", string(out), "cafe\n")
	}
}

func TestMergeFilesWithParams(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "primary.txt")
	p2 := filepath.Join(dir, "secondary.txt")
	outPath := filepath.Join(dir, "merged.txt")

	if err := os.WriteFile(p1, []byte("Apple\nBanana\nRésumé\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, []byte("banana\nresume\nCherry\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	written, stats, err := MergeFilesWithParams(MergeParams{
		PrimaryPath:   p1,
		SecondaryPath: p2,
		OutputPath:    outPath,
		Operation:     "exclude",
		Conversion:    "ascii-lower",
		Encoding:      "auto",
	})
	if err != nil {
		t.Fatalf("MergeFilesWithParams: %v", err)
	}
	if written != outPath {
		t.Fatalf("written = %q, want %q", written, outPath)
	}
	if stats.OutputEntries != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "apple\n" {
		t.Fatalf("output file = %q, want %q", string(data), "apple\n")
	}
}

func TestDefaultMergedOutputPath(t *testing.T) {
	inPath := filepath.Join("tmp", "words.txt")
	wantPath := filepath.Join("tmp", "words-merged.txt")
	if got := DefaultMergedOutputPath(inPath); got != wantPath {
		t.Fatalf("got %q, want %q", got, wantPath)
	}
	if got := DefaultMergedOutputPath("list.lst"); got != "list-merged.txt" {
		t.Fatalf("got %q", got)
	}
	if got := DefaultMergedOutputPath(""); got != "merged.txt" {
		t.Fatalf("got %q", got)
	}
}

func TestParseValidationErrors(t *testing.T) {
	if _, err := ParseConversion("bogus"); err == nil {
		t.Fatal("expected error for invalid conversion")
	}
	if _, err := ParseOperation("xor"); err == nil {
		t.Fatal("expected error for invalid operation")
	}
	if _, _, err := MergeBytes([]byte("a\n"), []byte("b\n"), MergeOptions{Encoding: tsd.InputEncoding(99)}); err == nil {
		t.Fatal("expected error for invalid InputEncoding")
	}
}

func TestCROnlyLineEndingsExtendedLatinNFCNFDAndSameFileCollision(t *testing.T) {
	// Classic Mac CR-only line endings + Vietnamese NFC ("Phở") vs NFD ("Pho\u031B\u0309") + Indic "ṛṣi".
	primaryCR := []byte("Phở\rṛṣi\ralpha\r")
	secondaryNFD := []byte("Pho\u031B\u0309\rr\u0323s\u0323i\rbeta\r")

	out, stats, err := MergeBytes(primaryCR, secondaryNFD, MergeOptions{
		Operation:  OpAND,
		Conversion: ConvertASCIILower,
	})
	if err != nil {
		t.Fatalf("MergeBytes CR+NFC/NFD: %v", err)
	}
	if string(out) != "pho\nrsi\n" || stats.PrimaryLines != 3 || stats.SecondaryLines != 3 || stats.OutputEntries != 2 {
		t.Fatalf("got %q (%+v), want \"pho\\nrsi\\n\"", string(out), stats)
	}

	dir := t.TempDir()
	p1 := filepath.Join(dir, "p1.txt")
	p2 := filepath.Join(dir, "p2.txt")
	linkOut := filepath.Join(dir, "alias-p1.txt")
	if err := os.WriteFile(p1, primaryCR, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, secondaryNFD, 0o644); err != nil {
		t.Fatal(err)
	}

	// Direct same-path collision must fail without truncating input.
	if _, err := MergeFiles(p1, p2, p1, MergeOptions{}); err == nil {
		t.Fatal("expected error when outputPath == primaryPath")
	}
	// Hardlink/symlink os.SameFile collision must also be rejected.
	if err := os.Link(p1, linkOut); err == nil {
		if _, err := MergeFiles(p1, p2, linkOut, MergeOptions{}); err == nil {
			t.Fatal("expected error when outputPath is os.SameFile with primaryPath")
		}
	}
}
