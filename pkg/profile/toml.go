package profile

import (
	"fmt"
	"strconv"
	"strings"

	"tisane/pkg/lookup"
)

// MarshalTOML serializes Config into clean, human-editable TOML.
func MarshalTOML(cfg *Config) []byte {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	var sb strings.Builder
	sb.WriteString("# Tisane Configuration\n\n")
	fmt.Fprintf(&sb, "at_macro = %s\n", strconv.Quote(cfg.AtMacro))
	fmt.Fprintf(&sb, "hash_macro = %s\n", strconv.Quote(cfg.HashMacro))
	fmt.Fprintf(&sb, "min_length = %d\n", cfg.MinLength)
	fmt.Fprintf(&sb, "max_length = %d\n", cfg.MaxLength)
	fmt.Fprintf(&sb, "exclusive_vars = %t\n", cfg.ExclusiveVars)
	fmt.Fprintf(&sb, "ignore_unused_punctuation = %t\n", cfg.IgnoreUnusedPunctuation)
	fmt.Fprintf(&sb, "show_balances = %t\n", cfg.ShowBalances)
	fmt.Fprintf(&sb, "show_annotations = %t\n", cfg.ShowAnnotations)
	fmt.Fprintf(&sb, "suppress_duplicates = %t\n", cfg.SuppressDuplicates)
	fmt.Fprintf(&sb, "entry_filter = %s\n", formatStringArray(cfg.EntryFilter))
	fmt.Fprintf(&sb, "sort_mode = %s\n", strconv.Quote(cfg.SortMode))
	fmt.Fprintf(&sb, "layout_mode = %s\n", strconv.Quote(cfg.LayoutMode))
	fmt.Fprintf(&sb, "auto_web_fallback = %t\n", cfg.AutoWebFallback)
	fmt.Fprintf(&sb, "default_web_provider = %s\n", strconv.Quote(cfg.DefaultWebProvider))
	fmt.Fprintf(&sb, "dict_dirs = %s\n", formatStringArray(cfg.DictDirs))
	fmt.Fprintf(&sb, "recent_queries = %s\n", formatStringArray(cfg.RecentQueries))

	for _, dc := range cfg.Dictionaries {
		sb.WriteString("\n[[dictionaries]]\n")
		fmt.Fprintf(&sb, "path = %s\n", strconv.Quote(dc.Path))
		fmt.Fprintf(&sb, "name = %s\n", strconv.Quote(dc.Name))
		fmt.Fprintf(&sb, "enabled = %t\n", dc.Enabled)
	}

	for _, wp := range cfg.WebProviders {
		sb.WriteString("\n[[web_providers]]\n")
		fmt.Fprintf(&sb, "id = %s\n", strconv.Quote(wp.ID))
		fmt.Fprintf(&sb, "name = %s\n", strconv.Quote(wp.Name))
		fmt.Fprintf(&sb, "url_template = %s\n", strconv.Quote(wp.URLTemplate))
	}

	return []byte(sb.String())
}

func formatStringArray(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	parts := make([]string, len(items))
	for i, s := range items {
		parts[i] = strconv.Quote(s)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// ParseTOML parses a Tisane TOML configuration file, layering values over DefaultConfig().
func ParseTOML(data []byte) (*Config, error) {
	cfg := DefaultConfig()
	sawWebProviders := false
	var parsedWeb []lookup.WebProvider

	section := "" // "", "dictionaries", "web_providers"
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")

	for lineIdx, rawLine := range lines {
		line := stripTOMLComment(rawLine)
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
			secName := strings.TrimSpace(line[2 : len(line)-2])
			switch secName {
			case "dictionaries":
				section = "dictionaries"
				cfg.Dictionaries = append(cfg.Dictionaries, DictConfig{Enabled: true})
			case "web_providers":
				section = "web_providers"
				sawWebProviders = true
				parsedWeb = append(parsedWeb, lookup.WebProvider{})
			default:
				section = secName
			}
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}

		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			return nil, fmt.Errorf("config.toml:%d: expected key = value", lineIdx+1)
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])

		switch section {
		case "":
			if err := applyTopKey(cfg, key, val, lineIdx+1); err != nil {
				return nil, err
			}
		case "dictionaries":
			dc := &cfg.Dictionaries[len(cfg.Dictionaries)-1]
			switch key {
			case "path":
				s, err := parseTOMLString(val)
				if err != nil {
					return nil, fmt.Errorf("config.toml:%d: %w", lineIdx+1, err)
				}
				dc.Path = s
			case "name":
				s, err := parseTOMLString(val)
				if err != nil {
					return nil, fmt.Errorf("config.toml:%d: %w", lineIdx+1, err)
				}
				dc.Name = s
			case "enabled":
				b, err := strconv.ParseBool(val)
				if err != nil {
					return nil, fmt.Errorf("config.toml:%d: %w", lineIdx+1, err)
				}
				dc.Enabled = b
			}
		case "web_providers":
			wp := &parsedWeb[len(parsedWeb)-1]
			switch key {
			case "id":
				s, err := parseTOMLString(val)
				if err != nil {
					return nil, fmt.Errorf("config.toml:%d: %w", lineIdx+1, err)
				}
				wp.ID = s
			case "name":
				s, err := parseTOMLString(val)
				if err != nil {
					return nil, fmt.Errorf("config.toml:%d: %w", lineIdx+1, err)
				}
				wp.Name = s
			case "url_template":
				s, err := parseTOMLString(val)
				if err != nil {
					return nil, fmt.Errorf("config.toml:%d: %w", lineIdx+1, err)
				}
				wp.URLTemplate = s
			}
		}
	}

	if sawWebProviders {
		cfg.WebProviders = parsedWeb
	}
	return cfg, nil
}

