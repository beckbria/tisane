package profile_test

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"tisane/pkg/profile"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b    string
		want    int
		wantErr bool
	}{
		{"", "", 0, false},
		{"", "0.0.0", 0, false},
		{"", "0.7.1", -1, false},
		{"0.7.1", "0.7.2", -1, false},
		{"v0.7.2", "0.7.2", 0, false},
		{"0.7.10", "0.7.2", 1, false},
		{"0.7.2-rc.1", "0.7.2", -1, false},
		{"0.7.2-rc.2", "0.7.2-rc.10", -1, false},
		{"0.7.2-alpha", "0.7.2-beta", -1, false},
		{"0.7.2+build.42", "0.7.2", 0, false},
		{"1.0.0", "0.99.99", 1, false},
		{"bogus", "0.7.2", 0, true},
		{"0.7.2-", "0.7.2", 0, true},
		{"1.2.3.4.5", "1.0.0", 0, true},
	}

	for _, tc := range cases {
		got, err := profile.CompareVersions(tc.a, tc.b)
		if tc.wantErr {
			if err == nil {
				t.Errorf("CompareVersions(%q, %q): expected error, got %d", tc.a, tc.b, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("CompareVersions(%q, %q) unexpected error: %v", tc.a, tc.b, err)
			continue
		}
		if got != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestTOMLConfigVersionSemantics(t *testing.T) {
	// Legacy config.toml files (<= 0.7.1) have no config_version key and must parse as "" (unmigrated).
	legacyTOML := []byte("at_macro = \"[aeiou]\"\nsort_mode = \"dict\"\n")
	legacyCfg, err := profile.ParseTOML(legacyTOML)
	if err != nil {
		t.Fatalf("ParseTOML(legacy): %v", err)
	}
	if legacyCfg.ConfigVersion != "" {
		t.Fatalf("expected legacy ConfigVersion == \"\", got %q", legacyCfg.ConfigVersion)
	}

	// Marshaling stamps profile.Version when ConfigVersion is empty, and round-trips explicitly.
	cfg := profile.DefaultConfig()
	cfg.ConfigVersion = "0.7.2"
	roundTripped, err := profile.ParseTOML(profile.MarshalTOML(cfg))
	if err != nil {
		t.Fatalf("ParseTOML(roundTrip): %v", err)
	}
	if roundTripped.ConfigVersion != "0.7.2" {
		t.Fatalf("expected round-tripped ConfigVersion = \"0.7.2\", got %q", roundTripped.ConfigVersion)
	}
}

func TestMigrateWordNetRenameDedupeAndPostInstallerHeal(t *testing.T) {
	t.Run("renames_in_place_preserving_order_enabled_and_deletes_old_file", func(t *testing.T) {
		dir := t.TempDir()
		customPath := filepath.Join(dir, "Custom.tsd")
		oldWordNet := filepath.Join(dir, "english-wordnet-2025-plus.tsd")
		newWordNet := filepath.Join(dir, "English Wordnet 2025 Plus.tsd")
		for _, p := range []string{customPath, oldWordNet, newWordNet} {
			if err := os.WriteFile(p, []byte("TSD1"), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		cfg := profile.DefaultConfig()
		cfg.ConfigVersion = "" // pre-0.7.2 config
		cfg.Dictionaries = []profile.DictConfig{
			{Path: customPath, Name: "Custom", Enabled: true},
			{Path: oldWordNet, Name: "english-wordnet-2025-plus", Enabled: false},
		}

		env := profile.MigrationEnv{SearchDirs: []string{dir}, AllowFileDelete: true}
		res, err := profile.Migrate(cfg, env, "0.7.2")
		if err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		if !res.StateChanged || !res.VersionUpdated || cfg.ConfigVersion != "0.7.2" {
			t.Fatalf("unexpected migration result: %+v, ConfigVersion=%q", res, cfg.ConfigVersion)
		}
		if len(cfg.Dictionaries) != 2 {
			t.Fatalf("expected 2 dictionaries, got %+v", cfg.Dictionaries)
		}
		if cfg.Dictionaries[0].Path != customPath {
			t.Fatalf("priority order changed: %+v", cfg.Dictionaries)
		}
		if cfg.Dictionaries[1].Path != newWordNet || cfg.Dictionaries[1].Name != "English Wordnet 2025 Plus" || cfg.Dictionaries[1].Enabled {
			t.Fatalf("migrated dictionary mismatch: %+v", cfg.Dictionaries[1])
		}
		if _, err := os.Stat(oldWordNet); !os.IsNotExist(err) {
			t.Fatalf("expected old file %q to be deleted, stat err = %v", oldWordNet, err)
		}
		if _, err := os.Stat(newWordNet); err != nil {
			t.Fatalf("expected new file %q to remain intact: %v", newWordNet, err)
		}

		// Subsequent startup at the same version is a fast-path no-op.
		res2, err := profile.Migrate(cfg, env, "0.7.2")
		if err != nil || res2.Changed() {
			t.Fatalf("expected idempotent fast-path no-op on second run, got %+v, %v", res2, err)
		}
	})

	t.Run("preserves_custom_name_and_deduplicates_dual_entries", func(t *testing.T) {
		dir := t.TempDir()
		oldWordNet := filepath.Join(dir, "english-wordnet-2025-plus.tsd")
		newWordNet := filepath.Join(dir, "English Wordnet 2025 Plus.tsd")
		for _, p := range []string{oldWordNet, newWordNet} {
			if err := os.WriteFile(p, []byte("TSD1"), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		cfg := profile.DefaultConfig()
		cfg.ConfigVersion = "0.7.0"
		cfg.Dictionaries = []profile.DictConfig{
			{Path: oldWordNet, Name: "My Custom WordNet", Enabled: false},
			{Path: newWordNet, Name: "English Wordnet 2025 Plus", Enabled: true},
		}

		res, err := profile.Migrate(cfg, profile.MigrationEnv{SearchDirs: []string{dir}, AllowFileDelete: true}, "0.7.5")
		if err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		if !res.StateChanged || cfg.ConfigVersion != "0.7.5" {
			t.Fatalf("expected StateChanged and ConfigVersion=0.7.5, got %+v (%q)", res, cfg.ConfigVersion)
		}
		if len(cfg.Dictionaries) != 1 {
			t.Fatalf("expected deduplicated dictionary slice of len 1, got %+v", cfg.Dictionaries)
		}
		if cfg.Dictionaries[0].Path != newWordNet || cfg.Dictionaries[0].Name != "My Custom WordNet" || cfg.Dictionaries[0].Enabled {
			t.Fatalf("expected custom name and Enabled=false preserved, got %+v", cfg.Dictionaries[0])
		}
	})

	t.Run("heals_path_after_installer_already_deleted_old_file", func(t *testing.T) {
		dir := t.TempDir()
		oldWordNet := filepath.Join(dir, "english-wordnet-2025-plus.tsd")
		newWordNet := filepath.Join(dir, "English Wordnet 2025 Plus.tsd")
		if err := os.WriteFile(newWordNet, []byte("TSD1"), 0o644); err != nil {
			t.Fatal(err)
		}

		cfg := profile.DefaultConfig()
		cfg.ConfigVersion = "0.7.1"
		cfg.Dictionaries = []profile.DictConfig{
			{Path: oldWordNet, Name: "english-wordnet-2025-plus", Enabled: true},
		}

		res, err := profile.Migrate(cfg, profile.MigrationEnv{SearchDirs: []string{dir}, AllowFileDelete: true}, "0.7.2")
		if err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		if !res.StateChanged || cfg.Dictionaries[0].Path != newWordNet || cfg.Dictionaries[0].Name != "English Wordnet 2025 Plus" {
			t.Fatalf("expected broken path healed to %q, got %+v", newWordNet, cfg.Dictionaries[0])
		}
	})
}

func TestMigrateSafetyInvariantsAndOrdering(t *testing.T) {
	t.Run("never_deletes_old_file_when_replacement_is_absent", func(t *testing.T) {
		dir := t.TempDir()
		oldWordNet := filepath.Join(dir, "english-wordnet-2025-plus.tsd")
		if err := os.WriteFile(oldWordNet, []byte("TSD1"), 0o644); err != nil {
			t.Fatal(err)
		}

		cfg := profile.DefaultConfig()
		cfg.ConfigVersion = ""
		cfg.Dictionaries = []profile.DictConfig{
			{Path: oldWordNet, Name: "english-wordnet-2025-plus", Enabled: true},
		}

		_, err := profile.Migrate(cfg, profile.MigrationEnv{SearchDirs: []string{dir}, AllowFileDelete: true}, "0.7.2")
		if err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		if _, err := os.Stat(oldWordNet); err != nil {
			t.Fatalf("old file was deleted despite missing replacement: %v", err)
		}
		if cfg.Dictionaries[0].Path != oldWordNet {
			t.Fatalf("expected Path preserved when replacement missing, got %q", cfg.Dictionaries[0].Path)
		}
	})

	t.Run("respects_allow_file_delete_false_and_readonly_dirs", func(t *testing.T) {
		dir := t.TempDir()
		oldWordNet := filepath.Join(dir, "english-wordnet-2025-plus.tsd")
		newWordNet := filepath.Join(dir, "English Wordnet 2025 Plus.tsd")
		for _, p := range []string{oldWordNet, newWordNet} {
			if err := os.WriteFile(p, []byte("TSD1"), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		// AllowFileDelete: false rewrites config in memory without touching disk files.
		cfg := profile.DefaultConfig()
		cfg.ConfigVersion = ""
		cfg.Dictionaries = []profile.DictConfig{{Path: oldWordNet, Name: "english-wordnet-2025-plus", Enabled: true}}
		if _, err := profile.Migrate(cfg, profile.MigrationEnv{SearchDirs: []string{dir}, AllowFileDelete: false}, "0.7.2"); err != nil {
			t.Fatalf("Migrate(AllowFileDelete=false): %v", err)
		}
		if cfg.Dictionaries[0].Path != newWordNet {
			t.Fatalf("expected config Path updated to %q, got %q", newWordNet, cfg.Dictionaries[0].Path)
		}
		if _, err := os.Stat(oldWordNet); err != nil {
			t.Fatalf("expected old file preserved when AllowFileDelete=false: %v", err)
		}

		// Read-only directory (simulating %ProgramFiles% or /usr/share) must not fail Migrate.
		if runtime.GOOS != "windows" && os.Geteuid() != 0 {
			if err := os.Chmod(dir, 0o555); err != nil {
				t.Fatal(err)
			}
			defer os.Chmod(dir, 0o755)

			cfgRO := profile.DefaultConfig()
			cfgRO.ConfigVersion = ""
			cfgRO.Dictionaries = []profile.DictConfig{{Path: oldWordNet, Name: "english-wordnet-2025-plus", Enabled: true}}
			if _, err := profile.Migrate(cfgRO, profile.MigrationEnv{SearchDirs: []string{dir}, AllowFileDelete: true}, "0.7.2"); err != nil {
				t.Fatalf("Migrate on read-only dir failed: %v", err)
			}
			if cfgRO.Dictionaries[0].Path != newWordNet || cfgRO.ConfigVersion != "0.7.2" {
				t.Fatalf("expected config migrated despite read-only dir: %+v", cfgRO)
			}
		}
	})

	t.Run("downgrade_safety_and_semver_step_ordering", func(t *testing.T) {
		var order []string
		steps := []profile.MigrationStep{
			{Version: "0.9.0", ID: "step-0.9.0", Apply: func(*profile.Config, profile.MigrationEnv) (bool, error) {
				order = append(order, "0.9.0")
				return true, nil
			}},
			{Version: "0.7.2", ID: "step-0.7.2", Apply: func(*profile.Config, profile.MigrationEnv) (bool, error) {
				order = append(order, "0.7.2")
				return true, nil
			}},
			{Version: "0.8.0", ID: "step-0.8.0", Apply: func(*profile.Config, profile.MigrationEnv) (bool, error) {
				order = append(order, "0.8.0")
				return true, nil
			}},
		}

		cfg := profile.DefaultConfig()
		cfg.ConfigVersion = "0.7.0"
		res, err := profile.MigrateWithSteps(cfg, profile.MigrationEnv{}, "0.8.5", steps)
		if err != nil {
			t.Fatalf("MigrateWithSteps: %v", err)
		}
		if !slices.Equal(order, []string{"0.7.2", "0.8.0"}) {
			t.Fatalf("expected steps [0.7.2 0.8.0], got %v (applied=%v)", order, res.Applied)
		}
		if cfg.ConfigVersion != "0.8.5" {
			t.Fatalf("expected ConfigVersion=0.8.5, got %q", cfg.ConfigVersion)
		}

		// Running an older binary (0.7.2) against a 0.8.5 config must skip steps and preserve 0.8.5.
		order = nil
		resDown, err := profile.MigrateWithSteps(cfg, profile.MigrationEnv{}, "0.7.2", steps)
		if err != nil {
			t.Fatalf("MigrateWithSteps(downgrade): %v", err)
		}
		if resDown.Changed() || len(order) != 0 || cfg.ConfigVersion != "0.8.5" {
			t.Fatalf("expected downgrade no-op preserving 0.8.5, got %+v, order=%v, ver=%q", resDown, order, cfg.ConfigVersion)
		}
	})
}

func TestMigrateAddBundledDictionaries075(t *testing.T) {
	writeDummy := func(t *testing.T, p string) {
		t.Helper()
		if err := os.WriteFile(p, []byte("TSD1"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("upgrade_from_pre_072_renames_wordnet_and_adds_new_bundled_dicts", func(t *testing.T) {
		dir := t.TempDir()
		oldWordNet := filepath.Join(dir, "english-wordnet-2025-plus.tsd")
		newWordNet := filepath.Join(dir, "English Wordnet 2025 Plus.tsd")
		corePath := filepath.Join(dir, "Core English.tsd")
		uneditedPath := filepath.Join(dir, "Unedited English.tsd")
		wikiPath := filepath.Join(dir, "Wikipedia English 2026.tsd")
		for _, p := range []string{oldWordNet, newWordNet, corePath, uneditedPath, wikiPath} {
			writeDummy(t, p)
		}

		cfg := profile.DefaultConfig()
		cfg.ConfigVersion = "0.7.1"
		cfg.Dictionaries = []profile.DictConfig{
			{Path: oldWordNet, Name: "english-wordnet-2025-plus", Enabled: true},
		}

		env := profile.MigrationEnv{SearchDirs: []string{dir}, AllowFileDelete: true}
		res, err := profile.Migrate(cfg, env, "0.7.5")
		if err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		if !res.StateChanged || !res.VersionUpdated || cfg.ConfigVersion != "0.7.5" {
			t.Fatalf("unexpected migration result: %+v, ConfigVersion=%q", res, cfg.ConfigVersion)
		}

		want := []profile.DictConfig{
			{Path: newWordNet, Name: "English Wordnet 2025 Plus", Enabled: true},
			{Path: corePath, Name: "Core English", Enabled: true},
			{Path: uneditedPath, Name: "Unedited English", Enabled: true},
			{Path: wikiPath, Name: "Wikipedia English 2026", Enabled: true},
		}
		if !slices.Equal(cfg.Dictionaries, want) {
			t.Fatalf("migrated dictionaries mismatch:\ngot:  %+v\nwant: %+v", cfg.Dictionaries, want)
		}

		// Subsequent startup at 0.7.5 is an idempotent no-op.
		res2, err := profile.Migrate(cfg, env, "0.7.5")
		if err != nil || res2.Changed() {
			t.Fatalf("expected idempotent no-op on second run, got %+v, %v", res2, err)
		}
	})

	t.Run("upgrade_from_072_deduplicates_existing_core_and_skips_missing_files", func(t *testing.T) {
		dir := t.TempDir()
		wordNetPath := filepath.Join(dir, "English Wordnet 2025 Plus.tsd")
		corePath := filepath.Join(dir, "Core English.tsd")
		uneditedPath := filepath.Join(dir, "Unedited English.tsd")
		// Intentionally omit Wikipedia English 2026.tsd to verify missing files are skipped.
		for _, p := range []string{wordNetPath, corePath, uneditedPath} {
			writeDummy(t, p)
		}

		cfg := profile.DefaultConfig()
		cfg.ConfigVersion = "0.7.2"
		cfg.Dictionaries = []profile.DictConfig{
			{Path: wordNetPath, Name: "English Wordnet 2025 Plus", Enabled: true},
			{Path: corePath, Name: "My Core", Enabled: false},
		}

		// Empty SearchDirs verifies fallback to existing cfg.Dictionaries parent directories.
		res, err := profile.Migrate(cfg, profile.MigrationEnv{}, "0.7.5")
		if err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		if !res.StateChanged || cfg.ConfigVersion != "0.7.5" {
			t.Fatalf("expected StateChanged and ConfigVersion=0.7.5, got %+v (%q)", res, cfg.ConfigVersion)
		}

		want := []profile.DictConfig{
			{Path: wordNetPath, Name: "English Wordnet 2025 Plus", Enabled: true},
			{Path: corePath, Name: "My Core", Enabled: false},
			{Path: uneditedPath, Name: "Unedited English", Enabled: true},
		}
		if !slices.Equal(cfg.Dictionaries, want) {
			t.Fatalf("migrated dictionaries mismatch:\ngot:  %+v\nwant: %+v", cfg.Dictionaries, want)
		}
	})

	t.Run("first_run_skips_adding_dicts_while_still_cleaning_obsolete_files", func(t *testing.T) {
		dir := t.TempDir()
		oldWordNet := filepath.Join(dir, "english-wordnet-2025-plus.tsd")
		newWordNet := filepath.Join(dir, "English Wordnet 2025 Plus.tsd")
		corePath := filepath.Join(dir, "Core English.tsd")
		for _, p := range []string{oldWordNet, newWordNet, corePath} {
			writeDummy(t, p)
		}

		cfg := profile.DefaultConfig()
		cfg.ConfigVersion = ""
		res, err := profile.Migrate(cfg, profile.MigrationEnv{
			SearchDirs:      []string{dir},
			AllowFileDelete: true,
			FirstRun:        true,
		}, "0.7.5")
		if err != nil {
			t.Fatalf("Migrate(FirstRun): %v", err)
		}
		if len(cfg.Dictionaries) != 0 {
			t.Fatalf("expected FirstRun to leave cfg.Dictionaries empty, got %+v", cfg.Dictionaries)
		}
		if !res.StateChanged {
			t.Fatalf("expected obsolete file cleanup to report StateChanged=true")
		}
		if _, err := os.Stat(oldWordNet); !os.IsNotExist(err) {
			t.Fatalf("expected obsolete %q deleted on FirstRun, stat err = %v", oldWordNet, err)
		}
	})
}

func TestMigrateAddEditedEnglish090(t *testing.T) {
	writeDummy := func(t *testing.T, p string) {
		t.Helper()
		if err := os.WriteFile(p, []byte("TSD1"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("upgrade_from_pre_090_adds_edited_english", func(t *testing.T) {
		dir := t.TempDir()
		corePath := filepath.Join(dir, "Core English.tsd")
		editedPath := filepath.Join(dir, "Edited English.tsd")
		writeDummy(t, corePath)
		writeDummy(t, editedPath)

		cfg := profile.DefaultConfig()
		cfg.ConfigVersion = "0.8.0"
		cfg.Dictionaries = []profile.DictConfig{
			{Path: corePath, Name: "Core English", Enabled: true},
		}

		// Empty SearchDirs also verifies fallback to existing cfg.Dictionaries parent directories.
		res, err := profile.Migrate(cfg, profile.MigrationEnv{}, "0.9.0")
		if err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		if !res.StateChanged || !res.VersionUpdated || cfg.ConfigVersion != "0.9.0" {
			t.Fatalf("unexpected migration result: %+v, ConfigVersion=%q", res, cfg.ConfigVersion)
		}

		want := []profile.DictConfig{
			{Path: corePath, Name: "Core English", Enabled: true},
			{Path: editedPath, Name: "Edited English", Enabled: true},
		}
		if !slices.Equal(cfg.Dictionaries, want) {
			t.Fatalf("migrated dictionaries mismatch:\ngot:  %+v\nwant: %+v", cfg.Dictionaries, want)
		}

		// Subsequent startup at 0.9.0 is an idempotent no-op.
		res2, err := profile.Migrate(cfg, profile.MigrationEnv{SearchDirs: []string{dir}}, "0.9.0")
		if err != nil || res2.Changed() {
			t.Fatalf("expected idempotent no-op on second run, got %+v, %v", res2, err)
		}
	})

	t.Run("preserves_existing_edited_english_customization", func(t *testing.T) {
		dir := t.TempDir()
		editedPath := filepath.Join(dir, "Edited English.tsd")
		writeDummy(t, editedPath)

		cfg := profile.DefaultConfig()
		cfg.ConfigVersion = "0.8.9"
		cfg.Dictionaries = []profile.DictConfig{
			{Path: editedPath, Name: "Custom Edited", Enabled: false},
		}

		res, err := profile.Migrate(cfg, profile.MigrationEnv{SearchDirs: []string{dir}}, "0.9.0")
		if err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		if res.StateChanged || !res.VersionUpdated || cfg.ConfigVersion != "0.9.0" {
			t.Fatalf("expected VersionUpdated only, got %+v (%q)", res, cfg.ConfigVersion)
		}
		want := []profile.DictConfig{
			{Path: editedPath, Name: "Custom Edited", Enabled: false},
		}
		if !slices.Equal(cfg.Dictionaries, want) {
			t.Fatalf("dictionaries changed unexpectedly:\ngot:  %+v\nwant: %+v", cfg.Dictionaries, want)
		}
	})

	t.Run("first_run_skips_adding_edited_english", func(t *testing.T) {
		dir := t.TempDir()
		writeDummy(t, filepath.Join(dir, "Edited English.tsd"))

		cfg := profile.DefaultConfig()
		cfg.ConfigVersion = ""
		res, err := profile.Migrate(cfg, profile.MigrationEnv{
			SearchDirs: []string{dir},
			FirstRun:   true,
		}, "0.9.0")
		if err != nil {
			t.Fatalf("Migrate(FirstRun): %v", err)
		}
		if res.StateChanged || len(cfg.Dictionaries) != 0 || cfg.ConfigVersion != "0.9.0" {
			t.Fatalf("expected FirstRun no-op on dictionaries, got %+v, dicts=%+v", res, cfg.Dictionaries)
		}
	})
}
