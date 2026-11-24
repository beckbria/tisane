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

	"tisane/pkg/lookup"
	"tisane/pkg/profile"
	"tisane/pkg/tsd"
)

// HostBridge abstracts OS/Wails runtime operations (events, browser URL launching, native file dialogs)
// so all desktop services can be unit-tested deterministically without a GUI display.
type HostBridge interface {
	EmitEvent(name string, data any)
	OpenURL(url string) error
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
	WordCount      uint64 `json:"wordCount"`
	MaxWordLength  int    `json:"maxWordLength"`
	HasDefinitions bool   `json:"hasDefinitions"`
	HasFTS         bool   `json:"hasFTS"`
	Enabled        bool   `json:"enabled"`
}

type loadedDict struct {
	info DictionaryInfo
	dict *tsd.Dictionary
	refs atomic.Int32
}

func (ld *loadedDict) acquire() {
	ld.refs.Add(1)
}

func (ld *loadedDict) release() error {
	if ld.refs.Add(-1) == 0 && ld.dict != nil {
		return ld.dict.Close()
	}
	return nil
}

// State manages shared configuration and open *tsd.Dictionary handles across all bound services.
type State struct {
	mu      sync.RWMutex
	cfg     *profile.Config
	cfgPath string
	dicts   []*loadedDict
	bridge  HostBridge
}

// NewState initializes desktop application state from cfgPath (or DefaultConfigPath if empty),
// auto-discovering .tsd dictionaries on first run if none are configured yet.
func NewState(cfgPath string, bridge HostBridge) (*State, error) {
	if bridge == nil {
		bridge = NoopBridge{}
	}

	var (
		cfg *profile.Config
		err error
	)
	if cfgPath != "" {
		cfg, err = profile.Load(cfgPath)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				backupCorruptConfig(cfgPath)
			}
			cfg = profile.DefaultConfig()
		}
	} else {
		cfg, cfgPath, err = profile.LoadOrDefault()
		if err != nil {
			if cfgPath != "" && !errors.Is(err, os.ErrNotExist) {
				backupCorruptConfig(cfgPath)
			}
			cfg = profile.DefaultConfig()
		}
	}

	s := &State{
		cfg:     cfg,
		cfgPath: cfgPath,
		bridge:  bridge,
	}

	if len(cfg.Dictionaries) > 0 {
		for _, dc := range cfg.Dictionaries {
			_, _ = s.openAndAppendLocked(dc.Path, dc.Name, dc.Enabled)
		}
	}
	if len(s.dicts) == 0 {
		s.runFirstRunLocked(true)
	}

	return s, nil
}

func (s *State) runFirstRunLocked(resetDictionaries bool) {
	if resetDictionaries {
		// First-run discovery across TISANE_DICT_PATH, the executable directory,
		// the current working directory, and the dictionaries subdirectory.
		discovered, _ := profile.DiscoverDictionaries(profile.DefaultSearchDirs(s.cfg)...)
		for _, p := range discovered {
			_, _ = s.openAndAppendLocked(p, "", true)
		}
	}
	s.syncConfigDictionariesLocked()
	_ = s.saveConfigLocked()
}

func (s *State) resetLocked(resetDictionaries bool) error {
	s.cfg = profile.DefaultConfig()
	if resetDictionaries {
		for _, ld := range s.dicts {
			_ = ld.release()
		}
		s.dicts = nil
	}
	if s.cfgPath != "" {
		_ = os.Remove(s.cfgPath)
	}
	s.runFirstRunLocked(resetDictionaries)
	return s.saveConfigLocked()
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
		dict: d,
		info: DictionaryInfo{
			ID:             id,
			Path:           absPath,
			Name:           dispName,
			Description:    d.Description(),
			Version:        string(d.Version()),
			WordCount:      d.TotalWords(),
			MaxWordLength:  d.MaxWordLength(),
			HasDefinitions: d.HasDefinitions(),
			HasFTS:         d.HasFTSIndex(),
			Enabled:        enabled,
		},
	}
	ld.refs.Store(1)
	s.dicts = append(s.dicts, ld)
	return ld, nil
}

func (s *State) syncConfigDictionariesLocked() {
	s.cfg.Dictionaries = make([]profile.DictConfig, len(s.dicts))
	for i, ld := range s.dicts {
		s.cfg.Dictionaries[i] = profile.DictConfig{
			Path:    ld.info.Path,
			Name:    ld.info.Name,
			Enabled: ld.info.Enabled,
		}
	}
}

func (s *State) saveConfigLocked() error {
	if s.cfgPath == "" {
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

// RecordRecentQuery prepends query to RecentQueries (deduplicated, max 50) and saves config.
func (s *State) RecordRecentQuery(query string) {
	query = strings.TrimSpace(query)
	if query == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

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
