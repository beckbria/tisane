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
	if err := ts.state.checkHostIOAllowed("browsing host files"); err != nil {
		return "", err
	}
	return ts.state.Bridge().PickWordListFile()
}

// BrowseSaveDictionaryFile opens the native OS save dialog for choosing the output .tsd file path.
func (ts *ToolsService) BrowseSaveDictionaryFile(defaultPath string) (string, error) {
	if err := ts.state.checkHostIOAllowed("browsing host save paths"); err != nil {
		return "", err
	}
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
	if err := ts.state.checkHostIOAllowed("browsing host files"); err != nil {
		return "", err
	}
	return ts.state.Bridge().PickAlphabetMapFile()
}

// BuildDictionary compiles a word/definition list file into a .tsd dictionary using the
// shared pkg/tsd builder logic, optionally registering or reloading the output in Tisane.
func (ts *ToolsService) BuildDictionary(ctx context.Context, req BuildDictionaryRequest) (*BuildDictionaryResult, error) {
	if err := ts.state.checkHostIOAllowed("building dictionaries on the server host"); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	inputPath := strings.TrimSpace(req.InputPath)
	if inputPath == "" {
		return nil, fmt.Errorf("input word list file is required")
	}
	inInfo, err := os.Stat(inputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect input word list %q: %w", inputPath, err)
	}
	if !inInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("input word list %q is not a regular file", inputPath)
	}
	if inInfo.Size() > tsd.MaxBuildInputBytes {
		return nil, fmt.Errorf("input word list %q exceeds maximum size (%d MiB)", inputPath, tsd.MaxBuildInputBytes>>20)
	}

	mapPath := strings.TrimSpace(req.MapPath)
	var mapInfo os.FileInfo
	if mapPath != "" && !strings.EqualFold(mapPath, "default") {
		mapInfo, err = os.Stat(mapPath)
		if err != nil {
			return nil, fmt.Errorf("failed to inspect alphabet map %q: %w", mapPath, err)
		}
		if !mapInfo.Mode().IsRegular() {
			return nil, fmt.Errorf("alphabet map %q is not a regular file", mapPath)
		}
	}

	outputPath := strings.TrimSpace(req.OutputPath)
	if outputPath == "" {
		outputPath = tsd.DefaultOutputPath(inputPath)
	}
	if outputPath == "" {
		return nil, fmt.Errorf("output dictionary path is required")
	}
	if ts.state.IsConfigPath(outputPath) {
		return nil, fmt.Errorf("output dictionary path %q cannot overwrite application configuration", outputPath)
	}

	absOut, err := filepath.Abs(outputPath)
	if err != nil {
		absOut = filepath.Clean(outputPath)
	}
	if filepath.Clean(outputPath) == filepath.Clean(inputPath) || (mapPath != "" && !strings.EqualFold(mapPath, "default") && filepath.Clean(outputPath) == filepath.Clean(mapPath)) {
		return nil, fmt.Errorf("output dictionary path %q cannot overwrite an input file", outputPath)
	}
	if preOutInfo, preErr := os.Stat(absOut); preErr == nil {
		if preOutInfo.IsDir() {
			return nil, fmt.Errorf("output dictionary path %q is a directory", outputPath)
		}
		if os.SameFile(preOutInfo, inInfo) || (mapInfo != nil && os.SameFile(preOutInfo, mapInfo)) {
			return nil, fmt.Errorf("output dictionary path %q refers to the same file as an input file", outputPath)
		}
	}

	// Compile into a temporary sibling file first so State.dicts and config.toml stay untouched
	// if compilation fails or is cancelled.
	outDir := filepath.Dir(absOut)
	if outDir == "" {
		outDir = "."
	}
	if outDir != "." {
		if mkErr := os.MkdirAll(outDir, 0o755); mkErr != nil {
			return nil, fmt.Errorf("create output directory %q: %w", outDir, mkErr)
		}
	}
	stageFile, err := os.CreateTemp(outDir, ".tisane-stage-*.tsd.tmp")
	if err != nil {
		return nil, fmt.Errorf("create staging dictionary in %q: %w", outDir, err)
	}
	stagePath := stageFile.Name()
	_ = stageFile.Close()
	keepStage := false
	defer func() {
		if !keepStage {
			_ = os.Remove(stagePath)
		}
	}()

	params := tsd.BuildParams{
		InputPath:               inputPath,
		OutputPath:              stagePath,
		MapPath:                 mapPath,
		Description:             req.Description,
		MinLen:                  req.MinLen,
		MaxLen:                  req.MaxLen,
		MaxFTSPercent:           req.MaxFTSPercent,
		SortAlphabetically:      req.SortAlphabetically,
		TabSeparatedAnnotations: req.TabSeparatedAnnotations,
		Encoding:                req.Encoding,
	}

	start := time.Now()
	_, stats, buildErr := tsd.BuildFileWithParamsContext(ctx, params)
	elapsedMs := float64(time.Since(start).Microseconds()) / 1000.0
	if buildErr != nil {
		return nil, buildErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Detach any existing loaded handle at absOut and wait for in-flight readers to release
	// before renaming over absOut (avoiding Windows ERROR_SHARING_VIOLATION).
	var (
		detached    *loadedDict
		prevIdx     = -1
		prevPath    string
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
			detached = existing
			prevIdx = i
			prevPath = existing.info.Path
			prevName = existing.info.Name
			prevEnabled = existing.info.Enabled
			_ = existing.release()
			ts.state.dicts = append(ts.state.dicts[:i], ts.state.dicts[i+1:]...)
			break
		}
	}
	ts.state.mu.Unlock()

	if detached != nil && detached.closed != nil {
		select {
		case <-detached.closed:
		case <-ctx.Done():
			// Restore previous handle if cancellation raced before rename.
			ts.state.mu.Lock()
			if ld, reopenErr := ts.state.openAndAppendLocked(prevPath, prevName, prevEnabled); reopenErr == nil {
				moveLoadedDictLocked(ts.state, ld, prevIdx)
			}
			ts.state.syncConfigDictionariesLocked()
			_ = ts.state.saveConfigLocked()
			ts.state.mu.Unlock()
			ts.state.emitDictsUpdated()
			return nil, ctx.Err()
		}
	}

	if renameErr := os.Rename(stagePath, outputPath); renameErr != nil {
		if prevIdx >= 0 {
			ts.state.mu.Lock()
			if ld, reopenErr := ts.state.openAndAppendLocked(prevPath, prevName, prevEnabled); reopenErr == nil {
				moveLoadedDictLocked(ts.state, ld, prevIdx)
			}
			ts.state.syncConfigDictionariesLocked()
			_ = ts.state.saveConfigLocked()
			ts.state.mu.Unlock()
			ts.state.emitDictsUpdated()
		}
		return nil, fmt.Errorf("replace dictionary %q: %w", outputPath, renameErr)
	}
	keepStage = true

	addedToLibrary := false
	var saveErr error
	var openErr error
	if prevIdx >= 0 || req.AddToLibrary {
		ts.state.mu.Lock()
		customName := ""
		enabled := true
		if prevIdx >= 0 {
			customName = prevName
			enabled = prevEnabled
		}
		var ld *loadedDict
		ld, openErr = ts.state.openAndAppendLocked(outputPath, customName, enabled)
		if openErr == nil {
			if prevIdx >= 0 {
				moveLoadedDictLocked(ts.state, ld, prevIdx)
			}
			addedToLibrary = true
		}
		ts.state.syncConfigDictionariesLocked()
		saveErr = ts.state.saveConfigLocked()
		ts.state.mu.Unlock()

		ts.state.emitDictsUpdated()
	}

	res := &BuildDictionaryResult{
		OutputPath:       outputPath,
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
	if openErr != nil {
		return res, fmt.Errorf("reload built dictionary %q: %w", outputPath, openErr)
	}
	if saveErr != nil {
		return res, fmt.Errorf("save config: %w", saveErr)
	}
	return res, nil
}

func moveLoadedDictLocked(s *State, ld *loadedDict, targetIdx int) {
	curIdx := -1
	for i, item := range s.dicts {
		if item == ld {
			curIdx = i
			break
		}
	}
	if curIdx < 0 {
		return
	}
	if targetIdx < 0 {
		targetIdx = 0
	} else if targetIdx >= len(s.dicts) {
		targetIdx = len(s.dicts) - 1
	}
	if curIdx == targetIdx {
		return
	}
	s.dicts = append(s.dicts[:curIdx], s.dicts[curIdx+1:]...)
	s.dicts = append(s.dicts[:targetIdx], append([]*loadedDict{ld}, s.dicts[targetIdx:]...)...)
}
