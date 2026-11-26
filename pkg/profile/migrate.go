package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"tisane/internal/fsutil"
)

// MigrationEnv supplies filesystem search directories and deletion permissions
// to versioned profile migrations so they remain deterministic and testable.
type MigrationEnv struct {
	SearchDirs      []string
	AllowFileDelete bool
}

// DefaultMigrationEnv constructs a MigrationEnv using DefaultSearchDirs(cfg).
func DefaultMigrationEnv(cfg *Config, allowFileDelete bool) MigrationEnv {
	return MigrationEnv{
		SearchDirs:      DefaultSearchDirs(cfg),
		AllowFileDelete: allowFileDelete,
	}
}

// MigrationStep defines one SemVer-gated local state / file upgrade action.
type MigrationStep struct {
	Version string
	ID      string
	Apply   func(cfg *Config, env MigrationEnv) (changed bool, err error)
}

// MigrationResult reports what Migrate changed.
type MigrationResult struct {
	StateChanged   bool
	VersionUpdated bool
	Applied        []string
}

// Changed reports whether either profile/filesystem state or ConfigVersion changed.
func (r MigrationResult) Changed() bool {
	return r.StateChanged || r.VersionUpdated
}

// RenameBundledDictionaryRule describes a bundled .tsd filename rename across versions.
type RenameBundledDictionaryRule struct {
	OldFilename string
	NewFilename string
}

var defaultMigrations = []MigrationStep{
	{
		Version: "0.7.2",
		ID:      "rename-english-wordnet-2025-plus",
		Apply: func(cfg *Config, env MigrationEnv) (bool, error) {
			return ApplyDictionaryRename(cfg, env, RenameBundledDictionaryRule{
				OldFilename: "english-wordnet-2025-plus.tsd",
				NewFilename: "English Wordnet 2025 Plus.tsd",
			})
		},
	},
}

// DefaultMigrations returns a copy of Tisane's registered release migrations.
func DefaultMigrations() []MigrationStep {
	out := make([]MigrationStep, len(defaultMigrations))
	copy(out, defaultMigrations)
	return out
}

// Migrate runs all DefaultMigrations whose Version is strictly greater than cfg.ConfigVersion
// and less than or equal to targetVersion (defaulting to profile.Version when empty).
func Migrate(cfg *Config, env MigrationEnv, targetVersion string) (MigrationResult, error) {
	return MigrateWithSteps(cfg, env, targetVersion, defaultMigrations)
}

// MigrateWithSteps executes the matching subset of steps in ascending SemVer order.
func MigrateWithSteps(cfg *Config, env MigrationEnv, targetVersion string, steps []MigrationStep) (MigrationResult, error) {
	var res MigrationResult
	if cfg == nil {
		return res, nil
	}

	targetVersion = strings.TrimSpace(targetVersion)
	if targetVersion == "" {
		targetVersion = Version
	}

	curVersion := strings.TrimSpace(cfg.ConfigVersion)
	if curVersion == targetVersion {
		return res, nil
	}

	if _, err := parseSemVer(targetVersion); err != nil {
		return res, fmt.Errorf("invalid target migration version %q: %w", targetVersion, err)
	}

	// Treat an unparseable hand-edited config_version as "0.0.0" rather than blocking startup,
	// while strictly respecting valid higher versions (downgrade safety).
	if _, err := parseSemVer(curVersion); err != nil {
		curVersion = ""
	}
	cmpTarget, err := CompareVersions(curVersion, targetVersion)
	if err != nil {
		return res, err
	}
	if cmpTarget >= 0 {
		return res, nil
	}

	sorted := make([]MigrationStep, len(steps))
	copy(sorted, steps)
	for i := range sorted {
		if _, err := parseSemVer(sorted[i].Version); err != nil {
			return res, fmt.Errorf("migration %q has invalid version %q: %w", sorted[i].ID, sorted[i].Version, err)
		}
	}
	slices.SortStableFunc(sorted, func(a, b MigrationStep) int {
		c, _ := CompareVersions(a.Version, b.Version)
		return c
	})

	for _, step := range sorted {
		afterCur, _ := CompareVersions(curVersion, step.Version)
		withinTarget, _ := CompareVersions(step.Version, targetVersion)
		if afterCur < 0 && withinTarget <= 0 {
			changed, err := step.Apply(cfg, env)
			if err != nil {
				return res, fmt.Errorf("migration %s (%s): %w", step.ID, step.Version, err)
			}
			res.Applied = append(res.Applied, step.ID)
			if changed {
				res.StateChanged = true
			}
		}
	}

	cfg.ConfigVersion = targetVersion
	res.VersionUpdated = true
	return res, nil
}

