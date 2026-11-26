package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyAndClearDir(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	if err := os.MkdirAll(filepath.Join(src, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte("<h1>hi</h1>"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "src", "app.ts"), []byte("export {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "node_modules", "pkg", "index.js"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyDir(src, dst, map[string]bool{"node_modules": true}); err != nil {
		t.Fatalf("copyDir: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dst, "index.html"))
	if err != nil || string(got) != "<h1>hi</h1>" {
		t.Fatalf("unexpected index.html: %q, err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "src", "app.ts")); err != nil {
		t.Fatalf("expected src/app.ts to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "node_modules")); !os.IsNotExist(err) {
		t.Fatalf("expected node_modules to be skipped, got err=%v", err)
	}

	// Verify copied read-only files are writable in dst.
	if err := os.WriteFile(filepath.Join(dst, "index.html"), []byte("updated"), 0o644); err != nil {
		t.Fatalf("expected dst/index.html to be writable: %v", err)
	}

	if err := clearDir(dst); err != nil {
		t.Fatalf("clearDir: %v", err)
	}
	entries, err := os.ReadDir(dst)
	if err != nil || len(entries) != 0 {
		t.Fatalf("expected empty dir after clearDir, got %d entries, err=%v", len(entries), err)
	}
}

func TestLinkDir(t *testing.T) {
	target := t.TempDir()
	marker := filepath.Join(target, "marker.txt")
	if err := os.WriteFile(marker, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	parent := t.TempDir()
	link := filepath.Join(parent, "linked")
	if err := linkDir(target, link); err != nil {
		t.Fatalf("linkDir: %v", err)
	}

	if _, err := os.Stat(filepath.Join(link, "marker.txt")); err != nil {
		t.Fatalf("expected marker through link: %v", err)
	}

	// Removing the link itself must leave target contents intact.
	if err := os.Remove(link); err != nil {
		t.Fatalf("os.Remove(link): %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("target marker was removed when unlinking: %v", err)
	}
}

func TestResolveCreditsFile(t *testing.T) {
	if got, err := resolveCreditsFile(""); err != nil || got != "" {
		t.Fatalf("empty flag: got (%q, %v), want (\"\", nil)", got, err)
	}

	dir := t.TempDir()
	credits := filepath.Join(dir, "CREDITS.md")
	if err := os.WriteFile(credits, []byte("# Credits\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveCreditsFile(credits)
	if err != nil {
		t.Fatalf("resolveCreditsFile(file): %v", err)
	}
	if got != credits {
		t.Fatalf("resolveCreditsFile(file): got %q, want %q", got, credits)
	}

	if _, err := resolveCreditsFile(dir); err == nil {
		t.Fatalf("expected error for directory path")
	}
	if _, err := resolveCreditsFile(filepath.Join(dir, "missing.md")); err == nil {
		t.Fatalf("expected error for missing file")
	}
}
