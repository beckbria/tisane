package profile

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"tisane/pkg/lookup"
	"tisane/pkg/tsd"
)

// MarshalTOML serializes Config into clean, human-editable TOML.
func MarshalTOML(cfg *Config) []byte {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	minLen, maxLen := cfg.MinLength, cfg.MaxLength
	if minLen == 0 && maxLen == 0 {
		minLen, maxLen = 1, tsd.MaxWordLengthLimit
	} else {
		minLen, maxLen = NormalizeLengthBounds(minLen, maxLen)
	}
	var sb strings.Builder
	sb.WriteString("# Tisane Configuration\n\n")
	fmt.Fprintf(&sb, "at_macro = %s\n", quoteTOMLString(cfg.AtMacro))
	fmt.Fprintf(&sb, "hash_macro = %s\n", quoteTOMLString(cfg.HashMacro))
	fmt.Fprintf(&sb, "min_length = %d\n", minLen)
	fmt.Fprintf(&sb, "max_length = %d\n", maxLen)
	fmt.Fprintf(&sb, "exclusive_vars = %t\n", cfg.ExclusiveVars)
	fmt.Fprintf(&sb, "ignore_unused_punctuation = %t\n", cfg.IgnoreUnusedPunctuation)
	fmt.Fprintf(&sb, "show_balances = %t\n", cfg.ShowBalances)
	fmt.Fprintf(&sb, "show_annotations = %t\n", cfg.ShowAnnotations)
	fmt.Fprintf(&sb, "suppress_duplicates = %t\n", cfg.SuppressDuplicates)
	fmt.Fprintf(&sb, "entry_filter = %s\n", formatStringArray(NormalizeEntryFilter(cfg.EntryFilter)))
	fmt.Fprintf(&sb, "sort_mode = %s\n", quoteTOMLString(NormalizeSortMode(cfg.SortMode)))
	fmt.Fprintf(&sb, "layout_mode = %s\n", quoteTOMLString(NormalizeLayoutMode(cfg.LayoutMode)))
	fmt.Fprintf(&sb, "sidebar_width = %d\n", NormalizeSidebarWidth(cfg.SidebarWidth))
	fmt.Fprintf(&sb, "sidebar_collapsed = %t\n", cfg.SidebarCollapsed)
	fmt.Fprintf(&sb, "definition_height = %d\n", NormalizeDefinitionHeight(cfg.DefinitionHeight))
	fmt.Fprintf(&sb, "definition_collapsed = %t\n", cfg.DefinitionCollapsed)
	fmt.Fprintf(&sb, "auto_web_fallback = %t\n", cfg.AutoWebFallback)
	fmt.Fprintf(&sb, "default_web_provider = %s\n", quoteTOMLString(cfg.DefaultWebProvider))
	fmt.Fprintf(&sb, "dict_dirs = %s\n", formatStringArray(cfg.DictDirs))
	fmt.Fprintf(&sb, "recent_queries = %s\n", formatStringArray(cfg.RecentQueries))

	fonts := NormalizeFontConfig(cfg.Fonts)
	sb.WriteString("\n[fonts]\n")
	fmt.Fprintf(&sb, "results_family = %s\n", quoteTOMLString(fonts.Results.Family))
	fmt.Fprintf(&sb, "results_size = %d\n", fonts.Results.Size)
	fmt.Fprintf(&sb, "definitions_family = %s\n", quoteTOMLString(fonts.Definitions.Family))
	fmt.Fprintf(&sb, "definitions_size = %d\n", fonts.Definitions.Size)
	fmt.Fprintf(&sb, "pattern_input_family = %s\n", quoteTOMLString(fonts.PatternInput.Family))
	fmt.Fprintf(&sb, "pattern_input_size = %d\n", fonts.PatternInput.Size)
	fmt.Fprintf(&sb, "dictionaries_list_family = %s\n", quoteTOMLString(fonts.DictionariesList.Family))
	fmt.Fprintf(&sb, "dictionaries_list_size = %d\n", fonts.DictionariesList.Size)
	fmt.Fprintf(&sb, "status_bar_family = %s\n", quoteTOMLString(fonts.StatusBar.Family))
	fmt.Fprintf(&sb, "status_bar_size = %d\n", fonts.StatusBar.Size)
	fmt.Fprintf(&sb, "toolbar_family = %s\n", quoteTOMLString(fonts.Toolbar.Family))
	fmt.Fprintf(&sb, "toolbar_size = %d\n", fonts.Toolbar.Size)

	for _, dc := range cfg.Dictionaries {
		sb.WriteString("\n[[dictionaries]]\n")
		fmt.Fprintf(&sb, "path = %s\n", quoteTOMLString(dc.Path))
		fmt.Fprintf(&sb, "name = %s\n", quoteTOMLString(dc.Name))
		fmt.Fprintf(&sb, "enabled = %t\n", dc.Enabled)
	}

	for _, wp := range cfg.WebProviders {
		if lookup.BuildWebURL(wp.URLTemplate, "test") == "" {
			continue
		}
		sb.WriteString("\n[[web_providers]]\n")
		fmt.Fprintf(&sb, "id = %s\n", quoteTOMLString(wp.ID))
		fmt.Fprintf(&sb, "name = %s\n", quoteTOMLString(wp.Name))
		fmt.Fprintf(&sb, "url_template = %s\n", quoteTOMLString(wp.URLTemplate))
	}

	return []byte(sb.String())
}

