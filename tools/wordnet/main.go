package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"tisane/pkg/tsd"
	"tisane/pkg/wordlist"
)

type wordnetConfig struct {
	outputPath  string
	workDir     string
	keepDump    bool
	keepTxt     bool
	txtOnly     bool
	conversion  wordlist.Conversion
	description string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(runContext(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runContext(context.Background(), args, stdout, stderr)
}

func runContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tisane-wordnet", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		inputPath  string
		sourceURL  string
		convertStr string
		cfg        wordnetConfig
	)

	fs.StringVar(&inputPath, "i", "", "Local already-downloaded WordNet LMF XML dump file (.xml.gz or .xml)")
	fs.StringVar(&inputPath, "input", "", "Local already-downloaded WordNet LMF XML dump file (.xml.gz or .xml)")
	fs.StringVar(&sourceURL, "u", "", "Remote WordNet downloads page or direct dump URL (default: "+DefaultDownloadsURL+")")
	fs.StringVar(&sourceURL, "url", "", "Remote WordNet downloads page or direct dump URL (default: "+DefaultDownloadsURL+")")
	fs.StringVar(&cfg.outputPath, "o", "", "Output .tsd path (or .txt with --txt-only; defaults to <dump-basename>.tsd)")
	fs.StringVar(&cfg.outputPath, "output", "", "Output .tsd path (or .txt with --txt-only; defaults to <dump-basename>.tsd)")
	fs.StringVar(&cfg.workDir, "work-dir", "", "Directory for downloaded dump file and temporary staging files")
	fs.BoolVar(&cfg.keepDump, "keep-dump", false, "Preserve downloaded dump file after processing instead of deleting")
	fs.StringVar(&convertStr, "convert", "none", "Optional word list conversion applied before building: none, ascii, ascii-letters, ascii-lower, or ascii-upper")
	fs.StringVar(&cfg.description, "desc", "", "Dictionary description stored in the .tsd header (up to 999 bytes)")
	fs.BoolVar(&cfg.keepTxt, "keep-txt", false, "Keep intermediate .txt word list alongside the .tsd output")
	fs.BoolVar(&cfg.txtOnly, "txt-only", false, "Write only the .txt word list without compiling a .tsd dictionary")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "Tisane Open English WordNet Dictionary Generator\n\n")
		fmt.Fprintf(stderr, "Extracts words from Open English WordNet LMF XML dumps, optionally normalizes\n")
		fmt.Fprintf(stderr, "them with pkg/wordlist, and compiles a Tisane .tsd dictionary with pkg/tsd.\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  tisane-wordnet [options] [local-dump-file | remote-url]\n\n")
		fmt.Fprintf(stderr, "Examples:\n")
		fmt.Fprintf(stderr, "  tisane-wordnet                                       # Download latest Open English WordNet (preferring plus) and build .tsd\n")
		fmt.Fprintf(stderr, "  tisane-wordnet english-wordnet-2025-plus.xml.gz      # Build english-wordnet-2025-plus.tsd from a local dump\n")
		fmt.Fprintf(stderr, "  tisane-wordnet --txt-only english-wordnet-2025-plus.xml.gz\n\n")
		fmt.Fprintf(stderr, "Options:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	conv, err := wordlist.ParseConversion(convertStr)
	if err != nil {
		fmt.Fprintf(stderr, "tisane-wordnet: %v\n", err)
		return 2
	}
	cfg.conversion = conv

	for _, arg := range fs.Args() {
		if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
			if sourceURL != "" {
				fmt.Fprintln(stderr, "tisane-wordnet: multiple URLs specified")
				return 2
			}
			sourceURL = arg
		} else {
			if inputPath != "" {
				fmt.Fprintln(stderr, "tisane-wordnet: multiple input files specified")
				return 2
			}
			inputPath = arg
		}
	}

	if inputPath != "" && sourceURL != "" {
		fmt.Fprintln(stderr, "tisane-wordnet: specify either a local dump file or a remote URL, not both")
		return 2
	}

	if inputPath != "" {
		return runLocal(ctx, inputPath, cfg, stdout, stderr)
	}

	if sourceURL == "" {
		sourceURL = DefaultDownloadsURL
	}
	return runRemote(ctx, sourceURL, cfg, stdout, stderr)
}

