// Package app implements the bound Wails v3 backend services for the Tisane desktop application.
package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"tisane/internal/fsutil"
	"tisane/pkg/lookup"
	"tisane/pkg/profile"
	"tisane/pkg/tsd"
)

// EnvServerAllowHostIO opts headless "-tags server" deployments into allowing host
// filesystem mutations and server-side browser launches ("1" or "true").
const EnvServerAllowHostIO = "TISANE_SERVER_ALLOW_HOST_IO"

// ServerHostIOAllowedFromEnv reports whether TISANE_SERVER_ALLOW_HOST_IO explicitly enables host I/O.
func ServerHostIOAllowedFromEnv() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(EnvServerAllowHostIO)))
	return v == "1" || v == "true" || v == "yes"
}

// HostBridge abstracts OS/Wails runtime operations (events, browser URL launching, native file dialogs)
// so all desktop services can be unit-tested deterministically without a GUI display.
type HostBridge interface {
	EmitEvent(name string, data any)
	OpenURL(url string) error
	OpenDirectory(dir string) error
	PickDictionaryFiles() ([]string, error)
	PickDictionaryDirectory() (string, error)
	PickWordListFile() (string, error)
	PickSaveDictionaryFile(defaultDir, defaultName string) (string, error)
	PickSaveWordListFile(defaultDir, defaultName string) (string, error)
	PickAlphabetMapFile() (string, error)
}

// NoopBridge is a default safe HostBridge when running headless or before Wails startup.
type NoopBridge struct{}

func (NoopBridge) EmitEvent(string, any)                                 {}
func (NoopBridge) OpenURL(string) error                                  { return nil }
func (NoopBridge) OpenDirectory(string) error                            { return nil }
func (NoopBridge) PickDictionaryFiles() ([]string, error)                { return nil, nil }
func (NoopBridge) PickDictionaryDirectory() (string, error)              { return "", nil }
func (NoopBridge) PickWordListFile() (string, error)                     { return "", nil }
func (NoopBridge) PickSaveDictionaryFile(string, string) (string, error) { return "", nil }
func (NoopBridge) PickSaveWordListFile(string, string) (string, error)   { return "", nil }
func (NoopBridge) PickAlphabetMapFile() (string, error)                  { return "", nil }

