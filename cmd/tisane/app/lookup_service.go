package app

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"tisane/pkg/lookup"
)

// LookupService provides local .tsd RTF-to-HTML definition retrieval and external web lookup fallback.
type LookupService struct {
	state *State
}

// NewLookupService creates a LookupService backed by state.
func NewLookupService(state *State) *LookupService {
	return &LookupService{state: state}
}

// GetDefinitions queries all registered dictionaries (active first, then reference dictionaries)
// for local definitions of word and returns sanitized HTML cards + web lookup URLs.
func (ls *LookupService) GetDefinitions(ctx context.Context, word string, includeMentions bool) (*lookup.Result, error) {
	dicts, cfg := ls.state.SnapshotAllDicts()
	return lookup.Lookup(ctx, word, dicts, lookup.Options{
		CaseInsensitiveFallback: true,
		IncludeMentions:         includeMentions,
		WebProviders:            cfg.WebProviders,
	})
}

// OpenWebLookup builds the external lookup URL for word using providerID (or the profile's default provider)
// and launches it in the user's system browser via Wails Browser.OpenURL.
func (ls *LookupService) OpenWebLookup(word string, providerID string) (string, error) {
	_, cfg := ls.state.SnapshotAllDicts()
	providers := cfg.WebProviders
	if len(providers) == 0 {
		providers = lookup.DefaultWebProviders()
	}
	if providerID == "" {
		providerID = cfg.DefaultWebProvider
	}

	var chosen *lookup.WebProvider
	for i := range providers {
		if strings.EqualFold(providers[i].ID, providerID) {
			chosen = &providers[i]
			break
		}
	}
	if chosen == nil {
		chosen = &providers[0]
	}

	targetURL := lookup.BuildWebURL(chosen.URLTemplate, word)
	validated, err := validateExternalURL(targetURL, false)
	if err != nil {
		return "", fmt.Errorf("invalid web lookup URL for provider %q: %w", chosen.ID, err)
	}
	if err := ls.state.Bridge().OpenURL(validated); err != nil {
		return validated, err
	}
	return validated, nil
}

// OpenExternalURL safely validates that rawURL uses http, https, or mailto before opening it in the system browser.
func (ls *LookupService) OpenExternalURL(rawURL string) error {
	validated, err := validateExternalURL(rawURL, true)
	if err != nil {
		return err
	}
	return ls.state.Bridge().OpenURL(validated)
}

func validateExternalURL(rawURL string, allowMailto bool) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		if parsed.Host == "" {
			return "", fmt.Errorf("URL must include a host")
		}
		return parsed.String(), nil
	case "mailto":
		if allowMailto {
			return parsed.String(), nil
		}
	}
	if allowMailto {
		return "", fmt.Errorf("unsupported URL scheme %q (only http, https, and mailto are allowed)", parsed.Scheme)
	}
	return "", fmt.Errorf("unsupported URL scheme %q (only http and https are allowed)", parsed.Scheme)
}

func validateWebProviderURLTemplate(tmpl string) error {
	trimmed := strings.TrimSpace(tmpl)
	if trimmed == "" {
		return fmt.Errorf("web provider URL template cannot be empty")
	}
	sample := lookup.BuildWebURL(trimmed, "test")
	if _, err := validateExternalURL(sample, false); err != nil {
		return fmt.Errorf("invalid web provider URL template %q: %w", tmpl, err)
	}
	return nil
}
