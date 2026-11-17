package app

import (
	"tisane/pkg/lookup"
	"tisane/pkg/profile"
)

// ConfigPatch allows partial or full updates to user preferences from the Settings dialog or toolbar.
type ConfigPatch struct {
	AtMacro                 *string              `json:"atMacro,omitempty"`
	HashMacro               *string              `json:"hashMacro,omitempty"`
	MinLength               *int                 `json:"minLength,omitempty"`
	MaxLength               *int                 `json:"maxLength,omitempty"`
	ExclusiveVars           *bool                `json:"exclusiveVars,omitempty"`
	IgnoreUnusedPunctuation *bool                `json:"ignoreUnusedPunctuation,omitempty"`
	ShowBalances            *bool                `json:"showBalances,omitempty"`
	ShowAnnotations         *bool                `json:"showAnnotations,omitempty"`
	SuppressDuplicates      *bool                `json:"suppressDuplicates,omitempty"`
	EntryFilter             []string             `json:"entryFilter,omitempty"`
	SortMode                *string              `json:"sortMode,omitempty"`
	LayoutMode              *string              `json:"layoutMode,omitempty"` // "tiled" or "unified"
	AutoWebFallback         *bool                `json:"autoWebFallback,omitempty"`
	DefaultWebProvider      *string              `json:"defaultWebProvider,omitempty"`
	WebProviders            []lookup.WebProvider `json:"webProviders,omitempty"`
}

// ProfileService exposes persistent user settings (~/.config/tisane/config.toml) to Wails v3.
type ProfileService struct {
	state *State
}

// NewProfileService creates a ProfileService backed by state.
func NewProfileService(state *State) *ProfileService {
	return &ProfileService{state: state}
}

// GetConfig returns a snapshot of the current user configuration.
func (ps *ProfileService) GetConfig() profile.Config {
	ps.state.mu.RLock()
	defer ps.state.mu.RUnlock()
	cfgCopy := *ps.state.cfg
	cfgCopy.DictDirs = append([]string{}, ps.state.cfg.DictDirs...)
	cfgCopy.Dictionaries = append([]profile.DictConfig{}, ps.state.cfg.Dictionaries...)
	cfgCopy.EntryFilter = append([]string{}, ps.state.cfg.EntryFilter...)
	cfgCopy.WebProviders = append([]lookup.WebProvider{}, ps.state.cfg.WebProviders...)
	cfgCopy.RecentQueries = append([]string{}, ps.state.cfg.RecentQueries...)
	return cfgCopy
}

// UpdateConfig applies non-nil fields from patch and persists config.toml atomically.
func (ps *ProfileService) UpdateConfig(patch ConfigPatch) (profile.Config, error) {
	ps.state.mu.Lock()
	cfg := ps.state.cfg
	if patch.AtMacro != nil {
		cfg.AtMacro = *patch.AtMacro
	}
	if patch.HashMacro != nil {
		cfg.HashMacro = *patch.HashMacro
	}
	if patch.MinLength != nil && *patch.MinLength >= 1 && *patch.MinLength <= 100 {
		cfg.MinLength = *patch.MinLength
	}
	if patch.MaxLength != nil && *patch.MaxLength >= 1 && *patch.MaxLength <= 100 {
		cfg.MaxLength = *patch.MaxLength
	}
	if patch.ExclusiveVars != nil {
		cfg.ExclusiveVars = *patch.ExclusiveVars
	}
	if patch.IgnoreUnusedPunctuation != nil {
		cfg.IgnoreUnusedPunctuation = *patch.IgnoreUnusedPunctuation
	}
	if patch.ShowBalances != nil {
		cfg.ShowBalances = *patch.ShowBalances
	}
	if patch.ShowAnnotations != nil {
		cfg.ShowAnnotations = *patch.ShowAnnotations
	}
	if patch.SuppressDuplicates != nil {
		cfg.SuppressDuplicates = *patch.SuppressDuplicates
	}
	if patch.EntryFilter != nil {
		cfg.EntryFilter = append([]string(nil), patch.EntryFilter...)
	}
	if patch.SortMode != nil {
		cfg.SortMode = *patch.SortMode
	}
	if patch.LayoutMode != nil && (*patch.LayoutMode == "tiled" || *patch.LayoutMode == "unified") {
		cfg.LayoutMode = *patch.LayoutMode
	}
	if patch.AutoWebFallback != nil {
		cfg.AutoWebFallback = *patch.AutoWebFallback
	}
	if patch.DefaultWebProvider != nil {
		cfg.DefaultWebProvider = *patch.DefaultWebProvider
	}
	if patch.WebProviders != nil {
		cfg.WebProviders = append([]lookup.WebProvider(nil), patch.WebProviders...)
	}

	err := ps.state.saveConfigLocked()
	ps.state.mu.Unlock()
	return ps.GetConfig(), err
}
