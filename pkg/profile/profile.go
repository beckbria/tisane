// Package profile manages portable Tisane user configuration (~/.config/tisane/config.toml),
// ordered dictionary profiles, macro presets, web lookup templates, and dictionary path resolution.
package profile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"tisane"
	"tisane/pkg/lookup"
	"tisane/pkg/pattern"
	"tisane/pkg/tsd"
)

// Version is the current release version of Tisane.
var Version = tisane.Version

const (
	// EnvDictPath overrides or extends dictionary search directories (':' or ';' separated).
	EnvDictPath = "TISANE_DICT_PATH"
	// EnvConfigPath overrides the default config.toml location.
	EnvConfigPath = "TISANE_CONFIG"

	// DefaultSidebarWidth is the default pixel width of the dictionary sidebar.
	DefaultSidebarWidth = 272
	// MinSidebarWidth is the minimum expanded pixel width of the dictionary sidebar.
	MinSidebarWidth = 180
	// MaxSidebarWidth is the maximum expanded pixel width of the dictionary sidebar.
	MaxSidebarWidth = 560

	// DefaultDefinitionHeight is the default pixel height of the definition pane.
	DefaultDefinitionHeight = 256
	// MinDefinitionHeight is the minimum expanded pixel height of the definition pane.
	MinDefinitionHeight = 120
	// MaxDefinitionHeight is the maximum expanded pixel height of the definition pane.
	MaxDefinitionHeight = 640

	// DefaultMaxRecentQueries is the default cap on Config.RecentQueries.
	DefaultMaxRecentQueries = 50

	maxConfigBytes = 4 << 20 // 4 MiB
)

// NormalizeSidebarWidth clamps a sidebar pixel width into [MinSidebarWidth, MaxSidebarWidth],
// falling back to DefaultSidebarWidth when non-positive.
func NormalizeSidebarWidth(w int) int {
	if w <= 0 {
		return DefaultSidebarWidth
	}
	if w < MinSidebarWidth {
		return MinSidebarWidth
	}
	if w > MaxSidebarWidth {
		return MaxSidebarWidth
	}
	return w
}

// NormalizeDefinitionHeight clamps a definition pane pixel height into [MinDefinitionHeight, MaxDefinitionHeight],
// falling back to DefaultDefinitionHeight when non-positive.
func NormalizeDefinitionHeight(h int) int {
	if h <= 0 {
		return DefaultDefinitionHeight
	}
	if h < MinDefinitionHeight {
		return MinDefinitionHeight
	}
	if h > MaxDefinitionHeight {
		return MaxDefinitionHeight
	}
	return h
}

// DictConfig represents one configured .tsd dictionary in user priority order.
type DictConfig struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// ElementFont holds the font family and pixel size for a specific UI text area.
type ElementFont struct {
	Family string `json:"family"`
	Size   int    `json:"size"`
}

// FontConfig holds customizable typography settings for Tisane UI text elements.
type FontConfig struct {
	Results          ElementFont `json:"results"`
	Definitions      ElementFont `json:"definitions"`
	PatternInput     ElementFont `json:"patternInput"`
	DictionariesList ElementFont `json:"dictionariesList"`
	StatusBar        ElementFont `json:"statusBar"`
	Toolbar          ElementFont `json:"toolbar"`
}

// DefaultFontConfig returns the default font families and pixel sizes for all UI text areas.
func DefaultFontConfig() FontConfig {
	return FontConfig{
		Results:          ElementFont{Family: "monospace", Size: 13},
		Definitions:      ElementFont{Family: "sans-serif", Size: 15},
		PatternInput:     ElementFont{Family: "monospace", Size: 14},
		DictionariesList: ElementFont{Family: "sans-serif", Size: 12},
		StatusBar:        ElementFont{Family: "sans-serif", Size: 11},
		Toolbar:          ElementFont{Family: "sans-serif", Size: 12},
	}
}

func normalizeElementFont(ef ElementFont, def ElementFont) ElementFont {
	family := strings.TrimSpace(ef.Family)
	if family == "" {
		family = def.Family
	}
	size := ef.Size
	if size <= 0 {
		size = def.Size
	} else if size < 8 {
		size = 8
	} else if size > 48 {
		size = 48
	}
	return ElementFont{Family: family, Size: size}
}

