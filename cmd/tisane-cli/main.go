// Command tisane-cli searches Tisane/TEA (.tsd) dictionaries from the command line,
// providing full legacy TEACON.exe compatibility alongside Full Text Search,
// local definition viewing, crossword enumerations, structured JSON/CSV output,
// and an interactive REPL.
package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"tisane/pkg/lookup"
	"tisane/pkg/profile"
	"tisane/pkg/search"
	"tisane/pkg/tsd"
)

const cliVersion = profile.Version

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	// Pre-scan for --config before loading profile defaults.
	customCfgPath := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			break
		}
		if args[i] == "--config" && i+1 < len(args) {
			customCfgPath = args[i+1]
		} else if strings.HasPrefix(args[i], "--config=") {
			customCfgPath = strings.TrimPrefix(args[i], "--config=")
		}
	}

	var (
		cfg     *profile.Config
		cfgPath string
		err     error
	)
	if customCfgPath != "" {
		cfgPath = customCfgPath
		cfg, err = profile.Load(customCfgPath)
		if err != nil {
			fmt.Fprintf(stderr, "tisane-cli: failed to load config %q: %v\n", customCfgPath, err)
			return 2
		}
	} else {
		cfg, cfgPath, err = profile.LoadOrDefault()
		if err != nil {
			fmt.Fprintf(stderr, "tisane-cli: failed to parse %q: %v\n", cfgPath, err)
			return 2
		}
	}

	opts, err := parseCLIArgs(args, defaultCLIOptions(cfg))
	if err != nil {
		fmt.Fprintf(stderr, "tisane-cli: %v\n", err)
		return 2
	}

	if opts.ShowHelp {
		printHelp(stdout)
		return 0
	}
	if opts.ShowVersion {
		fmt.Fprintf(stdout, "tisane-cli %s\n", cliVersion)
		return 0
	}
	if opts.MinLen > opts.MaxLen {
		fmt.Fprintf(stderr, "tisane-cli: minimum length (%d) cannot exceed maximum length (%d)\n", opts.MinLen, opts.MaxLen)
		return 2
	}

	dictPaths, err := profile.ResolveDictionaryPaths(opts.DictSpecs, cfg)
	if err != nil {
		fmt.Fprintf(stderr, "\"%s\" is not a valid dictionary (%v)\n", strings.Join(opts.DictSpecs, ";"), err)
		return 1
	}
	// Helpful fallback: if no profile dictionaries were configured yet and -d was omitted,
	// discover *.tsd in TISANE_DICT_PATH or ./Dictionaries.
	if len(dictPaths) == 0 && len(opts.DictSpecs) == 0 {
		searchDirs := append(profile.SplitDictPathEnv(os.Getenv(profile.EnvDictPath)), cfg.DictDirs...)
		searchDirs = append(searchDirs, "Dictionaries", "dictionaries", "binary/Dictionaries", ".")
		discovered, _ := profile.DiscoverDictionaries(searchDirs...)
		dictPaths = discovered
	}

	if len(dictPaths) == 0 && !opts.Interactive {
		fmt.Fprintln(stderr, "tisane-cli: no dictionaries specified (use -d / /D:<file.tsd> or set TISANE_DICT_PATH)")
		return 1
	}

	dicts := make([]*tsd.Dictionary, 0, len(dictPaths))
	defer func() {
		for _, d := range dicts {
			_ = d.Close()
		}
	}()
	for _, p := range dictPaths {
		d, oErr := tsd.Open(p)
		if oErr != nil {
			fmt.Fprintf(stderr, "%q is not a valid dictionary\n", p)
			return 1
		}
		dicts = append(dicts, d)
	}

	// Also open any non-active profile dictionaries that carry definitions so --define and HasDefinition
	// can consult all configured reference dictionaries!
	var extraDefDicts []*tsd.Dictionary
	defer func() {
		for _, d := range extraDefDicts {
			_ = d.Close()
		}
	}()
	openedPaths := make(map[string]bool, len(dictPaths))
	for _, p := range dictPaths {
		openedPaths[p] = true
	}
	for _, dc := range cfg.Dictionaries {
		if dc.Path != "" && !openedPaths[dc.Path] {
			if d, oErr := tsd.Open(dc.Path); oErr == nil {
				if d.HasDefinitions() {
					extraDefDicts = append(extraDefDicts, d)
				} else {
					_ = d.Close()
				}
			}
		}
	}

	// Mode 1: Interactive TUI / REPL (-i)
	if opts.Interactive {
		return runREPL(ctx, cfg, cfgPath, opts, dicts, stdin, stdout, stderr)
	}

	// Mode 2: Local Definition Lookup (--define)
	if opts.DefineMode {
		words := opts.DefineWords
		if len(words) == 0 {
			// Read words from stdin if none given on command line
			sc := bufio.NewScanner(stdin)
			for sc.Scan() {
				if w := strings.TrimSpace(sc.Text()); w != "" {
					words = append(words, w)
				}
			}
		}
		if len(words) == 0 {
			fmt.Fprintln(stderr, "tisane-cli: --define requires at least one word")
			return 2
		}
		allLookupDicts := append(append([]*tsd.Dictionary(nil), dicts...), extraDefDicts...)
		for idx, w := range words {
			if idx > 0 && opts.Format == FormatText {
				fmt.Fprintln(stdout)
			}
			res, lErr := lookup.Lookup(ctx, w, allLookupDicts, lookup.Options{
				CaseInsensitiveFallback: true,
				IncludeMentions:         opts.IncludeMentions,
				WebProviders:            cfg.WebProviders,
			})
			if lErr != nil {
				fmt.Fprintf(stderr, "tisane-cli: %v\n", lErr)
				return 1
			}
			if err := writeDefinitionResult(stdout, res, opts.Format); err != nil {
				fmt.Fprintf(stderr, "tisane-cli: %v\n", err)
				return 1
			}
		}
		return 0
	}

	// Mode 3: Command-line patterns or TEACON Batch Stdin Mode
	eng := search.NewEngine()
	exitCode := 0

	var jsonAggregate []jsonMatchRecord
	var csvWriter *csv.Writer
	multiPattern := len(opts.Patterns) != 1 // Stdin or >=2 CLI patterns

	if opts.Format == FormatCSV {
		csvWriter = csv.NewWriter(stdout)
		if err := writeCSVHeader(csvWriter, multiPattern); err != nil {
			fmt.Fprintf(stderr, "tisane-cli: %v\n", err)
			return 1
		}
	}

	executeOnePattern := func(pat string) {
		if opts.Format == FormatText && !opts.Quiet && !opts.CountOnly {
			// Exact TEACON pattern header echo: single leading space + pattern!
			fmt.Fprintf(stdout, " %s\n", pat)
		}

		req := buildSearchRequest(pat, dicts, extraDefDicts, &opts)
		start := time.Now()

		// Stream immediately when SortDict + non-aggregated output for instant latency!
		canStream := opts.Sort == search.SortDict &&
			opts.Format != FormatJSON &&
			req.DedupeMode != search.DedupeMergeSources &&
			!opts.CountOnly

		if canStream {
			count := 0
			var scanned uint64
			for batch, sErr := range eng.SearchStream(ctx, req) {
				if sErr != nil {
					if sErr == context.Canceled {
						return
					}
					formatPatternError(stderr, pat, sErr, opts.Quiet)
					exitCode = 1
					return
				}
				scanned = batch.Progress.WordsScanned
				for _, m := range batch.Matches {
					count++
					switch opts.Format {
					case FormatText:
						fmt.Fprintln(stdout, formatTextMatchLine(m, dicts, &opts))
					case FormatJSONL:
						_ = writeJSONLMatch(stdout, pat, multiPattern, m, dicts, &opts)
					case FormatCSV:
						_ = writeCSVMatch(csvWriter, pat, multiPattern, m, &opts)
					}
				}
			}
			if csvWriter != nil {
				csvWriter.Flush()
			}
			if opts.ShowStats && opts.Format == FormatText {
				fmt.Fprintf(stdout, " [%d matches, %d scanned in %s]\n",
					count, scanned, time.Since(start).Round(time.Microsecond))
			}
			return
		}

		sum, sErr := eng.Search(ctx, req)
		if sErr != nil {
			if sErr == context.Canceled {
				return
			}
			formatPatternError(stderr, pat, sErr, opts.Quiet)
			exitCode = 1
			return
		}

		if opts.CountOnly && opts.Format == FormatText {
			if multiPattern && !opts.Quiet {
				fmt.Fprintf(stdout, " %s\n", pat)
			}
			fmt.Fprintln(stdout, len(sum.Matches))
			return
		}

		for _, m := range sum.Matches {
			switch opts.Format {
			case FormatText:
				fmt.Fprintln(stdout, formatTextMatchLine(m, dicts, &opts))
			case FormatJSON:
				jsonAggregate = append(jsonAggregate, toJSONRecord(pat, multiPattern, m, dicts, &opts))
			case FormatJSONL:
				_ = writeJSONLMatch(stdout, pat, multiPattern, m, dicts, &opts)
			case FormatCSV:
				_ = writeCSVMatch(csvWriter, pat, multiPattern, m, &opts)
			}
		}
		if csvWriter != nil {
			csvWriter.Flush()
		}
		if opts.ShowStats && opts.Format == FormatText {
			fmt.Fprintf(stdout, " [%d matches, %d scanned in %s]\n",
				len(sum.Matches), sum.WordsScanned, time.Since(start).Round(time.Microsecond))
		}
	}

	if len(opts.Patterns) > 0 {
		for _, pat := range opts.Patterns {
			if ctx.Err() != nil {
				break
			}
			executeOnePattern(pat)
		}
	} else {
		// Batch Stdin Mode (exact TEACON semantics: lines starting with ' ' or '\t' are comments!)
		br := bufio.NewReader(stdin)
		for {
			if ctx.Err() != nil {
				break
			}
			rawLine, rErr := br.ReadString('\n')
			if rErr != nil && len(rawLine) == 0 {
				break
			}
			line := strings.TrimRight(rawLine, "\r\n")
			if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
				if opts.Format == FormatText && !opts.Quiet {
					fmt.Fprintln(stdout, line)
				}
				if rErr == io.EOF {
					break
				}
				continue
			}
			if line == "" {
				if rErr == io.EOF {
					break
				}
				continue
			}
			executeOnePattern(line)
			if rErr == io.EOF {
				break
			}
		}
	}

	if opts.Format == FormatJSON {
		if jsonAggregate == nil {
			jsonAggregate = []jsonMatchRecord{}
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(jsonAggregate); err != nil {
			fmt.Fprintf(stderr, "tisane-cli: %v\n", err)
			return 1
		}
	}
	if csvWriter != nil {
		csvWriter.Flush()
		if err := csvWriter.Error(); err != nil {
			fmt.Fprintf(stderr, "tisane-cli: %v\n", err)
			return 1
		}
	}

	return exitCode
}

