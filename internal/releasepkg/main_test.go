package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestTarGzPackaging(t *testing.T) {
	tmp := t.TempDir()
	binSrc := filepath.Join(tmp, "tisane")
	dictSrc := filepath.Join(tmp, "english-wordnet-2025-plus.tsd")
	outTarGz := filepath.Join(tmp, "out", "linux-release.tar.gz")

	if err := os.WriteFile(binSrc, []byte("ELF-binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dictSrc, []byte("TSD1-dict"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := run([]string{
		"-format=tar.gz",
		"-out=" + outTarGz,
		"-bin=" + binSrc + "=tisane",
		"-file=" + dictSrc + "=dictionaries/english-wordnet-2025-plus.tsd",
	})
	if err != nil {
		t.Fatalf("run(tar.gz): %v", err)
	}

	f, err := os.Open(outTarGz)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	gotFiles := make(map[string]string)
	gotModes := make(map[string]int64)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		gotModes[hdr.Name] = hdr.Mode
		if hdr.Typeflag == tar.TypeReg {
			b, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			gotFiles[hdr.Name] = string(b)
		}
	}

	if gotFiles["tisane"] != "ELF-binary" {
		t.Fatalf("unexpected tisane content: %q", gotFiles["tisane"])
	}
	if gotModes["tisane"] != 0o755 {
		t.Fatalf("expected mode 0755 for tisane, got %o", gotModes["tisane"])
	}
	if gotFiles["dictionaries/english-wordnet-2025-plus.tsd"] != "TSD1-dict" {
		t.Fatalf("unexpected dictionary content: %q", gotFiles["dictionaries/english-wordnet-2025-plus.tsd"])
	}
	if gotModes["dictionaries/english-wordnet-2025-plus.tsd"] != 0o644 {
		t.Fatalf("expected mode 0644 for dictionary, got %o", gotModes["dictionaries/english-wordnet-2025-plus.tsd"])
	}
}

func TestZipPackaging(t *testing.T) {
	tmp := t.TempDir()
	binSrc := filepath.Join(tmp, "tisane.exe")
	dictSrc := filepath.Join(tmp, "english-wordnet-2025-plus.tsd")
	outZip := filepath.Join(tmp, "out", "windows-release.zip")

	if err := os.WriteFile(binSrc, []byte("PE-binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dictSrc, []byte("TSD1-dict"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := run([]string{
		"-format=zip",
		"-out=" + outZip,
		"-bin=" + binSrc + "=tisane.exe",
		"-file=" + dictSrc + "=dictionaries/english-wordnet-2025-plus.tsd",
	})
	if err != nil {
		t.Fatalf("run(zip): %v", err)
	}

	zr, err := zip.OpenReader(outZip)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	gotFiles := make(map[string]string)
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		gotFiles[zf.Name] = string(b)
	}

	if gotFiles["tisane.exe"] != "PE-binary" {
		t.Fatalf("unexpected tisane.exe content: %q", gotFiles["tisane.exe"])
	}
	if gotFiles["dictionaries/english-wordnet-2025-plus.tsd"] != "TSD1-dict" {
		t.Fatalf("unexpected dictionary content: %q", gotFiles["dictionaries/english-wordnet-2025-plus.tsd"])
	}
}
