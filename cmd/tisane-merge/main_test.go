package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIMergeOperationsAndConversions(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "list1.txt")
	p2 := filepath.Join(dir, "list2.txt")
	outPath := filepath.Join(dir, "merged.txt")

	if err := os.WriteFile(p1, []byte("Café\nO'Brien\nAlpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, []byte("cafe\nobrien\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("AND with ascii-lower positional args", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"--op", "and", "--convert", "ascii-lower", p1, p2, outPath}, nil, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit %d, stderr=%s", code, stderr.String())
		}
		got, err := os.ReadFile(outPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "cafe\nobrien\n" {
			t.Fatalf("got %q, want %q", string(got), "cafe\nobrien\n")
		}
		if !strings.Contains(stdout.String(), "Output:      2 entries") {
			t.Fatalf("unexpected stdout: %s", stdout.String())
		}
	})

	t.Run("EXCLUDE with ascii-upper and flags", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"-1", p1, "-2", p2, "-o", outPath, "--op", "exclude", "-c", "ascii-upper", "-q"}, nil, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit %d, stderr=%s", code, stderr.String())
		}
		got, err := os.ReadFile(outPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "ALPHA\n" {
			t.Fatalf("got %q, want %q", string(got), "ALPHA\n")
		}
		if stdout.Len() != 0 {
			t.Fatalf("expected quiet stdout, got %q", stdout.String())
		}
	})

	t.Run("stdout output mode", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"--op", "or", "-c", "ascii-letters", p1, p2, "-"}, nil, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit %d, stderr=%s", code, stderr.String())
		}
		want := "Alpha\nCafe\nOBrien\nbeta\ncafe\nobrien\n"
		if stdout.String() != want {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
		if !strings.Contains(stderr.String(), "Output:      6 entries") {
			t.Fatalf("expected summary on stderr, got %q", stderr.String())
		}
	})

	t.Run("version flag", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"--version"}, nil, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		if !strings.HasPrefix(stdout.String(), "tisane-merge ") {
			t.Fatalf("unexpected version output: %q", stdout.String())
		}
	})

	t.Run("missing args returns exit 2", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{p1}, nil, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit 2, got %d", code)
		}
	})

	t.Run("invalid conversion returns exit 2", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"-c", "invalid", p1, p2, outPath}, nil, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit 2, got %d", code)
		}
	})

	t.Run("misplaced trailing flag returns exit 2", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{p1, p2, "-q"}, nil, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit 2 for misplaced trailing flag, got %d", code)
		}
	})
}
