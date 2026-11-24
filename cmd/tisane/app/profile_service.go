package app

import (
	"fmt"
	"strings"

	"tisane/pkg/lookup"
	"tisane/pkg/profile"
	"tisane/pkg/search"
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
// All fields are validated against a cloned Config before mutating live State.cfg.
func (ps *ProfileService) UpdateConfig(patch ConfigPatch) (profile.Config, error) {
	var cleanedProviders []lookup.WebProvider
	if patch.WebProviders != nil {
		seenIDs := make(map[string]bool, len(patch.WebProviders))
		cleanedProviders = make([]lookup.WebProvider, 0, len(patch.WebProviders))
		for _, wp := range patch.WebProviders {
			id := strings.TrimSpace(wp.ID)
			name := strings.TrimSpace(wp.Name)
			if id == "" || name == "" {
				return ps.GetConfig(), fmt.Errorf("web provider ID and name cannot be empty")
			}
			lowerID := strings.ToLower(id)
			if seenIDs[lowerID] {
				return ps.GetConfig(), fmt.Errorf("duplicate web provider ID %q", id)
			}
			seenIDs[lowerID] = true
			if err := validateWebProviderURLTemplate(wp.URLTemplate); err != nil {
				return ps.GetConfig(), err
			}
			cleanedProviders = append(cleanedProviders, lookup.WebProvider{
				ID:          id,
				Name:        name,
				URLTemplate: strings.TrimSpace(wp.URLTemplate),
			})
		}
	}

	var normalizedSort string
	if patch.SortMode != nil {
		if _, err := search.ParseSortMode(*patch.SortMode); err != nil {
			return ps.GetConfig(), err
		}
		normalizedSort = profile.NormalizeSortMode(*patch.SortMode)
	}

	var normalizedLayout string
	if patch.LayoutMode != nil {
		lm := strings.ToLower(strings.TrimSpace(*patch.LayoutMode))
		if lm != "tiled" && lm != "unified" {
			return ps.GetConfig(), fmt.Errorf("unsupported layoutMode %q (expected tiled or unified)", *patch.LayoutMode)
		}
		normalizedLayout = lm
	}

	ps.state.mu.Lock()
	next := ps.state.cloneConfigLocked()

	if patch.AtMacro != nil {
		next.AtMacro = *patch.AtMacro
	}
	if patch.HashMacro != nil {
		next.HashMacro = *patch.HashMacro
	}
	nextMin := next.MinLength
	if patch.MinLength != nil {
		if *patch.MinLength < 1 || *patch.MinLength > 100 {
			ps.state.mu.Unlock()
			return ps.GetConfig(), fmt.Errorf("minLength must be between 1 and 100")
		}
		nextMin = *patch.MinLength
	}
	nextMax := next.MaxLength
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
	next.MinLength = nextMin
	next.MaxLength = nextMax
	if patch.ExclusiveVars != nil {
		next.ExclusiveVars = *patch.ExclusiveVars
	}
	if patch.IgnoreUnusedPunctuation != nil {
		next.IgnoreUnusedPunctuation = *patch.IgnoreUnusedPunctuation
	}
	if patch.ShowBalances != nil {
		next.ShowBalances = *patch.ShowBalances
	}
	if patch.ShowAnnotations != nil {
		next.ShowAnnotations = *patch.ShowAnnotations
	}
	if patch.SuppressDuplicates != nil {
		next.SuppressDuplicates = *patch.SuppressDuplicates
	}
	if patch.EntryFilter != nil {
		next.EntryFilter = profile.NormalizeEntryFilter(patch.EntryFilter)
	}
	if patch.SortMode != nil {
		next.SortMode = normalizedSort
	}
	if patch.LayoutMode != nil {
		next.LayoutMode = normalizedLayout
	}
	if patch.SidebarWidth != nil {
		next.SidebarWidth = profile.NormalizeSidebarWidth(*patch.SidebarWidth)
	}
	if patch.SidebarCollapsed != nil {
		next.SidebarCollapsed = *patch.SidebarCollapsed
	}
	if patch.DefinitionHeight != nil {
		next.DefinitionHeight = profile.NormalizeDefinitionHeight(*patch.DefinitionHeight)
	}
	if patch.DefinitionCollapsed != nil {
		next.DefinitionCollapsed = *patch.DefinitionCollapsed
	}
	if patch.Theme != nil {
		next.Theme = profile.NormalizeTheme(*patch.Theme)
	}
	if patch.UIZoom != nil {
		next.UIZoom = profile.NormalizeUIZoom(*patch.UIZoom)
	}
	if patch.AutoWebFallback != nil {
		next.AutoWebFallback = *patch.AutoWebFallback
	}
	if patch.DefaultWebProvider != nil {
		next.DefaultWebProvider = *patch.DefaultWebProvider
	}
	if patch.WebProviders != nil {
		next.WebProviders = cleanedProviders
	}
	if patch.Fonts != nil {
		next.Fonts = profile.NormalizeFontConfig(*patch.Fonts)
	}

	*ps.state.cfg = next
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
	ps.state.mu.Unlock()

	ps.state.emitDictsUpdated()
	return ps.GetConfig(), err
}
