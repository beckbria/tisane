// Command tisane launches the Tisane desktop GUI application powered by Go, Wails v3, and Svelte 5.
package main

import (
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"

	"tisane/cmd/tisane/app"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed icon.png
var appIcon []byte

const appVersion = app.Version

// wailsHostBridge connects the testable app.State services to the live Wails v3 runtime.
type wailsHostBridge struct {
	wailsApp *application.App
	mainWin  *application.WebviewWindow
}

func (b *wailsHostBridge) EmitEvent(name string, data any) {
	if b.wailsApp != nil && b.wailsApp.Event != nil {
		b.wailsApp.Event.Emit(name, data)
	}
}

func (b *wailsHostBridge) OpenURL(url string) error {
	if b.wailsApp != nil && b.wailsApp.Browser != nil {
		return b.wailsApp.Browser.OpenURL(url)
	}
	return nil
}

func (b *wailsHostBridge) PickDictionaryFiles() ([]string, error) {
	if b.wailsApp == nil || b.wailsApp.Dialog == nil {
		return nil, nil
	}
	dlg := b.wailsApp.Dialog.OpenFile().
		SetTitle("Add Tisane / TEA Dictionary (.tsd)").
		AddFilter("TEA Dictionary (*.tsd)", "*.tsd;*.TSD").
		AddFilter("All Files (*.*)", "*.*")
	if b.mainWin != nil {
		dlg.AttachToWindow(b.mainWin)
	}
	return dlg.PromptForMultipleSelection()
}

func (b *wailsHostBridge) PickDictionaryDirectory() (string, error) {
	if b.wailsApp == nil || b.wailsApp.Dialog == nil {
		return "", nil
	}
	dlg := b.wailsApp.Dialog.OpenFile().
		SetTitle("Add All Dictionaries from Directory").
		CanChooseDirectories(true).
		CanChooseFiles(false)
	if b.mainWin != nil {
		dlg.AttachToWindow(b.mainWin)
	}
	return dlg.PromptForSingleSelection()
}

func (b *wailsHostBridge) PickWordListFile() (string, error) {
	if b.wailsApp == nil || b.wailsApp.Dialog == nil {
		return "", nil
	}
	dlg := b.wailsApp.Dialog.OpenFile().
		SetTitle("Select Word List File").
		AddFilter("Word Lists (*.txt, *.tsv, *.lst, *.dic)", "*.txt;*.TXT;*.tsv;*.TSV;*.lst;*.LST;*.dic;*.DIC").
		AddFilter("All Files (*.*)", "*.*")
	if b.mainWin != nil {
		dlg.AttachToWindow(b.mainWin)
	}
	return dlg.PromptForSingleSelection()
}

func (b *wailsHostBridge) PickSaveDictionaryFile(defaultDir, defaultName string) (string, error) {
	if b.wailsApp == nil || b.wailsApp.Dialog == nil {
		return "", nil
	}
	dlg := b.wailsApp.Dialog.SaveFile().
		SetMessage("Save Tisane / TEA Dictionary (.tsd)").
		AddFilter("TEA Dictionary (*.tsd)", "*.tsd;*.TSD").
		CanCreateDirectories(true)
	if defaultDir != "" {
		dlg.SetDirectory(defaultDir)
	}
	if defaultName != "" {
		dlg.SetFilename(defaultName)
	}
	if b.mainWin != nil {
		dlg.AttachToWindow(b.mainWin)
	}
	return dlg.PromptForSingleSelection()
}

func (b *wailsHostBridge) PickSaveWordListFile(defaultDir, defaultName string) (string, error) {
	if b.wailsApp == nil || b.wailsApp.Dialog == nil {
		return "", nil
	}
	dlg := b.wailsApp.Dialog.SaveFile().
		SetMessage("Save Merged Word List (.txt)").
		AddFilter("Word List (*.txt)", "*.txt;*.TXT").
		AddFilter("All Files (*.*)", "*.*").
		CanCreateDirectories(true)
	if defaultDir != "" {
		dlg.SetDirectory(defaultDir)
	}
	if defaultName != "" {
		dlg.SetFilename(defaultName)
	}
	if b.mainWin != nil {
		dlg.AttachToWindow(b.mainWin)
	}
	return dlg.PromptForSingleSelection()
}

func (b *wailsHostBridge) PickAlphabetMapFile() (string, error) {
	if b.wailsApp == nil || b.wailsApp.Dialog == nil {
		return "", nil
	}
	dlg := b.wailsApp.Dialog.OpenFile().
		SetTitle("Select Alphabet Map (.tsm)").
		AddFilter("Alphabet Map (*.tsm)", "*.tsm;*.TSM").
		AddFilter("All Files (*.*)", "*.*")
	if b.mainWin != nil {
		dlg.AttachToWindow(b.mainWin)
	}
	return dlg.PromptForSingleSelection()
}

func newServices(state *app.State) (
	*app.DictionaryService,
	*app.SearchService,
	*app.LookupService,
	*app.ProfileService,
	[]application.Service,
) {
	dictSvc := app.NewDictionaryService(state)
	searchSvc := app.NewSearchService(state)
	lookupSvc := app.NewLookupService(state)
	profileSvc := app.NewProfileService(state)

	services := []application.Service{
		application.NewService(dictSvc),
		application.NewService(searchSvc),
		application.NewService(lookupSvc),
		application.NewService(profileSvc),
	}
	return dictSvc, searchSvc, lookupSvc, profileSvc, services
}

func buildAppMenu(
	wailsApp *application.App,
	dictSvc *app.DictionaryService,
	searchSvc *app.SearchService,
) *application.Menu {
	menu := wailsApp.Menu.New()

	fileMenu := menu.AddSubmenu("File")
	fileMenu.Add("Add Dictionary (.tsd)...").
		SetAccelerator("CmdOrCtrl+O").
		OnClick(func(*application.Context) {
			_, _ = dictSvc.BrowseAndAddDictionaries()
		})
	fileMenu.Add("Add All from Directory...").
		OnClick(func(*application.Context) {
			_, _ = dictSvc.BrowseAndAddDirectory()
		})
	fileMenu.Add("Build Dictionary from Word List...").
		SetAccelerator("CmdOrCtrl+Shift+B").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit("ui:open-dict-builder", nil)
		})
	fileMenu.Add("Merge Word Lists...").
		SetAccelerator("CmdOrCtrl+Shift+M").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit("ui:open-wordlist-merge", nil)
		})
	fileMenu.AddSeparator()
	fileMenu.Add("Settings...").
		SetAccelerator("CmdOrCtrl+,").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit("ui:open-settings", nil)
		})
	fileMenu.AddSeparator()
	fileMenu.Add("Quit").
		SetAccelerator("CmdOrCtrl+Q").
		OnClick(func(*application.Context) {
			wailsApp.Quit()
		})

	menu.AddRole(application.EditMenu)

	searchMenu := menu.AddSubmenu("Search")
	searchMenu.Add("Word Search Mode").
		SetAccelerator("CmdOrCtrl+W").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit("ui:set-mode", "word")
		})
	searchMenu.Add("Full Text Search Mode").
		SetAccelerator("CmdOrCtrl+F").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit("ui:set-mode", "fts")
		})
	searchMenu.AddSeparator()
	searchMenu.Add("Cancel Active Search").
		SetAccelerator("Escape").
		OnClick(func(*application.Context) {
			searchSvc.CancelSearch()
		})

	viewMenu := menu.AddSubmenu("View")
	viewMenu.Add("Separate Tiled Panes (Default)").
		SetAccelerator("CmdOrCtrl+1").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit("ui:set-layout", "tiled")
		})
	viewMenu.Add("Unified Merged List").
		SetAccelerator("CmdOrCtrl+2").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit("ui:set-layout", "unified")
		})
	viewMenu.Add("Toggle Dictionaries Panel").
		SetAccelerator("CmdOrCtrl+B").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit("ui:toggle-sidebar", nil)
		})
	viewMenu.AddSeparator()
	viewMenu.Add("Pattern Syntax Reference").
		SetAccelerator("F1").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit("ui:toggle-cheatsheet", nil)
		})
	viewMenu.Add("Customize Fonts...").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit("ui:open-font-settings", nil)
		})

	helpMenu := menu.AddSubmenu("Help")
	helpMenu.Add("About Tisane").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit("ui:open-about", nil)
		})

	return menu
}

func main() {
	cfgPath := os.Getenv("TISANE_CONFIG")
	state, err := app.NewState(cfgPath, nil)
	if err != nil {
		log.Fatalf("tisane: failed to initialize state: %v", err)
	}

	dictSvc, searchSvc, _, _, services := newServices(state)

	wailsApp := application.New(application.Options{
		Name:        "Tisane",
		Description: "High-speed crossword, anagram, and wordplay dictionary search",
		Icon:        appIcon,
		Services:    services,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		OnShutdown: func() {
			searchSvc.CancelSearch()
			_ = state.Close()
		},
	})

	mainWin := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      "main",
		Title:     "Tisane",
		Width:     1320,
		Height:    840,
		MinWidth:  920,
		MinHeight: 600,
		URL:       "/",
		Linux: application.LinuxWindow{
			Icon: appIcon,
		},
	})

	state.SetBridge(&wailsHostBridge{
		wailsApp: wailsApp,
		mainWin:  mainWin,
	})

	wailsApp.Menu.SetApplicationMenu(buildAppMenu(wailsApp, dictSvc, searchSvc))

	if err := wailsApp.Run(); err != nil {
		log.Fatal(err)
	}
}
