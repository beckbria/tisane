// Command tisane-cli searches Tisane/TEA (.tsd) dictionaries from the command line,
// providing legacy TEACON switch compatibility alongside Full Text Search,
// local definition viewing, crossword enumerations, structured JSON/CSV output,
// and an interactive REPL.
package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"tisane"
	"tisane/pkg/lookup"
	"tisane/pkg/profile"
	"tisane/pkg/search"
	"tisane/pkg/tsd"
)

const maxStdinLineBytes = 1 << 20 // 1 MiB max pattern/word line on stdin

var cliVersion = tisane.Version

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
	// When no -d dictionary flags are given, search TISANE_DICT_PATH, configured dict_dirs,
	// the executable directory, current working directory, and the dictionaries subdirectory,
	// adding any dictionaries found there.
	if len(opts.DictSpecs) == 0 {
		discovered, _ := profile.DiscoverDictionaries(profile.DefaultSearchDirs(cfg)...)
		dictPaths = mergeDiscoveredDictionaries(dictPaths, discovered)
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
	openedPaths := make(map[string]bool, len(dictPaths))
	var openedInfos []os.FileInfo
	for _, p := range dictPaths {
		d, oErr := tsd.Open(p)
		if oErr != nil {
			fmt.Fprintf(stderr, "%q is not a valid dictionary\n", p)
			return 1
		}
		dicts = append(dicts, d)
		if abs, aErr := filepath.Abs(p); aErr == nil {
			openedPaths[filepath.Clean(abs)] = true
		} else {
			openedPaths[filepath.Clean(p)] = true
		}
		if info, sErr := os.Stat(p); sErr == nil {
			openedInfos = append(openedInfos, info)
		}
	}

	// Also open any non-active profile dictionaries that carry definitions so --define and HasDefinition
	// can consult all configured reference dictionaries without opening duplicate handles.
	var extraDefDicts []*tsd.Dictionary
	defer func() {
		for _, d := range extraDefDicts {
			_ = d.Close()
		}
	}()
	for _, dc := range cfg.Dictionaries {
		if dc.Path == "" {
			continue
		}
		absDC := filepath.Clean(dc.Path)
		if abs, aErr := filepath.Abs(dc.Path); aErr == nil {
			absDC = filepath.Clean(abs)
		}
		if openedPaths[absDC] {
			continue
		}
		info, sErr := os.Stat(dc.Path)
		if sErr != nil || !info.Mode().IsRegular() {
			continue
		}
		dup := false
		for _, oi := range openedInfos {
			if os.SameFile(info, oi) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		if d, oErr := tsd.Open(dc.Path); oErr == nil {
			if d.HasDefinitions() {
				extraDefDicts = append(extraDefDicts, d)
				openedPaths[absDC] = true
				openedInfos = append(openedInfos, info)
			} else {
				_ = d.Close()
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
			br := bufio.NewReader(stdin)
			for {
				rawLine, rErr := readBoundedLine(br, maxStdinLineBytes)
				if rErr != nil && !errors.Is(rErr, io.EOF) {
					fmt.Fprintf(stderr, "tisane-cli: %v\n", rErr)
					return 1
				}
				if w := strings.TrimSpace(rawLine); w != "" {
					words = append(words, w)
				}
				if errors.Is(rErr, io.EOF) {
					break
				}
			}
		}
		if len(words) == 0 {
			fmt.Fprintln(stderr, "tisane-cli: --define requires at least one word")
			return 2
		}
		allLookupDicts := append(append([]*tsd.Dictionary(nil), dicts...), extraDefDicts...)
		var jsonResults []*lookup.Result
		var defCSV *csv.Writer
		if opts.Format == FormatCSV {
			defCSV = csv.NewWriter(stdout)
			if err := writeDefinitionCSVHeader(defCSV); err != nil {
				fmt.Fprintf(stderr, "tisane-cli: %v\n", err)
				return 1
			}
		}
		for idx, w := range words {
			if ctx.Err() != nil {
				return 130
			}
			if idx > 0 && opts.Format == FormatText {
				if _, err := fmt.Fprintln(stdout); err != nil {
					return 1
				}
			}
			res, lErr := lookup.Lookup(ctx, w, allLookupDicts, lookup.Options{
				CaseInsensitiveFallback: true,
				IncludeMentions:         opts.IncludeMentions,
				WebProviders:            cfg.WebProviders,
			})
			if lErr != nil {
				if errors.Is(lErr, context.Canceled) || errors.Is(lErr, context.DeadlineExceeded) {
					return 130
				}
				fmt.Fprintf(stderr, "tisane-cli: %v\n", lErr)
				return 1
			}
			switch opts.Format {
			case FormatJSON:
				jsonResults = append(jsonResults, res)
			case FormatCSV:
				if err := writeDefinitionCSVRows(defCSV, res); err != nil {
					fmt.Fprintf(stderr, "tisane-cli: %v\n", err)
					return 1
				}
			default:
				if err := writeDefinitionResult(stdout, res, opts.Format); err != nil {
					fmt.Fprintf(stderr, "tisane-cli: %v\n", err)
					return 1
				}
			}
		}
		if opts.Format == FormatJSON {
			enc := json.NewEncoder(stdout)
			enc.SetIndent("", "  ")
			var encodeTarget any = jsonResults
			if len(jsonResults) == 1 {
				encodeTarget = jsonResults[0]
			}
			if err := enc.Encode(encodeTarget); err != nil {
				fmt.Fprintf(stderr, "tisane-cli: %v\n", err)
				return 1
			}
		} else if defCSV != nil {
			defCSV.Flush()
			if err := defCSV.Error(); err != nil {
				fmt.Fprintf(stderr, "tisane-cli: %v\n", err)
				return 1
			}
		}
		return 0
	}

	// Mode 3: Command-line patterns or batch stdin mode
	eng := search.NewEngine()
	exitCode := 0
	writeFailed := false
	interrupted := false

	var jsonAggregate []jsonMatchRecord
	var jsonCountAggregate []jsonCountRecord
	var csvWriter *csv.Writer
	multiPattern := len(opts.Patterns) != 1 // Stdin or >=2 CLI patterns

	if opts.Format == FormatCSV {
		csvWriter = csv.NewWriter(stdout)
		var hdrErr error
		if opts.CountOnly {
			if multiPattern {
				hdrErr = csvWriter.Write([]string{"pattern", "count"})
			} else {
				hdrErr = csvWriter.Write([]string{"count"})
			}
		} else {
			hdrErr = writeCSVHeader(csvWriter, multiPattern)
		}
		if hdrErr != nil {
			fmt.Fprintf(stderr, "tisane-cli: %v\n", hdrErr)
			return 1
		}
	}

	executeOnePattern := func(pat string) {
		if opts.Format == FormatText && !opts.Quiet && !opts.CountOnly {
			if _, err := fmt.Fprintf(stdout, " %s\n", pat); err != nil {
				writeFailed = true
				exitCode = 1
				return
			}
		}

		req := buildSearchRequest(pat, dicts, extraDefDicts, &opts)
		start := time.Now()

		// Stream immediately when SortDict + non-aggregated output for instant latency.
		canStream := opts.Sort == search.SortDict &&
			opts.Format != FormatJSON &&
			req.DedupeMode != search.DedupeMergeSources &&
			!opts.CountOnly

		if canStream {
			count := 0
			var scanned uint64
			for batch, sErr := range eng.SearchStream(ctx, req) {
				if sErr != nil {
					if errors.Is(sErr, context.Canceled) || errors.Is(sErr, context.DeadlineExceeded) {
						interrupted = true
						return
					}
					formatPatternError(stderr, pat, sErr, opts.Quiet)
					exitCode = 1
					return
				}
				scanned = batch.Progress.WordsScanned
				for _, m := range batch.Matches {
					count++
					var wErr error
					switch opts.Format {
					case FormatText:
						_, wErr = fmt.Fprintln(stdout, formatTextMatchLine(m, dicts, &opts))
					case FormatJSONL:
						wErr = writeJSONLMatch(stdout, pat, multiPattern, m, dicts, &opts)
					case FormatCSV:
						wErr = writeCSVMatch(csvWriter, pat, multiPattern, m, &opts)
					}
					if wErr != nil {
						writeFailed = true
						exitCode = 1
						return
					}
				}
			}
			if csvWriter != nil {
				csvWriter.Flush()
				if err := csvWriter.Error(); err != nil {
					writeFailed = true
					exitCode = 1
					return
				}
			}
			if opts.ShowStats && opts.Format == FormatText {
				_, _ = fmt.Fprintf(stdout, " [%d matches, %d scanned in %s]\n",
					count, scanned, time.Since(start).Round(time.Microsecond))
			}
			return
		}

		sum, sErr := eng.Search(ctx, req)
		if sErr != nil {
			if errors.Is(sErr, context.Canceled) || errors.Is(sErr, context.DeadlineExceeded) {
				interrupted = true
				return
			}
			formatPatternError(stderr, pat, sErr, opts.Quiet)
			exitCode = 1
			return
		}

		if opts.CountOnly {
			n := len(sum.Matches)
			switch opts.Format {
			case FormatText:
				if multiPattern && !opts.Quiet {
					_, _ = fmt.Fprintf(stdout, " %s\n", pat)
				}
				if _, err := fmt.Fprintln(stdout, n); err != nil {
					writeFailed = true
					exitCode = 1
				}
			case FormatJSON:
				rec := jsonCountRecord{Count: n}
				if multiPattern {
					rec.Pattern = pat
				}
				jsonCountAggregate = append(jsonCountAggregate, rec)
			case FormatJSONL:
				rec := jsonCountRecord{Count: n}
				if multiPattern {
					rec.Pattern = pat
				}
				data, _ := json.Marshal(rec)
				data = append(data, '\n')
				if _, err := stdout.Write(data); err != nil {
					writeFailed = true
					exitCode = 1
				}
			case FormatCSV:
				var err error
				if multiPattern {
					err = csvWriter.Write([]string{pat, strconv.Itoa(n)})
				} else {
					err = csvWriter.Write([]string{strconv.Itoa(n)})
				}
				if err != nil {
					writeFailed = true
					exitCode = 1
				} else {
					csvWriter.Flush()
				}
			}
			return
		}

		for _, m := range sum.Matches {
			var wErr error
			switch opts.Format {
			case FormatText:
				_, wErr = fmt.Fprintln(stdout, formatTextMatchLine(m, dicts, &opts))
			case FormatJSON:
				jsonAggregate = append(jsonAggregate, toJSONRecord(pat, multiPattern, m, dicts, &opts))
			case FormatJSONL:
				wErr = writeJSONLMatch(stdout, pat, multiPattern, m, dicts, &opts)
			case FormatCSV:
				wErr = writeCSVMatch(csvWriter, pat, multiPattern, m, &opts)
			}
			if wErr != nil {
				writeFailed = true
				exitCode = 1
				return
			}
		}
		if csvWriter != nil {
			csvWriter.Flush()
			if err := csvWriter.Error(); err != nil {
				writeFailed = true
				exitCode = 1
				return
			}
		}
		if opts.ShowStats && opts.Format == FormatText {
			_, _ = fmt.Fprintf(stdout, " [%d matches, %d scanned in %s]\n",
				len(sum.Matches), sum.WordsScanned, time.Since(start).Round(time.Microsecond))
		}
	}

	if len(opts.Patterns) > 0 {
		for _, pat := range opts.Patterns {
			if ctx.Err() != nil {
				interrupted = true
				break
			}
			executeOnePattern(pat)
			if interrupted || writeFailed {
				break
			}
		}
	} else {
		// Batch stdin mode: lines starting with ' ' or '\t' are comments and echoed verbatim.
		br := bufio.NewReader(stdin)
		for {
			if ctx.Err() != nil {
				interrupted = true
				break
			}
			line, rErr := readBoundedLine(br, maxStdinLineBytes)
			if rErr != nil && !errors.Is(rErr, io.EOF) {
				fmt.Fprintf(stderr, "tisane-cli: %v\n", rErr)
				return 1
			}
			if errors.Is(rErr, io.EOF) && line == "" {
				break
			}
			if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
				if opts.Format == FormatText && !opts.Quiet {
					if _, err := fmt.Fprintln(stdout, line); err != nil {
						return 1
					}
				}
				if errors.Is(rErr, io.EOF) {
					break
				}
				continue
			}
			if line == "" {
				if errors.Is(rErr, io.EOF) {
					break
				}
				continue
			}
			executeOnePattern(line)
			if interrupted || writeFailed || errors.Is(rErr, io.EOF) {
				break
			}
		}
	}

	if interrupted || ctx.Err() != nil {
		return 130
	}
	if writeFailed {
		return 1
	}

	if opts.Format == FormatJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		var payload any
		if opts.CountOnly {
			if jsonCountAggregate == nil {
				jsonCountAggregate = []jsonCountRecord{}
			}
			if !multiPattern && len(jsonCountAggregate) == 1 {
				payload = jsonCountAggregate[0]
			} else {
				payload = jsonCountAggregate
			}
		} else {
			if jsonAggregate == nil {
				jsonAggregate = []jsonMatchRecord{}
			}
			payload = jsonAggregate
		}
		if err := enc.Encode(payload); err != nil {
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

func readBoundedLine(br *bufio.Reader, maxBytes int) (string, error) {
	var buf []byte
	for {
		frag, isPrefix, err := br.ReadLine()
		buf = append(buf, frag...)
		if len(buf) > maxBytes {
			return "", fmt.Errorf("input line exceeds maximum length (%d bytes)", maxBytes)
		}
		if err != nil {
			return string(buf), err
		}
		if !isPrefix {
			return string(buf), nil
		}
	}
}

func mergeDiscoveredDictionaries(activePaths []string, discovered []string) []string {
	var knownInfos []os.FileInfo
	seenPaths := make(map[string]bool)

	recordKnown := func(p string) {
		if p == "" {
			return
		}
		if abs, err := filepath.Abs(p); err == nil {
			seenPaths[filepath.Clean(abs)] = true
		} else {
			seenPaths[filepath.Clean(p)] = true
		}
		if info, err := os.Stat(p); err == nil {
			knownInfos = append(knownInfos, info)
		}
	}

	for _, p := range activePaths {
		recordKnown(p)
	}

	out := append([]string(nil), activePaths...)
	for _, p := range discovered {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = filepath.Clean(p)
		} else {
			abs = filepath.Clean(abs)
		}
		if seenPaths[abs] {
			continue
		}
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		dup := false
		for _, ki := range knownInfos {
			if os.SameFile(info, ki) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		seenPaths[abs] = true
		knownInfos = append(knownInfos, info)
		out = append(out, p)
	}
	return out
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
