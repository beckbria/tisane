package profile_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"tisane/pkg/lookup"
	"tisane/pkg/profile"
)

func TestProfileSaveLoadRoundTripAndResolution(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "tisane", "config.toml")

	cfg := profile.DefaultConfig()
	if cfg.LayoutMode != "tiled" {
		t.Fatalf("expected default LayoutMode='tiled', got %q", cfg.LayoutMode)
	}

	dictsDir, err := filepath.Abs(filepath.Join("..", "..", "dictionaries"))
	if err != nil {
		t.Fatal(err)
	}

	cfg.AtMacro = "[aeiouy]"
	cfg.MinLength = 3
	cfg.MaxLength = 15
	cfg.SortMode = "smart"
	cfg.Theme = "light"
	cfg.UIZoom = 125
	cfg.DictDirs = []string{tmp, dictsDir}
	cfg.Dictionaries = []profile.DictConfig{
		{Path: filepath.Join(dictsDir, "English Wordnet 2025 Plus.tsd"), Name: "WordNet", Enabled: true},
		{Path: filepath.Join(tmp, "custom.tsd"), Name: "Custom", Enabled: false},
	}
	cfg.RecentQueries = []string{".....*;aadmort", `c@t & !#at`}
	cfg.WebProviders = []lookup.WebProvider{
		{ID: "custom", Name: "Custom Dict", URLTemplate: "https://example.com/q?w={entry}#top"},
	}
	cfg.Fonts.Results = profile.ElementFont{Family: "JetBrains Mono", Size: 16}
	cfg.Fonts.Definitions = profile.ElementFont{Family: "Georgia", Size: 18}
	cfg.Fonts.PatternInput = profile.ElementFont{Family: "Consolas", Size: 15}
	cfg.Fonts.DictionariesList = profile.ElementFont{Family: "Inter", Size: 13}
	cfg.Fonts.StatusBar = profile.ElementFont{Family: "monospace", Size: 12}
	cfg.Fonts.Toolbar = profile.ElementFont{Family: "sans-serif", Size: 14}

	if err := profile.Save(cfgPath, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := profile.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.AtMacro != cfg.AtMacro || loaded.MinLength != 3 || loaded.MaxLength != 15 || loaded.SortMode != "smart" || loaded.LayoutMode != "tiled" || loaded.Theme != "light" || loaded.UIZoom != 125 {
		t.Fatalf("scalar round-trip mismatch: %+v", loaded)
	}
	if loaded.Fonts != cfg.Fonts {
		t.Fatalf("fonts round-trip mismatch: got %+v, want %+v", loaded.Fonts, cfg.Fonts)
	}
	if !slices.Equal(loaded.RecentQueries, cfg.RecentQueries) || len(loaded.Dictionaries) != 2 || len(loaded.WebProviders) != 1 {
		t.Fatalf("slice/table round-trip mismatch: %+v", loaded)
	}

	if _, err := os.Stat(filepath.Join(dictsDir, "English Wordnet 2025 Plus.tsd")); err == nil {
		// Default resolution returns Enabled dictionaries in priority order.
		defPaths, err := profile.ResolveDictionaryPaths(nil, loaded)
		if err != nil || len(defPaths) != 1 || filepath.Base(defPaths[0]) != "English Wordnet 2025 Plus.tsd" {
			t.Fatalf("default ResolveDictionaryPaths = %v, %v", defPaths, err)
		}

		// Legacy ';'-separated spec resolving profile alias + directory stem case-insensitively.
		resolved, err := profile.ResolveDictionaryPaths([]string{"wordnet"}, loaded)
		if err != nil || len(resolved) != 1 || filepath.Base(resolved[0]) != "English Wordnet 2025 Plus.tsd" {
			t.Fatalf("ResolveDictionaryPaths: %v, %v", resolved, err)
		}
	}
}

