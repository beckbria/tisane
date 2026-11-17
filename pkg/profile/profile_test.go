package profile_test

import (
	"os"
	"path/filepath"
	"slices"
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

	binDir, err := filepath.Abs(filepath.Join("..", "..", "binary"))
	if err != nil {
		t.Fatal(err)
	}
	dictsDir := filepath.Join(binDir, "Dictionaries")

	cfg.AtMacro = "[aeiouy]"
	cfg.MinLength = 3
	cfg.MaxLength = 15
	cfg.SortMode = "smart"
	cfg.DictDirs = []string{binDir, dictsDir}
	cfg.Dictionaries = []profile.DictConfig{
		{Path: filepath.Join(dictsDir, "Core English.tsd"), Name: "Core", Enabled: true},
		{Path: filepath.Join(binDir, "tsbuild2.tsd"), Name: "Example 2", Enabled: false},
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
	if loaded.AtMacro != cfg.AtMacro || loaded.MinLength != 3 || loaded.MaxLength != 15 || loaded.SortMode != "smart" || loaded.LayoutMode != "tiled" {
		t.Fatalf("scalar round-trip mismatch: %+v", loaded)
	}
	if loaded.Fonts != cfg.Fonts {
		t.Fatalf("fonts round-trip mismatch: got %+v, want %+v", loaded.Fonts, cfg.Fonts)
	}
	if !slices.Equal(loaded.RecentQueries, cfg.RecentQueries) || len(loaded.Dictionaries) != 2 || len(loaded.WebProviders) != 1 {
		t.Fatalf("slice/table round-trip mismatch: %+v", loaded)
	}

	if _, err := os.Stat(dictsDir); err == nil {
		// Default resolution returns Enabled dictionaries in priority order.
		defPaths, err := profile.ResolveDictionaryPaths(nil, loaded)
		if err != nil || len(defPaths) != 1 || filepath.Base(defPaths[0]) != "Core English.tsd" {
			t.Fatalf("default ResolveDictionaryPaths = %v, %v", defPaths, err)
		}

		// Legacy ';'-separated spec resolving profile alias + directory stem case-insensitively.
		resolved, err := profile.ResolveDictionaryPaths([]string{"example 2;the devil's dictionary"}, loaded)
		if err != nil || len(resolved) != 2 {
			t.Fatalf("ResolveDictionaryPaths(';'): %v, %v", resolved, err)
		}
	}
}

func TestExecutableAndSubdirectoryDiscovery(t *testing.T) {
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
