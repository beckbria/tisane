package app

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"unicode"

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
	dicts, cfg, release := ls.state.SnapshotAllDicts()
	defer release()
	return lookup.Lookup(ctx, word, dicts, lookup.Options{
		CaseInsensitiveFallback: true,
		IncludeMentions:         includeMentions,
		WebProviders:            cfg.WebProviders,
	})
}

// OpenWebLookup builds the external lookup URL for word using providerID (or the profile's default provider)
// and launches it in the user's system browser via Wails Browser.OpenURL.
func (ls *LookupService) OpenWebLookup(word string, providerID string) (string, error) {
	if err := ls.state.checkHostIOAllowed("opening external web lookups on the server host"); err != nil {
		return "", err
	}
	_, cfg, release := ls.state.SnapshotAllDicts()
	release()
	providers := cfg.WebProviders
	if len(providers) == 0 {
		providers = lookup.DefaultWebProviders()
	}
	explicitID := strings.TrimSpace(providerID)
	if explicitID == "" {
		providerID = cfg.DefaultWebProvider
	} else {
		providerID = explicitID
	}

	var chosen *lookup.WebProvider
	for i := range providers {
		if strings.EqualFold(providers[i].ID, providerID) {
			chosen = &providers[i]
			break
		}
	}
	if chosen == nil {
		if explicitID != "" {
			return "", fmt.Errorf("unknown or invalid web lookup provider %q", explicitID)
		}
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
	if err := ls.state.checkHostIOAllowed("opening external URLs on the server host"); err != nil {
		return err
	}
	validated, err := validateExternalURL(rawURL, true)
	if err != nil {
		return err
	}
	return ls.state.Bridge().OpenURL(validated)
}

func hasForbiddenURLCharacters(s string) bool {
	for _, r := range s {
		if r <= 0x20 || r == 0x7F || unicode.IsSpace(r) || unicode.IsControl(r) {
			return true
		}
		switch r {
		case '"', '\'', '\\', '<', '>', '`', '|', ';', '$':
			return true
		}
		if (r >= 0x200B && r <= 0x200F) ||
			(r >= 0x2028 && r <= 0x202F) ||
			(r >= 0x2060 && r <= 0x206F) ||
			r == 0xFEFF ||
			(r >= 0xFFF0 && r <= 0xFFFF) {
			return true
		}
	}
	return false
}

func validateExternalURL(rawURL string, allowMailto bool) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", fmt.Errorf("URL cannot be empty")
	}
	if hasForbiddenURLCharacters(trimmed) {
		return "", fmt.Errorf("URL contains forbidden whitespace, control, or quote characters")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("URL must not include userinfo credentials")
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		if parsed.Host == "" {
			return "", fmt.Errorf("URL must include a host")
		}
		out := parsed.String()
		if hasForbiddenURLCharacters(out) {
			return "", fmt.Errorf("URL contains forbidden characters")
		}
		return out, nil
	case "mailto":
		if allowMailto {
			if parsed.Host != "" || strings.TrimSpace(parsed.Opaque) == "" {
				return "", fmt.Errorf("mailto URL must specify a recipient without a host authority")
			}
			out := parsed.String()
			if hasForbiddenURLCharacters(out) {
				return "", fmt.Errorf("mailto URL contains forbidden characters")
			}
			return out, nil
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
	for _, probe := range []string{"test", "a:b"} {
		sample := lookup.BuildWebURL(trimmed, probe)
		if sample == "" {
			return fmt.Errorf("invalid web provider URL template %q: must use http/https with {entry} in path or query", tmpl)
		}
		if _, err := validateExternalURL(sample, false); err != nil {
			return fmt.Errorf("invalid web provider URL template %q: %w", tmpl, err)
		}
	}
	return nil
}
