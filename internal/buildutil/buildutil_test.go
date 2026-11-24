package buildutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateDestRelPathAndParseEntries(t *testing.T) {
	badMappings := []string{
		"src=/abs/path.txt",
		`src=\abs\win.txt`,
		"src=../escape.txt",
		"src=sub/../escape.txt",
		"src=sub//empty.txt",
		"src=C:drive.txt",
		"src=wild*.txt",
		"src=CON.txt",
		"src=dictionaries/nul",
		"src=dictionaries/com1.tsd",
		"src=trailingdot.",
		"src=trailingspace ",
		"src=dir/",
	}
	for _, m := range badMappings {
		if _, err := ParseEntries(nil, []string{m}); err == nil {
			t.Errorf("expected error for invalid mapping %q", m)
		}
	}

	// Case-insensitive duplicate must fail.
	if _, err := ParseEntries([]string{"a=Tisane.exe", "b=tisane.exe"}, nil); err == nil {
		t.Fatal("expected error for case-insensitive duplicate destination")
	}

	// File/directory prefix collision must fail in both orders.
	if _, err := ParseEntries(nil, []string{"a=docs", "b=docs/readme.txt"}); err == nil {
		t.Fatal("expected error for file vs directory prefix collision")
	}
	if _, err := ParseEntries(nil, []string{"b=docs/readme.txt", "a=docs"}); err == nil {
		t.Fatal("expected error for directory vs file prefix collision")
	}
}

func TestParseAndFormatVersionQuad(t *testing.T) {
	if got, err := FormatVersionQuad("0.5.2-rc1+sha"); err != nil || got != "0.5.2.0" {
		t.Fatalf("FormatVersionQuad = (%q, %v), want (\"0.5.2.0\", nil)", got, err)
	}
	if _, err := FormatVersionQuad("not-a-version"); err == nil {
		t.Fatal("expected error for non-numeric version")
	}
	if _, err := FormatVersionQuad("1.2.3.4.5"); err == nil {
		t.Fatal("expected error for 5-component version")
	}
}

func TestCopyRegularFileRejectsDirectories(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.bin")
	if err := CopyRegularFile(dir, dst); err == nil {
		t.Fatal("expected CopyRegularFile to reject directory source")
	}
	src := filepath.Join(dir, "in.bin")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CopyRegularFile(src, dst); err != nil {
		t.Fatalf("CopyRegularFile: %v", err)
	}
}
