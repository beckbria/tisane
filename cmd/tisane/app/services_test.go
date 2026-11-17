package app_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"tisane/cmd/tisane/app"
	"tisane/pkg/search"
)

const repoBinDir = "../../../binary"

type recordedEvent struct {
	name string
	data any
}

type mockBridge struct {
	mu         sync.Mutex
	events     []recordedEvent
	openedURLs []string
	picked     []string
	pickedDir  string
	completeCh chan app.SearchCompleteEvent
}

func newMockBridge() *mockBridge {
	return &mockBridge{
		completeCh: make(chan app.SearchCompleteEvent, 8),
	}
}

func (m *mockBridge) EmitEvent(name string, data any) {
	m.mu.Lock()
	m.events = append(m.events, recordedEvent{name: name, data: data})
	m.mu.Unlock()
	if name == "search:complete" {
		if ev, ok := data.(app.SearchCompleteEvent); ok {
			m.completeCh <- ev
		}
	}
}

func (m *mockBridge) OpenURL(u string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.openedURLs = append(m.openedURLs, u)
	return nil
}

func (m *mockBridge) PickDictionaryFiles() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.picked, nil
}

func (m *mockBridge) PickDictionaryDirectory() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pickedDir, nil
}

func TestDesktopServicesEndToEnd(t *testing.T) {
	if _, err := os.Stat(repoBinDir); os.IsNotExist(err) {
		t.Skip("binary test corpus not present")
	}

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")
	bridge := newMockBridge()

	state, err := app.NewState(cfgPath, bridge)
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	defer state.Close()

	dictSvc := app.NewDictionaryService(state)
	searchSvc := app.NewSearchService(state)
	lookupSvc := app.NewLookupService(state)
	profSvc := app.NewProfileService(state)

	// Verify default layout mode is "tiled" (2+ separate tiled panes per dictionary).
	cfg := profSvc.GetConfig()
	if cfg.LayoutMode != "tiled" {
		t.Fatalf("expected default LayoutMode='tiled', got %q", cfg.LayoutMode)
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal(cfg): %v", err)
	}
	if strings.Contains(string(cfgJSON), `"recentQueries":null`) || strings.Contains(string(cfgJSON), `"dictDirs":null`) {
		t.Fatalf("expected non-null slice fields in JSON config, got %s", string(cfgJSON))
	}

	// Register Core English.tsd and The Devil's Dictionary.tsd via BrowseAndAddDictionaries.
	corePath := filepath.Join(repoBinDir, "Dictionaries", "Core English.tsd")
	devilPath := filepath.Join(repoBinDir, "Dictionaries", "The Devil's Dictionary.tsd")
	tsb2Path := filepath.Join(repoBinDir, "tsbuild2.tsd")

	// Remove any auto-discovered dicts first so test order is exact.
	for _, info := range dictSvc.ListDictionaries() {
		_, _ = dictSvc.RemoveDictionary(info.ID)
	}

	// Verify BrowseAndAddDirectory scans a directory and adds all .tsd files in it.
	bridge.pickedDir = filepath.Join(repoBinDir, "Dictionaries")
	dirInfos, err := dictSvc.BrowseAndAddDirectory()
	if err != nil || len(dirInfos) < 2 {
		t.Fatalf("BrowseAndAddDirectory: len=%d err=%v", len(dirInfos), err)
	}
	for _, info := range dirInfos {
		_, _ = dictSvc.RemoveDictionary(info.ID)
	}

	bridge.picked = []string{corePath, devilPath, tsb2Path}
	infos, err := dictSvc.BrowseAndAddDictionaries()
	if err != nil || len(infos) != 3 {
		t.Fatalf("BrowseAndAddDictionaries: len=%d err=%v", len(infos), err)
	}

	// Reorder so Devil's Dictionary precedes Core English, then swap back.
	reordered, err := dictSvc.ReorderDictionaries([]string{infos[1].ID, infos[0].ID, infos[2].ID})
	if err != nil || reordered[0].Name != "The Devil's Dictionary" {
		t.Fatalf("ReorderDictionaries failed: %+v, err=%v", reordered, err)
	}
	_, err = dictSvc.ReorderDictionaries([]string{infos[0].ID, infos[1].ID, infos[2].ID})
	if err != nil {
		t.Fatal(err)
	}

	// Live pattern validation
	vr := searchSvc.ValidatePattern(app.ValidateRequest{
		Query:                   ".....*;aadmort",
		Mode:                    "word",
		MinLength:               1,
		MaxLength:               100,
		IgnoreUnusedPunctuation: true,
	})
	if !vr.Valid || vr.MinMatchedLength != 5 || vr.MaxMatchedLength != 7 {
		t.Fatalf("ValidatePattern: %+v", vr)
	}

	// Streaming multi-dictionary search with Smart Balance sort
	sid, err := searchSvc.StartSearch(app.SearchRequest{
		Query:                   ".....*;aadmort",
		Mode:                    "word",
		MinLength:               1,
		MaxLength:               100,
		IgnoreUnusedPunctuation: true,
		ShowBalances:            true,
		ShowAnnotations:         true,
		SuppressDuplicates:      true,
		SortMode:                "smart",
	})
	if err != nil {
		t.Fatalf("StartSearch: %v", err)
	}

	select {
	case done := <-bridge.completeCh:
		if done.SearchID != sid || done.TotalMatches == 0 {
			t.Fatalf("unexpected SearchCompleteEvent: %+v", done)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for search:complete")
	}

	// Verify batch events arrived and top match is "matador".
	bridge.mu.Lock()
	var gotMatches []search.Match
	for _, ev := range bridge.events {
		if ev.name == "search:batch" {
			b := ev.data.(app.SearchBatchEvent)
			if b.SearchID == sid {
				gotMatches = append(gotMatches, b.Matches...)
			}
		}
	}
	bridge.mu.Unlock()
	if len(gotMatches) == 0 || gotMatches[0].Word != "matador" {
		t.Fatalf("unexpected streamed matches: %+v", gotMatches)
	}

	// Local RTF definition lookup + Web Search fallback + URL scheme guard
	defRes, err := lookupSvc.GetDefinitions(context.Background(), "Crossword Man", true)
	if err != nil || len(defRes.Entries) != 2 {
		t.Fatalf("GetDefinitions(Crossword Man): %+v, err=%v", defRes, err)
	}

	launchedURL, err := lookupSvc.OpenWebLookup("obelisk", "wiktionary")
	if err != nil || launchedURL != "https://en.wiktionary.org/wiki/obelisk" {
		t.Fatalf("OpenWebLookup: url=%q err=%v", launchedURL, err)
	}
	if err := lookupSvc.OpenExternalURL("javascript:alert(1)"); err == nil {
		t.Fatalf("expected unsafe javascript: scheme to be rejected")
	}

	// Toggle layout mode to "unified" and update custom fonts, verifying persistence
	unified := "unified"
	customFonts := cfg.Fonts
	customFonts.Results.Family = "Fira Code"
	customFonts.Results.Size = 17
	customFonts.Definitions.Family = "Georgia"
	customFonts.Definitions.Size = 18
	updatedCfg, err := profSvc.UpdateConfig(app.ConfigPatch{
		LayoutMode: &unified,
		Fonts:      &customFonts,
	})
	if err != nil || updatedCfg.LayoutMode != "unified" || updatedCfg.Fonts.Results.Family != "Fira Code" || updatedCfg.Fonts.Results.Size != 17 {
		t.Fatalf("UpdateConfig: %+v, err=%v", updatedCfg, err)
	}
}
