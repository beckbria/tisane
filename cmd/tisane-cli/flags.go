package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"tisane/pkg/pattern"
	"tisane/pkg/profile"
	"tisane/pkg/search"
)

// OutputFormat selects human text vs. structured machine output.
type OutputFormat string

const (
	FormatText  OutputFormat = "text"
	FormatJSON  OutputFormat = "json"
	FormatJSONL OutputFormat = "jsonl"
	FormatCSV   OutputFormat = "csv"
)

type cliOptions struct {
	DictSpecs       []string
	ConfigPath      string
	AtMacro         string
	HashMacro       string
	MinLen          int
	MaxLen          int
	ShowAnnotations bool
	ShowBalances    bool
	ExclusiveVars   bool
	IgnorePunct     bool
	OutputTransform int // /O:0..4 (0=exact, 1=ascii, 2=ascii-letters, 3=ascii-lower, 4=ascii-upper)
	Quiet           bool
	Limit           int
	CountOnly       bool
	ShowStats       bool
	FTSMode         bool
	DefineMode      bool
	DefineWords     []string
	IncludeMentions bool
	Sort            search.SortMode
	Dedupe          bool
	DedupeExplicit  bool
	ShowDict        bool
	EntryFilter     search.EntryTypeMask
	NoPhrases       bool
	NoProper        bool
	EnumSpec        string
	EnumParts       []pattern.EnumerationPart
	Format          OutputFormat
	Interactive     bool
	ShowHelp        bool
	ShowVersion     bool
	Patterns        []string
}

func defaultCLIOptions(cfg *profile.Config) cliOptions {
	if cfg == nil {
		cfg = profile.DefaultConfig()
	}
	sortMode, err := search.ParseSortMode(cfg.SortMode)
	if err != nil {
		sortMode = search.SortDict
	}
	return cliOptions{
		AtMacro:         cfg.AtMacro,
		HashMacro:       cfg.HashMacro,
		MinLen:          cfg.MinLength,
		MaxLen:          cfg.MaxLength,
		ShowAnnotations: cfg.ShowAnnotations,
		ShowBalances:    cfg.ShowBalances,
		ExclusiveVars:   cfg.ExclusiveVars,
		IgnorePunct:     cfg.IgnoreUnusedPunctuation,
		OutputTransform: 0,
		Sort:            sortMode,
		Dedupe:          false, // TEACON default searches dicts sequentially unless --dedupe enabled
		EntryFilter:     search.EntryFilterFromProfile(cfg.EntryFilter),
		Format:          FormatText,
	}
}

// isLegacySwitch detects TEACON-style /X:... switches while never misclassifying Unix paths like /home/user/dict.tsd.
func isLegacySwitch(arg string) bool {
	if len(arg) < 2 || arg[0] != '/' {
		return false
	}
	if len(arg) == 2 {
		c := arg[1]
		return c == '?' || c == 'q' || c == 'Q' || c == 'h' || c == 'H'
	}
	if len(arg) >= 3 && arg[2] == ':' {
		switch arg[1] {
		case 'd', 'D', 'h', 'H', 'a', 'A', 's', 'S', 'l', 'L',
			'n', 'N', 'b', 'B', 'x', 'X', 'u', 'U', 'o', 'O':
			return true
		}
	}
	return false
}

func parseZeroOne(val, flagName string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value %q for %s (expected 0 or 1)", val, flagName)
	}
}

