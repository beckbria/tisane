package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIBuildAndInfo(t *testing.T) {
	wordnetPath := filepath.Join("..", "..", "dictionaries", "English Wordnet 2025 Plus.tsd")
	if _, err := os.Stat(wordnetPath); os.IsNotExist(err) {
		t.Skip("dictionaries/English Wordnet 2025 Plus.tsd not present")
	}

	tmpDir := t.TempDir()
	inPath := filepath.Join(tmpDir, "input.txt")
	if err := os.WriteFile(inPath, []byte("alpha\nbeta\ngamma|Greek letter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(tmpDir, "built.tsd")

	var stdout, stderr bytes.Buffer
	code := run(
		[]string{
			"-d", "Synthetic Test Dictionary",
			inPath,
			outPath,
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("run build exited %d; stderr=%s", code, stderr.String())
	}

	// Test --info on both the newly built TSD1 file and the bundled WordNet TSD1 file.
	stdout.Reset()
	stderr.Reset()
	code = run(
		[]string{
			"--info",
			outPath,
			wordnetPath,
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("run --info exited %d; stderr=%s", code, stderr.String())
	}
	outStr := stdout.String()
	if strings.Count(outStr, "Format:        TSD1") != 2 || !strings.Contains(outStr, "English WordNet 2025+") {
		t.Fatalf("unexpected --info output:\n%s", outStr)
	}

	// Test stdin ('-') build mode.
	stdinOut := filepath.Join(tmpDir, "from_stdin.tsd")
	stdout.Reset()
	stderr.Reset()
	code = run(
		[]string{"-q", "-i", "-", "-o", stdinOut},
		strings.NewReader("alpha\nbeta\ngamma|Greek letter\n"),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("stdin build exited %d; stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected quiet mode to produce empty stdout, got %q", stdout.String())
	}
	if info, err := os.Stat(stdinOut); err != nil || info.Size() == 0 {
		t.Fatalf("stdin output missing or empty: %v", err)
	}

	// Misplaced flag after positional argument returns exit 2 instead of writing a file named "-q".
	if code := run([]string{inPath, "-q"}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("expected exit 2 for misplaced trailing flag, got %d", code)
	}
	// Invalid length bounds and input==output collision return exit 2.
	if code := run([]string{"--min-len", "20", "--max-len", "5", inPath, outPath}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("expected exit 2 for min-len > max-len, got %d", code)
	}
	if code := run([]string{inPath, inPath}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("expected exit 2 for input == output collision, got %d", code)
	}
}
