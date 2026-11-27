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
	"strings"
	"syscall"
	"time"

	"tisane/pkg/tsd"
	"tisane/pkg/wordlist"
)

type wikiConfig struct {
	outputPath   string
	workDir      string
	dateOverride string
	keepDump     bool
	keepTxt      bool
	txtOnly      bool
	noMerge      bool
	conversion   wordlist.Conversion
	description  string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(runContext(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runContext(context.Background(), args, stdin, stdout, stderr)
}

func runContext(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tisane-wiki", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		inputFlag  string
		sourceURL  string
		convertStr string
		cfg        wikiConfig
	)

	fs.StringVar(&inputFlag, "i", "", "Local already-downloaded dump file(s), comma-separated")
	fs.StringVar(&inputFlag, "input", "", "Local already-downloaded dump file(s), comma-separated")
	fs.StringVar(&sourceURL, "u", "", "Remote Wikimedia dump URL or index URL (default: "+DefaultPageTitlesURL+")")
	fs.StringVar(&sourceURL, "url", "", "Remote Wikimedia dump URL or index URL (default: "+DefaultPageTitlesURL+")")
	fs.StringVar(&cfg.outputPath, "o", "", "Output .tsd path (or .txt with --txt-only; dump date is enforced)")
	fs.StringVar(&cfg.outputPath, "output", "", "Output .tsd path (or .txt with --txt-only; dump date is enforced)")
	fs.StringVar(&cfg.workDir, "work-dir", "", "Directory for temporary downloaded dump files and intermediate word lists")
	fs.StringVar(&cfg.dateOverride, "date", "", "Override dump date (YYYYMMDD) for output filename")
	fs.BoolVar(&cfg.keepDump, "keep-dump", false, "Preserve downloaded dump file(s) after processing instead of deleting")
	fs.StringVar(&convertStr, "convert", "ascii-lower", "Normalization applied to deduplication keys (output preserves original title): ascii-lower, none, ascii, ascii-letters, or ascii-upper")
	fs.StringVar(&cfg.description, "desc", "", "Dictionary description stored in the .tsd header (up to 999 bytes)")
	fs.BoolVar(&cfg.noMerge, "no-merge", false, "Skip title deduplication step")
	fs.BoolVar(&cfg.keepTxt, "keep-txt", false, "Keep intermediate extracted/deduplicated .txt file(s) alongside the .tsd output")
	fs.BoolVar(&cfg.txtOnly, "txt-only", false, "Stop after writing the .txt word list without compiling a .tsd dictionary")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "Tisane Wikipedia Dictionary Generator\n\n")
		fmt.Fprintf(stderr, "Extracts Wikipedia article titles, deduplicates them by normalized key while\n")
		fmt.Fprintf(stderr, "preserving proper names, and compiles a Tisane .tsd dictionary with pkg/tsd.\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  tisane-wiki [options] [local-dump-file...]\n\n")
		fmt.Fprintf(stderr, "Examples:\n")
		fmt.Fprintf(stderr, "  tisane-wiki                                  # Fetch latest enwiki pagetitles dump (prompts for confirmation)\n")
		fmt.Fprintf(stderr, "  tisane-wiki enwiki-20261116-all-titles-in-ns-0.gz\n")
		fmt.Fprintf(stderr, "  tisane-wiki --convert none enwiki-20261116-all-titles-in-ns-0.gz\n")
		fmt.Fprintf(stderr, "  tisane-wiki --txt-only -o titles.txt enwiki-20261116-all-titles-in-ns-0.gz\n\n")
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
		fmt.Fprintf(stderr, "tisane-wiki: %v\n", err)
		return 2
	}
	cfg.conversion = conv

	var localFiles []string
	if inputFlag != "" {
		for _, part := range strings.Split(inputFlag, ",") {
			p := strings.TrimSpace(part)
			if p != "" {
				localFiles = append(localFiles, p)
			}
		}
	}
	for _, arg := range fs.Args() {
		if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
			if sourceURL != "" {
				fmt.Fprintln(stderr, "tisane-wiki: multiple URLs specified")
				return 2
			}
			sourceURL = arg
		} else {
			localFiles = append(localFiles, arg)
		}
	}

	if len(localFiles) > 0 && sourceURL != "" {
		fmt.Fprintln(stderr, "tisane-wiki: specify either local dump file(s) or a remote URL, not both")
		return 2
	}

	if len(localFiles) > 0 {
		return runLocal(ctx, localFiles, cfg, stdout, stderr)
	}

	if sourceURL == "" {
		sourceURL = DefaultPageTitlesURL
	}
	return runRemote(ctx, sourceURL, cfg, stdin, stdout, stderr)
}

