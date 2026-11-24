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
func (ds *DictionaryService) BrowseWordListFile() (string, error) {
	return ds.state.Bridge().PickWordListFile()
}

// BrowseSaveDictionaryFile opens the native OS save dialog for choosing the output .tsd file path.
func (ds *DictionaryService) BrowseSaveDictionaryFile(defaultPath string) (string, error) {
	defaultDir := ""
	defaultName := "dictionary.tsd"
	if trimmed := strings.TrimSpace(defaultPath); trimmed != "" {
		dir := filepath.Dir(trimmed)
		if dir != "" && dir != "." {
			defaultDir = dir
		}
		base := filepath.Base(trimmed)
		if base != "" && base != "." && base != string(filepath.Separator) {
			defaultName = base
		}
	}
	chosen, err := ds.state.Bridge().PickSaveDictionaryFile(defaultDir, defaultName)
	if err != nil || strings.TrimSpace(chosen) == "" {
		return chosen, err
	}
	if filepath.Ext(chosen) == "" {
		chosen += ".tsd"
	}
	return chosen, nil
}

// BrowseAlphabetMapFile opens the native OS file picker for selecting an optional .tsm alphabet map file.
func (ds *DictionaryService) BrowseAlphabetMapFile() (string, error) {
	return ds.state.Bridge().PickAlphabetMapFile()
}

// BuildDictionary compiles a word/definition list file into a .tsd dictionary using the
// shared pkg/tsd builder logic, optionally registering or reloading the output in Tisane.
func (ds *DictionaryService) BuildDictionary(ctx context.Context, req BuildDictionaryRequest) (*BuildDictionaryResult, error) {
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
	ds.state.mu.Lock()
	outInfo, outStatErr := os.Stat(absOut)
	outID := makeDictID(absOut)
	for i, existing := range ds.state.dicts {
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
			ds.state.dicts = append(ds.state.dicts[:i], ds.state.dicts[i+1:]...)
			break
		}
	}
	ds.state.mu.Unlock()

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
			ds.state.mu.Lock()
			if ld, reopenErr := ds.state.openAndAppendLocked(absOut, prevName, prevEnabled); reopenErr == nil {
				if prevIdx < len(ds.state.dicts)-1 {
					// Move restored entry back to its original priority index.
					ds.state.dicts = ds.state.dicts[:len(ds.state.dicts)-1]
					ds.state.dicts = append(ds.state.dicts[:prevIdx], append([]*loadedDict{ld}, ds.state.dicts[prevIdx:]...)...)
				}
				ds.state.mu.Unlock()
			} else {
				ds.state.syncConfigDictionariesLocked()
				_ = ds.state.saveConfigLocked()
				infos := ds.state.listInfosLocked()
				bridge := ds.state.bridge
				ds.state.mu.Unlock()
				bridge.EmitEvent("dicts:updated", infos)
			}
		}
		return nil, buildErr
	}

	addedToLibrary := false
	var saveErr error
	if prevIdx >= 0 || req.AddToLibrary {
		ds.state.mu.Lock()
		customName := ""
		enabled := true
		if prevIdx >= 0 {
			customName = prevName
			enabled = prevEnabled
		}
		ld, openErr := ds.state.openAndAppendLocked(writtenPath, customName, enabled)
		if openErr == nil {
			if prevIdx >= 0 && prevIdx < len(ds.state.dicts)-1 {
				// Restore original priority position.
				ds.state.dicts = ds.state.dicts[:len(ds.state.dicts)-1]
				ds.state.dicts = append(ds.state.dicts[:prevIdx], append([]*loadedDict{ld}, ds.state.dicts[prevIdx:]...)...)
			}
			ds.state.syncConfigDictionariesLocked()
			saveErr = ds.state.saveConfigLocked()
			addedToLibrary = true
		}
		infos := ds.state.listInfosLocked()
		bridge := ds.state.bridge
		ds.state.mu.Unlock()

		bridge.EmitEvent("dicts:updated", infos)
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
