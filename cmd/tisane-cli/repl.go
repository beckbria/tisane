package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tisane/pkg/lookup"
	"tisane/pkg/pattern"
	"tisane/pkg/profile"
	"tisane/pkg/search"
	"tisane/pkg/tsd"
)

const replPageSize = 40

type replSession struct {
	cfg     *profile.Config
	cfgPath string
	opts    cliOptions
	dicts   []*tsd.Dictionary
	enabled []bool
	eng     *search.Engine
	history []string
	stdin   *bufio.Reader
	stdout  io.Writer
	stderr  io.Writer
}

func runREPL(
	ctx context.Context,
	cfg *profile.Config,
	cfgPath string,
	opts cliOptions,
	dicts []*tsd.Dictionary,
	stdin io.Reader,
	stdout, stderr io.Writer,
) int {
	enabled := make([]bool, len(dicts))
	for i := range enabled {
		enabled[i] = true
	}

	s := &replSession{
		cfg:     cfg,
		cfgPath: cfgPath,
		opts:    opts,
		dicts:   dicts,
		enabled: enabled,
		eng:     search.NewEngine(),
		history: append([]string(nil), cfg.RecentQueries...),
		stdin:   bufio.NewReader(stdin),
		stdout:  stdout,
		stderr:  stderr,
	}

	if !opts.Quiet {
		fmt.Fprintf(stdout, "Tisane Interactive Mode (v%s) — %d dictionary(ies) loaded.\n", cliVersion, len(dicts))
		fmt.Fprintln(stdout, "Enter any TEA pattern to search, ':def <word>' for definitions, ':help' for commands, or ':q' to quit.")
	}

	for {
		if err := ctx.Err(); err != nil {
			return 0
		}

		modeTag := "word"
		if s.opts.FTSMode {
			modeTag = "fts"
		}
		enumTag := ""
		if s.opts.EnumSpec != "" && !s.opts.FTSMode {
			enumTag = "(" + s.opts.EnumSpec + ")"
		}
		fmt.Fprintf(stdout, "tisane[%s%s]> ", modeTag, enumTag)

		line, err := s.stdin.ReadString('\n')
		if err != nil && len(line) == 0 {
			if err == io.EOF {
				fmt.Fprintln(stdout)
				s.persistHistory()
				return 0
			}
			fmt.Fprintf(stderr, "read error: %v\n", err)
			return 1
		}

		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r\n"))
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, ":") {
			quit := s.handleCommand(ctx, trimmed)
			if quit {
				s.persistHistory()
				return 0
			}
			continue
		}

		s.recordHistory(trimmed)
		s.executeREPLQuery(ctx, trimmed)

		if err == io.EOF {
			s.persistHistory()
			return 0
		}
	}
}

func (s *replSession) activeDicts() []*tsd.Dictionary {
	var out []*tsd.Dictionary
	for i, d := range s.dicts {
		if s.enabled[i] {
			out = append(out, d)
		}
	}
	return out
}

func (s *replSession) recordHistory(q string) {
	if len(s.history) > 0 && s.history[len(s.history)-1] == q {
		return
	}
	s.history = append(s.history, q)
	if len(s.history) > 100 {
		s.history = s.history[len(s.history)-100:]
	}
}

func (s *replSession) persistHistory() {
	if s.cfg == nil || s.cfgPath == "" {
		return
	}
	if _, err := os.Stat(s.cfgPath); err == nil {
		s.cfg.RecentQueries = s.history
		_ = profile.Save(s.cfgPath, s.cfg)
	}
}

