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
	"tisane/pkg/lookup"
	"tisane/pkg/profile"
	"tisane/pkg/search"
	"tisane/pkg/tsd"
)

type recordedEvent struct {
	name string
	data any
}

type mockBridge struct {
	mu              sync.Mutex
	events          []recordedEvent
	openedURLs      []string
	picked          []string
	pickedDir       string
	pickedWord      string
	pickedSave      string
	pickedSaveWord  string
	pickedMap       string
	saveDefDir      string
	saveDefName     string
	saveWordDefDir  string
	saveWordDefName string
	completeCh      chan app.SearchCompleteEvent
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

func (m *mockBridge) PickWordListFile() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pickedWord, nil
}

func (m *mockBridge) PickSaveDictionaryFile(defaultDir, defaultName string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveDefDir = defaultDir
	m.saveDefName = defaultName
	return m.pickedSave, nil
}

func (m *mockBridge) PickSaveWordListFile(defaultDir, defaultName string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveWordDefDir = defaultDir
	m.saveWordDefName = defaultName
	return m.pickedSaveWord, nil
}

func (m *mockBridge) PickAlphabetMapFile() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pickedMap, nil
}

func TestDesktopServicesEndToEnd(t *testing.T) {
	t.Setenv(profile.EnvDictPath, "")
	t.Setenv("BUILD_WORKING_DIRECTORY", "")

	tmpDir := t.TempDir()
	dictsSub := filepath.Join(tmpDir, "Dictionaries")
	if err := os.MkdirAll(dictsSub, 0o755); err != nil {
		t.Fatal(err)
	}

	corePath := filepath.Join(dictsSub, "Core English.tsd")
	devilPath := filepath.Join(dictsSub, "The Devil's Dictionary.tsd")
	tsb2Path := filepath.Join(tmpDir, "tsbuild2.tsd")

	coreBytes, _, err := tsd.BuildBytes([]byte("aorta\ndrama\nmatador\n"), tsd.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(corePath, coreBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	devilBytes, _, err := tsd.BuildBytes([]byte("matador|{\\rtf1 A bullfighter.}\ncynic|{\\rtf1 Faulty vision.}\n"), tsd.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(devilPath, devilBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	tsb2Opts := tsd.DefaultBuildOptions()
	tsb2Opts.MaxFTSPercent = 50
	tsb2Src := strings.Join([]string{
		`Crossword Man|creator of TEA at http://www.crosswordman.com`,
		`TEA Support|contact crosswordman for dictionary help`,
		`plate|flat dish`,
		`spoon|shallow utensil`,
	}, "\n") + "\n"
	tsb2Bytes, _, err := tsd.BuildBytes([]byte(tsb2Src), tsb2Opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tsb2Path, tsb2Bytes, 0o644); err != nil {
		t.Fatal(err)
	}

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

	// Remove any auto-discovered dicts first so test order is exact.
	for _, info := range dictSvc.ListDictionaries() {
		_, _ = dictSvc.RemoveDictionary(info.ID)
	}

	// Verify BrowseAndAddDirectory scans a directory and adds all .tsd files in it.
	bridge.pickedDir = dictsSub
	dirInfos, err := dictSvc.BrowseAndAddDirectory(context.Background())
	if err != nil || len(dirInfos) < 2 {
		t.Fatalf("BrowseAndAddDirectory: len=%d err=%v", len(dirInfos), err)
	}
	for _, info := range dirInfos {
		_, _ = dictSvc.RemoveDictionary(info.ID)
	}

	bridge.picked = []string{corePath, devilPath, tsb2Path}
	infos, err := dictSvc.BrowseAndAddDictionaries(context.Background())
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

func TestFirstRunDiscoversExecutableDirAndDictionariesSubdir(t *testing.T) {
	exeDir := t.TempDir()
	dictsSubdir := filepath.Join(exeDir, "dictionaries")
	if err := os.MkdirAll(dictsSubdir, 0o755); err != nil {
		t.Fatal(err)
	}

	topBytes, _, err := tsd.BuildBytes([]byte("alpha\nbeta\n"), tsd.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	subBytes, _, err := tsd.BuildBytes([]byte("gamma\ndelta\n"), tsd.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}

	topPath := filepath.Join(exeDir, "root-dict.tsd")
	subPath := filepath.Join(dictsSubdir, "bundled-dict.tsd")
	if err := os.WriteFile(topPath, topBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subPath, subBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	origExeFunc := profile.ExecutablePathFunc
	profile.ExecutablePathFunc = func() (string, error) {
		return filepath.Join(exeDir, "tisane"), nil
	}
	defer func() {
		profile.ExecutablePathFunc = origExeFunc
	}()

	// Run from an unrelated empty working directory so discovery must use the executable directory.
	t.Chdir(t.TempDir())

	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state, err := app.NewState(cfgPath, app.NoopBridge{})
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	defer state.Close()

	dictSvc := app.NewDictionaryService(state)
	infos := dictSvc.ListDictionaries()
	if len(infos) != 2 {
		t.Fatalf("expected 2 discovered dictionaries on first run, got %d: %+v", len(infos), infos)
	}
	if infos[0].Path != topPath || !infos[0].Enabled {
		t.Fatalf("unexpected first dictionary: %+v (want path=%q enabled=true)", infos[0], topPath)
	}
	if infos[1].Path != subPath || !infos[1].Enabled {
		t.Fatalf("unexpected second dictionary: %+v (want path=%q enabled=true)", infos[1], subPath)
	}

	// Verify config.toml was saved on first run with both dictionaries.
	savedCfg, err := profile.Load(cfgPath)
	if err != nil {
		t.Fatalf("expected config.toml to be saved on first run: %v", err)
	}
	if len(savedCfg.Dictionaries) != 2 {
		t.Fatalf("expected 2 dictionaries in saved config.toml, got %+v", savedCfg.Dictionaries)
	}
}

func TestResetConfigPreservesOrResetsDictionaries(t *testing.T) {
	exeDir := t.TempDir()
	dictsSubdir := filepath.Join(exeDir, "dictionaries")
	if err := os.MkdirAll(dictsSubdir, 0o755); err != nil {
		t.Fatal(err)
	}

	topBytes, _, err := tsd.BuildBytes([]byte("alpha\nbeta\n"), tsd.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	subBytes, _, err := tsd.BuildBytes([]byte("gamma\ndelta\n"), tsd.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	extraBytes, _, err := tsd.BuildBytes([]byte("epsilon\nzeta\n"), tsd.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}

	topPath := filepath.Join(exeDir, "root-dict.tsd")
	subPath := filepath.Join(dictsSubdir, "bundled-dict.tsd")
	extraDir := t.TempDir()
	extraPath := filepath.Join(extraDir, "custom-extra.tsd")
	if err := os.WriteFile(topPath, topBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subPath, subBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extraPath, extraBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	origExeFunc := profile.ExecutablePathFunc
	profile.ExecutablePathFunc = func() (string, error) {
		return filepath.Join(exeDir, "tisane"), nil
	}
	defer func() {
		profile.ExecutablePathFunc = origExeFunc
	}()

	t.Chdir(t.TempDir())

	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	bridge := newMockBridge()
	state, err := app.NewState(cfgPath, bridge)
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	defer state.Close()

	dictSvc := app.NewDictionaryService(state)
	profSvc := app.NewProfileService(state)

	// Customize dictionaries: add extraPath, disable topPath, and remove subPath.
	bridge.picked = []string{extraPath}
	if _, err := dictSvc.BrowseAndAddDictionaries(context.Background()); err != nil {
		t.Fatalf("BrowseAndAddDictionaries: %v", err)
	}
	if _, err := dictSvc.SetEnabled(topPath, false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if _, err := dictSvc.RemoveDictionary(subPath); err != nil {
		t.Fatalf("RemoveDictionary: %v", err)
	}

	// Customize settings and record a recent query.
	state.RecordRecentQuery("c.o..w.rd")
	customAt := "[aeiouy]"
	customHash := "[^aeiouy]"
	minLen := 5
	maxLen := 12
	unified := "unified"
	sortAlpha := "alpha"
	tr := true
	fl := false
	customFonts := profile.DefaultFontConfig()
	customFonts.Results.Family = "Iosevka"
	customFonts.Results.Size = 20
	if _, err := profSvc.UpdateConfig(app.ConfigPatch{
		AtMacro:            &customAt,
		HashMacro:          &customHash,
		MinLength:          &minLen,
		MaxLength:          &maxLen,
		LayoutMode:         &unified,
		SortMode:           &sortAlpha,
		ExclusiveVars:      &tr,
		ShowBalances:       &fl,
		SuppressDuplicates: &fl,
		AutoWebFallback:    &tr,
		EntryFilter:        []string{"lowercase"},
		Fonts:              &customFonts,
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	// 1. Reset settings WITHOUT resetting dictionaries (resetDictionaries = false).
	resetCfgKeepDicts, err := profSvc.ResetConfig(false)
	if err != nil {
		t.Fatalf("ResetConfig(false): %v", err)
	}
	defCfg := profile.DefaultConfig()
	if resetCfgKeepDicts.AtMacro != defCfg.AtMacro ||
		resetCfgKeepDicts.HashMacro != defCfg.HashMacro ||
		resetCfgKeepDicts.MinLength != defCfg.MinLength ||
		resetCfgKeepDicts.MaxLength != defCfg.MaxLength ||
		resetCfgKeepDicts.LayoutMode != defCfg.LayoutMode ||
		resetCfgKeepDicts.SortMode != defCfg.SortMode ||
		resetCfgKeepDicts.ExclusiveVars != defCfg.ExclusiveVars ||
		resetCfgKeepDicts.ShowBalances != defCfg.ShowBalances ||
		resetCfgKeepDicts.SuppressDuplicates != defCfg.SuppressDuplicates ||
		resetCfgKeepDicts.AutoWebFallback != defCfg.AutoWebFallback ||
		len(resetCfgKeepDicts.EntryFilter) != len(defCfg.EntryFilter) ||
		len(resetCfgKeepDicts.RecentQueries) != 0 ||
		resetCfgKeepDicts.Fonts != defCfg.Fonts {
		t.Fatalf("expected all settings reset to defaults, got %+v", resetCfgKeepDicts)
	}

	// Verify loaded & selected dictionaries were preserved when resetDictionaries = false.
	infosKeep := dictSvc.ListDictionaries()
	if len(infosKeep) != 2 || infosKeep[0].Path != topPath || infosKeep[0].Enabled || infosKeep[1].Path != extraPath || !infosKeep[1].Enabled {
		t.Fatalf("expected custom dictionaries to be preserved on ResetConfig(false), got %+v", infosKeep)
	}

	// 2. Reset settings WITH resetting dictionaries (resetDictionaries = true).
	resetCfgWithDicts, err := profSvc.ResetConfig(true)
	if err != nil {
		t.Fatalf("ResetConfig(true): %v", err)
	}
	infosReset := dictSvc.ListDictionaries()
	if len(infosReset) != 2 {
		t.Fatalf("expected 2 first-run dictionaries after ResetConfig(true), got %+v", infosReset)
	}
	if infosReset[0].Path != topPath || !infosReset[0].Enabled {
		t.Fatalf("expected first dictionary %q enabled=true, got %+v", topPath, infosReset[0])
	}
	if infosReset[1].Path != subPath || !infosReset[1].Enabled {
		t.Fatalf("expected second dictionary %q enabled=true, got %+v", subPath, infosReset[1])
	}
	if len(resetCfgWithDicts.Dictionaries) != 2 {
		t.Fatalf("expected config.Dictionaries synced with 2 dictionaries, got %+v", resetCfgWithDicts.Dictionaries)
	}

	// Verify disk persistence after ResetConfig(true).
	diskCfg, err := profile.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load(cfgPath) after ResetConfig(true): %v", err)
	}
	if diskCfg.AtMacro != defCfg.AtMacro || len(diskCfg.Dictionaries) != 2 || !diskCfg.Dictionaries[0].Enabled || !diskCfg.Dictionaries[1].Enabled {
		t.Fatalf("unexpected persisted config after ResetConfig(true): %+v", diskCfg)
	}
}

func TestProfileServiceSidebarSettingsPersistence(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state, err := app.NewState(cfgPath, app.NoopBridge{})
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	defer state.Close()

	profSvc := app.NewProfileService(state)
	w := 340
	h := 380
	collapsed := true
	updated, err := profSvc.UpdateConfig(app.ConfigPatch{
		SidebarWidth:        &w,
		SidebarCollapsed:    &collapsed,
		DefinitionHeight:    &h,
		DefinitionCollapsed: &collapsed,
	})
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if updated.SidebarWidth != 340 || !updated.SidebarCollapsed || updated.DefinitionHeight != 380 || !updated.DefinitionCollapsed {
		t.Fatalf("unexpected updated panel config: %+v", updated)
	}

	diskCfg, err := profile.Load(cfgPath)
	if err != nil {
		t.Fatalf("profile.Load: %v", err)
	}
	if diskCfg.SidebarWidth != 340 || !diskCfg.SidebarCollapsed || diskCfg.DefinitionHeight != 380 || !diskCfg.DefinitionCollapsed {
		t.Fatalf("unexpected persisted panel config: %+v", diskCfg)
	}

	resetCfg, err := profSvc.ResetConfig(false)
	if err != nil {
		t.Fatalf("ResetConfig: %v", err)
	}
	if resetCfg.DefinitionHeight != profile.DefaultDefinitionHeight || resetCfg.DefinitionCollapsed {
		t.Fatalf("expected definition pane config reset to defaults, got height=%d collapsed=%v", resetCfg.DefinitionHeight, resetCfg.DefinitionCollapsed)
	}
}

func TestDictionaryServiceBuildDictionary(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")
	bridge := newMockBridge()

	state, err := app.NewState(cfgPath, bridge)
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	defer state.Close()

	dictSvc := app.NewDictionaryService(state)
	toolsSvc := app.NewToolsService(state)
	for _, info := range dictSvc.ListDictionaries() {
		_, _ = dictSvc.RemoveDictionary(info.ID)
	}

	// Verify file picker helpers.
	wordListPath := filepath.Join(tmpDir, "custom_words.txt")
	wordListData := strings.Join([]string{
		"a",
		"cat\tnoun|A small domesticated feline",
		"zebra:zebras|A striped African equine",
		"elephant|A large mammal with a trunk",
		"supercalifragilistic",
	}, "\n") + "\n"
	if err := os.WriteFile(wordListPath, []byte(wordListData), 0o644); err != nil {
		t.Fatalf("WriteFile word list: %v", err)
	}

	bridge.pickedWord = wordListPath
	gotWord, err := toolsSvc.BrowseWordListFile()
	if err != nil || gotWord != wordListPath {
		t.Fatalf("BrowseWordListFile: got %q err=%v", gotWord, err)
	}

	outNoExt := filepath.Join(tmpDir, "animals")
	bridge.pickedSave = outNoExt
	gotSave, err := toolsSvc.BrowseSaveDictionaryFile(filepath.Join(tmpDir, "custom_words.tsd"))
	if err != nil || gotSave != outNoExt+".tsd" {
		t.Fatalf("BrowseSaveDictionaryFile: got %q err=%v", gotSave, err)
	}
	if bridge.saveDefDir != tmpDir || bridge.saveDefName != "custom_words.tsd" {
		t.Fatalf("unexpected save dialog defaults: dir=%q name=%q", bridge.saveDefDir, bridge.saveDefName)
	}

	// Build with custom min/max length, description, TSV mode, and AddToLibrary=true.
	outPath := filepath.Join(tmpDir, "animals.tsd")
	res, err := toolsSvc.BuildDictionary(context.Background(), app.BuildDictionaryRequest{
		InputPath:               wordListPath,
		OutputPath:              outPath,
		Description:             "Custom Animals Dictionary",
		MinLen:                  3,
		MaxLen:                  10,
		MaxFTSPercent:           10,
		SortAlphabetically:      true,
		TabSeparatedAnnotations: true,
		Encoding:                "utf8",
		AddToLibrary:            true,
	})
	if err != nil {
		t.Fatalf("BuildDictionary: %v", err)
	}
	if res.OutputPath != outPath {
		t.Fatalf("expected OutputPath=%q, got %q", outPath, res.OutputPath)
	}
	if !res.AddedToLibrary {
		t.Fatalf("expected AddedToLibrary=true")
	}
	// "a" (len 1) and "supercalifragilistic" (len 20) should be excluded; cat, zebra, zebras, elephant remain.
	if res.UniqueWords != 4 || res.MaxWordLength != 8 {
		t.Fatalf("unexpected build stats: %+v", res)
	}
	if res.Pass2Definitions != 3 {
		t.Fatalf("expected 3 headword definitions, got %+v", res)
	}

	// Verify the built dictionary is registered in Tisane with the exact description and word count.
	dicts := dictSvc.ListDictionaries()
	if len(dicts) != 1 {
		t.Fatalf("expected 1 registered dictionary, got %d", len(dicts))
	}
	if dicts[0].Description != "Custom Animals Dictionary" || dicts[0].WordCount != 4 || !dicts[0].HasDefinitions || !dicts[0].HasFTS {
		t.Fatalf("unexpected registered dictionary info: %+v", dicts[0])
	}

	// Verify output bytes match direct pkg/tsd.BuildBytes with identical options.
	expectedBytes, _, err := tsd.BuildBytes([]byte(wordListData), tsd.BuildOptions{
		MinLen:                  3,
		MaxLen:                  10,
		MaxFTSPercent:           10,
		SortAlphabetically:      true,
		Description:             "Custom Animals Dictionary",
		Encoding:                tsd.EncodingUTF8,
		TabSeparatedAnnotations: true,
	})
	if err != nil {
		t.Fatalf("tsd.BuildBytes: %v", err)
	}
	actualBytes, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", outPath, err)
	}
	if string(actualBytes) != string(expectedBytes) {
		t.Fatalf("GUI builder output differs from shared pkg/tsd.BuildBytes (%d vs %d bytes)", len(actualBytes), len(expectedBytes))
	}

	// Rebuilding over the already-loaded dictionary reloads its handle cleanly with updated metadata.
	res2, err := toolsSvc.BuildDictionary(context.Background(), app.BuildDictionaryRequest{
		InputPath:          wordListPath,
		OutputPath:         outPath,
		Description:        "Updated Animals",
		MinLen:             1,
		MaxLen:             100,
		MaxFTSPercent:      0, // disable FTS
		SortAlphabetically: false,
		Encoding:           "auto",
		AddToLibrary:       false,
	})
	if err != nil {
		t.Fatalf("rebuild BuildDictionary: %v", err)
	}
	if res2.UniqueWords != 6 {
		t.Fatalf("expected 6 unique words on rebuild, got %+v", res2)
	}
	dictsAfter := dictSvc.ListDictionaries()
	if len(dictsAfter) != 1 || dictsAfter[0].Description != "Updated Animals" || dictsAfter[0].WordCount != 6 || dictsAfter[0].MaxWordLength != 20 {
		t.Fatalf("expected reloaded dictionary with updated stats, got %+v", dictsAfter)
	}
}

func TestWordListMergeService(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")
	bridge := newMockBridge()
	state, err := app.NewState(cfgPath, bridge)
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	defer state.Close()

	toolsSvc := app.NewToolsService(state)

	// BrowseSaveWordListFile suggests merged.txt by default and appends .txt when missing extension.
	bridge.pickedSaveWord = filepath.Join(tmp, "combined")
	gotSave, err := toolsSvc.BrowseSaveWordListFile("")
	if err != nil {
		t.Fatalf("BrowseSaveWordListFile: %v", err)
	}
	if bridge.saveWordDefName != "merged.txt" {
		t.Fatalf("expected default save filename merged.txt, got %q", bridge.saveWordDefName)
	}
	if gotSave != filepath.Join(tmp, "combined.txt") {
		t.Fatalf("expected .txt extension appended, got %q", gotSave)
	}

	p1 := filepath.Join(tmp, "primary.txt")
	p2 := filepath.Join(tmp, "secondary.txt")
	outPath := filepath.Join(tmp, "out.txt")
	if err := os.WriteFile(p1, []byte("Café\nO'Brien\nAlpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, []byte("cafe\nobrien\nBeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := toolsSvc.MergeWordLists(context.Background(), app.MergeWordListsRequest{
		PrimaryPath:   p1,
		SecondaryPath: p2,
		OutputPath:    outPath,
		Operation:     "and",
		Conversion:    "ascii-lower",
		Encoding:      "auto",
	})
	if err != nil {
		t.Fatalf("MergeWordLists: %v", err)
	}
	if res.OutputEntries != 2 || res.Operation != "and" || res.Conversion != "ascii-lower" {
		t.Fatalf("unexpected merge result: %+v", res)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "cafe\nobrien\n" {
		t.Fatalf("got %q, want %q", string(data), "cafe\nobrien\n")
	}
}

func TestWebProviderURLSchemeSecurity(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")
	bridge := newMockBridge()

	// Seed a config file with a malicious file:// web provider template to simulate a tampered config.toml.
	cfg := profile.DefaultConfig()
	cfg.WebProviders = []lookup.WebProvider{
		{ID: "evil", Name: "Evil", URLTemplate: "file:///etc/passwd?{entry}"},
	}
	cfg.DefaultWebProvider = "evil"
	if err := profile.Save(cfgPath, cfg); err != nil {
		t.Fatalf("profile.Save: %v", err)
	}

	state, err := app.NewState(cfgPath, bridge)
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	defer state.Close()

	lookupSvc := app.NewLookupService(state)
	if _, err := lookupSvc.OpenWebLookup("hello", "evil"); err == nil {
		t.Fatalf("expected OpenWebLookup to reject file:// URL template")
	}
	if len(bridge.openedURLs) != 0 {
		t.Fatalf("expected no URLs opened, got %v", bridge.openedURLs)
	}

	profileSvc := app.NewProfileService(state)
	for _, bad := range []string{
		"file:///etc/passwd?{entry}",
		"javascript:alert('{entry}')",
		"ms-msdt:/id/{entry}",
		"https:///missing-host/{entry}",
	} {
		if _, err := profileSvc.UpdateConfig(app.ConfigPatch{
			WebProviders: []lookup.WebProvider{{ID: "bad", Name: "Bad", URLTemplate: bad}},
		}); err == nil {
			t.Fatalf("expected UpdateConfig to reject %q", bad)
		}
	}

	if _, err := profileSvc.UpdateConfig(app.ConfigPatch{
		WebProviders: []lookup.WebProvider{{ID: "good", Name: "Good", URLTemplate: "https://example.com/?q={entry}"}},
	}); err != nil {
		t.Fatalf("expected valid https template to succeed: %v", err)
	}
	if got, err := lookupSvc.OpenWebLookup("hello", "good"); err != nil || got != "https://example.com/?q=hello" {
		t.Fatalf("expected valid OpenWebLookup, got (%q, %v)", got, err)
	}
}

func TestConcurrentDictionaryRemovalDuringSearch(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")
	bridge := newMockBridge()

	// Build a synthetic .tsd dictionary so the test is self-contained.
	wordListPath := filepath.Join(tmp, "words.txt")
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		sb.WriteString("alpha" + strings.Repeat("a", i%10) + "\n")
		sb.WriteString("beta" + strings.Repeat("b", i%10) + "\n")
	}
	if err := os.WriteFile(wordListPath, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	tsdPath := filepath.Join(tmp, "test.tsd")
	if _, _, err := tsd.BuildFileWithParams(tsd.BuildParams{
		InputPath:  wordListPath,
		OutputPath: tsdPath,
	}); err != nil {
		t.Fatalf("BuildFileWithParams: %v", err)
	}

	state, err := app.NewState(cfgPath, bridge)
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	defer state.Close()

	dictSvc := app.NewDictionaryService(state)
	searchSvc := app.NewSearchService(state)
	lookupSvc := app.NewLookupService(state)

	bridge.picked = []string{tsdPath}
	infos, err := dictSvc.BrowseAndAddDictionaries(context.Background())
	if err != nil || len(infos) == 0 {
		t.Fatalf("BrowseAndAddDictionaries: %v (len=%d)", err, len(infos))
	}
	targetID := infos[len(infos)-1].ID

	// Kick off a search and immediately remove the dictionary while lookup & search run concurrently.
	if _, err := searchSvc.StartSearch(app.SearchRequest{
		Query:    "*",
		Mode:     "word",
		SortMode: "dict",
	}); err != nil {
		t.Fatalf("StartSearch: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = lookupSvc.GetDefinitions(context.Background(), "alpha", false)
	}()

	if _, err := dictSvc.RemoveDictionary(targetID); err != nil {
		t.Fatalf("RemoveDictionary: %v", err)
	}

	wg.Wait()
	app.ShutdownSearch(searchSvc)
}

func TestContextCancellationAndLengthValidation(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")
	state, err := app.NewState(cfgPath, newMockBridge())
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	defer state.Close()

	dictSvc := app.NewDictionaryService(state)
	toolsSvc := app.NewToolsService(state)
	profileSvc := app.NewProfileService(state)

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := toolsSvc.BuildDictionary(canceledCtx, app.BuildDictionaryRequest{}); err == nil {
		t.Fatalf("expected BuildDictionary to return context error")
	}
	if _, err := toolsSvc.MergeWordLists(canceledCtx, app.MergeWordListsRequest{}); err == nil {
		t.Fatalf("expected MergeWordLists to return context error")
	}
	if _, err := dictSvc.BrowseAndAddDirectory(canceledCtx); err == nil {
		t.Fatalf("expected BrowseAndAddDirectory to return context error")
	}
	if _, err := dictSvc.BrowseAndAddDictionaries(canceledCtx); err == nil {
		t.Fatalf("expected BrowseAndAddDictionaries to return context error")
	}

	minLen, maxLen := 12, 5
	if _, err := profileSvc.UpdateConfig(app.ConfigPatch{MinLength: &minLen, MaxLength: &maxLen}); err == nil {
		t.Fatalf("expected UpdateConfig to reject minLength > maxLength")
	}

	// Corrupt config.toml should be backed up to config.toml.bak rather than preventing startup or silently vanishing.
	corruptCfg := filepath.Join(tmp, "corrupt.toml")
	if err := os.WriteFile(corruptCfg, []byte("[dictionaries]\npath = \"broken\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	recoveredState, err := app.NewState(corruptCfg, newMockBridge())
	if err != nil {
		t.Fatalf("NewState with corrupt config: %v", err)
	}
	defer recoveredState.Close()
	if _, err := os.Stat(corruptCfg + ".bak"); err != nil {
		t.Fatalf("expected corrupt config backup %q: %v", corruptCfg+".bak", err)
	}
}

func TestConcurrentSearchLookupRemoveAndRebuild(t *testing.T) {
	t.Setenv(profile.EnvDictPath, "")
	t.Setenv("BUILD_WORKING_DIRECTORY", "")

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.toml")
	srcWords := filepath.Join(tmp, "words.txt")
	dictA := filepath.Join(tmp, "dictA.tsd")
	dictB := filepath.Join(tmp, "dictB.tsd")

	if err := os.WriteFile(srcWords, []byte("alpha|First\nbeta|Second\ngamma|Third\ndelta|Fourth\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{dictA, dictB} {
		if _, err := tsd.BuildFile(srcWords, p, tsd.DefaultBuildOptions()); err != nil {
			t.Fatal(err)
		}
	}

	bridge := newMockBridge()
	state, err := app.NewState(cfgPath, bridge)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()

	dictSvc := app.NewDictionaryService(state)
	toolsSvc := app.NewToolsService(state)
	searchSvc := app.NewSearchService(state)
	lookupSvc := app.NewLookupService(state)

	for _, info := range dictSvc.ListDictionaries() {
		_, _ = dictSvc.RemoveDictionary(info.ID)
	}
	bridge.picked = []string{dictA, dictB}
	infos, err := dictSvc.BrowseAndAddDictionaries(context.Background())
	if err != nil || len(infos) != 2 {
		t.Fatalf("BrowseAndAddDictionaries: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(4)

	go func() {
		defer wg.Done()
		for i := 0; i < 15; i++ {
			_, _ = searchSvc.StartSearch(app.SearchRequest{
				SearchID: uint64(i + 1),
				Query:    "*",
				Mode:     "word",
				SortMode: "alpha",
			})
			if i%3 == 0 {
				searchSvc.CancelSearch()
			}
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 15; i++ {
			_, _ = lookupSvc.GetDefinitions(context.Background(), "alpha", true)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			_, _ = toolsSvc.BuildDictionary(context.Background(), app.BuildDictionaryRequest{
				InputPath:    srcWords,
				OutputPath:   dictA,
				AddToLibrary: true,
			})
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			_, _ = dictSvc.RemoveDictionary(infos[1].ID)
			bridge.mu.Lock()
			bridge.picked = []string{dictB}
			bridge.mu.Unlock()
			_, _ = dictSvc.BrowseAndAddDictionaries(context.Background())
		}
	}()

	wg.Wait()
	app.ShutdownSearch(searchSvc)
}