func runLocal(ctx context.Context, inputPath string, cfg wordnetConfig, stdout, stderr io.Writer) int {
	info, err := os.Stat(inputPath)
	if err != nil {
		fmt.Fprintf(stderr, "tisane-wordnet: cannot access %q: %v\n", inputPath, err)
		return 1
	}
	if info.IsDir() {
		fmt.Fprintf(stderr, "tisane-wordnet: %q is a directory, expected a dump file\n", inputPath)
		return 1
	}

	dumpBase := DeriveDumpBase(inputPath)
	finalOut := DeriveOutputFilename(inputPath, cfg.outputPath, cfg.txtOnly)
	return extractAndBuild(ctx, inputPath, dumpBase, finalOut, cfg, stdout, stderr)
}

func runRemote(ctx context.Context, sourceURL string, cfg wordnetConfig, stdout, stderr io.Writer) int {
	client := NewWordnetClient()
	fmt.Fprintf(stdout, "Checking %s for latest Open English WordNet dump...\n", sourceURL)
	cand, err := client.ResolveDumpURL(sourceURL)
	if err != nil {
		fmt.Fprintf(stderr, "tisane-wordnet: %v\n", err)
		return 1
	}

	dumpBase := DeriveDumpBase(cand.Filename)
	finalOut := DeriveOutputFilename(cand.Filename, cfg.outputPath, cfg.txtOnly)

	dlDir := cfg.workDir
	createdTemp := false
	if dlDir == "" {
		if cfg.keepDump {
			dlDir = "."
		} else {
			dlDir, err = os.MkdirTemp("", "tisane-wordnet-*")
			if err != nil {
				fmt.Fprintf(stderr, "tisane-wordnet: failed creating temp directory: %v\n", err)
				return 1
			}
			createdTemp = true
		}
	}
	if createdTemp {
		defer os.RemoveAll(dlDir)
	}

	destPath := filepath.Join(dlDir, cand.Filename)
	fmt.Fprintf(stdout, "Downloading %s...\n", cand.URL)
	if err := client.DownloadFile(cand.URL, destPath); err != nil {
		fmt.Fprintf(stderr, "tisane-wordnet: %v\n", err)
		return 1
	}
	if !cfg.keepDump && !createdTemp {
		defer os.Remove(destPath)
	}

	return extractAndBuild(ctx, destPath, dumpBase, finalOut, cfg, stdout, stderr)
}