func runLocal(ctx context.Context, files []string, cfg wikiConfig, stdout, stderr io.Writer) int {
	date := strings.TrimSpace(cfg.dateOverride)
	prefix := ""
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			fmt.Fprintf(stderr, "tisane-wiki: cannot access %q: %v\n", f, err)
			return 1
		}
		if info.IsDir() {
			fmt.Fprintf(stderr, "tisane-wiki: %q is a directory, expected a dump file\n", f)
			return 1
		}
		if date == "" {
			date = ExtractDateFromStrings(filepath.Base(f))
		}
		if prefix == "" {
			prefix = extractWikiPrefix(filepath.Base(f))
		}
	}
	if date == "" {
		if info, err := os.Stat(files[0]); err == nil {
			date = info.ModTime().UTC().Format("20060102")
		} else {
			date = time.Now().UTC().Format("20060102")
		}
	}

	finalOut := EnsureOutputFilename(cfg.outputPath, prefix, date, cfg.txtOnly)
	extractFn := func(bw *bufio.Writer) (int64, error) {
		tw := NewTieredTitleWriter(bw)
		defer tw.Close()

		var totalTitles int64
		for _, f := range files {
			if err := ctx.Err(); err != nil {
				return totalTitles, err
			}
			fmt.Fprintf(stdout, "Extracting titles from %s...\n", f)
			n, err := extractTitlesToWriter(f, tw)
			if err != nil {
				return totalTitles, err
			}
			totalTitles += n
		}
		if err := tw.Finalize(); err != nil {
			return totalTitles, fmt.Errorf("failed finalizing sorted output: %w", err)
		}
		return totalTitles, nil
	}

	return finishPipeline(ctx, finalOut, date, cfg, extractFn, stdout, stderr)
}

func runRemote(
	ctx context.Context,
	sourceURL string,
	cfg wikiConfig,
	stdin io.Reader,
	stdout, stderr io.Writer,
) int {
	if IsHistoryDumpURL(sourceURL) {
		fmt.Fprintf(stderr, "tisane-wiki: refusing full-history dump %q (only latest revision dumps are allowed)\n", sourceURL)
		return 2
	}

	inReader := bufio.NewReader(stdin)
	ua, err := PromptUserAgent(inReader, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "tisane-wiki: %v\n", err)
		return 1
	}

	client := NewWikimediaClient(ua)
	fmt.Fprintf(stdout, "Checking %s...\n", sourceURL)
	plan, err := client.ResolveDumpURLs(sourceURL)
	if err != nil {
		fmt.Fprintf(stderr, "tisane-wiki: %v\n", err)
		return 1
	}

	if strings.TrimSpace(cfg.dateOverride) != "" {
		plan.Date = strings.TrimSpace(cfg.dateOverride)
	}
	finalOut := EnsureOutputFilename(cfg.outputPath, plan.WikiPrefix, plan.Date, cfg.txtOnly)

	confirmed, err := ConfirmDownload(inReader, stdout, plan, finalOut)
	if err != nil {
		fmt.Fprintf(stderr, "tisane-wiki: failed reading confirmation: %v\n", err)
		return 1
	}
	if !confirmed {
		fmt.Fprintln(stdout, "Download cancelled by user.")
		return 0
	}

	tempWorkDir := cfg.workDir
	createdTemp := false
	if tempWorkDir == "" {
		if cfg.keepDump {
			tempWorkDir = "downloads"
		} else {
			tempWorkDir, err = os.MkdirTemp("", "tisane-wiki-*")
			if err != nil {
				fmt.Fprintf(stderr, "tisane-wiki: failed creating temp directory: %v\n", err)
				return 1
			}
			createdTemp = true
		}
	}
	if createdTemp {
		defer os.RemoveAll(tempWorkDir)
	}

	extractFn := func(bw *bufio.Writer) (int64, error) {
		return RunDownloadPipeline(ctx, client, plan.URLs, tempWorkDir, bw, cfg.keepDump, stdout)
	}

	return finishPipeline(ctx, finalOut, plan.Date, cfg, extractFn, stdout, stderr)
}

