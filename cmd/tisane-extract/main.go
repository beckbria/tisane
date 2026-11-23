// Command tisane-extract extracts word lists from Tisane / TEA (.tsd) dictionary files,
// emitting either a simple one-word-per-line list or a full tisane-build metadata source file.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"tisane/pkg/tsd"
)

const version = "0.5.0"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
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
	fs.BoolVar(&metaMode, "m", false, "Metadata mode: emit tisane-build / TSBuild format (+annotations, ;aliases, |definitions)")
	fs.BoolVar(&metaMode, "metadata", false, "Metadata mode: emit tisane-build / TSBuild format (+annotations, ;aliases, |definitions)")
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
		fmt.Fprintf(stderr, "  tisane-extract --metadata --no-defs \"tsbuild1.tsd\" annotated-words.txt\n\n")
		fmt.Fprintf(stderr, "Options:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
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

	pos := fs.Args()
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

	start := time.Now()
	dict, err := tsd.Open(inputPath)
	if err != nil {
		fmt.Fprintf(stderr, "tisane-extract: %v\n", err)
		return 1
	}
	defer dict.Close()

	if saveMap != "" {
		tsmBytes := dict.AlphabetMap().MarshalTSM()
		if err := os.WriteFile(saveMap, tsmBytes, 0o644); err != nil {
			fmt.Fprintf(stderr, "tisane-extract: failed to write alphabet map %s: %v\n", saveMap, err)
			return 1
		}
	}

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
