// Command tisane-extract extracts word lists from Tisane / TEA (.tsd) dictionary files,
// emitting either a simple one-word-per-line list or a full tisane-build metadata source file.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"tisane"
	"tisane/pkg/tsd"
)

var version = tisane.Version

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(runContext(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runContext(context.Background(), args, stdout, stderr)
}

func runContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tisane-extract", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		inputPath  string
		outputPath string
		simpleMode bool
		metaMode   bool
		noDefs     bool
		noHeader   bool
		noDedupe   bool
		sortFlag   string
		minLen     int
		maxLen     int
		encoding   string
		saveMap    string
		quiet      bool
		showVer    bool
	)

	fs.StringVar(&inputPath, "i", "", "Input .tsd dictionary file")
	fs.StringVar(&inputPath, "input", "", "Input .tsd dictionary file")
	fs.StringVar(&outputPath, "o", "", "Output .txt path ('-' or omit for stdout)")
	fs.StringVar(&outputPath, "output", "", "Output .txt path ('-' or omit for stdout)")
	fs.BoolVar(&simpleMode, "s", false, "Simple mode: one word per line, discarding definitions & annotations (default)")
	fs.BoolVar(&simpleMode, "simple", false, "Simple mode: one word per line, discarding definitions & annotations (default)")
	fs.BoolVar(&metaMode, "m", false, "Metadata mode: emit tisane-build source format (+annotations, ;aliases, |definitions)")
	fs.BoolVar(&metaMode, "metadata", false, "Metadata mode: emit tisane-build source format (+annotations, ;aliases, |definitions)")
	fs.BoolVar(&noDefs, "no-defs", false, "In --metadata mode, emit word+annotation only and discard definitions")
	fs.BoolVar(&noHeader, "no-header", false, "In --metadata mode, omit leading '| <description>' comment line")
	fs.BoolVar(&noDedupe, "no-dedupe", false, "In --simple mode, keep duplicate words that differed only by annotation")
	fs.StringVar(&sortFlag, "sort", "auto", "Sort order: auto, alpha, section, or length")
	fs.IntVar(&minLen, "min-len", 1, "Minimum canonical word length (1..100)")
	fs.IntVar(&maxLen, "max-len", tsd.MaxWordLengthLimit, "Maximum canonical word length (1..100)")
	fs.StringVar(&encoding, "encoding", "utf8", "Output text encoding: utf8 or cp1252")
	fs.StringVar(&saveMap, "save-map", "", "Optional path to also extract the dictionary's 256-byte .tsm alphabet map")
	fs.BoolVar(&quiet, "q", false, "Suppress summary output to stderr when writing to a file")
	fs.BoolVar(&quiet, "quiet", false, "Suppress summary output to stderr when writing to a file")
	fs.BoolVar(&showVer, "v", false, "Print version and exit")
	fs.BoolVar(&showVer, "version", false, "Print version and exit")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "Tisane Dictionary Extractor v%s\n\n", version)
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  tisane-extract [options] <dict.tsd> [output.txt]\n\n")
		fmt.Fprintf(stderr, "Examples:\n")
		fmt.Fprintf(stderr, "  tisane-extract \"Core English.tsd\" > words.txt\n")
		fmt.Fprintf(stderr, "  tisane-extract --simple \"The Devil's Dictionary.tsd\" words.txt\n")
		fmt.Fprintf(stderr, "  tisane-extract --metadata \"The Devil's Dictionary.tsd\" devils-source.txt\n")
		fmt.Fprintf(stderr, "  tisane-extract --metadata --no-defs \"annotated.tsd\" annotated-words.txt\n\n")
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
		fmt.Fprintf(stdout, "tisane-extract %s\n", version)
		return 0
	}

	if simpleMode && metaMode {
		fmt.Fprintln(stderr, "tisane-extract: cannot specify both --simple and --metadata")
		return 2
	}
	if minLen < 1 || minLen > tsd.MaxWordLengthLimit || maxLen < 1 || maxLen > tsd.MaxWordLengthLimit || minLen > maxLen {
		fmt.Fprintf(stderr, "tisane-extract: invalid length bounds (expected 1 <= min-len (%d) <= max-len (%d) <= %d)\n", minLen, maxLen, tsd.MaxWordLengthLimit)
		return 2
	}

	pos := fs.Args()
	if badFlag := findMisplacedFlag(args, pos); badFlag != "" {
		fmt.Fprintf(stderr, "tisane-extract: unexpected flag %q after positional argument (place flags first, or use '--' before filenames starting with '-')\n", badFlag)
		return 2
	}
	if inputPath == "" {
		if len(pos) == 0 {
			fs.Usage()
			return 2
		}
		inputPath = pos[0]
		pos = pos[1:]
	}
	if outputPath == "" && len(pos) > 0 {
		outputPath = pos[0]
		pos = pos[1:]
	}
	if len(pos) > 0 {
		fmt.Fprintf(stderr, "tisane-extract: unexpected extra arguments: %v\n", pos)
		return 2
	}

	if err := checkExtractCollisions(inputPath, outputPath, saveMap); err != nil {
		fmt.Fprintf(stderr, "tisane-extract: %v\n", err)
		return 2
	}

	mode := tsd.ExportSimple
	if metaMode {
		mode = tsd.ExportMetadata
	}
	opts := tsd.DefaultExportOptions(mode)
	opts.MinLen = minLen
	opts.MaxLen = maxLen
	opts.OmitDefinitions = noDefs
	if noHeader {
		opts.IncludeHeader = false
	}
	if noDedupe {
		opts.DedupeSimpleWords = false
	}

	switch strings.ToLower(strings.TrimSpace(sortFlag)) {
	case "", "auto":
		opts.Sort = tsd.ExportSortAuto
	case "alpha", "az", "a-z":
		opts.Sort = tsd.ExportSortAlpha
	case "section", "def", "source":
		opts.Sort = tsd.ExportSortSection
	case "length", "len", "bucket":
		opts.Sort = tsd.ExportSortLength
	default:
		fmt.Fprintf(stderr, "tisane-extract: invalid --sort %q (expected auto, alpha, section, or length)\n", sortFlag)
		return 2
	}

	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "utf8", "utf-8":
		opts.Encoding = tsd.EncodingUTF8
	case "cp1252", "windows-1252":
		opts.Encoding = tsd.EncodingCP1252
	default:
		fmt.Fprintf(stderr, "tisane-extract: invalid --encoding %q (expected utf8 or cp1252)\n", encoding)
		return 2
	}

	if ctx.Err() != nil {
		return 130
	}

	start := time.Now()
	dict, err := tsd.Open(inputPath)
	if err != nil {
		fmt.Fprintf(stderr, "tisane-extract: %v\n", err)
		return 1
	}
	defer dict.Close()

	var stats *tsd.ExportStats
	if outputPath == "" || outputPath == "-" {
		stats, err = dict.Export(stdout, opts)
	} else {
		stats, err = dict.ExportFile(outputPath, opts)
	}
	if err != nil {
		fmt.Fprintf(stderr, "tisane-extract: %v\n", err)
		return 1
	}

	if saveMap != "" {
		tsmBytes := dict.AlphabetMap().MarshalTSM()
		if err := writeMapAtomic(saveMap, tsmBytes); err != nil {
			fmt.Fprintf(stderr, "tisane-extract: failed to write alphabet map %s: %v\n", saveMap, err)
			return 1
		}
	}

	if outputPath != "" && outputPath != "-" && !quiet {
		modeName := "simple"
		if mode == tsd.ExportMetadata {
			modeName = "metadata"
		}
		fmt.Fprintf(stderr, "Extracted %s (%s mode) -> %s in %s\n",
			dict.Name(), modeName, outputPath, time.Since(start).Round(time.Millisecond))
		fmt.Fprintf(stderr, "  Words: %d  |  Definitions: %d  |  Lines written: %d\n",
			stats.WordsExported, stats.DefinitionsExport, stats.LinesWritten)
	}

	return 0
}

func findMisplacedFlag(args, pos []string) string {
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

func sameOrCleanEqual(a, b string) bool {
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

func checkExtractCollisions(inputPath, outputPath, saveMap string) error {
	if sameOrCleanEqual(outputPath, inputPath) {
		return fmt.Errorf("output path %q cannot overwrite input dictionary %q", outputPath, inputPath)
	}
	if sameOrCleanEqual(saveMap, inputPath) {
		return fmt.Errorf("--save-map path %q cannot overwrite input dictionary %q", saveMap, inputPath)
	}
	if sameOrCleanEqual(outputPath, saveMap) {
		return fmt.Errorf("output path %q and --save-map %q cannot be the same file", outputPath, saveMap)
	}
	return nil
}

func writeMapAtomic(path string, data []byte) error {
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeCharDevice) != 0 {
		return os.WriteFile(path, data, 0o644)
	}
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.CreateTemp(dir, ".tisane-map-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := f.Name()
	keepTmp := false
	defer func() {
		if !keepTmp {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	_ = f.Chmod(0o644)
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	keepTmp = true
	return nil
}