func (s *replSession) handleCommand(ctx context.Context, cmdLine string) (quit bool) {
	fields := strings.Fields(cmdLine)
	cmd := strings.ToLower(fields[0])
	argRest := strings.TrimSpace(strings.TrimPrefix(cmdLine, fields[0]))

	switch cmd {
	case ":q", ":quit", ":exit":
		return true

	case ":h", ":help", ":?":
		fmt.Fprintln(s.stdout, `Interactive Commands:
  <pattern>                  Run word or full-text search with live syntax validation
  :val <pattern>             Validate a pattern and display matched length range
  :def <word>                Look up local .tsd definitions (and fallback web links)
  :mode word|fts             Switch between Word Search and Full Text Search (:word / :fts)
  :enum [spec|off]           Set or clear crossword enumeration (e.g. :enum 4,3 or :enum off)
  :range <min> <max>         Set minimum and maximum word length (1..100)
  :sort <mode>               Set sort mode: dict, alpha, alpha-desc, len, len-desc, smart
  :filter <cats>             Set entry filter: all, or comma list of lower,upper,punct,phrase
  :dedupe [on|off]           Toggle cross-dictionary duplicate suppression
  :balances [on|off]         Toggle anagram balance display (-OMITTED+ADDED)
  :exclusive [on|off]        Toggle exclusive letter variables (0..9)
  :punct [ignore|strict]     Toggle Ignore Unused Punctuation mode
  :dicts                     List loaded dictionaries and active status
  :toggle <index>            Enable or disable dictionary #index (1-based)
  :add <path|name>           Load and append an additional .tsd dictionary
  :history                   Show recent search queries
  :set                       Show current session configuration
  :quit (:q)                 Exit interactive mode`)

	case ":word":
		s.opts.FTSMode = false
		fmt.Fprintln(s.stdout, "Switched to Word Search mode.")

	case ":fts":
		s.opts.FTSMode = true
		fmt.Fprintln(s.stdout, "Switched to Full Text Search mode.")

	case ":mode":
		switch strings.ToLower(argRest) {
		case "word", "w":
			s.opts.FTSMode = false
			fmt.Fprintln(s.stdout, "Switched to Word Search mode.")
		case "fts", "fulltext", "f":
			s.opts.FTSMode = true
			fmt.Fprintln(s.stdout, "Switched to Full Text Search mode.")
		default:
			fmt.Fprintln(s.stderr, "Usage: :mode word|fts")
		}

	case ":val", ":validate":
		if argRest == "" {
			fmt.Fprintln(s.stderr, "Usage: :val <pattern>")
			return false
		}
		pOpts := pattern.ParseOptions{
			AtMacro:     s.opts.AtMacro,
			HashMacro:   s.opts.HashMacro,
			Enumeration: s.opts.EnumParts,
		}
		cOpts := pattern.CompileOptions{
			MinLength:               s.opts.MinLen,
			MaxLength:               s.opts.MaxLen,
			ExclusiveVars:           s.opts.ExclusiveVars,
			IgnoreUnusedPunctuation: s.opts.IgnorePunct,
		}
		vr := pattern.Validate(argRest, pOpts, cOpts, s.activeDicts()...)
		if !vr.Valid {
			formatPatternError(s.stdout, argRest, &pattern.SyntaxError{Code: vr.Code, Span: vr.Span, Msg: vr.Message}, false)
		} else if vr.HasWarning {
			fmt.Fprintf(s.stdout, "Warning: %s\n", vr.Message)
		} else {
			fmt.Fprintf(s.stdout, "Valid (%s) — matches word lengths %d..%d\n",
				vr.Message, vr.MinMatchedLength, vr.MaxMatchedLength)
		}

	case ":def", ":define":
		if argRest == "" {
			fmt.Fprintln(s.stderr, "Usage: :def <word>")
			return false
		}
		res, err := lookup.Lookup(ctx, argRest, s.dicts, lookup.Options{
			CaseInsensitiveFallback: true,
			IncludeMentions:         s.opts.IncludeMentions,
			WebProviders:            s.cfg.WebProviders,
		})
		if err != nil {
			fmt.Fprintf(s.stderr, "lookup error: %v\n", err)
			return false
		}
		_ = writeDefinitionResult(s.stdout, res, FormatText)

	case ":enum":
		if argRest == "" || strings.EqualFold(argRest, "off") || strings.EqualFold(argRest, "clear") {
			s.opts.EnumSpec = ""
			s.opts.EnumParts = nil
			fmt.Fprintln(s.stdout, "Enumeration cleared.")
			return false
		}
		parts, err := pattern.ParseEnumeration(argRest)
		if err != nil {
			fmt.Fprintf(s.stderr, "Invalid enumeration: %v\n", err)
			return false
		}
		s.opts.EnumSpec = argRest
		s.opts.EnumParts = parts
		fmt.Fprintf(s.stdout, "Enumeration set to %s -> template %q (%d letters).\n",
			argRest, pattern.EnumerationTemplate(parts), pattern.TotalEnumerationLength(parts))

	case ":range":
		if len(fields) != 3 {
			fmt.Fprintln(s.stderr, "Usage: :range <min> <max>")
			return false
		}
		mn, err1 := strconv.Atoi(fields[1])
		mx, err2 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil || mn < 1 || mx > 100 || mn > mx {
			fmt.Fprintln(s.stderr, "Invalid range (expected 1 <= min <= max <= 100)")
			return false
		}
		s.opts.MinLen = mn
		s.opts.MaxLen = mx
		fmt.Fprintf(s.stdout, "Length range set to %d..%d.\n", mn, mx)

	case ":sort":
		sm, err := search.ParseSortMode(argRest)
		if err != nil {
			fmt.Fprintln(s.stderr, err)
			return false
		}
		s.opts.Sort = sm
		fmt.Fprintf(s.stdout, "Sort mode set to %q.\n", argRest)

	case ":filter":
		mask, err := search.ParseEntryFilter(argRest)
		if err != nil {
			fmt.Fprintln(s.stderr, err)
			return false
		}
		s.opts.EntryFilter = mask
		fmt.Fprintf(s.stdout, "Entry filter updated (0x%02X).\n", uint8(mask))

	case ":dedupe":
		s.opts.Dedupe = toggleFlag(s.opts.Dedupe, argRest)
		fmt.Fprintf(s.stdout, "Duplicate suppression: %v\n", s.opts.Dedupe)

	case ":balances":
		s.opts.ShowBalances = toggleFlag(s.opts.ShowBalances, argRest)
		fmt.Fprintf(s.stdout, "Show balances: %v\n", s.opts.ShowBalances)

	case ":exclusive":
		s.opts.ExclusiveVars = toggleFlag(s.opts.ExclusiveVars, argRest)
		fmt.Fprintf(s.stdout, "Exclusive variables (0..9): %v\n", s.opts.ExclusiveVars)

	case ":punct":
		switch strings.ToLower(argRest) {
		case "ignore", "on", "1":
			s.opts.IgnorePunct = true
		case "strict", "off", "0":
			s.opts.IgnorePunct = false
		default:
			s.opts.IgnorePunct = !s.opts.IgnorePunct
		}
		fmt.Fprintf(s.stdout, "Ignore unused punctuation: %v\n", s.opts.IgnorePunct)

	case ":dicts":
		if len(s.dicts) == 0 {
			fmt.Fprintln(s.stdout, "No dictionaries loaded. Use ':add <path>' to load one.")
			return false
		}
		for i, d := range s.dicts {
			mark := "✓"
			if !s.enabled[i] {
				mark = " "
			}
			caps := ""
			if d.HasDefinitions() {
				caps = " [defs"
				if d.HasFTSIndex() {
					caps += "+fts"
				}
				caps += "]"
			}
			fmt.Fprintf(s.stdout, "  %2d. [%s] %-32s (%7d words, %s)%s\n",
				i+1, mark, d.Name(), d.TotalWords(), d.Version(), caps)
		}

	case ":toggle":
		idx, err := strconv.Atoi(argRest)
		if err != nil || idx < 1 || idx > len(s.dicts) {
			fmt.Fprintf(s.stderr, "Usage: :toggle <1..%d>\n", len(s.dicts))
			return false
		}
		s.enabled[idx-1] = !s.enabled[idx-1]
		fmt.Fprintf(s.stdout, "%s enabled=%v\n", s.dicts[idx-1].Name(), s.enabled[idx-1])

	case ":add":
		if argRest == "" {
			fmt.Fprintln(s.stderr, "Usage: :add <path|name>")
			return false
		}
		paths, err := profile.ResolveDictionaryPaths([]string{argRest}, s.cfg)
		if err != nil {
			fmt.Fprintf(s.stderr, "%v\n", err)
			return false
		}
		for _, p := range paths {
			d, oErr := tsd.Open(p)
			if oErr != nil {
				fmt.Fprintf(s.stderr, "open %s: %v\n", filepath.Base(p), oErr)
				continue
			}
			s.dicts = append(s.dicts, d)
			s.enabled = append(s.enabled, true)
			fmt.Fprintf(s.stdout, "Added %s (%d words).\n", d.Name(), d.TotalWords())
		}

	case ":history":
		for i, q := range s.history {
			fmt.Fprintf(s.stdout, "  %3d  %s\n", i+1, q)
		}

	case ":set":
		fmt.Fprintf(s.stdout, "Mode:          %s\n", map[bool]string{false: "word", true: "fts"}[s.opts.FTSMode])
		fmt.Fprintf(s.stdout, "Length range:  %d..%d\n", s.opts.MinLen, s.opts.MaxLen)
		fmt.Fprintf(s.stdout, "Enumeration:   %q\n", s.opts.EnumSpec)
		fmt.Fprintf(s.stdout, "Macros:        @=%q  #=%q\n", s.opts.AtMacro, s.opts.HashMacro)
		fmt.Fprintf(s.stdout, "Exclusive 0-9: %v\n", s.opts.ExclusiveVars)
		fmt.Fprintf(s.stdout, "Ignore punct:  %v\n", s.opts.IgnorePunct)
		fmt.Fprintf(s.stdout, "Dedupe:        %v\n", s.opts.Dedupe)
		fmt.Fprintf(s.stdout, "Balances:      %v\n", s.opts.ShowBalances)
		fmt.Fprintf(s.stdout, "Annotations:   %v\n", s.opts.ShowAnnotations)

	default:
		fmt.Fprintf(s.stderr, "Unknown command %q (type ':help' for commands)\n", cmd)
	}
	return false
}

