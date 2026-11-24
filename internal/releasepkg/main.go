// Command releasepkg is a cross-platform Bazel build helper that packages
// compiled Tisane binaries and dictionary files into a deterministic .tar.gz
// or .zip archive for distribution without relying on host tar/zip shell tools.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"time"

	"tisane/internal/buildutil"
)

// deterministicModTime ensures reproducible archive outputs across builds.
var deterministicModTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "releasepkg: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("releasepkg", flag.ContinueOnError)
	format := fs.String("format", "", "archive format: tar.gz or zip")
	outPath := fs.String("out", "", "output archive path")
	var bins buildutil.EntrySliceFlag
	var files buildutil.EntrySliceFlag
	fs.Var(&bins, "bin", "executable binary mapping in src=dst format (repeatable)")
	fs.Var(&files, "file", "regular file mapping in src=dst format (repeatable)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}
	if *format != "tar.gz" && *format != "zip" {
		return fmt.Errorf("unsupported -format %q (want \"tar.gz\" or \"zip\")", *format)
	}
	if *outPath == "" {
		return fmt.Errorf("-out is required")
	}

	entries, err := buildutil.ParseEntries(bins, files)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("at least one -bin or -file entry is required")
	}

	switch *format {
	case "tar.gz":
		return writeAtomically(*outPath, func(f *os.File) error {
			return writeTarGzTo(f, entries)
		})
	case "zip":
		return writeAtomically(*outPath, func(f *os.File) error {
			return writeZipTo(f, entries)
		})
	default:
		return fmt.Errorf("unsupported format %q", *format)
	}
}

func writeAtomically(outPath string, writeFn func(f *os.File) error) error {
	dir := filepath.Dir(outPath)
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".releasepkg-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp archive: %w", err)
	}
	tmpPath := tmp.Name()
	keepTmp := false
	defer func() {
		if !keepTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := writeFn(tmp); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync archive: %w", err)
	}
	_ = tmp.Chmod(0o644)
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close archive: %w", err)
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		return fmt.Errorf("rename archive: %w", err)
	}
	keepTmp = true
	return nil
}

func collectParentDirs(entries []buildutil.PackagedEntry) []string {
	dirSet := make(map[string]bool)
	for _, e := range entries {
		dir := path.Dir(e.DstPath)
		for dir != "." && dir != "/" && dir != "" {
			dirSet[dir+"/"] = true
			dir = path.Dir(dir)
		}
	}
	dirs := make([]string, 0, len(dirSet))
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs
}

func writeTarGzTo(out io.Writer, entries []buildutil.PackagedEntry) (retErr error) {
	gzWriter, err := gzip.NewWriterLevel(out, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("init gzip writer: %w", err)
	}
	gzWriter.ModTime = deterministicModTime
	defer func() {
		if cerr := gzWriter.Close(); retErr == nil {
			retErr = cerr
		}
	}()

	tw := tar.NewWriter(gzWriter)
	defer func() {
		if cerr := tw.Close(); retErr == nil {
			retErr = cerr
		}
	}()

	for _, dir := range collectParentDirs(entries) {
		hdr := &tar.Header{
			Name:     dir,
			Mode:     0o755,
			Typeflag: tar.TypeDir,
			ModTime:  deterministicModTime,
			Format:   tar.FormatPAX,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("write tar dir header %q: %w", dir, err)
		}
	}

	for _, e := range entries {
		if err := addFileToTar(tw, e); err != nil {
			return err
		}
	}
	return nil
}

func addFileToTar(tw *tar.Writer, e buildutil.PackagedEntry) error {
	in, err := os.Open(e.SrcPath)
	if err != nil {
		return fmt.Errorf("open input %q: %w", e.SrcPath, err)
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat input %q: %w", e.SrcPath, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("input %q is not a regular file", e.SrcPath)
	}

	mode := int64(0o644)
	if e.Executable {
		mode = 0o755
	}

	hdr := &tar.Header{
		Name:     e.DstPath,
		Size:     info.Size(),
		Mode:     mode,
		Typeflag: tar.TypeReg,
		ModTime:  deterministicModTime,
		Format:   tar.FormatPAX,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("write tar header %q: %w", e.DstPath, err)
	}
	if _, err := io.Copy(tw, in); err != nil {
		return fmt.Errorf("copy %q into tar: %w", e.SrcPath, err)
	}
	return nil
}

func writeZipTo(out io.Writer, entries []buildutil.PackagedEntry) (retErr error) {
	zw := zip.NewWriter(out)
	defer func() {
		if cerr := zw.Close(); retErr == nil {
			retErr = cerr
		}
	}()

	for _, dir := range collectParentDirs(entries) {
		fh := &zip.FileHeader{
			Name:     dir,
			Modified: deterministicModTime,
		}
		fh.SetMode(os.ModeDir | 0o755)
		if _, err := zw.CreateHeader(fh); err != nil {
			return fmt.Errorf("create zip dir header %q: %w", dir, err)
		}
	}

	for _, e := range entries {
		if err := addFileToZip(zw, e); err != nil {
			return err
		}
	}
	return nil
}

func addFileToZip(zw *zip.Writer, e buildutil.PackagedEntry) error {
	in, err := os.Open(e.SrcPath)
	if err != nil {
		return fmt.Errorf("open input %q: %w", e.SrcPath, err)
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat input %q: %w", e.SrcPath, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("input %q is not a regular file", e.SrcPath)
	}

	fh, err := zip.FileInfoHeader(info)
	if err != nil {
		return fmt.Errorf("create zip file header %q: %w", e.SrcPath, err)
	}
	fh.Name = e.DstPath
	fh.Method = zip.Deflate
	fh.Modified = deterministicModTime
	if e.Executable {
		fh.SetMode(0o755)
	} else {
		fh.SetMode(0o644)
	}

	w, err := zw.CreateHeader(fh)
	if err != nil {
		return fmt.Errorf("write zip header %q: %w", e.DstPath, err)
	}
	if _, err := io.Copy(w, in); err != nil {
		return fmt.Errorf("copy %q into zip: %w", e.SrcPath, err)
	}
	return nil
}
