package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tisane/pkg/tsd"
)

// BuildDictionaryRequest configures compiling a plain-text word/definition list
// into a .tsd dictionary from the Tisane GUI. It mirrors all build options of tisane-build.
type BuildDictionaryRequest struct {
	InputPath               string `json:"inputPath"`
	OutputPath              string `json:"outputPath"`
	MapPath                 string `json:"mapPath"`
	Description             string `json:"description"`
	MinLen                  int    `json:"minLen"`
	MaxLen                  int    `json:"maxLen"`
	MaxFTSPercent           int    `json:"maxFTSPercent"`
	SortAlphabetically      bool   `json:"sortAlphabetically"`
	TabSeparatedAnnotations bool   `json:"tabSeparatedAnnotations"`
	Encoding                string `json:"encoding"`
	AddToLibrary            bool   `json:"addToLibrary"`
}

// BuildDictionaryResult summarizes a completed dictionary build for the GUI.
type BuildDictionaryResult struct {
	OutputPath       string  `json:"outputPath"`
	InputLines       int     `json:"inputLines"`
	UniqueWords      int     `json:"uniqueWords"`
	MaxWordLength    int     `json:"maxWordLength"`
	Pass1Definitions int     `json:"pass1Definitions"`
	Pass2Definitions int     `json:"pass2Definitions"`
	FTSOnlyWords     int     `json:"ftsOnlyWords"`
	UsedLetters      int     `json:"usedLetters"`
	TotalLetters     int     `json:"totalLetters"`
	BytesWritten     int     `json:"bytesWritten"`
	ElapsedMs        float64 `json:"elapsedMs"`
	AddedToLibrary   bool    `json:"addedToLibrary"`
}

// BrowseWordListFile opens the native OS file picker for selecting a source word list file.
func (ts *ToolsService) BrowseWordListFile() (string, error) {
	return ts.state.Bridge().PickWordListFile()
}

// BrowseSaveDictionaryFile opens the native OS save dialog for choosing the output .tsd file path.
func (ts *ToolsService) BrowseSaveDictionaryFile(defaultPath string) (string, error) {
	defaultDir, defaultName := splitDefaultSaveTarget(defaultPath, "dictionary.tsd")
	chosen, err := ts.state.Bridge().PickSaveDictionaryFile(defaultDir, defaultName)
	if err != nil || strings.TrimSpace(chosen) == "" {
		return chosen, err
	}
	if filepath.Ext(chosen) == "" {
		chosen += ".tsd"
	}
	return chosen, nil
}

// BrowseAlphabetMapFile opens the native OS file picker for selecting an optional .tsm alphabet map file.
func (ts *ToolsService) BrowseAlphabetMapFile() (string, error) {
	return ts.state.Bridge().PickAlphabetMapFile()
}

