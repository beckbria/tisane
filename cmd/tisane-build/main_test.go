package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIBuildAndInfo(t *testing.T) {
	binDir := filepath.Join("..", "..", "binary")
	if _, err := os.Stat(binDir); os.IsNotExist(err) {
		t.Skip("binary test corpus not present")
	}

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "tsbuild2_cli.tsd")

	var stdout, stderr bytes.Buffer
	code := run(
		[]string{
			"-d", "TEA&Sympathy Dictionary Builder Example 2",
			"-m", filepath.Join(binDir, "Default.TSM"),
			filepath.Join(binDir, "tsbuild2.txt"),
			outPath,
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("run build exited %d; stderr=%s", code, stderr.String())
	}

	gotBytes, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	wantBytes, err := os.ReadFile(filepath.Join(binDir, "tsbuild2.tsd"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotBytes, wantBytes) {
		t.Fatalf("tisane-build output did not match binary/tsbuild2.tsd byte-for-byte")
	}

	// Test --info on both TSD1 and TSD0 files.
	stdout.Reset()
	stderr.Reset()
	code = run(
		[]string{
			"--info",
			outPath,
			filepath.Join(binDir, "Dictionaries", "Core English.tsd"),
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("run --info exited %d; stderr=%s", code, stderr.String())
	}
	outStr := stdout.String()
	if !strings.Contains(outStr, "Format:        TSD1") || !strings.Contains(outStr, "Format:        TSD0") {
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
}
