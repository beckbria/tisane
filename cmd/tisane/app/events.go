package app

// Canonical Wails v3 event names shared between Go services, main.go menus, and the Svelte 5 frontend.
const (
	EventSearchBatch    = "search:batch"
	EventSearchProgress = "search:progress"
	EventSearchComplete = "search:complete"
	EventSearchError    = "search:error"

	EventDictsUpdated = "dicts:updated"

	EventUIOpenSettings      = "ui:open-settings"
	EventUIOpenFontSettings  = "ui:open-font-settings"
	EventUIOpenAbout         = "ui:open-about"
	EventUIOpenDictBuilder   = "ui:open-dict-builder"
	EventUIOpenWordListMerge = "ui:open-wordlist-merge"
	EventUISetMode           = "ui:set-mode"
	EventUISetLayout         = "ui:set-layout"
	EventUIToggleCheatsheet  = "ui:toggle-cheatsheet"
	EventUIToggleSidebar     = "ui:toggle-sidebar"
)
