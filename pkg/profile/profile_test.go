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