// ApplyDictionaryRename updates DictConfig references from rule.OldFilename to rule.NewFilename
// (preserving priority order, Enabled state, and custom display names), deduplicates resulting
// entries, and removes obsolete copies of OldFilename when NewFilename exists alongside it.
func ApplyDictionaryRename(cfg *Config, env MigrationEnv, rule RenameBundledDictionaryRule) (bool, error) {
	if cfg == nil {
		return false, nil
	}
	oldBase := strings.TrimSpace(rule.OldFilename)
	newBase := strings.TrimSpace(rule.NewFilename)
	if oldBase == "" || newBase == "" {
		return false, fmt.Errorf("dictionary rename rule requires non-empty old and new filenames")
	}
	oldStem := trimTSDExt(oldBase)
	newStem := trimTSDExt(newBase)

	changed := false

	// 1. Rewrite matching DictConfig entries in place, preserving Enabled and priority position.
	for i := range cfg.Dictionaries {
		dc := &cfg.Dictionaries[i]
		if dc.Path == "" {
			continue
		}
		base := filepath.Base(dc.Path)
		if !strings.EqualFold(base, oldBase) {
			continue
		}

		dir := filepath.Dir(dc.Path)
		siblingNew := resolveFilenameInDir(dir, newBase)
		repointed := false
		if siblingNew != "" {
			if cleanupObsoleteSibling(dc.Path, siblingNew, env.AllowFileDelete) {
				changed = true
			}
			if dc.Path != siblingNew {
				dc.Path = siblingNew
				changed = true
			}
			repointed = true
		} else if !fileExists(dc.Path) {
			if foundNew := findInSearchDirs(env.SearchDirs, newBase); foundNew != "" {
				dc.Path = foundNew
				changed = true
				repointed = true
			}
		}

		if repointed {
			trimmedName := strings.TrimSpace(dc.Name)
			if trimmedName == "" || strings.EqualFold(trimmedName, oldStem) || strings.EqualFold(trimmedName, oldBase) {
				if dc.Name != newStem {
					dc.Name = newStem
					changed = true
				}
			}
		}
	}

	// 2. Deduplicate cfg.Dictionaries if both old and new entries existed prior to migration.
	if len(cfg.Dictionaries) > 1 {
		deduped := make([]DictConfig, 0, len(cfg.Dictionaries))
		for _, dc := range cfg.Dictionaries {
			isDup := false
			for _, prev := range deduped {
				if dictPathsSameTarget(prev.Path, dc.Path) {
					isDup = true
					break
				}
			}
			if isDup {
				changed = true
				continue
			}
			deduped = append(deduped, dc)
		}
		cfg.Dictionaries = deduped
	}

	// 3. Sweep search directories for leftover OldFilename copies sitting beside NewFilename
	// (covers CLI-only users or first-run in-place archive upgrades without DictConfig entries).
	seenDirs := make(map[string]bool, len(env.SearchDirs))
	for _, rawDir := range env.SearchDirs {
		dir := strings.TrimSpace(rawDir)
		if dir == "" {
			continue
		}
		cleanDir := filepath.Clean(dir)
		if seenDirs[cleanDir] {
			continue
		}
		seenDirs[cleanDir] = true

		newPath := resolveFilenameInDir(dir, newBase)
		if newPath == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if strings.EqualFold(e.Name(), oldBase) && e.Name() != filepath.Base(newPath) {
				oldPath := filepath.Join(dir, e.Name())
				if cleanupObsoleteSibling(oldPath, newPath, env.AllowFileDelete) {
					changed = true
				}
			}
		}
	}

	return changed, nil
}

// resolveFilenameInDir returns the concrete path to targetBase inside dir if a matching regular file exists.
func resolveFilenameInDir(dir, targetBase string) string {
	exact := filepath.Join(dir, targetBase)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if fileExists(exact) {
			return exact
		}
		return ""
	}
	var ciMatch string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if e.Name() == targetBase {
			p := filepath.Join(dir, e.Name())
			if fileExists(p) {
				return p
			}
		} else if ciMatch == "" && strings.EqualFold(e.Name(), targetBase) {
			p := filepath.Join(dir, e.Name())
			if fileExists(p) {
				ciMatch = p
			}
		}
	}
	return ciMatch
}

func findInSearchDirs(dirs []string, targetBase string) string {
	for _, dir := range dirs {
		if p := resolveFilenameInDir(dir, targetBase); p != "" {
			return p
		}
	}
	return ""
}

