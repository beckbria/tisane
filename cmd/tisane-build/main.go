// Command tisane-build compiles plain-text word and definition lists into
// Tisane / TEA (.tsd, format TSD1) dictionary files, or inspects existing .tsd files.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"tisane"
	"tisane/internal/fsutil"
	"tisane/pkg/tsd"
)

const maxStdinBuildBytes = 512 << 20 // 512 MiB

var version = tisane.Version

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(runContext(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runContext(context.Background(), args, stdin, stdout, stderr)
}

func runContext(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tisane-build", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		inputPath  string
		outputPath string
		mapPath    string
		desc       string
		minLen     int
		maxLen     int
		maxFTSPct  int
		noSort     bool
		tsvMode    bool
		encoding   string
		infoMode   bool
		quiet      bool
		showVer    bool
	)

	fs.StringVar(&inputPath, "i", "", "Input word/definition .txt file ('-' for stdin)")
	fs.StringVar(&inputPath, "input", "", "Input word/definition .txt file ('-' for stdin)")
	fs.StringVar(&outputPath, "o", "", "Output .tsd dictionary path (defaults to <input>.tsd)")
	fs.StringVar(&outputPath, "output", "", "Output .tsd dictionary path (defaults to <input>.tsd)")
	fs.StringVar(&mapPath, "m", "", "Path to .tsm alphabet map file (defaults to built-in Default.TSM)")
	fs.StringVar(&mapPath, "map", "", "Path to .tsm alphabet map file (defaults to built-in Default.TSM)")
	fs.StringVar(&desc, "d", "", "Dictionary description (up to 999 bytes)")
	fs.StringVar(&desc, "desc", "", "Dictionary description (up to 999 bytes)")
	fs.IntVar(&minLen, "min-len", 1, "Minimum canonical word length (1..100)")
	fs.IntVar(&maxLen, "max-len", tsd.MaxWordLengthLimit, "Maximum canonical word length (1..100)")
	fs.IntVar(&maxFTSPct, "max-fts-pct", 10, "Max % of definitions before omitting FTS list (0..100; 0 disables FTS)")
	fs.BoolVar(&noSort, "no-sort", false, "Preserve input word order within each length bucket")
	fs.BoolVar(&tsvMode, "tsv", false, "Treat tab ('\\t') as annotation delimiter in TSV word lists")
	fs.StringVar(&encoding, "encoding", "auto", "Input text encoding: auto, cp1252, or utf8")
	fs.BoolVar(&infoMode, "info", false, "Inspect and print summary information for existing .tsd file(s)")
	fs.BoolVar(&quiet, "q", false, "Suppress progress and summary output")
	fs.BoolVar(&quiet, "quiet", false, "Suppress progress and summary output")
	fs.BoolVar(&showVer, "v", false, "Print version and exit")
	fs.BoolVar(&showVer, "version", false, "Print version and exit")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "Tisane Dictionary Builder v%s\n\n", version)
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  tisane-build [options] <input.txt> [output.tsd]\n")
		fmt.Fprintf(stderr, "  tisane-build --info <dict.tsd>...\n\n")
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
		fmt.Fprintf(stdout, "tisane-build %s\n", version)
		return 0
	}

	pos := fs.Args()
	if badFlag := fsutil.FindMisplacedFlag(args, pos); badFlag != "" {
		fmt.Fprintf(stderr, "tisane-build: unexpected flag %q after positional argument (place flags first, or use '--' before filenames starting with '-')\n", badFlag)
		return 2
	}

	if infoMode {
		targets := pos
		if inputPath != "" {
			targets = append([]string{inputPath}, targets...)
		}
		if len(targets) == 0 {
			fmt.Fprintln(stderr, "tisane-build: --info requires at least one .tsd file path")
			return 2
		}
		for idx, p := range targets {
			if ctx.Err() != nil {
				return 130
			}
			if idx > 0 {
				fmt.Fprintln(stdout)
			}
			if err := printDictInfo(p, stdout); err != nil {
				fmt.Fprintf(stderr, "tisane-build: %v\n", err)
				return 1
			}
		}
		return 0
	}

	if minLen < 1 || minLen > tsd.MaxWordLengthLimit || maxLen < 1 || maxLen > tsd.MaxWordLengthLimit || minLen > maxLen {
		fmt.Fprintf(stderr, "tisane-build: invalid length bounds (expected 1 <= min-len (%d) <= max-len (%d) <= %d)\n", minLen, maxLen, tsd.MaxWordLengthLimit)
		return 2
	}
	if maxFTSPct < 0 || maxFTSPct > 100 {
		fmt.Fprintf(stderr, "tisane-build: invalid --max-fts-pct %d (must be 0..100)\n", maxFTSPct)
		return 2
	}

	if inputPath == "" && len(pos) >= 1 {
		inputPath = pos[0]
		pos = pos[1:]
	}
	if outputPath == "" && len(pos) >= 1 {
		outputPath = pos[0]
		pos = pos[1:]
	}
	if len(pos) > 0 {
		fmt.Fprintf(stderr, "tisane-build: unexpected extra arguments: %v\n", pos)
		return 2
	}
	if inputPath == "" {
		fs.Usage()
		return 2
	}
	if outputPath == "" {
		if inputPath == "-" {
			fmt.Fprintln(stderr, "tisane-build: -o / --output is required when reading from stdin ('-')")
			return 2
		}
		outputPath = tsd.DefaultOutputPath(inputPath)
	}

	if err := checkBuildPathCollisions(inputPath, outputPath, mapPath); err != nil {
		fmt.Fprintf(stderr, "tisane-build: %v\n", err)
		return 2
	}

	if _, err := tsd.ParseInputEncoding(encoding); err != nil {
		fmt.Fprintf(stderr, "tisane-build: %v\n", err)
		return 2
	}

	params := tsd.BuildParams{
		InputPath:               inputPath,
		OutputPath:              outputPath,
		MapPath:                 mapPath,
		Description:             desc,
		MinLen:                  minLen,
		MaxLen:                  maxLen,
		MaxFTSPercent:           maxFTSPct,
		SortAlphabetically:      !noSort,
		TabSeparatedAnnotations: tsvMode,
		Encoding:                encoding,
	}

	opts, err := tsd.ResolveBuildOptions(params)
	if err != nil {
		fmt.Fprintf(stderr, "tisane-build: %v\n", err)
		return 1
	}

	if ctx.Err() != nil {
		return 130
	}

	start := time.Now()
	var stats *tsd.BuildStats
	if inputPath == "-" {
		rawInput, readErr := io.ReadAll(io.LimitReader(stdin, maxStdinBuildBytes+1))
		if readErr != nil {
			fmt.Fprintf(stderr, "tisane-build: failed to read stdin: %v\n", readErr)
			return 1
		}
		if int64(len(rawInput)) > maxStdinBuildBytes {
			fmt.Fprintf(stderr, "tisane-build: stdin exceeds maximum size (%d MiB)\n", maxStdinBuildBytes>>20)
			return 1
		}
		if ctx.Err() != nil {
			return 130
		}
		outBytes, s, buildErr := tsd.BuildBytes(rawInput, opts)
		if buildErr != nil {
			fmt.Fprintf(stderr, "tisane-build: build failed: %v\n", buildErr)
			return 1
		}
		if writeErr := writeOutputAtomic(outputPath, outBytes); writeErr != nil {
			fmt.Fprintf(stderr, "tisane-build: failed to write %q: %v\n", outputPath, writeErr)
			return 1
		}
		stats = s
	} else {
		var buildErr error
		stats, buildErr = tsd.BuildFile(inputPath, outputPath, opts)
		if buildErr != nil {
			fmt.Fprintf(stderr, "tisane-build: %v\n", buildErr)
			return 1
		}
	}

	if !quiet {
		elapsed := time.Since(start).Round(time.Millisecond)
		fmt.Fprintf(stdout, "Built %s (%d bytes in %s)\n", outputPath, stats.BytesWritten, elapsed)
		fmt.Fprintf(stdout, "  Input lines:   %d\n", stats.InputLines)
		fmt.Fprintf(stdout, "  Unique words:  %d (max length %d)\n", stats.UniqueWords, stats.MaxWordLength)
		fmt.Fprintf(stdout, "  Definitions:   %d headword, %d alias-only\n", stats.Pass2Definitions, stats.Pass1Definitions)
		fmt.Fprintf(stdout, "  FTS-only keys: %d\n", stats.FTSOnlyWords)
		fmt.Fprintf(stdout, "  Alphabet:      %d/%d active letter classes\n", stats.UsedLetters, stats.TotalLetters)
	}
	return 0
}

