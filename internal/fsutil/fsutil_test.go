package fsutil

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	t.Run("public write and atomic overwrite", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "nested", "out.txt")

		if err := WritePublicBytes(out, []byte("first\n")); err != nil {
			t.Fatalf("WritePublicBytes initial: %v", err)
		}
		got, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if !bytes.Equal(got, []byte("first\n")) {
			t.Fatalf("got %q, want %q", got, "first\n")
		}

		if err := WritePublicBytes(out, []byte("second\n")); err != nil {
			t.Fatalf("WritePublicBytes overwrite: %v", err)
		}
		got, err = os.ReadFile(out)
		if err != nil {
			t.Fatalf("ReadFile after overwrite: %v", err)
		}
		if !bytes.Equal(got, []byte("second\n")) {
			t.Fatalf("got %q, want %q", got, "second\n")
		}
	})

	t.Run("private write sets 0600 on unix", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "priv", "config.toml")
		if err := WritePrivateBytes(out, []byte("theme = \"dark\"\n")); err != nil {
			t.Fatalf("WritePrivateBytes: %v", err)
		}
		if runtime.GOOS != "windows" {
			info, err := os.Stat(out)
			if err != nil {
				t.Fatalf("Stat: %v", err)
			}
			if perm := info.Mode().Perm(); perm != 0o600 {
				t.Fatalf("file perm = %04o, want 0600", perm)
			}
		}
	})

	t.Run("write failure cleans up temp file and preserves target", func(t *testing.T) {
		dir := t.TempDir()
		out := filepath.Join(dir, "keep.txt")
		if err := os.WriteFile(out, []byte("intact"), 0o644); err != nil {
			t.Fatalf("seed file: %v", err)
		}

		boom := errors.New("simulated write failure")
		err := WritePublicFile(out, func(f *os.File) error {
			_, _ = f.Write([]byte("partial"))
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want %v", err, boom)
		}

		got, rErr := os.ReadFile(out)
		if rErr != nil {
			t.Fatalf("ReadFile: %v", rErr)
		}
		if string(got) != "intact" {
			t.Fatalf("existing file corrupted: got %q", got)
		}

		entries, dErr := os.ReadDir(dir)
		if dErr != nil {
			t.Fatalf("ReadDir: %v", dErr)
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".tmp") {
				t.Fatalf("leftover temp file %q", e.Name())
			}
		}
	})

	t.Run("rejects empty path and directory target", func(t *testing.T) {
		if err := WritePublicBytes("   ", []byte("x")); err == nil {
			t.Fatal("expected error for blank path")
		}
		dir := t.TempDir()
		if err := WritePublicBytes(dir, []byte("x")); err == nil {
			t.Fatal("expected error when outputPath is an existing directory")
		}
	})

	t.Run("char device allowed for public but rejected for private", func(t *testing.T) {
		info, err := os.Stat(os.DevNull)
		if err != nil || (info.Mode()&os.ModeCharDevice) == 0 {
			t.Skip("os.DevNull is not a character device in this environment")
		}
		if err := WritePublicBytes(os.DevNull, []byte("discard")); err != nil {
			t.Fatalf("WritePublicBytes(os.DevNull): %v", err)
		}
		if err := WritePrivateBytes(os.DevNull, []byte("secret")); err == nil {
			t.Fatal("expected WritePrivateBytes(os.DevNull) to reject char device")
		}
	})
}

func TestSameFileOrClean(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "words.txt")
	if err := os.WriteFile(p, []byte("cat\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if SameFileOrClean("", p) || SameFileOrClean("-", "-") {
		t.Fatal("expected empty and '-' paths to return false")
	}
	if !SameFileOrClean(p, filepath.Join(dir, ".", "words.txt")) {
		t.Fatal("expected cleaned paths to match")
	}

	link := filepath.Join(dir, "alias.txt")
	if err := os.Symlink(p, link); err == nil {
		if !SameFileOrClean(p, link) {
			t.Fatal("expected symlink to match target via os.SameFile")
		}
	}
}

func TestFindMisplacedFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
		pos  []string
		want string
	}{
		{"no positionals", []string{"-q"}, nil, ""},
		{"clean positionals", []string{"-q", "in.txt", "out.tsd"}, []string{"in.txt", "out.tsd"}, ""},
		{"stdin dash allowed", []string{"-", "out.tsd"}, []string{"-", "out.tsd"}, ""},
		{"misplaced flag detected", []string{"in.txt", "--sort"}, []string{"in.txt", "--sort"}, "--sort"},
		{"double dash terminator respected", []string{"--", "-weird.txt"}, []string{"-weird.txt"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FindMisplacedFlag(tc.args, tc.pos); got != tc.want {
				t.Fatalf("FindMisplacedFlag(%v, %v) = %q, want %q", tc.args, tc.pos, got, tc.want)
			}
		})
	}
}
