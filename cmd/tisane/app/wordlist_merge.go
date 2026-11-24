package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"tisane/pkg/wordlist"
)

// MergeWordListsRequest configures merging two plain-text word lists from the Tisane GUI.
type MergeWordListsRequest struct {
	PrimaryPath   string `json:"primaryPath"`
	SecondaryPath string `json:"secondaryPath"`
	OutputPath    string `json:"outputPath"`
	Operation     string `json:"operation"`
	Conversion    string `json:"conversion"`
	Encoding      string `json:"encoding"`
}

// MergeWordListsResult summarizes a completed word list merge for the GUI.
type MergeWordListsResult struct {
	OutputPath       string  `json:"outputPath"`
	Operation        string  `json:"operation"`
	Conversion       string  `json:"conversion"`
	PrimaryLines     int     `json:"primaryLines"`
	PrimaryEntries   int     `json:"primaryEntries"`
	SecondaryLines   int     `json:"secondaryLines"`
	SecondaryEntries int     `json:"secondaryEntries"`
	OutputEntries    int     `json:"outputEntries"`
	BytesWritten     int     `json:"bytesWritten"`
	ElapsedMs        float64 `json:"elapsedMs"`
}

// BrowseSaveWordListFile opens the native OS save dialog for choosing the output .txt word list path.
func (ts *ToolsService) BrowseSaveWordListFile(defaultPath string) (string, error) {
	defaultDir, defaultName := splitDefaultSaveTarget(defaultPath, "merged.txt")
	chosen, err := ts.state.Bridge().PickSaveWordListFile(defaultDir, defaultName)
	if err != nil || strings.TrimSpace(chosen) == "" {
		return chosen, err
	}
	if filepath.Ext(chosen) == "" {
		chosen += ".txt"
	}
	return chosen, nil
}

// MergeWordLists converts and merges two word lists using the shared pkg/wordlist logic.
func (ts *ToolsService) MergeWordLists(ctx context.Context, req MergeWordListsRequest) (*MergeWordListsResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	primaryPath := strings.TrimSpace(req.PrimaryPath)
	if primaryPath == "" {
		return nil, fmt.Errorf("primary word list file is required")
	}
	secondaryPath := strings.TrimSpace(req.SecondaryPath)
	if secondaryPath == "" {
		return nil, fmt.Errorf("secondary word list file is required")
	}
	outputPath := strings.TrimSpace(req.OutputPath)
	if outputPath == "" {
		return nil, fmt.Errorf("output word list file is required")
	}

	params := wordlist.MergeParams{
		PrimaryPath:   primaryPath,
		SecondaryPath: secondaryPath,
		OutputPath:    outputPath,
		Operation:     req.Operation,
		Conversion:    req.Conversion,
		Encoding:      req.Encoding,
	}

	opts, err := wordlist.ResolveMergeOptions(params)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	stats, err := wordlist.MergeFiles(primaryPath, secondaryPath, outputPath, opts)
	elapsedMs := float64(time.Since(start).Microseconds()) / 1000.0
	if err != nil {
		return nil, err
	}

	return &MergeWordListsResult{
		OutputPath:       outputPath,
		Operation:        string(opts.Operation),
		Conversion:       string(opts.Conversion),
		PrimaryLines:     stats.PrimaryLines,
		PrimaryEntries:   stats.PrimaryEntries,
		SecondaryLines:   stats.SecondaryLines,
		SecondaryEntries: stats.SecondaryEntries,
		OutputEntries:    stats.OutputEntries,
		BytesWritten:     stats.BytesWritten,
		ElapsedMs:        elapsedMs,
	}, nil
}