// NormalizeFontConfig ensures all font families are non-empty and font sizes are within [8, 48].
func NormalizeFontConfig(fc FontConfig) FontConfig {
	def := DefaultFontConfig()
	return FontConfig{
		Results:          normalizeElementFont(fc.Results, def.Results),
		Definitions:      normalizeElementFont(fc.Definitions, def.Definitions),
		PatternInput:     normalizeElementFont(fc.PatternInput, def.PatternInput),
		DictionariesList: normalizeElementFont(fc.DictionariesList, def.DictionariesList),
		StatusBar:        normalizeElementFont(fc.StatusBar, def.StatusBar),
		Toolbar:          normalizeElementFont(fc.Toolbar, def.Toolbar),
	}
}

// Config holds user preferences shared by the Tisane desktop app and tisane-cli.
type Config struct {
	DictDirs                []string             `json:"dictDirs"`
	Dictionaries            []DictConfig         `json:"dictionaries"`
	AtMacro                 string               `json:"atMacro"`
	HashMacro               string               `json:"hashMacro"`
	MinLength               int                  `json:"minLength"`
	MaxLength               int                  `json:"maxLength"`
	ExclusiveVars           bool                 `json:"exclusiveVars"`
	IgnoreUnusedPunctuation bool                 `json:"ignoreUnusedPunctuation"`
	ShowBalances            bool                 `json:"showBalances"`
	ShowAnnotations         bool                 `json:"showAnnotations"`
	SuppressDuplicates      bool                 `json:"suppressDuplicates"`
	EntryFilter             []string             `json:"entryFilter"`         // "lowercase", "capitalized", "punctuated", "phrase"
	SortMode                string               `json:"sortMode"`            // "dict", "alpha", "alpha-desc", "len", "len-desc", "smart"
	LayoutMode              string               `json:"layoutMode"`          // "tiled" (default) or "unified"
	SidebarWidth            int                  `json:"sidebarWidth"`        // expanded pixel width of the dictionary sidebar
	SidebarCollapsed        bool                 `json:"sidebarCollapsed"`    // whether the dictionary sidebar is collapsed
	DefinitionHeight        int                  `json:"definitionHeight"`    // expanded pixel height of the definition pane
	DefinitionCollapsed     bool                 `json:"definitionCollapsed"` // whether the definition pane is collapsed
	AutoWebFallback         bool                 `json:"autoWebFallback"`
	DefaultWebProvider      string               `json:"defaultWebProvider"`
	WebProviders            []lookup.WebProvider `json:"webProviders"`
	RecentQueries           []string             `json:"recentQueries"`
	Fonts                   FontConfig           `json:"fonts"`
}

// NormalizeSortMode canonicalizes a sort mode string, defaulting unknown values to "dict".
func NormalizeSortMode(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "dict", "dictionary", "none":
		return "dict"
	case "alpha", "az", "a-z", "asc":
		return "alpha"
	case "alpha-desc", "za", "z-a", "desc":
		return "alpha-desc"
	case "len", "length", "len-asc":
		return "len"
	case "len-desc", "length-desc":
		return "len-desc"
	case "smart", "balance":
		return "smart"
	default:
		return "dict"
	}
}

// NormalizeLayoutMode canonicalizes a UI layout mode to "tiled" or "unified".
func NormalizeLayoutMode(s string) string {
	if strings.EqualFold(strings.TrimSpace(s), "unified") {
		return "unified"
	}
	return "tiled"
}

// NormalizeEntryFilter canonicalizes and deduplicates entry category names,
// falling back to all four categories when none are valid.
func NormalizeEntryFilter(items []string) []string {
	var seen [4]bool
	var out []string
	add := func(idx int, canon string) {
		if !seen[idx] {
			seen[idx] = true
			out = append(out, canon)
		}
	}
	for _, raw := range items {
		for _, tok := range strings.Split(raw, ",") {
			switch strings.ToLower(strings.TrimSpace(tok)) {
			case "lower", "lowercase", "abc":
				add(0, "lowercase")
			case "upper", "cap", "capitalized", "proper":
				add(1, "capitalized")
			case "punct", "punctuated", "hyphen":
				add(2, "punctuated")
			case "phrase", "phrases", "space", "multi":
				add(3, "phrase")
			case "all":
				add(0, "lowercase")
				add(1, "capitalized")
				add(2, "punctuated")
				add(3, "phrase")
			}
		}
	}
	if len(out) == 0 {
		return []string{"lowercase", "capitalized", "punctuated", "phrase"}
	}
	return out
}