func finishPipeline(
	ctx context.Context,
	finalOut, date string,
	cfg wikiConfig,
	extractFn func(*bufio.Writer) (int64, error),
	stdout, stderr io.Writer,
) int {
	// Determine where intermediate .txt files live.
	var (
		rawTxtPath     string
		dedupedTxtPath string
		tmpDir         string
		err            error
	)

	if cfg.txtOnly && cfg.noMerge {
		rawTxtPath = finalOut
	} else {
		if cfg.keepTxt {
			baseStem := strings.TrimSuffix(finalOut, filepath.Ext(finalOut))
			if cfg.txtOnly {
				rawTxtPath = baseStem + "-raw.txt"
				dedupedTxtPath = finalOut
			} else {
				rawTxtPath = baseStem + "-titles.txt"
				dedupedTxtPath = baseStem + "-titles-deduped.txt"
			}
		} else {
			if cfg.workDir != "" {
				if err := os.MkdirAll(cfg.workDir, 0o755); err != nil {
					fmt.Fprintf(stderr, "tisane-wiki: failed creating work directory %q: %v\n", cfg.workDir, err)
					return 1
				}
			}
			tmpDir, err = os.MkdirTemp(cfg.workDir, "tisane-wiki-stage-*")
			if err != nil {
				fmt.Fprintf(stderr, "tisane-wiki: failed creating staging directory: %v\n", err)
				return 1
			}
			defer os.RemoveAll(tmpDir)
			rawTxtPath = filepath.Join(tmpDir, "titles.txt")
			if cfg.txtOnly {
				dedupedTxtPath = finalOut
			} else {
				dedupedTxtPath = filepath.Join(tmpDir, "titles-deduped.txt")
			}
		}
	}

	// Step 1: Extract and tier-sort titles.
	outFile, bw, err := createOutputWriter(rawTxtPath)
	if err != nil {
		fmt.Fprintf(stderr, "tisane-wiki: %v\n", err)
		return 1
	}

	totalTitles, extractErr := extractFn(bw)
	flushErr := bw.Flush()
	closeErr := outFile.Close()

	if extractErr != nil {
		if ctx.Err() != nil || errors.Is(extractErr, context.Canceled) {
			return 130
		}
		fmt.Fprintf(stderr, "tisane-wiki: %v\n", extractErr)
		return 1
	}
	if flushErr != nil {
		fmt.Fprintf(stderr, "tisane-wiki: failed flushing output: %v\n", flushErr)
		return 1
	}
	if closeErr != nil {
		fmt.Fprintf(stderr, "tisane-wiki: failed closing output: %v\n", closeErr)
		return 1
	}

	fmt.Fprintf(stdout, "Extracted %d titles to %s\n", totalTitles, rawTxtPath)

	if cfg.txtOnly && cfg.noMerge {
		return 0
	}

	// Step 2: Deduplicate in priority tier order using normalized keys while preserving original titles.
	buildInput := rawTxtPath
	if !cfg.noMerge {
		dedupStart := time.Now()
		inLines, outEntries, dedupErr := deduplicateTitlesFile(ctx, rawTxtPath, dedupedTxtPath, cfg.conversion)
		if dedupErr != nil {
			if ctx.Err() != nil || errors.Is(dedupErr, context.Canceled) {
				return 130
			}
			fmt.Fprintf(stderr, "tisane-wiki: deduplication failed: %v\n", dedupErr)
			return 1
		}
		fmt.Fprintf(stdout, "Deduplicated %d -> %d entries (key: %s, %s) to %s\n",
			inLines, outEntries, cfg.conversion, time.Since(dedupStart).Round(time.Millisecond), dedupedTxtPath)
		buildInput = dedupedTxtPath
	}

	if cfg.txtOnly {
		return 0
	}

	// Step 3: Compile .tsd dictionary via pkg/tsd.
	buildOpts := tsd.DefaultBuildOptions()
	buildOpts.Encoding = tsd.EncodingUTF8
	desc := strings.TrimSpace(cfg.description)
	if desc == "" {
		desc = defaultDescription(date)
	}
	buildOpts.Description = desc

	buildStart := time.Now()
	buildStats, buildErr := tsd.BuildFileContext(ctx, buildInput, finalOut, buildOpts)
	if buildErr != nil {
		if ctx.Err() != nil || errors.Is(buildErr, context.Canceled) {
			return 130
		}
		fmt.Fprintf(stderr, "tisane-wiki: dictionary build failed: %v\n", buildErr)
		return 1
	}

	fmt.Fprintf(stdout, "Built %s (%d bytes, %d unique words, max length %d in %s)\n",
		finalOut, buildStats.BytesWritten, buildStats.UniqueWords, buildStats.MaxWordLength, time.Since(buildStart).Round(time.Millisecond))
	return 0
}

func defaultDescription(date string) string {
	return fmt.Sprintf("Wikipedia English article titles (%s)\nReleased under CC-BY-SA http://creativecommons.org/licenses/by-sa/4.0/", formatISODate(date))
}

func formatISODate(date string) string {
	d := strings.TrimSpace(date)
	if len(d) == 8 {
		allDigits := true
		for i := 0; i < 8; i++ {
			if d[i] < '0' || d[i] > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return d[0:4] + "-" + d[4:6] + "-" + d[6:8]
		}
	}
	if d == "" {
		return "unknown"
	}
	return d
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
