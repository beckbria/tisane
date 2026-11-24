package app

import (
	"path/filepath"
	"strings"
)

// ToolsService exposes dictionary compilation and word list merge utilities to Wails v3.
type ToolsService struct {
	state *State
}

// NewToolsService creates a ToolsService backed by state.
func NewToolsService(state *State) *ToolsService {
	return &ToolsService{state: state}
}

func splitDefaultSaveTarget(defaultPath, fallbackName string) (dir, name string) {
	name = fallbackName
	trimmed := strings.TrimSpace(defaultPath)
	if trimmed == "" {
		return "", name
	}
	if d := filepath.Dir(trimmed); d != "" && d != "." {
		dir = d
	}
	if b := filepath.Base(trimmed); b != "" && b != "." && b != string(filepath.Separator) {
		name = b
	}
	return dir, name
}