func parseCLIArgs(args []string, base cliOptions) (cliOptions, error) {
	opts := base
	endOfFlags := false

	nextValue := func(i *int, currentArg, flagName string) (string, error) {
		if eq := strings.IndexByte(currentArg, '='); eq >= 0 {
			return currentArg[eq+1:], nil
		}
		if *i+1 >= len(args) {
			return "", fmt.Errorf("flag %s requires a value", flagName)
		}
		*i++
		return args[*i], nil
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if endOfFlags {
			opts.Patterns = append(opts.Patterns, arg)
			continue
		}
		if arg == "--" {
			endOfFlags = true
			continue
		}

		// 1. Legacy TEACON /X:... switches
		if isLegacySwitch(arg) {
			if len(arg) == 2 {
				switch arg[1] {
				case '?', 'h', 'H':
					opts.ShowHelp = true
				case 'q', 'Q':
					opts.Quiet = true
				}
				continue
			}
			sw := arg[1]
			val := arg[3:]
			switch sw {
			case 'd', 'D':
				opts.DictSpecs = append(opts.DictSpecs, val)
			case 'h', 'H':
				opts.HashMacro = val
			case 'a', 'A':
				opts.AtMacro = val
			case 's', 'S':
				n, err := strconv.Atoi(val)
				if err != nil || n < 1 || n > 100 {
					return opts, fmt.Errorf("invalid /S: minimum length %q (must be 1..100)", val)
				}
				opts.MinLen = n
			case 'l', 'L':
				n, err := strconv.Atoi(val)
				if err != nil || n < 1 || n > 100 {
					return opts, fmt.Errorf("invalid /L: maximum length %q (must be 1..100)", val)
				}
				opts.MaxLen = n
			case 'n', 'N':
				b, err := parseZeroOne(val, "/N:")
				if err != nil {
					return opts, err
				}
				opts.ShowAnnotations = b
			case 'b', 'B':
				b, err := parseZeroOne(val, "/B:")
				if err != nil {
					return opts, err
				}
				opts.ShowBalances = b
			case 'x', 'X':
				b, err := parseZeroOne(val, "/X:")
				if err != nil {
					return opts, err
				}
				opts.ExclusiveVars = b
			case 'u', 'U':
				b, err := parseZeroOne(val, "/U:")
				if err != nil {
					return opts, err
				}
				opts.IgnorePunct = b
			case 'o', 'O':
				n, err := strconv.Atoi(val)
				if err != nil || n < 0 || n > 4 {
					return opts, fmt.Errorf("invalid /O: output mode %q (must be 0..4)", val)
				}
				opts.OutputTransform = n
			}
			continue
		}

		// 2. Modern POSIX / GNU flags (-x, --long, --long=val)
		if strings.HasPrefix(arg, "-") && len(arg) > 1 {
			name := arg
			hasEq := false
			if eq := strings.IndexByte(arg, '='); eq >= 0 {
				name = arg[:eq]
				hasEq = true
			}

			switch name {
			case "-h", "--help":
				opts.ShowHelp = true
			case "-v", "--version":
				opts.ShowVersion = true
			case "-q", "--quiet", "--quiet-banner":
				opts.Quiet = true
			case "-i", "--interactive", "--repl":
				opts.Interactive = true
			case "-F", "--fts", "--full-text":
				opts.FTSMode = true
			case "-D", "--define":
				opts.DefineMode = true
				if hasEq {
					val, _ := nextValue(&i, arg, name)
					if val != "" {
						opts.DefineWords = append(opts.DefineWords, val)
					}
				}
			case "--mentions":
				opts.IncludeMentions = true
			case "-c", "--count":
				opts.CountOnly = true
			case "--stats":
				opts.ShowStats = true
			case "--dedupe":
				opts.Dedupe = true
				opts.DedupeExplicit = true
			case "--no-dedupe":
				opts.Dedupe = false
				opts.DedupeExplicit = true
			case "--show-dict":
				opts.ShowDict = true
			case "--no-phrases":
				opts.NoPhrases = true
			case "--no-proper":
				opts.NoProper = true
			case "-a", "--annotations":
				if hasEq {
					val, _ := nextValue(&i, arg, name)
					b, err := parseZeroOne(val, name)
					if err != nil {
						return opts, err
					}
					opts.ShowAnnotations = b
				} else {
					opts.ShowAnnotations = true
				}
			case "--no-annotations":
				opts.ShowAnnotations = false
			case "-b", "--balances":
				if hasEq {
					val, _ := nextValue(&i, arg, name)
					b, err := parseZeroOne(val, name)
					if err != nil {
						return opts, err
					}
					opts.ShowBalances = b
				} else {
					opts.ShowBalances = true
				}
			case "--no-balances":
				opts.ShowBalances = false
			case "-x", "--exclusive":
				if hasEq {
					val, _ := nextValue(&i, arg, name)
					b, err := parseZeroOne(val, name)
					if err != nil {
						return opts, err
					}
					opts.ExclusiveVars = b
				} else {
					opts.ExclusiveVars = true
				}
			case "--no-exclusive":
				opts.ExclusiveVars = false
			case "-u", "--ignore-punct":
				if hasEq {
					val, _ := nextValue(&i, arg, name)
					b, err := parseZeroOne(val, name)
					if err != nil {
						return opts, err
					}
					opts.IgnorePunct = b
				} else {
					opts.IgnorePunct = true
				}
			case "--no-ignore-punct", "--strict-punct":
				opts.IgnorePunct = false
			case "-d", "--dict":
				val, err := nextValue(&i, arg, name)
				if err != nil {
					return opts, err
				}
				opts.DictSpecs = append(opts.DictSpecs, val)
			case "--config":
				val, err := nextValue(&i, arg, name)
				if err != nil {
					return opts, err
				}
				opts.ConfigPath = val
			case "--at-macro":
				val, err := nextValue(&i, arg, name)
				if err != nil {
					return opts, err
				}
				opts.AtMacro = val
			case "--hash-macro":
				val, err := nextValue(&i, arg, name)
				if err != nil {
					return opts, err
				}
				opts.HashMacro = val
			case "-s", "--min-len":
				val, err := nextValue(&i, arg, name)
				if err != nil {
					return opts, err
				}
				n, err := strconv.Atoi(val)
				if err != nil || n < 1 || n > 100 {
					return opts, fmt.Errorf("invalid %s %q (must be 1..100)", name, val)
				}
				opts.MinLen = n
			case "-l", "--max-len":
				val, err := nextValue(&i, arg, name)
				if err != nil {
					return opts, err
				}
				n, err := strconv.Atoi(val)
				if err != nil || n < 1 || n > 100 {
					return opts, fmt.Errorf("invalid %s %q (must be 1..100)", name, val)
				}
				opts.MaxLen = n
			case "-n", "--limit":
				val, err := nextValue(&i, arg, name)
				if err != nil {
					return opts, err
				}
				n, err := strconv.Atoi(val)
				if err != nil || n < 0 {
					return opts, fmt.Errorf("invalid %s %q (must be >= 0)", name, val)
				}
				opts.Limit = n
			case "-o", "--output-mode":
				val, err := nextValue(&i, arg, name)
				if err != nil {
					return opts, err
				}
				n, err := strconv.Atoi(val)
				if err != nil || n < 0 || n > 4 {
					return opts, fmt.Errorf("invalid %s %q (must be 0..4)", name, val)
				}
				opts.OutputTransform = n
			case "--sort":
				val, err := nextValue(&i, arg, name)
				if err != nil {
					return opts, err
				}
				sm, err := search.ParseSortMode(val)
				if err != nil {
					return opts, err
				}
				opts.Sort = sm
			case "--filter":
				val, err := nextValue(&i, arg, name)
				if err != nil {
					return opts, err
				}
				mask, err := search.ParseEntryFilter(val)
				if err != nil {
					return opts, err
				}
				opts.EntryFilter = mask
			case "--enum":
				val, err := nextValue(&i, arg, name)
				if err != nil {
					return opts, err
				}
				parts, err := pattern.ParseEnumeration(val)
				if err != nil {
					return opts, err
				}
				opts.EnumSpec = val
				opts.EnumParts = parts
			case "--format":
				val, err := nextValue(&i, arg, name)
				if err != nil {
					return opts, err
				}
				switch strings.ToLower(strings.TrimSpace(val)) {
				case "text", "plain":
					opts.Format = FormatText
				case "json":
					opts.Format = FormatJSON
				case "jsonl", "ndjson":
					opts.Format = FormatJSONL
				case "csv":
					opts.Format = FormatCSV
				default:
					return opts, fmt.Errorf("unknown --format %q (valid: text, json, jsonl, csv)", val)
				}
			default:
				return opts, fmt.Errorf("unknown flag %q (use --help for usage)", arg)
			}
			continue
		}

		// Positional pattern (or word when --define is active)
		if opts.DefineMode {
			opts.DefineWords = append(opts.DefineWords, arg)
		} else {
			opts.Patterns = append(opts.Patterns, arg)
		}
	}

	if opts.NoPhrases {
		opts.EntryFilter &^= search.EntryPhrase
	}
	if opts.NoProper {
		opts.EntryFilter &^= search.EntryCapitalized
	}
	return opts, nil
}