func toggleFlag(cur bool, arg string) bool {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "on", "true", "1", "yes":
		return true
	case "off", "false", "0", "no":
		return false
	default:
		return !cur
	}
}

func (s *replSession) executeREPLQuery(ctx context.Context, query string) {
	active := s.activeDicts()
	if len(active) == 0 {
		fmt.Fprintln(s.stderr, "No active dictionaries. Use ':dicts' or ':add <path>'.")
		return
	}

	req := buildSearchRequest(query, active, s.dicts, &s.opts)
	start := time.Now()
	sum, err := s.eng.Search(ctx, req)
	if err != nil {
		formatPatternError(s.stderr, query, err, false)
		return
	}
	elapsed := time.Since(start).Round(time.Microsecond)

	total := len(sum.Matches)
	for i, m := range sum.Matches {
		if i > 0 && i%replPageSize == 0 && isInteractiveStdin(os.Stdin) {
			fmt.Fprintf(s.stdout, "-- More (%d/%d, Enter=next page, q=stop) -- ", i, total)
			resp, _ := s.stdin.ReadString('\n')
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(resp)), "q") {
				break
			}
		}
		line := formatTextMatchLine(m, active, &s.opts)
		if m.HasDefinition && !s.opts.FTSMode {
			line += " 📖"
		}
		fmt.Fprintln(s.stdout, line)
	}
	fmt.Fprintf(s.stdout, "(%d matches in %s)\n", total, elapsed)
}

func isInteractiveStdin(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