func applyTopKey(cfg *Config, key, val string, lineNum int) error {
	switch key {
	case "at_macro":
		s, err := parseTOMLString(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		cfg.AtMacro = s
	case "hash_macro":
		s, err := parseTOMLString(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		cfg.HashMacro = s
	case "min_length":
		v, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		cfg.MinLength = v
	case "max_length":
		v, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		cfg.MaxLength = v
	case "exclusive_vars":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		cfg.ExclusiveVars = b
	case "ignore_unused_punctuation":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		cfg.IgnoreUnusedPunctuation = b
	case "show_balances":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		cfg.ShowBalances = b
	case "show_annotations":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		cfg.ShowAnnotations = b
	case "suppress_duplicates":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		cfg.SuppressDuplicates = b
	case "entry_filter":
		arr, err := parseTOMLStringArray(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		cfg.EntryFilter = arr
	case "sort_mode":
		s, err := parseTOMLString(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		cfg.SortMode = s
	case "layout_mode":
		s, err := parseTOMLString(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		cfg.LayoutMode = s
	case "auto_web_fallback":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		cfg.AutoWebFallback = b
	case "default_web_provider":
		s, err := parseTOMLString(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		cfg.DefaultWebProvider = s
	case "dict_dirs":
		arr, err := parseTOMLStringArray(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		cfg.DictDirs = arr
	case "recent_queries":
		arr, err := parseTOMLStringArray(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		cfg.RecentQueries = arr
	}
	return nil
}

func stripTOMLComment(line string) string {
	inDouble := false
	inSingle := false
	escaped := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if escaped {
			escaped = false
			continue
		}
		if inDouble && c == '\\' {
			escaped = true
			continue
		}
		if c == '"' && !inSingle {
			inDouble = !inDouble
		} else if c == '\'' && !inDouble {
			inSingle = !inSingle
		} else if c == '#' && !inDouble && !inSingle {
			return line[:i]
		}
	}
	return line
}

func parseTOMLString(val string) (string, error) {
	val = strings.TrimSpace(val)
	if len(val) >= 2 && val[0] == '\'' && val[len(val)-1] == '\'' {
		return val[1 : len(val)-1], nil
	}
	return strconv.Unquote(val)
}

func parseTOMLStringArray(val string) ([]string, error) {
	val = strings.TrimSpace(val)
	if !strings.HasPrefix(val, "[") || !strings.HasSuffix(val, "]") {
		return nil, fmt.Errorf("expected array [...], got %q", val)
	}
	inner := strings.TrimSpace(val[1 : len(val)-1])
	if inner == "" {
		return []string{}, nil
	}

	out := []string{}
	curStart := 0
	inDouble := false
	inSingle := false
	escaped := false
	for i := 0; i <= len(inner); i++ {
		if i == len(inner) || (inner[i] == ',' && !inDouble && !inSingle) {
			tok := strings.TrimSpace(inner[curStart:i])
			if tok != "" {
				s, err := parseTOMLString(tok)
				if err != nil {
					return nil, err
				}
				out = append(out, s)
			}
			curStart = i + 1
			continue
		}
		c := inner[i]
		if escaped {
			escaped = false
			continue
		}
		if inDouble && c == '\\' {
			escaped = true
			continue
		}
		if c == '"' && !inSingle {
			inDouble = !inDouble
		} else if c == '\'' && !inDouble {
			inSingle = !inSingle
		}
	}
	return out, nil
}