// NormalizeLengthBounds clamps minLen and maxLen into [1, tsd.MaxWordLengthLimit] and ensures minLen <= maxLen.
func NormalizeLengthBounds(minLen, maxLen int) (int, int) {
	if minLen < 1 {
		minLen = 1
	} else if minLen > tsd.MaxWordLengthLimit {
		minLen = tsd.MaxWordLengthLimit
	}
	if maxLen < 1 {
		maxLen = 1
	} else if maxLen > tsd.MaxWordLengthLimit {
		maxLen = tsd.MaxWordLengthLimit
	}
	if minLen > maxLen {
		minLen, maxLen = maxLen, minLen
	}
	return minLen, maxLen
}

// RecordRecentQuery prepends query to c.RecentQueries, deduplicating prior occurrences
// and capping history length to maxItems (defaulting to DefaultMaxRecentQueries when <= 0).
func (c *Config) RecordRecentQuery(query string, maxItems int) {
	if c == nil {
		return
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return
	}
	if maxItems <= 0 {
		maxItems = DefaultMaxRecentQueries
	}
	next := make([]string, 0, min(len(c.RecentQueries)+1, maxItems))
	next = append(next, query)
	for _, existing := range c.RecentQueries {
		if existing == query {
			continue
		}
		if len(next) >= maxItems {
			break
		}
		next = append(next, existing)
	}
	c.RecentQueries = next
}

// DefaultConfig returns Tisane's standard out-of-the-box configuration.
// Note that LayoutMode defaults to "tiled" (separate tiled panes per dictionary).
func DefaultConfig() *Config {
	return &Config{
		DictDirs:                []string{},
		Dictionaries:            []DictConfig{},
		AtMacro:                 pattern.DefaultAtMacro,
		HashMacro:               pattern.DefaultHashMacro,
		MinLength:               1,
		MaxLength:               tsd.MaxWordLengthLimit,
		ExclusiveVars:           false,
		IgnoreUnusedPunctuation: true,
		ShowBalances:            true,
		ShowAnnotations:         true,
		SuppressDuplicates:      true,
		EntryFilter:             []string{"lowercase", "capitalized", "punctuated", "phrase"},
		SortMode:                "dict",
		LayoutMode:              "tiled",
		SidebarWidth:            DefaultSidebarWidth,
		SidebarCollapsed:        false,
		DefinitionHeight:        DefaultDefinitionHeight,
		DefinitionCollapsed:     false,
		AutoWebFallback:         false,
		DefaultWebProvider:      "onelook",
		WebProviders:            lookup.DefaultWebProviders(),
		RecentQueries:           []string{},
		Fonts:                   DefaultFontConfig(),
	}
}

// DefaultConfigPath returns the OS-appropriate config file path (~/.config/tisane/config.toml on Linux),
// honoring TISANE_CONFIG if set.
func DefaultConfigPath() (string, error) {
	if env := strings.TrimSpace(os.Getenv(EnvConfigPath)); env != "" {
		return env, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "tisane", "config.toml"), nil
}

// Load reads a TOML profile from path, requiring a regular file up to 4 MiB.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("config %q is not a regular file", path)
	}
	if info.Size() > maxConfigBytes {
		return nil, fmt.Errorf("config %q exceeds maximum size (%d bytes)", path, maxConfigBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxConfigBytes {
		return nil, fmt.Errorf("config %q exceeds maximum size (%d bytes)", path, maxConfigBytes)
	}
	return ParseTOML(data)
}

// LoadOrDefault loads DefaultConfigPath(), returning DefaultConfig() if no config file exists yet.
func LoadOrDefault() (*Config, string, error) {
	p, err := DefaultConfigPath()
	if err != nil {
		return DefaultConfig(), "", nil
	}
	cfg, err := Load(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultConfig(), p, nil
		}
		return nil, p, err
	}
	return cfg, p, nil
}