func printHelp(w io.Writer) {
	fmt.Fprintf(w, `Tisane CLI (%s) — Crossword, Anagram & Dictionary Search Tool

Usage:
  tisane-cli [options] [pattern ...]
  tisane-cli --define [options] <word ...>
  tisane-cli -i [options]

Search & Mode Options:
  -d, --dict <list>         Dictionary .tsd path(s) or names (';'-separated or repeatable)
  -F, --fts                 Full Text Search across dictionary definitions
  -D, --define <word>       Display local .tsd definitions (or web URLs if undefined)
      --mentions            Include FTS body mentions when viewing --define results
  -i, --interactive         Start interactive REPL mode with history & live validation

Pattern & Constraint Options:
  -s, --min-len <1..100>    Minimum canonical word length                 (legacy: /S:n)
  -l, --max-len <1..100>    Maximum canonical word length                 (legacy: /L:n)
      --enum <spec>         Crossword enumeration shorthand (e.g. 4,3 or 4-2-2)
      --at-macro <str>      '@' macro expansion (default: [aeiou])        (legacy: /A:str)
      --hash-macro <str>    '#' macro expansion (default: [^aeiou])       (legacy: /H:str)
  -x, --exclusive           Require variables 0..9 to bind distinct chars (legacy: /X:1)
  -u, --ignore-punct        Ignore unused punctuation in templates        (legacy: /U:1)
      --strict-punct        Require all punctuation in templates          (legacy: /U:0)

Filtering, Sorting & Output Options:
      --sort <mode>         Sort: dict, alpha, alpha-desc, len, len-desc, smart
      --dedupe              Suppress duplicate matches across dictionaries
      --show-dict           Prefix/tag each match with its source dictionary name
      --filter <cats>       Entry types: lower,upper,punct,phrase
      --no-phrases          Exclude multi-word phrases containing spaces
      --no-proper           Exclude capitalized words / proper nouns
  -n, --limit <max>         Maximum number of matches per pattern (0 = all)
  -a, --annotations         Include dictionary annotations                (legacy: /N:1)
      --no-annotations      Omit dictionary annotations                   (legacy: /N:0)
  -b, --balances            Include -OMITTED+ADDED anagram balances       (legacy: /B:1)
      --no-balances         Omit anagram balances                         (legacy: /B:0)
  -o, --output-mode <0..4>  Word text transform:                          (legacy: /O:0..4)
                              0 = exact dictionary text (default)
                              1 = ASCII (accents transliterated)
                              2 = ASCII letters only
                              3 = ASCII lowercase letters only
                              4 = ASCII uppercase letters only
      --format <fmt>        Output format: text, json, jsonl, csv
  -c, --count               Print match count only
      --stats               Print match count and scan timing summary
  -q, --quiet               Suppress pattern headers & comment echoing    (legacy: /Q)
      --config <path>       Custom config.toml path
  -v, --version             Print version and exit
  -h, --help                Show this help text (legacy: /?)

Batch Stdin Mode:
  When invoked without pattern arguments (and without -i or --define), patterns
  are read line-by-line from standard input. Lines starting with whitespace are
  echoed verbatim as comments unless -q / /Q is set.
`, cliVersion)
}
