// Package buildutil provides shared validation, file staging, and version parsing
// helpers for Tisane's cross-platform release packaging and Windows installer tools.
package buildutil

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// EntrySliceFlag implements flag.Value for repeatable <src>=<dst> CLI flags.
type EntrySliceFlag []string

func (f *EntrySliceFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *EntrySliceFlag) Set(val string) error {
	*f = append(*f, val)
	return nil
}

// PackagedEntry describes a validated source-to-destination file mapping.
type PackagedEntry struct {
	SrcPath    string
	DstPath    string // normalized forward-slash relative path
	Executable bool
}

var windowsReservedNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// ValidateDestRelPath validates and normalizes a relative archive/installer destination path.
// It rejects leading slashes, traversal segments, empty segments, Windows illegal characters,
// trailing spaces/dots, and Windows reserved device names.
func ValidateDestRelPath(rawDst string) (string, error) {
	if rawDst == "" {
		return "", fmt.Errorf("empty destination path")
	}
	if strings.HasPrefix(rawDst, "/") || strings.HasPrefix(rawDst, "\\") {
		return "", fmt.Errorf("destination path %q must be relative (no leading slash)", rawDst)
	}
	slashed := strings.ReplaceAll(rawDst, "\\", "/")
	if strings.HasSuffix(slashed, "/") {
		return "", fmt.Errorf("destination path %q must name a file, not a directory", rawDst)
	}

	segments := strings.Split(slashed, "/")
	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("invalid segment %q in destination path %q", seg, rawDst)
		}
		if strings.HasSuffix(seg, " ") || strings.HasSuffix(seg, ".") {
			return "", fmt.Errorf("segment %q in destination path %q has trailing space or dot", seg, rawDst)
		}
		for i := 0; i < len(seg); i++ {
			b := seg[i]
			if b < 0x20 || b == 0x7F {
				return "", fmt.Errorf("control character in destination path %q", rawDst)
			}
			switch b {
			case ':', '*', '?', '"', '<', '>', '|':
				return "", fmt.Errorf("illegal character %q in destination path %q", b, rawDst)
			}
		}
		stem := seg
		if dot := strings.IndexByte(stem, '.'); dot >= 0 {
			stem = stem[:dot]
		}
		if windowsReservedNames[strings.ToLower(stem)] {
			return "", fmt.Errorf("Windows reserved device name %q in destination path %q", seg, rawDst)
		}
	}

	cleaned := path.Clean(slashed)
	if cleaned == "." || cleaned == "" || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("invalid destination path %q", rawDst)
	}
	return cleaned, nil
}

// ParseEntries parses and validates repeatable <src>=<dst> mappings for binaries and data files,
// rejecting case-insensitive path collisions and file/directory prefix conflicts.
func ParseEntries(bins, files []string) ([]PackagedEntry, error) {
	seenLower := make(map[string]string, len(bins)+len(files))
	dirLower := make(map[string]string)
	entries := make([]PackagedEntry, 0, len(bins)+len(files))

	add := func(raw string, executable bool) error {
		idx := strings.LastIndex(raw, "=")
		if idx <= 0 || idx == len(raw)-1 {
			return fmt.Errorf("invalid mapping %q (expected <src>=<dst>)", raw)
		}
		src := filepath.FromSlash(raw[:idx])
		dst, err := ValidateDestRelPath(raw[idx+1:])
		if err != nil {
			return err
		}
		lowerDst := strings.ToLower(dst)
		if prev, dup := seenLower[lowerDst]; dup {
			return fmt.Errorf("duplicate destination path %q (conflicts with %q)", dst, prev)
		}
		if dirOwner, conflict := dirLower[lowerDst]; conflict {
			return fmt.Errorf("destination file %q conflicts with parent directory of %q", dst, dirOwner)
		}

		parent := path.Dir(dst)
		for parent != "." && parent != "/" && parent != "" {
			lowerParent := strings.ToLower(parent)
			if fileOwner, conflict := seenLower[lowerParent]; conflict {
				return fmt.Errorf("destination path %q requires directory %q which conflicts with file %q", dst, parent, fileOwner)
			}
			dirLower[lowerParent] = dst
			parent = path.Dir(parent)
		}

		seenLower[lowerDst] = dst
		entries = append(entries, PackagedEntry{
			SrcPath:    src,
			DstPath:    dst,
			Executable: executable,
		})
		return nil
	}

	for _, b := range bins {
		if err := add(b, true); err != nil {
			return nil, err
		}
	}
	for _, f := range files {
		if err := add(f, false); err != nil {
			return nil, err
		}
	}

	slices.SortFunc(entries, func(a, b PackagedEntry) int {
		return strings.Compare(a.DstPath, b.DstPath)
	})
	return entries, nil
}

// CopyRegularFile copies a regular file from srcPath to dstPath, rejecting directories or special files.
func CopyRegularFile(srcPath, dstPath string) (retErr error) {
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source %q is not a regular file", srcPath)
	}

	out, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); retErr == nil {
			retErr = cerr
		}
	}()

	_, err = io.Copy(out, in)
	return err
}

// ParseVersionQuad parses a SemVer or dotted 1..4 component version string into [4]uint16.
// Empty strings return {0, 0, 0, 0}; non-numeric or >4-part versions return an error.
func ParseVersionQuad(ver string) ([4]uint16, error) {
	var quad [4]uint16
	ver = strings.TrimSpace(ver)
	if ver == "" {
		return quad, nil
	}
	if i := strings.IndexAny(ver, "-+"); i >= 0 {
		ver = ver[:i]
	}
	if ver == "" {
		return quad, fmt.Errorf("invalid version %q", ver)
	}
	parts := strings.Split(ver, ".")
	if len(parts) > 4 {
		return quad, fmt.Errorf("version %q has more than 4 numeric components", ver)
	}
	for i, p := range parts {
		n, err := strconv.ParseUint(strings.TrimSpace(p), 10, 16)
		if err != nil {
			return quad, fmt.Errorf("invalid numeric version component %q in %q: %w", p, ver, err)
		}
		quad[i] = uint16(n)
	}
	return quad, nil
}

// FormatVersionQuad formats a version string as "A.B.C.D" after validating it with ParseVersionQuad.
func FormatVersionQuad(ver string) (string, error) {
	q, err := ParseVersionQuad(ver)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%d.%d.%d", q[0], q[1], q[2], q[3]), nil
}