// Save writes cfg as clean TOML to path atomically via a private temporary sibling file.
func Save(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory %q: %w", dir, err)
	}
	data := MarshalTOML(cfg)
	f, err := os.CreateTemp(dir, ".config-*.toml.tmp")
	if err != nil {
		return fmt.Errorf("create temp config in %q: %w", dir, err)
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
		return fmt.Errorf("write config %q: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync config %q: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp config %q: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace config %q: %w", path, err)
	}
	keepTmp = true
	return nil
}

// SplitDictPathEnv splits TISANE_DICT_PATH on os.PathListSeparator (and ';' on Unix when paths don't exist with ';').
func SplitDictPathEnv(val string) []string {
	val = strings.TrimSpace(val)
	if val == "" {
		return nil
	}
	var out []string
	for _, part := range filepath.SplitList(val) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if filepath.Separator == '/' && strings.Contains(part, ";") {
			if _, err := os.Stat(part); err != nil {
				for _, sub := range strings.Split(part, ";") {
					if sub = strings.TrimSpace(sub); sub != "" {
						out = append(out, sub)
					}
				}
				continue
			}
		}
		out = append(out, part)
	}
	return out
}

// ExecutablePathFunc is used to determine the path of the running executable.
// It defaults to os.Executable and may be overridden in tests.
var ExecutablePathFunc = os.Executable

// ExecutableSearchDirs returns candidate dictionary directories relative to the
// directory the executable is running from as well as the current working directory,
// including each location's "dictionaries" subdirectory.
func ExecutableSearchDirs() []string {
	var dirs []string
	seen := make(map[string]bool)
	addDir := func(d string) {
		d = strings.TrimSpace(d)
		if d == "" {
			return
		}
		clean := filepath.Clean(d)
		if !seen[clean] {
			seen[clean] = true
			dirs = append(dirs, d)
		}
	}
	addBase := func(base string) {
		base = strings.TrimSpace(base)
		if base == "" {
			return
		}
		addDir(base)
		addDir(filepath.Join(base, "dictionaries"))
		addDir(filepath.Join(base, "Dictionaries"))
	}

	if ExecutablePathFunc != nil {
		if exePath, err := ExecutablePathFunc(); err == nil && exePath != "" {
			exeDir := filepath.Dir(exePath)
			addBase(exeDir)
			if resolved, err := filepath.EvalSymlinks(exePath); err == nil && resolved != "" {
				realDir := filepath.Dir(resolved)
				if realDir != exeDir {
					addBase(realDir)
				}
			}
		}
	}

	addBase(".")

	if bwd := strings.TrimSpace(os.Getenv("BUILD_WORKING_DIRECTORY")); bwd != "" {
		addBase(bwd)
	}

	return dirs
}

// DefaultSearchDirs returns all dictionary search directories in priority order:
//  1. TISANE_DICT_PATH environment variable entries
//  2. Configured cfg.DictDirs entries
//  3. The executable's directory, the current working directory, and their "dictionaries" subdirectories
func DefaultSearchDirs(cfg *Config) []string {
	var searchDirs []string
	searchDirs = append(searchDirs, SplitDictPathEnv(os.Getenv(EnvDictPath))...)
	if cfg != nil {
		searchDirs = append(searchDirs, cfg.DictDirs...)
	}
	searchDirs = append(searchDirs, ExecutableSearchDirs()...)
	return searchDirs
}

// DiscoverDictionaries scans dirs for *.tsd files (case-insensitive extension) and returns sorted paths.
func DiscoverDictionaries(dirs ...string) ([]string, error) {
	seen := make(map[string]bool)
	var seenInfos []os.FileInfo
	var found []string

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || len(dirs) > 1 {
				continue
			}
			return nil, err
		}
		var dirHits []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if strings.EqualFold(filepath.Ext(e.Name()), ".tsd") {
				full := filepath.Join(dir, e.Name())
				info, statErr := os.Stat(full)
				if statErr != nil || !info.Mode().IsRegular() {
					continue
				}
				abs, err := filepath.Abs(full)
				if err != nil {
					abs = filepath.Clean(full)
				}
				if seen[abs] || isSameAsAny(info, seenInfos) {
					continue
				}
				seen[abs] = true
				seenInfos = append(seenInfos, info)
				dirHits = append(dirHits, full)
			}
		}
		sort.Strings(dirHits)
		found = append(found, dirHits...)
	}
	return found, nil
}