// cleanupObsoleteSibling deletes oldPath only when allowDelete is true, oldPath is a regular file
// (not a symlink or directory), newPath exists as a regular file, and they are distinct inodes/files.
// If oldPath and newPath refer to the same underlying file (case-only rename on a case-insensitive FS),
// it renames oldPath to newPath rather than unlinking.
func cleanupObsoleteSibling(oldPath, newPath string, allowDelete bool) bool {
	if !allowDelete {
		return false
	}
	oldInfo, err := os.Lstat(oldPath)
	if err != nil || !oldInfo.Mode().IsRegular() {
		return false
	}
	newInfo, err := os.Stat(newPath)
	if err != nil || !newInfo.Mode().IsRegular() {
		return false
	}
	if os.SameFile(oldInfo, newInfo) {
		if filepath.Base(oldPath) != filepath.Base(newPath) {
			target := filepath.Join(filepath.Dir(oldPath), filepath.Base(newPath))
			if rErr := os.Rename(oldPath, target); rErr == nil {
				return true
			}
		}
		return false
	}
	if err := os.Remove(oldPath); err == nil {
		return true
	}
	return false
}

func dictPathsSameTarget(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if fsutil.SameFileOrClean(a, b) {
		return true
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	return errA == nil && errB == nil && filepath.Clean(absA) == filepath.Clean(absB)
}

type parsedSemVer struct {
	nums [4]uint64
	pre  []string
}

func parseSemVer(raw string) (parsedSemVer, error) {
	var out parsedSemVer
	s := strings.TrimSpace(raw)
	if s == "" {
		return out, nil
	}
	if s[0] == 'v' || s[0] == 'V' {
		s = s[1:]
		if s == "" {
			return out, fmt.Errorf("empty version %q", raw)
		}
	}
	// Strip build metadata (+...) per SemVer 2.0.
	if plus := strings.IndexByte(s, '+'); plus >= 0 {
		s = s[:plus]
	}
	var preStr string
	if dash := strings.IndexByte(s, '-'); dash >= 0 {
		preStr = s[dash+1:]
		if preStr == "" {
			return out, fmt.Errorf("empty pre-release suffix in %q", raw)
		}
		s = s[:dash]
	}
	if s == "" {
		return out, fmt.Errorf("missing numeric core in %q", raw)
	}
	parts := strings.Split(s, ".")
	if len(parts) > 4 {
		return out, fmt.Errorf("version %q has more than 4 numeric components", raw)
	}
	for i, p := range parts {
		if p == "" {
			return out, fmt.Errorf("empty numeric component in %q", raw)
		}
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return out, fmt.Errorf("invalid numeric component %q in %q: %w", p, raw, err)
		}
		out.nums[i] = n
	}
	if preStr != "" {
		preParts := strings.Split(preStr, ".")
		for _, id := range preParts {
			if id == "" {
				return out, fmt.Errorf("empty pre-release identifier in %q", raw)
			}
		}
		out.pre = preParts
	}
	return out, nil
}

// CompareVersions compares two SemVer strings (treating "" as "0.0.0").
// It returns -1 if a < b, 0 if a == b, and +1 if a > b.
func CompareVersions(a, b string) (int, error) {
	va, err := parseSemVer(a)
	if err != nil {
		return 0, err
	}
	vb, err := parseSemVer(b)
	if err != nil {
		return 0, err
	}
	for i := range va.nums {
		if va.nums[i] < vb.nums[i] {
			return -1, nil
		}
		if va.nums[i] > vb.nums[i] {
			return 1, nil
		}
	}
	// Release without pre-release sorts higher than pre-release of the same numeric core (0.7.2 > 0.7.2-rc.1).
	if len(va.pre) == 0 && len(vb.pre) == 0 {
		return 0, nil
	}
	if len(va.pre) == 0 {
		return 1, nil
	}
	if len(vb.pre) == 0 {
		return -1, nil
	}
	for i := 0; i < len(va.pre) && i < len(vb.pre); i++ {
		c := comparePreIdent(va.pre[i], vb.pre[i])
		if c != 0 {
			return c, nil
		}
	}
	if len(va.pre) < len(vb.pre) {
		return -1, nil
	}
	if len(va.pre) > len(vb.pre) {
		return 1, nil
	}
	return 0, nil
}

func comparePreIdent(a, b string) int {
	na, errA := strconv.ParseUint(a, 10, 64)
	nb, errB := strconv.ParseUint(b, 10, 64)
	if errA == nil && errB == nil {
		if na < nb {
			return -1
		}
		if na > nb {
			return 1
		}
		return 0
	}
	// Numeric identifiers always have lower precedence than non-numeric identifiers per SemVer 2.0.
	if errA == nil {
		return -1
	}
	if errB == nil {
		return 1
	}
	return strings.Compare(a, b)
}