// DictionaryInfo describes a configured .tsd dictionary for the Svelte 5 UI.
type DictionaryInfo struct {
	ID             string `json:"id"`
	Path           string `json:"path"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Version        string `json:"version"`
	FileSize       int64  `json:"fileSize"`
	WordCount      uint64 `json:"wordCount"`
	MinWordLength  int    `json:"minWordLength"`
	MaxWordLength  int    `json:"maxWordLength"`
	UsedLetters    int    `json:"usedLetters"`
	TotalLetters   int    `json:"totalLetters"`
	FTSWordCount   uint64 `json:"ftsWordCount"`
	HasDefinitions bool   `json:"hasDefinitions"`
	HasFTS         bool   `json:"hasFTS"`
	Enabled        bool   `json:"enabled"`
}

// loadedDict wraps an open *tsd.Dictionary with atomic reference counting.
// State.dicts owns a base reference of 1 while registered; acquire() must only be called
// while holding State.mu on a live State.dicts entry so 0 -> 1 resurrection cannot occur.
type loadedDict struct {
	info   DictionaryInfo
	dict   *tsd.Dictionary
	refs   atomic.Int32
	closed chan struct{}
}

func (ld *loadedDict) acquire() {
	ld.refs.Add(1)
}

func (ld *loadedDict) release() error {
	if ld.refs.Add(-1) == 0 {
		var err error
		if ld.dict != nil {
			err = ld.dict.Close()
		}
		if ld.closed != nil {
			close(ld.closed)
		}
		return err
	}
	return nil
}

// State manages shared configuration and open *tsd.Dictionary handles across all bound services.
type State struct {
	eventMu          sync.Mutex
	mu               sync.RWMutex
	cfg              *profile.Config
	cfgPath          string
	dicts            []*loadedDict
	unavailableDicts []profile.DictConfig
	bridge           HostBridge
	restrictHostIO   bool
}

// NewState initializes desktop application state from cfgPath (or DefaultConfigPath if empty),
// auto-discovering .tsd dictionaries only on genuine first run (missing or corrupt config.toml).
func NewState(cfgPath string, bridge HostBridge) (*State, error) {
	return NewStateWithOptions(cfgPath, bridge, false)
}

// NewStateWithOptions initializes desktop application state with an explicit initial host-I/O
// restriction policy (used by "-tags server" builds before any startup migrations execute).
func NewStateWithOptions(cfgPath string, bridge HostBridge, restrictHostIO bool) (*State, error) {
	if bridge == nil {
		bridge = NoopBridge{}
	}

	var (
		cfg        *profile.Config
		err        error
		isFirstRun bool
	)
	cfgPath = strings.TrimSpace(cfgPath)
	if cfgPath != "" {
		cfg, err = profile.Load(cfgPath)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) && !restrictHostIO {
				backupCorruptConfig(cfgPath)
			}
			cfg = profile.DefaultConfig()
			isFirstRun = true
		}
	} else {
		if p, pErr := profile.DefaultConfigPath(); pErr == nil {
			cfgPath = p
		}
		if cfgPath == "" {
			cfg = profile.DefaultConfig()
			isFirstRun = true
		} else {
			cfg, err = profile.Load(cfgPath)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) && !restrictHostIO {
					backupCorruptConfig(cfgPath)
				}
				cfg = profile.DefaultConfig()
				isFirstRun = true
			}
		}
	}

	s := &State{
		cfg:            cfg,
		cfgPath:        cfgPath,
		bridge:         bridge,
		restrictHostIO: restrictHostIO,
	}

	migEnv := profile.DefaultMigrationEnv(cfg, !restrictHostIO)
	if isFirstRun {
		// Treat on-disk directories as potentially upgraded from an older archive without a prior config.toml
		// so obsolete bundled files are cleaned up before first-run discovery scans search dirs.
		migEnv.FirstRun = true
		cfg.ConfigVersion = ""
		_, _ = profile.Migrate(cfg, migEnv, profile.Version)
		if !restrictHostIO {
			_ = s.runFirstRunLocked(true)
		} else {
			discovered, _ := profile.DiscoverDictionaries(profile.DefaultSearchDirs(s.cfg)...)
			for _, p := range discovered {
				_, _ = s.openAndAppendLocked(p, "", true)
			}
			s.syncConfigDictionariesLocked()
		}
	} else {
		migRes, _ := profile.Migrate(cfg, migEnv, profile.Version)
		if migRes.Changed() && !restrictHostIO {
			_ = s.saveConfigLocked()
		}
		for _, dc := range cfg.Dictionaries {
			if _, openErr := s.openAndAppendLocked(dc.Path, dc.Name, dc.Enabled); openErr != nil {
				s.unavailableDicts = append(s.unavailableDicts, dc)
			}
		}
	}

	return s, nil
}

func (s *State) runFirstRunLocked(resetDictionaries bool) error {
	if resetDictionaries {
		// First-run discovery across TISANE_DICT_PATH, the executable directory,
		// the current working directory, and the dictionaries subdirectory.
		discovered, _ := profile.DiscoverDictionaries(profile.DefaultSearchDirs(s.cfg)...)
		for _, p := range discovered {
			_, _ = s.openAndAppendLocked(p, "", true)
		}
	}
	s.syncConfigDictionariesLocked()
	return s.saveConfigLocked()
}

func (s *State) resetLocked(resetDictionaries bool) error {
	s.cfg = profile.DefaultConfig()
	if resetDictionaries {
		for _, ld := range s.dicts {
			_ = ld.release()
		}
		s.dicts = nil
		s.unavailableDicts = nil
	}
	return s.runFirstRunLocked(resetDictionaries)
}

func (s *State) emitDictsUpdated() []DictionaryInfo {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()

	s.mu.RLock()
	infos := s.listInfosLocked()
	bridge := s.bridge
	s.mu.RUnlock()

	bridge.EmitEvent(EventDictsUpdated, infos)
	return infos
}

// SetBridge attaches the live Wails HostBridge once the application instance is created.
func (s *State) SetBridge(bridge HostBridge) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if bridge != nil {
		s.bridge = bridge
	}
}

// Bridge returns the active HostBridge.
func (s *State) Bridge() HostBridge {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bridge
}

// SetRestrictHostIO configures whether host file-mutation and server-side browser launch RPCs
// are blocked (enabled by default in "-tags server" builds unless TISANE_SERVER_ALLOW_HOST_IO=1).
func (s *State) SetRestrictHostIO(restrict bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.restrictHostIO = restrict
}

func (s *State) checkHostIOAllowed(action string) error {
	s.mu.RLock()
	restricted := s.restrictHostIO
	s.mu.RUnlock()
	if restricted {
		return fmt.Errorf("%s is disabled in headless server mode (set %s=1 to allow)", action, EnvServerAllowHostIO)
	}
	return nil
}

func pathsReferToSameTarget(a, b string) bool {
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

// IsConfigPath reports whether target refers to the active config.toml file.
func (s *State) IsConfigPath(target string) bool {
	s.mu.RLock()
	cfgPath := s.cfgPath
	s.mu.RUnlock()
	return pathsReferToSameTarget(target, cfgPath)
}

// HasOpenDictionaryAt reports whether target refers to any currently registered .tsd dictionary.
func (s *State) HasOpenDictionaryAt(target string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, ld := range s.dicts {
		if pathsReferToSameTarget(target, ld.info.Path) {
			return true
		}
	}
	return false
}

// Close releases all registered .tsd file handles cleanly on shutdown.
func (s *State) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var firstErr error
	for _, ld := range s.dicts {
		if err := ld.release(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	s.dicts = nil
	return firstErr
}

func makeDictID(absPath string) string {
	return filepath.Clean(absPath)
}

func (s *State) openAndAppendLocked(path string, customName string, enabled bool) (*loadedDict, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = filepath.Clean(path)
	}
	id := makeDictID(absPath)
	info, statErr := os.Stat(absPath)
	for _, existing := range s.dicts {
		if existing.info.ID == id {
			return existing, nil
		}
		if statErr == nil {
			if exInfo, exErr := os.Stat(existing.info.Path); exErr == nil && os.SameFile(info, exInfo) {
				return existing, nil
			}
		}
	}

	d, err := tsd.Open(absPath)
	if err != nil {
		return nil, err
	}
	dispName := strings.TrimSpace(customName)
	if dispName == "" {
		dispName = d.Name()
	}

	ld := &loadedDict{
		dict:   d,
		closed: make(chan struct{}),
		info: DictionaryInfo{
			ID:             id,
			Path:           absPath,
			Name:           dispName,
			Description:    d.Description(),
			Version:        string(d.Version()),
			FileSize:       d.Size(),
			WordCount:      d.TotalWords(),
			MinWordLength:  d.MinWordLength(),
			MaxWordLength:  d.MaxWordLength(),
			UsedLetters:    d.NumUsedLetters(),
			TotalLetters:   d.NumTotalLetters(),
			FTSWordCount:   d.TotalFTSOnlyWords(),
			HasDefinitions: d.HasDefinitions(),
			HasFTS:         d.HasFTSIndex(),
			Enabled:        enabled,
		},
	}
	ld.refs.Store(1)
	s.dicts = append(s.dicts, ld)
	if len(s.unavailableDicts) > 0 {
		kept := s.unavailableDicts[:0]
		for _, u := range s.unavailableDicts {
			if !pathsReferToSameTarget(u.Path, absPath) {
				kept = append(kept, u)
			}
		}
		s.unavailableDicts = kept
	}
	return ld, nil
}

func (s *State) syncConfigDictionariesLocked() {
	s.cfg.Dictionaries = make([]profile.DictConfig, 0, len(s.dicts)+len(s.unavailableDicts))
	for _, ld := range s.dicts {
		s.cfg.Dictionaries = append(s.cfg.Dictionaries, profile.DictConfig{
			Path:    ld.info.Path,
			Name:    ld.info.Name,
			Enabled: ld.info.Enabled,
		})
	}
	for _, u := range s.unavailableDicts {
		alreadyLoaded := false
		for _, ld := range s.dicts {
			if pathsReferToSameTarget(u.Path, ld.info.Path) {
				alreadyLoaded = true
				break
			}
		}
		if !alreadyLoaded {
			s.cfg.Dictionaries = append(s.cfg.Dictionaries, u)
		}
	}
}

func (s *State) saveConfigLocked() error {
	if s.cfgPath == "" || s.restrictHostIO {
		return nil
	}
	return profile.Save(s.cfgPath, s.cfg)
}

func (s *State) listInfosLocked() []DictionaryInfo {
	out := make([]DictionaryInfo, len(s.dicts))
	for i, ld := range s.dicts {
		out[i] = ld.info
	}
	return out
}

func (s *State) cloneConfigLocked() profile.Config {
	cfgCopy := *s.cfg
	cfgCopy.DictDirs = append([]string{}, s.cfg.DictDirs...)
	cfgCopy.Dictionaries = append([]profile.DictConfig{}, s.cfg.Dictionaries...)
	cfgCopy.EntryFilter = append([]string{}, s.cfg.EntryFilter...)
	cfgCopy.WebProviders = append([]lookup.WebProvider{}, s.cfg.WebProviders...)
	cfgCopy.RecentQueries = append([]string{}, s.cfg.RecentQueries...)
	cfgCopy.SidebarWidth = profile.NormalizeSidebarWidth(s.cfg.SidebarWidth)
	cfgCopy.DefinitionHeight = profile.NormalizeDefinitionHeight(s.cfg.DefinitionHeight)
	cfgCopy.Theme = profile.NormalizeTheme(s.cfg.Theme)
	cfgCopy.UIZoom = profile.NormalizeUIZoom(s.cfg.UIZoom)
	cfgCopy.Fonts = profile.NormalizeFontConfig(s.cfg.Fonts)
	return cfgCopy
}

func makeReleaseFunc(held []*loadedDict) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			for _, ld := range held {
				_ = ld.release()
			}
		})
	}
}

// SnapshotSearchDicts returns ordered active search dictionaries, their DictionaryInfo metadata,
// all additional definition-bearing dictionaries in the profile, a deep-copied config, and a
// release callback that must be called once the caller finishes reading the returned handles.
func (s *State) SnapshotSearchDicts() (active []*tsd.Dictionary, activeInfos []DictionaryInfo, extraDef []*tsd.Dictionary, cfgCopy profile.Config, release func()) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	activeInfos = []DictionaryInfo{}
	held := make([]*loadedDict, 0, len(s.dicts))
	for _, ld := range s.dicts {
		if ld.info.Enabled {
			ld.acquire()
			held = append(held, ld)
			active = append(active, ld.dict)
			activeInfos = append(activeInfos, ld.info)
		} else if ld.info.HasDefinitions {
			ld.acquire()
			held = append(held, ld)
			extraDef = append(extraDef, ld.dict)
		}
	}
	return active, activeInfos, extraDef, s.cloneConfigLocked(), makeReleaseFunc(held)
}

// SnapshotAllDicts returns all loaded dictionaries (enabled first in priority order, then disabled),
// a deep-copied config, and a release callback that must be called when done reading the handles.
func (s *State) SnapshotAllDicts() ([]*tsd.Dictionary, profile.Config, func()) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*tsd.Dictionary, 0, len(s.dicts))
	held := make([]*loadedDict, 0, len(s.dicts))
	for _, ld := range s.dicts {
		if ld.info.Enabled {
			ld.acquire()
			held = append(held, ld)
			out = append(out, ld.dict)
		}
	}
	for _, ld := range s.dicts {
		if !ld.info.Enabled {
			ld.acquire()
			held = append(held, ld)
			out = append(out, ld.dict)
		}
	}
	return out, s.cloneConfigLocked(), makeReleaseFunc(held)
}

// AcquireDictionaryByID looks up dictionary id, increments its reference count, and returns
// the open *tsd.Dictionary handle, its DictionaryInfo metadata, and a release callback.
func (s *State) AcquireDictionaryByID(id string) (*tsd.Dictionary, DictionaryInfo, func(), error) {
	s.mu.RLock()
	idx := s.findIndexLocked(id)
	if idx < 0 {
		s.mu.RUnlock()
		return nil, DictionaryInfo{}, nil, fmt.Errorf("dictionary %q not found", id)
	}
	ld := s.dicts[idx]
	ld.acquire()
	dict := ld.dict
	info := ld.info
	s.mu.RUnlock()
	return dict, info, makeReleaseFunc([]*loadedDict{ld}), nil
}

// RecordRecentQuery prepends query to RecentQueries (deduplicated, max 50) and saves config.
func (s *State) RecordRecentQuery(query string) {
	query = strings.TrimSpace(query)
	if query == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.cfg.RecentQueries) > 0 && s.cfg.RecentQueries[0] == query {
		return
	}
	s.cfg.RecordRecentQuery(query, profile.DefaultMaxRecentQueries)
	_ = s.saveConfigLocked()
}

func backupCorruptConfig(cfgPath string) {
	if cfgPath == "" {
		return
	}
	if info, err := os.Stat(cfgPath); err == nil && info.Mode().IsRegular() {
		_ = os.Rename(cfgPath, cfgPath+".bak")
	}
}

func (s *State) findIndexLocked(id string) int {
	for i, ld := range s.dicts {
		if ld.info.ID == id {
			return i
		}
	}
	return -1
}

func (s *State) reorderLocked(orderedIDs []string) error {
	if len(orderedIDs) != len(s.dicts) {
		return fmt.Errorf("expected %d dictionary IDs, got %d", len(s.dicts), len(orderedIDs))
	}
	byID := make(map[string]*loadedDict, len(s.dicts))
	for _, ld := range s.dicts {
		byID[ld.info.ID] = ld
	}
	next := make([]*loadedDict, 0, len(s.dicts))
	seen := make(map[string]bool, len(s.dicts))
	for _, id := range orderedIDs {
		ld, ok := byID[id]
		if !ok || seen[id] {
			return fmt.Errorf("invalid or duplicate dictionary ID %q", id)
		}
		seen[id] = true
		next = append(next, ld)
	}
	s.dicts = next
	s.syncConfigDictionariesLocked()
	return s.saveConfigLocked()
}