func isSameAsAny(info os.FileInfo, seen []os.FileInfo) bool {
	for _, s := range seen {
		if os.SameFile(info, s) {
			return true
		}
	}
	return false
}

// ResolveDictionaryPaths resolves CLI or UI dictionary specifications (such as "/D:core;edited"
// or bare names) into concrete .tsd filesystem paths using:
//  1. Direct path (with or without ".tsd" suffix)
//  2. Configured profile dictionary names/paths
//  3. Search directories in TISANE_DICT_PATH, cfg.DictDirs, executable directory, current directory, and "dictionaries" subdirectories
//
// If specs is empty, all Enabled dictionaries in cfg.Dictionaries are returned in priority order.
func ResolveDictionaryPaths(specs []string, cfg *Config) ([]string, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	var flatSpecs []string
	for _, rawSpec := range specs {
		for _, part := range strings.Split(rawSpec, ";") {
			if s := strings.TrimSpace(part); s != "" {
				flatSpecs = append(flatSpecs, s)
			}
		}
	}

	if len(flatSpecs) == 0 {
		var active []string
		for _, dc := range cfg.Dictionaries {
			if dc.Enabled && dc.Path != "" && fileExists(dc.Path) {
				active = append(active, dc.Path)
			}
		}
		return active, nil
	}

	searchDirs := DefaultSearchDirs(cfg)

	out := make([]string, 0, len(flatSpecs))
	for _, spec := range flatSpecs {
		resolved, ok := resolveSingleSpec(spec, cfg, searchDirs)
		if !ok {
			return nil, fmt.Errorf("dictionary %q not found", spec)
		}
		out = append(out, resolved)
	}
	return out, nil
}

func resolveSingleSpec(spec string, cfg *Config, searchDirs []string) (string, bool) {
	hasTSDExt := hasTSDSuffix(spec)

	// 1. Direct .tsd path (or spec + ".tsd"). Require .tsd suffix when probing bare paths
	// so an executable or non-dictionary file (e.g. "./tisane") never shadows "tisane.tsd".
	if hasTSDExt {
		if fileExists(spec) {
			return spec, true
		}
	} else if fileExists(spec + ".tsd") {
		return spec + ".tsd", true
	}

	// 2. Match configured profile dictionary by Name or base stem (case-insensitive)
	specStem := trimTSDExt(spec)
	for _, dc := range cfg.Dictionaries {
		base := filepath.Base(dc.Path)
		stem := trimTSDExt(base)
		if strings.EqualFold(dc.Name, spec) || strings.EqualFold(dc.Name, specStem) ||
			strings.EqualFold(base, spec) || strings.EqualFold(stem, specStem) {
			if fileExists(dc.Path) {
				return dc.Path, true
			}
		}
	}

	// 3. Search directories (exact .tsd filename, stem + .tsd, or case-insensitive stem match)
	for _, dir := range searchDirs {
		cand := filepath.Join(dir, spec)
		if hasTSDExt {
			if fileExists(cand) {
				return cand, true
			}
		} else {
			candTSD := cand + ".tsd"
			if fileExists(candTSD) {
				return candTSD, true
			}
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !hasTSDSuffix(e.Name()) {
				continue
			}
			stem := trimTSDExt(e.Name())
			if strings.EqualFold(e.Name(), spec) || strings.EqualFold(stem, specStem) {
				candMatch := filepath.Join(dir, e.Name())
				if fileExists(candMatch) {
					return candMatch, true
				}
			}
		}
	}

	return "", false
}

func hasTSDSuffix(name string) bool {
	return len(name) >= 4 && strings.EqualFold(name[len(name)-4:], ".tsd")
}

func trimTSDExt(name string) string {
	if hasTSDSuffix(name) {
		return name[:len(name)-4]
	}
	return name
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
