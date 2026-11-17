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
	"strings"
	"time"
)

// deterministicModTime ensures reproducible archive outputs across builds.
var deterministicModTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

type archiveEntry struct {
	srcPath    string
	dstPath    string
	executable bool
}

type entrySliceFlag []string

func (f *entrySliceFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *entrySliceFlag) Set(val string) error {
	*f = append(*f, val)
	return nil
}

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
	var bins entrySliceFlag
	var files entrySliceFlag
	fs.Var(&bins, "bin", "executable binary mapping in src=dst format (repeatable)")
	fs.Var(&files, "file", "regular file mapping in src=dst format (repeatable)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *format != "tar.gz" && *format != "zip" {
		return fmt.Errorf("unsupported -format %q (want \"tar.gz\" or \"zip\")", *format)
	}
	if *outPath == "" {
		return fmt.Errorf("-out is required")
	}

	entries, err := parseEntries(bins, files)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("at least one -bin or -file entry is required")
	}

	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	switch *format {
	case "tar.gz":
		return writeTarGz(*outPath, entries)
	case "zip":
		return writeZip(*outPath, entries)
	default:
		return fmt.Errorf("unsupported format %q", *format)
	}
}

func parseEntries(bins, files []string) ([]archiveEntry, error) {
	seen := make(map[string]bool, len(bins)+len(files))
	entries := make([]archiveEntry, 0, len(bins)+len(files))

	add := func(raw string, executable bool) error {
		idx := strings.LastIndex(raw, "=")
		if idx <= 0 || idx == len(raw)-1 {
			return fmt.Errorf("invalid mapping %q (expected <src>=<dst>)", raw)
		}
		src := filepath.FromSlash(raw[:idx])
		dst := path.Clean(strings.ReplaceAll(raw[idx+1:], "\\", "/"))
		dst = strings.TrimPrefix(dst, "/")
		if dst == "" || dst == "." || strings.HasPrefix(dst, "../") {
			return fmt.Errorf("invalid archive destination path %q", raw[idx+1:])
		}
		if seen[dst] {
			return fmt.Errorf("duplicate archive destination path %q", dst)
		}
		seen[dst] = true
		entries = append(entries, archiveEntry{
			srcPath:    src,
			dstPath:    dst,
			executable: executable,
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

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].dstPath < entries[j].dstPath
	})
	return entries, nil
}

func collectParentDirs(entries []archiveEntry) []string {
	dirSet := make(map[string]bool)
	for _, e := range entries {
		dir := path.Dir(e.dstPath)
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

func writeTarGz(outPath string, entries []archiveEntry) (retErr error) {
	outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open output file: %w", err)
	}
	defer func() {
		if cerr := outFile.Close(); retErr == nil {
			retErr = cerr
		}
	}()

	gzWriter, err := gzip.NewWriterLevel(outFile, gzip.BestCompression)
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

func addFileToTar(tw *tar.Writer, e archiveEntry) error {
	in, err := os.Open(e.srcPath)
	if err != nil {
		return fmt.Errorf("open input %q: %w", e.srcPath, err)
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat input %q: %w", e.srcPath, err)
	}

	mode := int64(0o644)
	if e.executable {
		mode = 0o755
	}

	hdr := &tar.Header{
		Name:     e.dstPath,
		Size:     info.Size(),
		Mode:     mode,
		Typeflag: tar.TypeReg,
		ModTime:  deterministicModTime,
		Format:   tar.FormatPAX,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("write tar header %q: %w", e.dstPath, err)
	}
	if _, err := io.Copy(tw, in); err != nil {
		return fmt.Errorf("copy %q into tar: %w", e.srcPath, err)
	}
	return nil
}

func writeZip(outPath string, entries []archiveEntry) (retErr error) {
	outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open output file: %w", err)
	}
	defer func() {
		if cerr := outFile.Close(); retErr == nil {
			retErr = cerr
		}
	}()

	zw := zip.NewWriter(outFile)
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

func addFileToZip(zw *zip.Writer, e archiveEntry) error {
	in, err := os.Open(e.srcPath)
	if err != nil {
		return fmt.Errorf("open input %q: %w", e.srcPath, err)
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat input %q: %w", e.srcPath, err)
	}

	fh, err := zip.FileInfoHeader(info)
	if err != nil {
		return fmt.Errorf("create zip file header %q: %w", e.srcPath, err)
	}
	fh.Name = e.dstPath
	fh.Method = zip.Deflate
	fh.Modified = deterministicModTime
	if e.executable {
		fh.SetMode(0o755)
	} else {
		fh.SetMode(0o644)
	}

	w, err := zw.CreateHeader(fh)
	if err != nil {
		return fmt.Errorf("write zip header %q: %w", e.dstPath, err)
	}
	if _, err := io.Copy(w, in); err != nil {
		return fmt.Errorf("copy %q into zip: %w", e.srcPath, err)
	}
	return nil
}
