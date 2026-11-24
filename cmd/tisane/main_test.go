package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"tisane"
	"tisane/build"
	"tisane/cmd/tisane/app"
)

// TestWailsServiceBindingsAndEmbeddedAssets verifies that every Go method invoked via @wailsio/runtime
// Call.ByName(...) from the Svelte 5 frontend is registered under its exact FQN, and that
// the embedded frontend/dist bundle serves index.html cleanly.
func TestWailsServiceBindingsAndEmbeddedAssets(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state, err := app.NewState(cfgPath, app.NoopBridge{})
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	defer state.Close()

	_, _, _, _, services := newServices(state)
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
		"tisane/cmd/tisane/app.DictionaryService.AddDictionaryPaths",
		"tisane/cmd/tisane/app.DictionaryService.BrowseAndAddDictionaries",
		"tisane/cmd/tisane/app.DictionaryService.BrowseAndAddDirectory",
		"tisane/cmd/tisane/app.DictionaryService.ScanDirectory",
		"tisane/cmd/tisane/app.DictionaryService.RemoveDictionary",
		"tisane/cmd/tisane/app.DictionaryService.BrowseWordListFile",
		"tisane/cmd/tisane/app.DictionaryService.BrowseSaveDictionaryFile",
		"tisane/cmd/tisane/app.DictionaryService.BrowseSaveWordListFile",
		"tisane/cmd/tisane/app.DictionaryService.BrowseAlphabetMapFile",
		"tisane/cmd/tisane/app.DictionaryService.BuildDictionary",
		"tisane/cmd/tisane/app.DictionaryService.MergeWordLists",
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

	// Internal helpers must not be exposed as Wails RPC methods.
	if bm := bindings.Get(&application.CallOptions{MethodName: "tisane/cmd/tisane/app.SearchService.RecordQueryAsync"}); bm != nil {
		t.Errorf("unexpectedly bound internal helper method: %s", bm.Name)
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

	if appVersion == "" || appVersion != app.Version || appVersion != tisane.Version {
		t.Fatalf("expected non-empty matching version %q, got appVersion=%q app.Version=%q", tisane.Version, appVersion, app.Version)
	}
}