// BuildDictionary compiles a word/definition list file into a .tsd dictionary using the
// shared pkg/tsd builder logic, optionally registering or reloading the output in Tisane.
func (ts *ToolsService) BuildDictionary(ctx context.Context, req BuildDictionaryRequest) (*BuildDictionaryResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	inputPath := strings.TrimSpace(req.InputPath)
	if inputPath == "" {
		return nil, fmt.Errorf("input word list file is required")
	}

	outputPath := strings.TrimSpace(req.OutputPath)
	if outputPath == "" {
		outputPath = tsd.DefaultOutputPath(inputPath)
	}
	if outputPath == "" {
		return nil, fmt.Errorf("output dictionary path is required")
	}

	absOut, err := filepath.Abs(outputPath)
	if err != nil {
		absOut = filepath.Clean(outputPath)
	}

	// If the target .tsd file is already open in Tisane, close its handle before overwriting
	// so OS file locks (especially on Windows) do not prevent writing and handles stay fresh.
	var (
		prevIdx     = -1
		prevName    string
		prevEnabled = true
	)
	ts.state.mu.Lock()
	outInfo, outStatErr := os.Stat(absOut)
	outID := makeDictID(absOut)
	for i, existing := range ts.state.dicts {
		match := existing.info.ID == outID
		if !match && outStatErr == nil {
			if exInfo, exErr := os.Stat(existing.info.Path); exErr == nil && os.SameFile(outInfo, exInfo) {
				match = true
			}
		}
		if match {
			prevIdx = i
			prevName = existing.info.Name
			prevEnabled = existing.info.Enabled
			_ = existing.release()
			ts.state.dicts = append(ts.state.dicts[:i], ts.state.dicts[i+1:]...)
			break
		}
	}
	ts.state.mu.Unlock()

	params := tsd.BuildParams{
		InputPath:               inputPath,
		OutputPath:              outputPath,
		MapPath:                 strings.TrimSpace(req.MapPath),
		Description:             req.Description,
		MinLen:                  req.MinLen,
		MaxLen:                  req.MaxLen,
		MaxFTSPercent:           req.MaxFTSPercent,
		SortAlphabetically:      req.SortAlphabetically,
		TabSeparatedAnnotations: req.TabSeparatedAnnotations,
		Encoding:                req.Encoding,
	}

	start := time.Now()
	writtenPath, stats, buildErr := tsd.BuildFileWithParams(params)
	elapsedMs := float64(time.Since(start).Microseconds()) / 1000.0

	if buildErr != nil {
		// Restore previously loaded dictionary handle if the file is still intact on disk.
		if prevIdx >= 0 {
			ts.state.mu.Lock()
			if ld, reopenErr := ts.state.openAndAppendLocked(absOut, prevName, prevEnabled); reopenErr == nil {
				if prevIdx < len(ts.state.dicts)-1 {
					// Move restored entry back to its original priority index.
					ts.state.dicts = ts.state.dicts[:len(ts.state.dicts)-1]
					ts.state.dicts = append(ts.state.dicts[:prevIdx], append([]*loadedDict{ld}, ts.state.dicts[prevIdx:]...)...)
				}
				ts.state.mu.Unlock()
			} else {
				ts.state.syncConfigDictionariesLocked()
				_ = ts.state.saveConfigLocked()
				infos := ts.state.listInfosLocked()
				bridge := ts.state.bridge
				ts.state.mu.Unlock()
				bridge.EmitEvent(EventDictsUpdated, infos)
			}
		}
		return nil, buildErr
	}

	addedToLibrary := false
	var saveErr error
	if prevIdx >= 0 || req.AddToLibrary {
		ts.state.mu.Lock()
		customName := ""
		enabled := true
		if prevIdx >= 0 {
			customName = prevName
			enabled = prevEnabled
		}
		ld, openErr := ts.state.openAndAppendLocked(writtenPath, customName, enabled)
		if openErr == nil {
			if prevIdx >= 0 && prevIdx < len(ts.state.dicts)-1 {
				// Restore original priority position.
				ts.state.dicts = ts.state.dicts[:len(ts.state.dicts)-1]
				ts.state.dicts = append(ts.state.dicts[:prevIdx], append([]*loadedDict{ld}, ts.state.dicts[prevIdx:]...)...)
			}
			ts.state.syncConfigDictionariesLocked()
			saveErr = ts.state.saveConfigLocked()
			addedToLibrary = true
		}
		infos := ts.state.listInfosLocked()
		bridge := ts.state.bridge
		ts.state.mu.Unlock()

		bridge.EmitEvent(EventDictsUpdated, infos)
	}

	res := &BuildDictionaryResult{
		OutputPath:       writtenPath,
		InputLines:       stats.InputLines,
		UniqueWords:      stats.UniqueWords,
		MaxWordLength:    stats.MaxWordLength,
		Pass1Definitions: stats.Pass1Definitions,
		Pass2Definitions: stats.Pass2Definitions,
		FTSOnlyWords:     stats.FTSOnlyWords,
		UsedLetters:      stats.UsedLetters,
		TotalLetters:     stats.TotalLetters,
		BytesWritten:     stats.BytesWritten,
		ElapsedMs:        elapsedMs,
		AddedToLibrary:   addedToLibrary,
	}
	if saveErr != nil {
		return res, fmt.Errorf("save config: %w", saveErr)
	}
	return res, nil
}
