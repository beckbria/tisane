// Package tisane exposes repository-wide metadata such as the release version.
package tisane

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var rawVersion string

// Version is the current release version of Tisane, read from the root VERSION file.
var Version = strings.TrimSpace(rawVersion)
