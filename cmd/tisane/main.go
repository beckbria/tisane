// Command tisane launches the Tisane desktop GUI application powered by Go, Wails v3, and Svelte 5.
package main

import (
	"context"
	"embed"
	"log"
	"os"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"

	"tisane/build"
	"tisane/cmd/tisane/app"
)

//go:generate go run ./internal/winrsrc -icon ../../build/appicon.png -manifest ../../build/windows/tisane.exe.manifest -info ../../build/windows/info.json -arch amd64 -out rsrc_windows_amd64.syso

//go:embed all:frontend/dist
var assets embed.FS

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
	*app.ToolsService,
	*app.SearchService,
	*app.LookupService,
	*app.ProfileService,
	[]application.Service,
) {
	dictSvc := app.NewDictionaryService(state)
	toolsSvc := app.NewToolsService(state)
	searchSvc := app.NewSearchService(state)
	lookupSvc := app.NewLookupService(state)
	profileSvc := app.NewProfileService(state)

	services := []application.Service{
		application.NewService(dictSvc),
		application.NewService(toolsSvc),
		application.NewService(searchSvc),
		application.NewService(lookupSvc),
		application.NewService(profileSvc),
	}
	return dictSvc, toolsSvc, searchSvc, lookupSvc, profileSvc, services
}

func buildAppMenu(
	wailsApp *application.App,
	dictSvc *app.DictionaryService,
	searchSvc *app.SearchService,
) *application.Menu {
	menu := wailsApp.Menu.New()
	if runtime.GOOS == "darwin" {
		menu.AddRole(application.AppMenu)
	}

	fileMenu := menu.AddSubmenu("File")
	fileMenu.Add("Add Dictionary (.tsd)...").
		SetAccelerator("CmdOrCtrl+O").
		OnClick(func(*application.Context) {
			go func() {
				if _, err := dictSvc.BrowseAndAddDictionaries(context.Background()); err != nil {
					log.Printf("tisane: add dictionaries: %v", err)
				}
			}()
		})
	fileMenu.Add("Add All from Directory...").
		OnClick(func(*application.Context) {
			go func() {
				if _, err := dictSvc.BrowseAndAddDirectory(context.Background()); err != nil {
					log.Printf("tisane: add directory: %v", err)
				}
			}()
		})
	fileMenu.Add("Build Dictionary from Word List...").
		SetAccelerator("CmdOrCtrl+Shift+B").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit(app.EventUIOpenDictBuilder, nil)
		})
	fileMenu.Add("Merge Word Lists...").
		SetAccelerator("CmdOrCtrl+Shift+M").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit(app.EventUIOpenWordListMerge, nil)
		})
	fileMenu.AddSeparator()
	fileMenu.Add("Settings...").
		SetAccelerator("CmdOrCtrl+,").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit(app.EventUIOpenSettings, nil)
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
			wailsApp.Event.Emit(app.EventUISetMode, "word")
		})
	searchMenu.Add("Full Text Search Mode").
		SetAccelerator("CmdOrCtrl+F").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit(app.EventUISetMode, "fts")
		})
	searchMenu.AddSeparator()
	searchMenu.Add("Cancel Active Search").
		OnClick(func(*application.Context) {
			searchSvc.CancelSearch()
		})

	viewMenu := menu.AddSubmenu("View")
	viewMenu.Add("Separate Tiled Panes (Default)").
		SetAccelerator("CmdOrCtrl+1").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit(app.EventUISetLayout, "tiled")
		})
	viewMenu.Add("Unified Merged List").
		SetAccelerator("CmdOrCtrl+2").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit(app.EventUISetLayout, "unified")
		})
	viewMenu.Add("Toggle Dictionaries Panel").
		SetAccelerator("CmdOrCtrl+B").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit(app.EventUIToggleSidebar, nil)
		})
	viewMenu.Add("Toggle Definitions Panel").
		SetAccelerator("CmdOrCtrl+D").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit(app.EventUIToggleDefinitions, nil)
		})
	viewMenu.AddSeparator()
	viewMenu.Add("Pattern Syntax Reference").
		SetAccelerator("F1").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit(app.EventUIToggleCheatsheet, nil)
		})
	viewMenu.Add("Customize Fonts...").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit(app.EventUIOpenFontSettings, nil)
		})
	themeMenu := viewMenu.AddSubmenu("Theme")
	themeMenu.Add("Dark").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit(app.EventUISetTheme, "dark")
		})
	themeMenu.Add("Light").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit(app.EventUISetTheme, "light")
		})
	themeMenu.Add("System").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit(app.EventUISetTheme, "system")
		})
	viewMenu.AddSeparator()
	viewMenu.Add("Zoom In").
		SetAccelerator("CmdOrCtrl+=").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit(app.EventUIZoom, "in")
		})
	viewMenu.Add("Zoom Out").
		SetAccelerator("CmdOrCtrl+-").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit(app.EventUIZoom, "out")
		})
	viewMenu.Add("Actual Size").
		SetAccelerator("CmdOrCtrl+0").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit(app.EventUIZoom, "reset")
		})

	helpMenu := menu.AddSubmenu("Help")
	helpMenu.Add("About Tisane").
		OnClick(func(*application.Context) {
			wailsApp.Event.Emit(app.EventUIOpenAbout, nil)
		})

	return menu
}

func main() {
	cfgPath := os.Getenv("TISANE_CONFIG")
	state, err := app.NewState(cfgPath, nil)
	if err != nil {
		log.Fatalf("tisane: failed to initialize state: %v", err)
	}

	dictSvc, _, searchSvc, _, profileSvc, services := newServices(state)

	wailsApp := application.New(application.Options{
		Name:        "Tisane",
		Description: "High-speed crossword, anagram, and wordplay dictionary search",
		Icon:        build.AppIcon,
		Services:    services,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		OnShutdown: func() {
			app.ShutdownSearch(searchSvc)
			_ = state.Close()
		},
	})

	bgColour := application.NewRGB(2, 6, 23)
	if profileSvc.GetConfig().Theme == "light" {
		bgColour = application.NewRGB(255, 255, 255)
	}

	mainWin := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Tisane",
		Width:            1320,
		Height:           840,
		MinWidth:         640,
		MinHeight:        480,
		BackgroundColour: bgColour,
		URL:              "/",
		Linux: application.LinuxWindow{
			Icon: build.AppIcon,
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
