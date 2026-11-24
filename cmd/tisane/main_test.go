package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"tisane"
	"tisane/build"
	"tisane/cmd/tisane/app"
)

// TestWailsServiceBindingsAndEmbeddedAssets verifies that the bound Go service methods exposed
// to Wails v3 match the expected FQN contract, and that embedded frontend/dist serves index.html.
func TestWailsServiceBindingsAndEmbeddedAssets(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state, err := app.NewState(cfgPath, app.NoopBridge{})
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	defer state.Close()

	dictSvc, toolsSvc, searchSvc, lookupSvc, profileSvc, services := newServices(state)
	assetHandler := application.AssetFileServerFS(assets)

	// Initialize headless Wails App container (without calling Run) so Wails internal logger is initialized.
	_ = application.New(application.Options{
		Name:     "TisaneTest",
		Services: services,
		Assets: application.AssetOptions{
			Handler: assetHandler,
		},
	})

	bindings := application.NewBindings(nil, nil)
	for _, svc := range services {
		if err := bindings.Add(svc); err != nil {
			t.Fatalf("bindings.Add: %v", err)
		}
	}

	expectedFQNs := []string{
		"tisane/cmd/tisane/app.DictionaryService.ListDictionaries",
		"tisane/cmd/tisane/app.DictionaryService.SetEnabled",
		"tisane/cmd/tisane/app.DictionaryService.ReorderDictionaries",
		"tisane/cmd/tisane/app.DictionaryService.BrowseAndAddDictionaries",
		"tisane/cmd/tisane/app.DictionaryService.BrowseAndAddDirectory",
		"tisane/cmd/tisane/app.DictionaryService.RemoveDictionary",
		"tisane/cmd/tisane/app.ToolsService.BrowseWordListFile",
		"tisane/cmd/tisane/app.ToolsService.BrowseSaveDictionaryFile",
		"tisane/cmd/tisane/app.ToolsService.BrowseSaveWordListFile",
		"tisane/cmd/tisane/app.ToolsService.BrowseAlphabetMapFile",
		"tisane/cmd/tisane/app.ToolsService.BuildDictionary",
		"tisane/cmd/tisane/app.ToolsService.MergeWordLists",
		"tisane/cmd/tisane/app.SearchService.ValidatePattern",
		"tisane/cmd/tisane/app.SearchService.StartSearch",
		"tisane/cmd/tisane/app.SearchService.CancelSearch",
		"tisane/cmd/tisane/app.LookupService.GetDefinitions",
		"tisane/cmd/tisane/app.LookupService.OpenWebLookup",
		"tisane/cmd/tisane/app.LookupService.OpenExternalURL",
		"tisane/cmd/tisane/app.ProfileService.GetConfig",
		"tisane/cmd/tisane/app.ProfileService.UpdateConfig",
		"tisane/cmd/tisane/app.ProfileService.ResetConfig",
	}

	for _, fqn := range expectedFQNs {
		bm := bindings.Get(&application.CallOptions{MethodName: fqn})
		if bm == nil {
			t.Errorf("missing expected Wails bound method FQN: %s", fqn)
		}
	}

	// Verify exact 1-to-1 exported method surface across all bound services (no unintended RPCs).
	var actualFQNs []string
	for _, inst := range []any{dictSvc, toolsSvc, searchSvc, lookupSvc, profileSvc} {
		rt := reflect.TypeOf(inst)
		elem := rt.Elem()
		prefix := elem.PkgPath() + "." + elem.Name() + "."
		for i := 0; i < rt.NumMethod(); i++ {
			actualFQNs = append(actualFQNs, prefix+rt.Method(i).Name)
		}
	}
	wantSorted := slices.Clone(expectedFQNs)
	slices.Sort(wantSorted)
	slices.Sort(actualFQNs)
	if !slices.Equal(actualFQNs, wantSorted) {
		t.Fatalf("bound service FQN mismatch:\n got: %v\nwant: %v", actualFQNs, wantSorted)
	}

	// Verify 1-to-1 parity against Call.ByName(...) FQNs in frontend/src/lib/bindings.ts.
	bindingsTS, err := os.ReadFile(filepath.Join("frontend", "src", "lib", "bindings.ts"))
	if err != nil {
		t.Fatalf("ReadFile(bindings.ts): %v", err)
	}
	fqnRe := regexp.MustCompile(`tisane/cmd/tisane/app\.[A-Za-z0-9_]+\.[A-Za-z0-9_]+`)
	tsMatches := fqnRe.FindAllString(string(bindingsTS), -1)
	slices.Sort(tsMatches)
	tsFQNs := slices.Compact(tsMatches)
	if !slices.Equal(tsFQNs, actualFQNs) {
		t.Fatalf("frontend bindings.ts FQN drift:\nbindings.ts: %v\nGo services: %v", tsFQNs, actualFQNs)
	}

	// Verify embedded Svelte 5 dist bundle serves index.html
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	assetHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK serving embedded '/', got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "<title>Tisane</title>") {
		t.Fatalf("unexpected embedded index.html body: %s", rec.Body.String())
	}

	// Verify embedded application icon (PNG) and frontend /icon.png asset
	pngMagic := "\x89PNG\r\n\x1a\n"
	if len(build.AppIcon) < len(pngMagic) || string(build.AppIcon[:len(pngMagic)]) != pngMagic {
		t.Fatalf("expected embedded build.AppIcon to be a valid PNG, got %d bytes", len(build.AppIcon))
	}
	iconReq := httptest.NewRequest(http.MethodGet, "/icon.png", nil)
	iconRec := httptest.NewRecorder()
	assetHandler.ServeHTTP(iconRec, iconReq)
	if iconRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK serving embedded '/icon.png', got %d", iconRec.Code)
	}
	if !strings.HasPrefix(iconRec.Body.String(), pngMagic) {
		t.Fatalf("expected embedded '/icon.png' response body to start with PNG signature")
	}

	bootReq := httptest.NewRequest(http.MethodGet, "/boot.js", nil)
	bootRec := httptest.NewRecorder()
	assetHandler.ServeHTTP(bootRec, bootReq)
	if bootRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK serving embedded '/boot.js', got %d", bootRec.Code)
	}
	if !strings.Contains(bootRec.Body.String(), "tisane:boot") {
		t.Fatalf("unexpected embedded '/boot.js' body: %s", bootRec.Body.String())
	}

	if tisane.Version == "" {
		t.Fatal("expected non-empty tisane.Version")
	}
}