// quoteTOMLString formats s as a standard TOML v1.0 basic string without Go-only \a, \v, or \xNN escapes.
func quoteTOMLString(s string) string {
	var sb strings.Builder
	sb.Grow(len(s) + 2)
	sb.WriteByte('"')
	for i := 0; i < len(s); {
		r, width := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && width == 1 {
			sb.WriteString(`\uFFFD`)
			i++
			continue
		}
		i += width
		switch r {
		case '\b':
			sb.WriteString(`\b`)
		case '\t':
			sb.WriteString(`\t`)
		case '\n':
			sb.WriteString(`\n`)
		case '\f':
			sb.WriteString(`\f`)
		case '\r':
			sb.WriteString(`\r`)
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		default:
			if r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0x9F) {
				fmt.Fprintf(&sb, `\u%04X`, r)
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

func formatStringArray(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	parts := make([]string, len(items))
	for i, s := range items {
		parts[i] = quoteTOMLString(s)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// ParseTOML parses a Tisane TOML configuration file, layering values over DefaultConfig().
func ParseTOML(data []byte) (*Config, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	cfg := DefaultConfig()
	sawWebProviders := false
	var parsedWeb []lookup.WebProvider

	section := "" // "", "dictionaries", "web_providers", "fonts", or ignored custom table
	normalized := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(normalized, "\n")

	for lineIdx, rawLine := range lines {
		line := stripTOMLComment(rawLine)
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
			secName := strings.TrimSpace(line[2 : len(line)-2])
			if secName == "" || strings.ContainsAny(secName, "[]") {
				return nil, fmt.Errorf("config.toml:%d: invalid array-of-tables header %q", lineIdx+1, line)
			}
			switch secName {
			case "dictionaries":
				section = "dictionaries"
				cfg.Dictionaries = append(cfg.Dictionaries, DictConfig{Enabled: true})
			case "web_providers":
				section = "web_providers"
				sawWebProviders = true
				parsedWeb = append(parsedWeb, lookup.WebProvider{})
			case "fonts":
				return nil, fmt.Errorf("config.toml:%d: [fonts] must be a single-bracket table, not [[fonts]]", lineIdx+1)
			default:
				section = "ignored:" + secName
			}
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			secName := strings.TrimSpace(line[1 : len(line)-1])
			if secName == "" || strings.ContainsAny(secName, "[]") {
				return nil, fmt.Errorf("config.toml:%d: invalid table header %q", lineIdx+1, line)
			}
			switch secName {
			case "dictionaries", "web_providers":
				return nil, fmt.Errorf("config.toml:%d: [[%s]] must be an array-of-tables (double brackets)", lineIdx+1, secName)
			default:
				section = secName
			}
			continue
		}

		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			return nil, fmt.Errorf("config.toml:%d: expected key = value", lineIdx+1)
		}
		key := strings.TrimSpace(line[:eq])
		if key == "" {
			return nil, fmt.Errorf("config.toml:%d: empty key before '='", lineIdx+1)
		}
		val := strings.TrimSpace(line[eq+1:])

		switch section {
		case "":
			if err := applyTopKey(cfg, key, val, lineIdx+1); err != nil {
				return nil, err
			}
		case "dictionaries":
			if len(cfg.Dictionaries) == 0 {
				return nil, fmt.Errorf("config.toml:%d: [[dictionaries]] entry missing header", lineIdx+1)
			}
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
			if len(parsedWeb) == 0 {
				return nil, fmt.Errorf("config.toml:%d: [[web_providers]] entry missing header", lineIdx+1)
			}
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
		case "fonts":
			if err := applyFontKey(&cfg.Fonts, key, val, lineIdx+1); err != nil {
				return nil, err
			}
		}
	}

	cfg.MinLength, cfg.MaxLength = NormalizeLengthBounds(cfg.MinLength, cfg.MaxLength)
	cfg.SortMode = NormalizeSortMode(cfg.SortMode)
	cfg.LayoutMode = NormalizeLayoutMode(cfg.LayoutMode)
	cfg.EntryFilter = NormalizeEntryFilter(cfg.EntryFilter)
	cfg.SidebarWidth = NormalizeSidebarWidth(cfg.SidebarWidth)
	cfg.DefinitionHeight = NormalizeDefinitionHeight(cfg.DefinitionHeight)
	cfg.Fonts = NormalizeFontConfig(cfg.Fonts)
	if sawWebProviders {
		validWeb := make([]lookup.WebProvider, 0, len(parsedWeb))
		for _, wp := range parsedWeb {
			if lookup.BuildWebURL(wp.URLTemplate, "test") != "" {
				validWeb = append(validWeb, wp)
			}
		}
		cfg.WebProviders = validWeb
	}
	cfg.DefaultWebProvider = normalizeDefaultWebProvider(cfg.DefaultWebProvider, cfg.WebProviders)
	return cfg, nil
}

func normalizeDefaultWebProvider(defaultID string, providers []lookup.WebProvider) string {
	effective := providers
	if len(effective) == 0 {
		effective = lookup.DefaultWebProviders()
	}
	for _, wp := range effective {
		if strings.EqualFold(wp.ID, defaultID) && wp.ID != "" {
			return wp.ID
		}
	}
	if len(effective) > 0 {
		return effective[0].ID
	}
	return "onelook"
}

func applyFontKey(fc *FontConfig, key, val string, lineNum int) error {
	switch key {
	case "results_family":
		s, err := parseTOMLString(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		fc.Results.Family = s
	case "results_size":
		v, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		fc.Results.Size = v
	case "definitions_family":
		s, err := parseTOMLString(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		fc.Definitions.Family = s
	case "definitions_size":
		v, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		fc.Definitions.Size = v
	case "pattern_input_family":
		s, err := parseTOMLString(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		fc.PatternInput.Family = s
	case "pattern_input_size":
		v, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		fc.PatternInput.Size = v
	case "dictionaries_list_family":
		s, err := parseTOMLString(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		fc.DictionariesList.Family = s
	case "dictionaries_list_size":
		v, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		fc.DictionariesList.Size = v
	case "status_bar_family":
		s, err := parseTOMLString(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		fc.StatusBar.Family = s
	case "status_bar_size":
		v, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		fc.StatusBar.Size = v
	case "toolbar_family":
		s, err := parseTOMLString(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		fc.Toolbar.Family = s
	case "toolbar_size":
		v, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		fc.Toolbar.Size = v
	}
	return nil
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
	case "sidebar_width":
		v, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		cfg.SidebarWidth = NormalizeSidebarWidth(v)
	case "sidebar_collapsed":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		cfg.SidebarCollapsed = b
	case "definition_height":
		v, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		cfg.DefinitionHeight = NormalizeDefinitionHeight(v)
	case "definition_collapsed":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("config.toml:%d: %w", lineNum, err)
		}
		cfg.DefinitionCollapsed = b
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
		if i == len(inner) {
			if inDouble || inSingle || escaped {
				return nil, fmt.Errorf("unterminated string in array %q", val)
			}
		}
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
