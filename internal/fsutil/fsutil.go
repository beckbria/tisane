// Package fsutil provides cross-platform atomic file writing, path identity checks,
// and CLI positional-argument validation shared across Tisane libraries and tools.
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AtomicOptions configures permissions, temp-file naming, and character-device handling
// for WriteFileAtomic.
type AtomicOptions struct {
	DirPerm         os.FileMode
	FilePerm        os.FileMode
	TempPattern     string
	AllowCharDevice bool
}

// WriteFileAtomic writes to a temporary sibling file in outputPath's parent directory,
// syncs and closes it, and renames it over outputPath so readers never observe partial writes.
func WriteFileAtomic(outputPath string, opts AtomicOptions, writeFn func(*os.File) error) error {
	if strings.TrimSpace(outputPath) == "" {
		return fmt.Errorf("output path cannot be empty")
	}

	dirPerm := opts.DirPerm
	if dirPerm == 0 {
		dirPerm = 0o755
	}
	filePerm := opts.FilePerm
	if filePerm == 0 {
		filePerm = 0o644
	}
	pattern := opts.TempPattern
	if pattern == "" {
		pattern = ".tisane-*.tmp"
	}

	if info, err := os.Stat(outputPath); err == nil {
		mode := info.Mode()
		if mode.IsDir() {
			return fmt.Errorf("output path %q is a directory", outputPath)
		}
		if (mode & os.ModeCharDevice) != 0 {
			if !opts.AllowCharDevice {
				return fmt.Errorf("output path %q is not a regular file", outputPath)
			}
			f, openErr := os.OpenFile(outputPath, os.O_WRONLY, filePerm)
			if openErr != nil {
				return openErr
			}
			wErr := writeFn(f)
			cErr := f.Close()
			if wErr != nil {
				return wErr
			}
			return cErr
		}
		if !mode.IsRegular() {
			return fmt.Errorf("output path %q is not a regular file", outputPath)
		}
	}

	dir := filepath.Dir(outputPath)
	if dir == "" {
		dir = "."
	}
	if dir != "." {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return fmt.Errorf("create directory %q: %w", dir, err)
		}
	}

	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return fmt.Errorf("create temp file in %q: %w", dir, err)
	}
	tmpPath := f.Name()
	keepTmp := false
	defer func() {
		if !keepTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := writeFn(f); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync temp file for %q: %w", outputPath, err)
	}
	// os.CreateTemp creates files with 0600; widen only when requested.
	if filePerm != 0o600 {
		_ = f.Chmod(filePerm)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp file for %q: %w", outputPath, err)
	}
	if err := os.Rename(tmpPath, outputPath); err != nil {
		return fmt.Errorf("replace %q: %w", outputPath, err)
	}
	keepTmp = true
	return nil
}

// WritePublicFile writes outputPath atomically with 0755 directory / 0644 file permissions,
// permitting character devices such as os.DevNull for CLI workflows.
func WritePublicFile(outputPath string, writeFn func(*os.File) error) error {
	return WriteFileAtomic(outputPath, AtomicOptions{
		DirPerm:         0o755,
		FilePerm:        0o644,
		AllowCharDevice: true,
	}, writeFn)
}

// WritePublicBytes writes data atomically to outputPath with 0755 directory / 0644 file permissions.
func WritePublicBytes(outputPath string, data []byte) error {
	return WritePublicFile(outputPath, func(f *os.File) error {
		_, err := f.Write(data)
		return err
	})
}

// WritePrivateBytes writes data atomically to outputPath with 0700 directory / 0600 file permissions,
// rejecting character devices and other non-regular targets.
func WritePrivateBytes(outputPath string, data []byte) error {
	return WriteFileAtomic(outputPath, AtomicOptions{
		DirPerm:         0o700,
		FilePerm:        0o600,
		TempPattern:     ".config-*.tmp",
		AllowCharDevice: false,
	}, func(f *os.File) error {
		_, err := f.Write(data)
		return err
	})
}

// SameFileOrClean reports whether non-empty, non-stdio paths a and b clean to the same path
// or resolve via os.Stat to the same underlying file.
func SameFileOrClean(a, b string) bool {
	if a == "" || b == "" || a == "-" || b == "-" {
		return false
	}
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ia, errA := os.Stat(a)
	ib, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(ia, ib)
}

// FindMisplacedFlag returns the first flag-like argument in pos when Go's standard flag package
// stopped parsing at the first positional argument (unless "--" explicitly ended option parsing).
func FindMisplacedFlag(args, pos []string) string {
	if len(pos) == 0 {
		return ""
	}
	parsed := len(args) - len(pos)
	if parsed > 0 && args[parsed-1] == "--" {
		return ""
	}
	for _, p := range pos {
		if len(p) > 1 && strings.HasPrefix(p, "-") {
			return p
		}
	}
	return ""
}