func TestExecutableAndSubdirectoryDiscovery(t *testing.T) {
	t.Setenv(profile.EnvDictPath, "")
	t.Setenv("BUILD_WORKING_DIRECTORY", "")

	exeRoot := t.TempDir()
	subDictsDir := filepath.Join(exeRoot, "dictionaries")
	if err := os.MkdirAll(subDictsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	topDict := filepath.Join(exeRoot, "top.tsd")
	subDict := filepath.Join(subDictsDir, "bundled.tsd")
	for _, p := range []string{topDict, subDict} {
		if err := os.WriteFile(p, []byte("TSD1"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	origExeFunc := profile.ExecutablePathFunc
	profile.ExecutablePathFunc = func() (string, error) {
		return filepath.Join(exeRoot, "tisane"), nil
	}
	defer func() {
		profile.ExecutablePathFunc = origExeFunc
	}()

	// Run from a separate empty working directory to ensure discovery uses the executable's directory.
	emptyCwd := t.TempDir()
	t.Chdir(emptyCwd)

	dirs := profile.DefaultSearchDirs(profile.DefaultConfig())
	discovered, err := profile.DiscoverDictionaries(dirs...)
	if err != nil {
		t.Fatalf("DiscoverDictionaries: %v", err)
	}
	if len(discovered) != 2 || discovered[0] != topDict || discovered[1] != subDict {
		t.Fatalf("DiscoverDictionaries = %v, want [%s %s]", discovered, topDict, subDict)
	}

	// Also verify no duplicates when CWD is the same as the executable directory.
	t.Chdir(exeRoot)
	discoveredSameDir, err := profile.DiscoverDictionaries(profile.DefaultSearchDirs(profile.DefaultConfig())...)
	if err != nil {
		t.Fatalf("DiscoverDictionaries (same dir): %v", err)
	}
	if len(discoveredSameDir) != 2 {
		t.Fatalf("expected 2 deduplicated dictionaries when CWD == exeDir, got %v", discoveredSameDir)
	}
}

func TestSidebarConfigRoundTripAndNormalization(t *testing.T) {
	cfg := profile.DefaultConfig()
	if cfg.SidebarWidth != profile.DefaultSidebarWidth || cfg.SidebarCollapsed {
		t.Fatalf("unexpected sidebar defaults: width=%d collapsed=%v", cfg.SidebarWidth, cfg.SidebarCollapsed)
	}

	cfg.SidebarWidth = 360
	cfg.SidebarCollapsed = true

	raw := profile.MarshalTOML(cfg)
	parsed, err := profile.ParseTOML(raw)
	if err != nil {
		t.Fatalf("ParseTOML: %v", err)
	}
	if parsed.SidebarWidth != 360 || !parsed.SidebarCollapsed {
		t.Fatalf("unexpected parsed sidebar config: width=%d collapsed=%v", parsed.SidebarWidth, parsed.SidebarCollapsed)
	}

	// Out-of-range widths are clamped to [MinSidebarWidth, MaxSidebarWidth].
	clampedLow, err := profile.ParseTOML([]byte("sidebar_width = 50\n"))
	if err != nil {
		t.Fatalf("ParseTOML low: %v", err)
	}
	if clampedLow.SidebarWidth != profile.MinSidebarWidth {
		t.Fatalf("expected clamped low width %d, got %d", profile.MinSidebarWidth, clampedLow.SidebarWidth)
	}

	clampedHigh, err := profile.ParseTOML([]byte("sidebar_width = 9999\n"))
	if err != nil {
		t.Fatalf("ParseTOML high: %v", err)
	}
	if clampedHigh.SidebarWidth != profile.MaxSidebarWidth {
		t.Fatalf("expected clamped high width %d, got %d", profile.MaxSidebarWidth, clampedHigh.SidebarWidth)
	}
}

func TestDefinitionPaneConfigRoundTripAndNormalization(t *testing.T) {
	cfg := profile.DefaultConfig()
	if cfg.DefinitionHeight != profile.DefaultDefinitionHeight || cfg.DefinitionCollapsed {
		t.Fatalf("unexpected definition pane defaults: height=%d collapsed=%v", cfg.DefinitionHeight, cfg.DefinitionCollapsed)
	}

	cfg.DefinitionHeight = 360
	cfg.DefinitionCollapsed = true

	raw := profile.MarshalTOML(cfg)
	parsed, err := profile.ParseTOML(raw)
	if err != nil {
		t.Fatalf("ParseTOML: %v", err)
	}
	if parsed.DefinitionHeight != 360 || !parsed.DefinitionCollapsed {
		t.Fatalf("unexpected parsed definition pane config: height=%d collapsed=%v", parsed.DefinitionHeight, parsed.DefinitionCollapsed)
	}

	// Out-of-range heights are clamped to [MinDefinitionHeight, MaxDefinitionHeight].
	clampedLow, err := profile.ParseTOML([]byte("definition_height = 40\n"))
	if err != nil {
		t.Fatalf("ParseTOML low: %v", err)
	}
	if clampedLow.DefinitionHeight != profile.MinDefinitionHeight {
		t.Fatalf("expected clamped low height %d, got %d", profile.MinDefinitionHeight, clampedLow.DefinitionHeight)
	}

	clampedHigh, err := profile.ParseTOML([]byte("definition_height = 9999\n"))
	if err != nil {
		t.Fatalf("ParseTOML high: %v", err)
	}
	if clampedHigh.DefinitionHeight != profile.MaxDefinitionHeight {
		t.Fatalf("expected clamped high height %d, got %d", profile.MaxDefinitionHeight, clampedHigh.DefinitionHeight)
	}
}

func TestSaveAtomicAndDoesNotClobberPredictableTmp(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sub", "config.toml")
	trapTarget := filepath.Join(dir, "victim.txt")
	if err := os.WriteFile(trapTarget, []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	// A pre-existing ".tmp" sibling must never be overwritten by Save.
	_ = os.Symlink(trapTarget, cfgPath+".tmp")

	cfg := profile.DefaultConfig()
	cfg.MinLength = 3
	if err := profile.Save(cfgPath, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	victimBytes, err := os.ReadFile(trapTarget)
	if err != nil {
		t.Fatal(err)
	}
	if string(victimBytes) != "untouched" {
		t.Fatalf("predictable .tmp symlink target was clobbered: %q", string(victimBytes))
	}

	loaded, err := profile.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.MinLength != 3 {
		t.Fatalf("MinLength = %d, want 3", loaded.MinLength)
	}
}

func TestMalformedTOMLHeadersBOMEscapesAndValidation(t *testing.T) {
	badInputs := []struct {
		name string
		toml string
	}{
		{name: "single_bracket_dictionaries", toml: "[dictionaries]\npath = \"a.tsd\"\n"},
		{name: "single_bracket_web_providers", toml: "[web_providers]\nid = \"x\"\n"},
		{name: "double_bracket_fonts", toml: "[[fonts]]\nresults_size = 14\n"},
		{name: "empty_table_header", toml: "[]\n"},
		{name: "empty_array_header", toml: "[[]]\n"},
		{name: "unterminated_array_quote", toml: "recent_queries = [\"abc]\n"},
	}
	for _, tc := range badInputs {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := profile.ParseTOML([]byte(tc.toml)); err == nil {
				t.Fatalf("expected ParseTOML error for %s", tc.name)
			}
		})
	}

	// Unsafe web_providers schemes are stripped on load without discarding the rest of config.toml.
	strippedWeb, err := profile.ParseTOML([]byte("min_length = 4\ndefault_web_provider = \"bad\"\n[[web_providers]]\nid = \"bad\"\nname = \"Bad\"\nurl_template = \"javascript:alert(1)\"\n"))
	if err != nil {
		t.Fatalf("ParseTOML with unsafe web_provider: %v", err)
	}
	if len(strippedWeb.WebProviders) != 0 || strippedWeb.MinLength != 4 || strippedWeb.DefaultWebProvider != "onelook" {
		t.Fatalf("unexpected strippedWeb config: %+v", strippedWeb)
	}

	// UTF-8 BOM + classic CR line endings + normalization of bounds, sort, layout, and filter.
	bomCR := "\xEF\xBB\xBFmin_length = 40\rmax_length = 5\rsort_mode = \"za\"\rlayout_mode = \"UNIFIED\"\rentry_filter = [\"upper\", \"invalid\", \"lower\"]\r"
	parsed, err := profile.ParseTOML([]byte(bomCR))
	if err != nil {
		t.Fatalf("ParseTOML BOM+CR: %v", err)
	}
	if parsed.MinLength != 5 || parsed.MaxLength != 40 {
		t.Fatalf("normalized lengths = (%d, %d), want (5, 40)", parsed.MinLength, parsed.MaxLength)
	}
	if parsed.SortMode != "alpha-desc" || parsed.LayoutMode != "unified" {
		t.Fatalf("normalized sort=%q layout=%q", parsed.SortMode, parsed.LayoutMode)
	}
	if !slices.Equal(parsed.EntryFilter, []string{"capitalized", "lowercase"}) {
		t.Fatalf("normalized entry_filter = %v", parsed.EntryFilter)
	}

	// Control characters serialize as standard TOML \uXXXX escapes (no Go \a, \v, or \xNN).
	ctrlCfg := profile.DefaultConfig()
	ctrlCfg.RecentQueries = []string{"bell:\a:vtab:\v:soh:\x01:del:\x7f"}
	marshaled := string(profile.MarshalTOML(ctrlCfg))
	if strings.Contains(marshaled, `\a`) || strings.Contains(marshaled, `\v`) || strings.Contains(marshaled, `\x`) {
		t.Fatalf("MarshalTOML emitted non-TOML Go escape sequence:\n%s", marshaled)
	}
	roundTrip, err := profile.ParseTOML([]byte(marshaled))
	if err != nil {
		t.Fatalf("round-trip ParseTOML with control escapes: %v", err)
	}
	if !slices.Equal(roundTrip.RecentQueries, ctrlCfg.RecentQueries) {
		t.Fatalf("control-char round-trip = %q, want %q", roundTrip.RecentQueries, ctrlCfg.RecentQueries)
	}
}

func TestResolveDictionaryIgnoresExecutableShadowingAndDottedStems(t *testing.T) {
	t.Setenv(profile.EnvDictPath, "")
	t.Setenv("BUILD_WORKING_DIRECTORY", "")

	exeRoot := t.TempDir()
	dictsSub := filepath.Join(exeRoot, "dictionaries")
	if err := os.MkdirAll(dictsSub, 0o755); err != nil {
		t.Fatal(err)
	}

	// Simulate "./tisane" executable in exeRoot alongside "dictionaries/tisane.tsd" and "WordNet-2.1.tsd".
	exeBin := filepath.Join(exeRoot, "tisane")
	tisaneDict := filepath.Join(dictsSub, "tisane.tsd")
	dottedDict := filepath.Join(dictsSub, "WordNet-2.1.tsd")
	if err := os.WriteFile(exeBin, []byte{0x7f, 'E', 'L', 'F'}, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{tisaneDict, dottedDict} {
		if err := os.WriteFile(p, []byte("TSD1"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	origExeFunc := profile.ExecutablePathFunc
	profile.ExecutablePathFunc = func() (string, error) { return exeBin, nil }
	defer func() { profile.ExecutablePathFunc = origExeFunc }()

	t.Chdir(exeRoot)

	resolved, err := profile.ResolveDictionaryPaths([]string{"tisane", "WordNet-2.1"}, profile.DefaultConfig())
	if err != nil {
		t.Fatalf("ResolveDictionaryPaths: %v", err)
	}
	if len(resolved) != 2 || resolved[0] != tisaneDict || resolved[1] != dottedDict {
		t.Fatalf("resolved = %v, want [%s %s]", resolved, tisaneDict, dottedDict)
	}
}

func TestRecordRecentQuery(t *testing.T) {
	cfg := profile.DefaultConfig()
	cfg.RecordRecentQuery("  ", 3)
	if len(cfg.RecentQueries) != 0 {
		t.Fatalf("blank query should be ignored, got %v", cfg.RecentQueries)
	}

	cfg.RecordRecentQuery("first", 3)
	cfg.RecordRecentQuery("second", 3)
	cfg.RecordRecentQuery("third", 3)
	cfg.RecordRecentQuery("first", 3) // move to front without growing past 3
	if !slices.Equal(cfg.RecentQueries, []string{"first", "third", "second"}) {
		t.Fatalf("RecentQueries = %v, want [first third second]", cfg.RecentQueries)
	}

	cfg.RecordRecentQuery("fourth", 3)
	if !slices.Equal(cfg.RecentQueries, []string{"fourth", "first", "third"}) {
		t.Fatalf("RecentQueries = %v, want [fourth first third]", cfg.RecentQueries)
	}
}

func TestNormalizeTheme(t *testing.T) {
	cases := map[string]string{
		"":        "dark",
		"dark":    "dark",
		" LIGHT ": "light",
		"system":  "system",
		"auto":    "system",
		"bogus":   "dark",
	}
	for in, want := range cases {
		if got := profile.NormalizeTheme(in); got != want {
			t.Errorf("NormalizeTheme(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeUIZoom(t *testing.T) {
	cases := map[int]int{
		0:   100,
		-20: 100,
		50:  75,
		100: 100,
		135: 135,
		250: 200,
	}
	for in, want := range cases {
		if got := profile.NormalizeUIZoom(in); got != want {
			t.Errorf("NormalizeUIZoom(%d) = %d, want %d", in, got, want)
		}
	}
}
