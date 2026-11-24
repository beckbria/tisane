// Command tisane-merge converts and merges two plain-text word lists using
// OR (union), AND (intersection), or EXCLUDE (difference) set operations.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tisane"
	"tisane/internal/fsutil"
	"tisane/pkg/wordlist"
)

const maxMergeInputBytes = 512 << 20 // 512 MiB

var version = tisane.Version

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(runContext(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runContext(context.Background(), args, stdin, stdout, stderr)
}

func runContext(ctx context.Context, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tisane-merge", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		primaryPath   string
		secondaryPath string
		outputPath    string
		operation     string
		conversion    string
		encoding      string
		quiet         bool
		showVer       bool
	)

	fs.StringVar(&primaryPath, "1", "", "Primary input word list file")
	fs.StringVar(&primaryPath, "primary", "", "Primary input word list file")
	fs.StringVar(&secondaryPath, "2", "", "Secondary input word list file")
	fs.StringVar(&secondaryPath, "secondary", "", "Secondary input word list file")
	fs.StringVar(&outputPath, "o", "", "Output word list file path ('-' for stdout)")
	fs.StringVar(&outputPath, "output", "", "Output word list file path ('-' for stdout)")
	fs.StringVar(&operation, "op", "or", "Merge operation applied after conversion: or, and, or exclude")
	fs.StringVar(&operation, "operation", "or", "Merge operation applied after conversion: or, and, or exclude")
	fs.StringVar(&conversion, "c", "none", "Entry conversion applied before operation: none, ascii, ascii-letters, ascii-lower, or ascii-upper")
	fs.StringVar(&conversion, "convert", "none", "Entry conversion applied before operation: none, ascii, ascii-letters, ascii-lower, or ascii-upper")
	fs.StringVar(&encoding, "encoding", "auto", "Input text encoding: auto, utf8, or cp1252")
	fs.BoolVar(&quiet, "q", false, "Suppress summary output")
	fs.BoolVar(&quiet, "quiet", false, "Suppress summary output")
	fs.BoolVar(&showVer, "v", false, "Print version and exit")
	fs.BoolVar(&showVer, "version", false, "Print version and exit")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "Tisane Word List Merge Tool v%s\n\n", version)
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  tisane-merge [options] <primary.txt> <secondary.txt> <output.txt>\n\n")
		fmt.Fprintf(stderr, "Conversions (applied before the merge operation):\n")
		fmt.Fprintf(stderr, "  none           No conversion (default)\n")
		fmt.Fprintf(stderr, "  ascii          Convert to ASCII characters\n")
		fmt.Fprintf(stderr, "  ascii-letters  Convert to ASCII characters, letters only\n")
		fmt.Fprintf(stderr, "  ascii-lower    Convert to ASCII lower case letters\n")
		fmt.Fprintf(stderr, "  ascii-upper    Convert to ASCII upper case letters\n\n")
		fmt.Fprintf(stderr, "Operations (applied after conversion):\n")
		fmt.Fprintf(stderr, "  or             Entries in either list (default)\n")
		fmt.Fprintf(stderr, "  and            Entries in both lists\n")
		fmt.Fprintf(stderr, "  exclude        Entries in the primary list and NOT in the secondary list\n\n")
		fmt.Fprintf(stderr, "Options:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if showVer {
		fmt.Fprintf(stdout, "tisane-merge %s\n", version)
		return 0
	}

	pos := fs.Args()
	if badFlag := fsutil.FindMisplacedFlag(args, pos); badFlag != "" {
		fmt.Fprintf(stderr, "tisane-merge: unexpected flag %q after positional argument (place flags first, or use '--' before filenames starting with '-')\n", badFlag)
		return 2
	}
	if primaryPath == "" && len(pos) >= 1 {
		primaryPath = pos[0]
		pos = pos[1:]
	}
	if secondaryPath == "" && len(pos) >= 1 {
		secondaryPath = pos[0]
		pos = pos[1:]
	}
	if outputPath == "" && len(pos) >= 1 {
		outputPath = pos[0]
		pos = pos[1:]
	}
	if len(pos) > 0 {
		fmt.Fprintf(stderr, "tisane-merge: unexpected extra arguments: %v\n", pos)
		return 2
	}
	if primaryPath == "" || secondaryPath == "" || outputPath == "" {
		fs.Usage()
		return 2
	}

	params := wordlist.MergeParams{
		PrimaryPath:   primaryPath,
		SecondaryPath: secondaryPath,
		OutputPath:    outputPath,
		Operation:     operation,
		Conversion:    conversion,
		Encoding:      encoding,
	}

	opts, err := wordlist.ResolveMergeOptions(params)
	if err != nil {
		fmt.Fprintf(stderr, "tisane-merge: %v\n", err)
		return 2
	}

	if ctx.Err() != nil {
		return 130
	}

	start := time.Now()
	var stats *wordlist.MergeStats
	if outputPath == "-" {
		primaryData, readErr := readBoundedRegularFile(primaryPath, "primary")
		if readErr != nil {
			fmt.Fprintf(stderr, "tisane-merge: %v\n", readErr)
			return 1
		}
		secondaryData, readErr := readBoundedRegularFile(secondaryPath, "secondary")
		if readErr != nil {
			fmt.Fprintf(stderr, "tisane-merge: %v\n", readErr)
			return 1
		}
		outBytes, s, mergeErr := wordlist.MergeBytesContext(ctx, primaryData, secondaryData, opts)
		if mergeErr != nil {
			if ctx.Err() != nil || errors.Is(mergeErr, context.Canceled) {
				return 130
			}
			fmt.Fprintf(stderr, "tisane-merge: %v\n", mergeErr)
			return 1
		}
		if _, writeErr := stdout.Write(outBytes); writeErr != nil {
			fmt.Fprintf(stderr, "tisane-merge: failed to write output: %v\n", writeErr)
			return 1
		}
		stats = s
	} else {
		var mergeErr error
		stats, mergeErr = wordlist.MergeFilesContext(ctx, primaryPath, secondaryPath, outputPath, opts)
		if mergeErr != nil {
			if ctx.Err() != nil || errors.Is(mergeErr, context.Canceled) {
				return 130
			}
			fmt.Fprintf(stderr, "tisane-merge: %v\n", mergeErr)
			return 1
		}
	}

	if !quiet {
		summaryOut := stdout
		if outputPath == "-" {
			summaryOut = stderr
		}
		elapsed := time.Since(start).Round(time.Millisecond)
		fmt.Fprintf(summaryOut, "Merged %s (%d bytes in %s)\n", outputPath, stats.BytesWritten, elapsed)
		fmt.Fprintf(summaryOut, "  Operation:   %s (conversion: %s)\n", opts.Operation, opts.Conversion)
		fmt.Fprintf(summaryOut, "  Primary:     %d lines (%d unique converted)\n", stats.PrimaryLines, stats.PrimaryEntries)
		fmt.Fprintf(summaryOut, "  Secondary:   %d lines (%d unique converted)\n", stats.SecondaryLines, stats.SecondaryEntries)
		fmt.Fprintf(summaryOut, "  Output:      %d entries\n", stats.OutputEntries)
	}
	return 0
}

func readBoundedRegularFile(path, label string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s word list %q: %w", label, path, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat %s word list %q: %w", label, path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s word list %q is not a regular file", label, path)
	}
	if info.Size() > maxMergeInputBytes {
		return nil, fmt.Errorf("%s word list %q exceeds maximum size (%d MiB)", label, path, maxMergeInputBytes>>20)
	}
	return io.ReadAll(io.LimitReader(f, maxMergeInputBytes))
}