func extractAndBuild(
	ctx context.Context,
	inputPath, dumpBase, finalOut string,
	cfg wordnetConfig,
	stdout, stderr io.Writer,
) int {
	needConvert := cfg.conversion != wordlist.ConvertNone

	var (
		rawTxtPath       string
		convertedTxtPath string
		tmpDir           string
		err              error
	)

	if cfg.txtOnly && !needConvert {
		rawTxtPath = finalOut
	} else if cfg.keepTxt {
		baseStem := strings.TrimSuffix(finalOut, filepath.Ext(finalOut))
		if cfg.txtOnly {
			rawTxtPath = baseStem + "-raw.txt"
			convertedTxtPath = finalOut
		} else if needConvert {
			rawTxtPath = baseStem + "-raw.txt"
			convertedTxtPath = baseStem + ".txt"
		} else {
			rawTxtPath = baseStem + ".txt"
		}
	} else {
		if cfg.workDir != "" {
			if err := os.MkdirAll(cfg.workDir, 0o755); err != nil {
				fmt.Fprintf(stderr, "tisane-wordnet: failed creating work directory %q: %v\n", cfg.workDir, err)
				return 1
			}
		}
		tmpDir, err = os.MkdirTemp(cfg.workDir, "tisane-wordnet-stage-*")
		if err != nil {
			fmt.Fprintf(stderr, "tisane-wordnet: failed creating staging directory: %v\n", err)
			return 1
		}
		defer os.RemoveAll(tmpDir)
		rawTxtPath = filepath.Join(tmpDir, "words.txt")
		if cfg.txtOnly {
			convertedTxtPath = finalOut
		} else {
			convertedTxtPath = filepath.Join(tmpDir, "words-converted.txt")
		}
	}

	// Step 1: Extract words from WordNet LMF XML.
	outFile, bw, err := createOutputWriter(rawTxtPath)
	if err != nil {
		fmt.Fprintf(stderr, "tisane-wordnet: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "Extracting words from %s...\n", inputPath)
	totalWords, extractErr := ExtractWordsFromFile(inputPath, bw)
	flushErr := bw.Flush()
	closeErr := outFile.Close()

	if extractErr != nil {
		fmt.Fprintf(stderr, "tisane-wordnet: %v\n", extractErr)
		return 1
	}
	if flushErr != nil {
		fmt.Fprintf(stderr, "tisane-wordnet: failed flushing output: %v\n", flushErr)
		return 1
	}
	if closeErr != nil {
		fmt.Fprintf(stderr, "tisane-wordnet: failed closing output: %v\n", closeErr)
		return 1
	}

	fmt.Fprintf(stdout, "Extracted %d unique words to %s\n", totalWords, rawTxtPath)

	if cfg.txtOnly && !needConvert {
		return 0
	}

	// Step 2: Optional conversion & deduplication via pkg/wordlist.
	buildInput := rawTxtPath
	if needConvert {
		mergeStart := time.Now()
		mergeOpts := wordlist.MergeOptions{
			Operation:  wordlist.OpOR,
			Conversion: cfg.conversion,
			Encoding:   tsd.EncodingUTF8,
		}
		stats, mergeErr := wordlist.MergeFilesContext(ctx, rawTxtPath, rawTxtPath, convertedTxtPath, mergeOpts)
		if mergeErr != nil {
			if ctx.Err() != nil || errors.Is(mergeErr, context.Canceled) {
				return 130
			}
			fmt.Fprintf(stderr, "tisane-wordnet: conversion failed: %v\n", mergeErr)
			return 1
		}
		fmt.Fprintf(stdout, "Converted %d -> %d entries (conversion: %s, %s) to %s\n",
			stats.PrimaryLines, stats.OutputEntries, cfg.conversion, time.Since(mergeStart).Round(time.Millisecond), convertedTxtPath)
		buildInput = convertedTxtPath
	}

	if cfg.txtOnly {
		return 0
	}

	// Step 3: Compile .tsd dictionary via pkg/tsd.
	buildOpts := tsd.DefaultBuildOptions()
	buildOpts.Encoding = tsd.EncodingUTF8
	desc := strings.TrimSpace(cfg.description)
	if desc == "" {
		desc = defaultDescription(dumpBase)
	}
	buildOpts.Description = desc

	buildStart := time.Now()
	buildStats, buildErr := tsd.BuildFileContext(ctx, buildInput, finalOut, buildOpts)
	if buildErr != nil {
		if ctx.Err() != nil || errors.Is(buildErr, context.Canceled) {
			return 130
		}
		fmt.Fprintf(stderr, "tisane-wordnet: dictionary build failed: %v\n", buildErr)
		return 1
	}

	fmt.Fprintf(stdout, "Built %s (%d bytes, %d unique words, max length %d in %s)\n",
		finalOut, buildStats.BytesWritten, buildStats.UniqueWords, buildStats.MaxWordLength, time.Since(buildStart).Round(time.Millisecond))
	return 0
}

var wordnetVersionRe = regexp.MustCompile(`(?:^|[^0-9])([0-9]{4}(?:\.[0-9]+)*)(?:[^0-9]|$)`)

func extractWordnetVersion(dumpBase string) string {
	if m := wordnetVersionRe.FindStringSubmatch(dumpBase); m != nil {
		return m[1]
	}
	return dumpBase
}

func defaultDescription(dumpBase string) string {
	return fmt.Sprintf("English WordNet %s\nUsed under the terms of the CC-BY 4.0 license https://creativecommons.org/licenses/by/4.0/", extractWordnetVersion(dumpBase))
}

func createOutputWriter(outputPath string) (*os.File, *bufio.Writer, error) {
	if dir := filepath.Dir(outputPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, fmt.Errorf("failed creating output directory %q: %w", dir, err)
		}
	}
	f, err := os.Create(outputPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed creating output file %q: %w", outputPath, err)
	}
	return f, bufio.NewWriterSize(f, 256*1024), nil
}
