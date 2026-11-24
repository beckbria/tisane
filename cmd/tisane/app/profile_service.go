package app

import (
	"fmt"

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
	SidebarWidth            *int                 `json:"sidebarWidth,omitempty"`
	SidebarCollapsed        *bool                `json:"sidebarCollapsed,omitempty"`
	DefinitionHeight        *int                 `json:"definitionHeight,omitempty"`
	DefinitionCollapsed     *bool                `json:"definitionCollapsed,omitempty"`
	Theme                   *string              `json:"theme,omitempty"`
	UIZoom                  *int                 `json:"uiZoom,omitempty"`
	AutoWebFallback         *bool                `json:"autoWebFallback,omitempty"`
	DefaultWebProvider      *string              `json:"defaultWebProvider,omitempty"`
	WebProviders            []lookup.WebProvider `json:"webProviders,omitempty"`
	Fonts                   *profile.FontConfig  `json:"fonts,omitempty"`
}

// ProfileService exposes persistent user settings (config.toml in the OS user config directory) to Wails v3.
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
	return ps.state.cloneConfigLocked()
}

// UpdateConfig applies non-nil fields from patch and persists config.toml atomically.
func (ps *ProfileService) UpdateConfig(patch ConfigPatch) (profile.Config, error) {
	if patch.WebProviders != nil {
		for _, wp := range patch.WebProviders {
			if err := validateWebProviderURLTemplate(wp.URLTemplate); err != nil {
				return ps.GetConfig(), err
			}
		}
	}

	ps.state.mu.Lock()
	cfg := ps.state.cfg
	if patch.AtMacro != nil {
		cfg.AtMacro = *patch.AtMacro
	}
	if patch.HashMacro != nil {
		cfg.HashMacro = *patch.HashMacro
	}
	nextMin := cfg.MinLength
	if patch.MinLength != nil {
		if *patch.MinLength < 1 || *patch.MinLength > 100 {
			ps.state.mu.Unlock()
			return ps.GetConfig(), fmt.Errorf("minLength must be between 1 and 100")
		}
		nextMin = *patch.MinLength
	}
	nextMax := cfg.MaxLength
	if patch.MaxLength != nil {
		if *patch.MaxLength < 1 || *patch.MaxLength > 100 {
			ps.state.mu.Unlock()
			return ps.GetConfig(), fmt.Errorf("maxLength must be between 1 and 100")
		}
		nextMax = *patch.MaxLength
	}
	if nextMin > nextMax {
		ps.state.mu.Unlock()
		return ps.GetConfig(), fmt.Errorf("minLength (%d) cannot exceed maxLength (%d)", nextMin, nextMax)
	}
	cfg.MinLength = nextMin
	cfg.MaxLength = nextMax
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
	if patch.SidebarWidth != nil {
		cfg.SidebarWidth = profile.NormalizeSidebarWidth(*patch.SidebarWidth)
	}
	if patch.SidebarCollapsed != nil {
		cfg.SidebarCollapsed = *patch.SidebarCollapsed
	}
	if patch.DefinitionHeight != nil {
		cfg.DefinitionHeight = profile.NormalizeDefinitionHeight(*patch.DefinitionHeight)
	}
	if patch.DefinitionCollapsed != nil {
		cfg.DefinitionCollapsed = *patch.DefinitionCollapsed
	}
	if patch.Theme != nil {
		cfg.Theme = profile.NormalizeTheme(*patch.Theme)
	}
	if patch.UIZoom != nil {
		cfg.UIZoom = profile.NormalizeUIZoom(*patch.UIZoom)
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
	if patch.Fonts != nil {
		cfg.Fonts = profile.NormalizeFontConfig(*patch.Fonts)
	}

	err := ps.state.saveConfigLocked()
	ps.state.mu.Unlock()
	return ps.GetConfig(), err
}

// ResetConfig clears all persisted user settings back to defaults and triggers first-run behavior.
// When resetDictionaries is true, all currently loaded dictionaries are closed and cleared before
// re-discovering default dictionaries from the executable directory and dictionaries subdirectory.
func (ps *ProfileService) ResetConfig(resetDictionaries bool) (profile.Config, error) {
	if err := ps.state.checkHostIOAllowed("resetting server configuration"); err != nil {
		return ps.GetConfig(), err
	}
	ps.state.mu.Lock()
	err := ps.state.resetLocked(resetDictionaries)
	infos := ps.state.listInfosLocked()
	bridge := ps.state.bridge
	ps.state.mu.Unlock()

	bridge.EmitEvent(EventDictsUpdated, infos)
	return ps.GetConfig(), err
}