func printDictInfo(path string, w io.Writer) error {
	d, err := tsd.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()

	fmt.Fprintf(w, "File:          %s\n", path)
	fmt.Fprintf(w, "Format:        %s (%d bytes)\n", d.Version(), d.Size())
	if d.Description() != "" {
		fmt.Fprintf(w, "Description:   %s\n", sanitizeControlChars(d.Description()))
	}
	fmt.Fprintf(w, "Words:         %d (max length %d)\n", d.TotalWords(), d.MaxWordLength())
	fmt.Fprintf(w, "Alphabet:      %d used / %d total letter classes\n", d.NumUsedLetters(), d.NumTotalLetters())
	fmt.Fprintf(w, "Definitions:   %v (FTS index: %v)\n", d.HasDefinitions(), d.HasFTSIndex())
	return nil
}

func checkBuildPathCollisions(inputPath, outputPath, mapPath string) error {
	if fsutil.SameFileOrClean(outputPath, inputPath) {
		return fmt.Errorf("output path %q cannot overwrite input file %q", outputPath, inputPath)
	}
	if fsutil.SameFileOrClean(outputPath, mapPath) {
		return fmt.Errorf("output path %q cannot overwrite alphabet map %q", outputPath, mapPath)
	}
	return nil
}

func sanitizeControlChars(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		if r == '\r' || r == '\n' || r == '\t' {
			sb.WriteByte(' ')
		} else if r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0x9F) {
			continue
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func writeOutputAtomic(outputPath string, data []byte) error {
	return fsutil.WritePublicBytes(outputPath, data)
}