func buildSearchRequest(
	query string,
	dicts []*tsd.Dictionary,
	extraDefDicts []*tsd.Dictionary,
	opts *cliOptions,
) search.Request {
	mode := search.ModeWord
	if opts.FTSMode {
		mode = search.ModeFullText
	}

	dedupe := search.DedupeNone
	if opts.Dedupe {
		if opts.ShowDict || opts.Format == FormatJSON || opts.Format == FormatJSONL {
			dedupe = search.DedupeMergeSources
		} else {
			dedupe = search.DedupeSuppress
		}
	}

	return search.Request{
		Query:                   query,
		Dictionaries:            dicts,
		DefinitionDictionaries:  extraDefDicts,
		Mode:                    mode,
		MinLength:               opts.MinLen,
		MaxLength:               opts.MaxLen,
		Enumeration:             opts.EnumParts,
		AtMacro:                 opts.AtMacro,
		HashMacro:               opts.HashMacro,
		DisableDefaultMacros:    true,
		ExclusiveVars:           opts.ExclusiveVars,
		IgnoreUnusedPunctuation: opts.IgnorePunct,
		ShowBalances:            opts.ShowBalances,
		ShowAnnotations:         opts.ShowAnnotations,
		DedupeMode:              dedupe,
		EntryFilter:             opts.EntryFilter,
		Sort:                    opts.Sort,
		Limit:                   opts.Limit,
	}
}
