<script lang="ts">
  import { onMount } from 'svelte';
  import { getBackend, isDesktopWebview } from './lib/bindings';
  import { closeTopModalOnEscape, hasActiveModal } from './lib/actions/modalTrap';
  import {
    createConfigStore,
    DEFAULT_DEFINITION_HEIGHT,
    DEFAULT_SIDEBAR_WIDTH,
    DEFINITION_COLLAPSE_THRESHOLD,
    MAX_DEFINITION_HEIGHT,
    MAX_SIDEBAR_WIDTH,
    MIN_DEFINITION_HEIGHT,
    MIN_SIDEBAR_WIDTH,
    SIDEBAR_COLLAPSE_THRESHOLD
  } from './lib/stores/config.svelte';
  import { createDictionaryStore } from './lib/stores/dictionaries.svelte';
  import { createSearchStore } from './lib/stores/search.svelte';
  import { createDefinitionStore } from './lib/stores/definitions.svelte';
  import type { ConfigPatch, EntryCategory, LayoutMode, Match, SortMode } from './lib/types';

  import PatternBar from './lib/components/PatternBar.svelte';
  import FilterToolbar from './lib/components/FilterToolbar.svelte';
  import DictionarySidebar from './lib/components/DictionarySidebar.svelte';
  import VirtualResultPane from './lib/components/VirtualResultPane.svelte';
  import DefinitionPane from './lib/components/DefinitionPane.svelte';
  import SettingsModal from './lib/components/SettingsModal.svelte';
  import FontSettingsModal from './lib/components/FontSettingsModal.svelte';
  import AboutModal from './lib/components/AboutModal.svelte';
  import BuildDictionaryModal from './lib/components/BuildDictionaryModal.svelte';
  import MergeWordListsModal from './lib/components/MergeWordListsModal.svelte';

  const cfgStore = createConfigStore();
  const defStore = createDefinitionStore({
    onError: (msg) => {
      searchStore.errorMessage = msg;
    }
  });
  const dictStore = createDictionaryStore({
    onChanged: (active) => searchStore.onDictionariesChanged(active),
    onError: (msg) => {
      searchStore.errorMessage = msg;
    },
    onClearError: () => {
      searchStore.errorMessage = '';
    }
  });
  const searchStore = createSearchStore({
    getConfig: () => cfgStore.value,
    getActiveDictionaries: () => dictStore.active,
    onRecordRecentQuery: (q) => cfgStore.recordRecentQuery(q),
    onFirstMatch: (word) => {
      if (!defStore.selectedWord) {
        void defStore.load(word, true);
      }
    }
  });

  // Modal visibility flags
  let showCheatsheet = $state(false);
  let showSettings = $state(false);
  let showFontSettings = $state(false);
  let showAbout = $state(false);
  let showDictBuilder = $state(false);
  let showWordListMerger = $state(false);

  // Resizable & collapsible dictionary sidebar state
  let isResizingSidebar = $state(false);
  let dragSidebarWidth: number | null = $state(null);
  let cleanupSidebarDrag: (() => void) | null = null;

  // Resizable & collapsible definition pane state
  let isResizingDefinition = $state(false);
  let dragDefinitionHeight: number | null = $state(null);
  let cleanupDefinitionDrag: (() => void) | null = null;

  // Reactive viewport dimensions for compact/split-screen adaptation
  let viewportWidth = $state(typeof window !== 'undefined' ? window.innerWidth || 1320 : 1320);
  let viewportHeight = $state(typeof window !== 'undefined' ? window.innerHeight || 840 : 840);

  let patternBarRef: ReturnType<typeof PatternBar> | undefined = $state();

  const zoomScale = $derived((cfgStore.value.uiZoom || 100) / 100);
  const isCompactViewport = $derived(viewportWidth > 0 && viewportWidth / zoomScale < 768);

  const effectiveSidebarWidth = $derived.by(() => {
    const raw = dragSidebarWidth ?? cfgStore.value.sidebarWidth;
    if (viewportWidth <= 0) return raw;
    const maxW = isCompactViewport
      ? Math.max(MIN_SIDEBAR_WIDTH, Math.min(320, Math.floor(viewportWidth / zoomScale) - 48))
      : Math.min(MAX_SIDEBAR_WIDTH, Math.max(MIN_SIDEBAR_WIDTH, Math.floor((viewportWidth / zoomScale) * 0.55)));
    return Math.min(raw, maxW);
  });

  const effectiveDefinitionHeight = $derived.by(() => {
    const raw = dragDefinitionHeight ?? cfgStore.value.definitionHeight;
    if (viewportHeight <= 0) return raw;
    const maxH = Math.min(
      MAX_DEFINITION_HEIGHT,
      Math.max(MIN_DEFINITION_HEIGHT, Math.floor((viewportHeight / zoomScale) * 0.55))
    );
    return Math.min(raw, maxH);
  });
  const anyModalOpen = $derived(
    showSettings || showFontSettings || showAbout || showDictBuilder || showWordListMerger
  );

  function handleSkipToResults(e: MouseEvent) {
    e.preventDefault();
    const firstListbox = document.querySelector<HTMLElement>(
      '[data-testid="results-workspace"] [role="listbox"]'
    );
    firstListbox?.focus();
  }

  const displayedTiledDicts = $derived(
    (searchStore.isSearching || searchStore.hasCompletedSearch) &&
      searchStore.query.trim() !== '' &&
      searchStore.searchActiveDicts.length > 0
      ? searchStore.searchActiveDicts
      : dictStore.active
  );

  const tiledGridClass = $derived.by(() => {
    const n = displayedTiledDicts.length;
    if (n <= 1) return 'grid-cols-1';
    if (n === 2) return 'grid-cols-1 @md:grid-cols-2';
    return 'grid-cols-1 @md:grid-cols-2 @2xl:grid-cols-3';
  });

  async function applyConfigPatch(patch: ConfigPatch, rerunIfSearched = false) {
    await cfgStore.applyPatch(patch);
    searchStore.scheduleValidation(true);
    if (
      rerunIfSearched &&
      searchStore.query.trim() !== '' &&
      (searchStore.hasCompletedSearch || searchStore.isSearching)
    ) {
      void searchStore.execute();
    }
  }

  async function handleResetSettings(resetDictionaries: boolean) {
    await cfgStore.reset(resetDictionaries);
    const nextDicts = await dictStore.load();
    const syncedDicts = dictStore.sync(dictStore.list);
    if (!syncedDicts) {
      searchStore.initActiveDicts(nextDicts);
      searchStore.scheduleValidation(true);
      if (
        searchStore.query.trim() !== '' &&
        (searchStore.hasCompletedSearch || searchStore.isSearching)
      ) {
        void searchStore.execute();
      }
    }
    if (defStore.selectedWord) {
      void defStore.load(defStore.selectedWord, false);
    }
  }

  function handleSelectMatch(m: Match) {
    void defStore.load(m.word, true);
  }

  async function handleDoubleClickMatch(m: Match) {
    await defStore.load(m.word, true);
    if (
      cfgStore.value.autoWebFallback &&
      !m.hasDefinition &&
      (!defStore.lookupResult || (defStore.lookupResult.entries ?? []).length === 0)
    ) {
      await defStore.openWebLookup(m.word, cfgStore.value.defaultWebProvider);
    } else if (cfgStore.value.definitionCollapsed) {
      setDefinitionCollapsed(false);
    }
  }

  function handleToggleCategory(cat: EntryCategory) {
    void applyConfigPatch({ entryFilter: cfgStore.nextEntryFilterForToggle(cat) }, true);
  }

  function setSidebarCollapsed(collapsed: boolean) {
    if (cleanupSidebarDrag) {
      cleanupSidebarDrag();
    }
    void applyConfigPatch({ sidebarCollapsed: collapsed }, false);
  }

  function toggleSidebarCollapsed() {
    setSidebarCollapsed(!cfgStore.value.sidebarCollapsed);
  }

  function getZoomScale(): number {
    return (cfgStore.value.uiZoom || 100) / 100;
  }

  function getMaxSidebarWidth(): number {
    if (typeof window === 'undefined' || !window.innerWidth) {
      return MAX_SIDEBAR_WIDTH;
    }
    const logicalWidth = window.innerWidth / getZoomScale();
    return Math.min(
      MAX_SIDEBAR_WIDTH,
      Math.max(MIN_SIDEBAR_WIDTH, Math.floor(logicalWidth * 0.55))
    );
  }

  function handleSidebarResizeStart(e: MouseEvent) {
    if (e.button !== 0 || cfgStore.value.sidebarCollapsed) return;
    e.preventDefault();

    if (cleanupSidebarDrag) {
      cleanupSidebarDrag();
    }

    const startX = e.clientX;
    const startWidth = cfgStore.value.sidebarWidth;
    let collapsedDuringDrag = false;
    let latestWidth = startWidth;

    isResizingSidebar = true;
    dragSidebarWidth = startWidth;

    const prevCursor = document.body.style.cursor;
    const prevUserSelect = document.body.style.userSelect;
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';

    const finishDrag = (commit: boolean) => {
      window.removeEventListener('mousemove', onMouseMove);
      window.removeEventListener('mouseup', onMouseUp);
      window.removeEventListener('blur', onBlur);
      document.body.style.cursor = prevCursor;
      document.body.style.userSelect = prevUserSelect;
      cleanupSidebarDrag = null;
      isResizingSidebar = false;
      dragSidebarWidth = null;

      if (commit && !collapsedDuringDrag && latestWidth !== cfgStore.value.sidebarWidth) {
        void applyConfigPatch({ sidebarWidth: latestWidth }, false);
      }
    };

    const onMouseMove = (moveEvent: MouseEvent) => {
      const rawWidth = startWidth + (moveEvent.clientX - startX) / getZoomScale();
      if (rawWidth < SIDEBAR_COLLAPSE_THRESHOLD) {
        collapsedDuringDrag = true;
        finishDrag(false);
        void applyConfigPatch({ sidebarCollapsed: true }, false);
        return;
      }
      const maxAllowed = getMaxSidebarWidth();
      latestWidth = Math.max(MIN_SIDEBAR_WIDTH, Math.min(maxAllowed, Math.round(rawWidth)));
      dragSidebarWidth = latestWidth;
    };

    const onMouseUp = () => finishDrag(true);
    const onBlur = () => finishDrag(true);

    cleanupSidebarDrag = () => finishDrag(false);
    window.addEventListener('mousemove', onMouseMove);
    window.addEventListener('mouseup', onMouseUp);
    window.addEventListener('blur', onBlur);
  }

  function handleSidebarResizeReset() {
    void applyConfigPatch({ sidebarWidth: DEFAULT_SIDEBAR_WIDTH }, false);
  }

  function handleSidebarResizeKeydown(e: KeyboardEvent) {
    const step = e.shiftKey ? 32 : 16;
    const maxAllowed = getMaxSidebarWidth();
    if (e.key === 'ArrowLeft') {
      e.preventDefault();
      const next = Math.max(MIN_SIDEBAR_WIDTH, cfgStore.value.sidebarWidth - step);
      void applyConfigPatch({ sidebarWidth: next }, false);
    } else if (e.key === 'ArrowRight') {
      e.preventDefault();
      const next = Math.min(maxAllowed, cfgStore.value.sidebarWidth + step);
      void applyConfigPatch({ sidebarWidth: next }, false);
    } else if (e.key === 'Home') {
      e.preventDefault();
      void applyConfigPatch({ sidebarWidth: MIN_SIDEBAR_WIDTH }, false);
    } else if (e.key === 'End') {
      e.preventDefault();
      void applyConfigPatch({ sidebarWidth: maxAllowed }, false);
    }
  }

  function setDefinitionCollapsed(collapsed: boolean) {
    if (cleanupDefinitionDrag) {
      cleanupDefinitionDrag();
    }
    void applyConfigPatch({ definitionCollapsed: collapsed }, false);
  }

  function toggleDefinitionCollapsed() {
    setDefinitionCollapsed(!cfgStore.value.definitionCollapsed);
  }

  function getMaxDefinitionHeight(): number {
    if (typeof window === 'undefined' || !window.innerHeight) {
      return MAX_DEFINITION_HEIGHT;
    }
    const logicalHeight = window.innerHeight / getZoomScale();
    return Math.min(
      MAX_DEFINITION_HEIGHT,
      Math.max(MIN_DEFINITION_HEIGHT, Math.floor(logicalHeight * 0.6))
    );
  }

  function handleDefinitionResizeStart(e: MouseEvent) {
    if (e.button !== 0 || cfgStore.value.definitionCollapsed) return;
    e.preventDefault();

    if (cleanupDefinitionDrag) {
      cleanupDefinitionDrag();
    }

    const startY = e.clientY;
    const startHeight = cfgStore.value.definitionHeight;
    let collapsedDuringDrag = false;
    let latestHeight = startHeight;

    isResizingDefinition = true;
    dragDefinitionHeight = startHeight;

    const prevCursor = document.body.style.cursor;
    const prevUserSelect = document.body.style.userSelect;
    document.body.style.cursor = 'row-resize';
    document.body.style.userSelect = 'none';

    const finishDrag = (commit: boolean) => {
      window.removeEventListener('mousemove', onMouseMove);
      window.removeEventListener('mouseup', onMouseUp);
      window.removeEventListener('blur', onBlur);
      document.body.style.cursor = prevCursor;
      document.body.style.userSelect = prevUserSelect;
      cleanupDefinitionDrag = null;
      isResizingDefinition = false;
      dragDefinitionHeight = null;

      if (commit && !collapsedDuringDrag && latestHeight !== cfgStore.value.definitionHeight) {
        void applyConfigPatch({ definitionHeight: latestHeight }, false);
      }
    };

    const onMouseMove = (moveEvent: MouseEvent) => {
      const rawHeight = startHeight + (startY - moveEvent.clientY) / getZoomScale();
      if (rawHeight < DEFINITION_COLLAPSE_THRESHOLD) {
        collapsedDuringDrag = true;
        finishDrag(false);
        void applyConfigPatch({ definitionCollapsed: true }, false);
        return;
      }
      const maxAllowed = getMaxDefinitionHeight();
      latestHeight = Math.max(MIN_DEFINITION_HEIGHT, Math.min(maxAllowed, Math.round(rawHeight)));
      dragDefinitionHeight = latestHeight;
    };

    const onMouseUp = () => finishDrag(true);
    const onBlur = () => finishDrag(true);

    cleanupDefinitionDrag = () => finishDrag(false);
    window.addEventListener('mousemove', onMouseMove);
    window.addEventListener('mouseup', onMouseUp);
    window.addEventListener('blur', onBlur);
  }

  function handleDefinitionResizeReset() {
    void applyConfigPatch({ definitionHeight: DEFAULT_DEFINITION_HEIGHT }, false);
  }

  function handleDefinitionResizeKeydown(e: KeyboardEvent) {
    const step = e.shiftKey ? 32 : 16;
    const maxAllowed = getMaxDefinitionHeight();
    if (e.key === 'ArrowUp') {
      e.preventDefault();
      const next = Math.min(maxAllowed, cfgStore.value.definitionHeight + step);
      void applyConfigPatch({ definitionHeight: next }, false);
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      const next = Math.max(MIN_DEFINITION_HEIGHT, cfgStore.value.definitionHeight - step);
      void applyConfigPatch({ definitionHeight: next }, false);
    } else if (e.key === 'Home') {
      e.preventDefault();
      void applyConfigPatch({ definitionHeight: MIN_DEFINITION_HEIGHT }, false);
    } else if (e.key === 'End') {
      e.preventDefault();
      void applyConfigPatch({ definitionHeight: maxAllowed }, false);
    }
  }

  function handleWindowKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      if (closeTopModalOnEscape()) {
        e.preventDefault();
        return;
      }
      if (showAbout) {
        showAbout = false;
      } else if (showWordListMerger) {
        showWordListMerger = false;
      } else if (showDictBuilder) {
        showDictBuilder = false;
      } else if (showFontSettings) {
        showFontSettings = false;
      } else if (showSettings) {
        showSettings = false;
      } else if (showCheatsheet) {
        showCheatsheet = false;
      } else if (searchStore.isSearching) {
        void searchStore.cancel();
      }
      return;
    }

    // Native Wails menu accelerators own these shortcuts on desktop; keep webview fallback for server mode and tests.
    if (isDesktopWebview()) {
      return;
    }

    const mod = e.ctrlKey || e.metaKey;
    if (mod && e.key.toLowerCase() === 'w') {
      e.preventDefault();
      searchStore.mode = 'word';
      patternBarRef?.focusInput();
      searchStore.scheduleValidation(true);
    } else if (mod && e.key.toLowerCase() === 'f') {
      e.preventDefault();
      searchStore.mode = 'fts';
      patternBarRef?.focusInput();
      searchStore.scheduleValidation(true);
    } else if (mod && e.shiftKey && e.key.toLowerCase() === 'b') {
      e.preventDefault();
      showDictBuilder = true;
    } else if (mod && e.shiftKey && e.key.toLowerCase() === 'm') {
      e.preventDefault();
      showWordListMerger = true;
    } else if (mod && !e.shiftKey && e.key.toLowerCase() === 'b') {
      e.preventDefault();
      toggleSidebarCollapsed();
    } else if (mod && !e.shiftKey && e.key.toLowerCase() === 'd') {
      e.preventDefault();
      toggleDefinitionCollapsed();
    } else if (mod && e.key === ',') {
      e.preventDefault();
      showSettings = true;
    } else if (mod && e.key === '1') {
      e.preventDefault();
      void applyConfigPatch({ layoutMode: 'tiled' }, false);
    } else if (mod && e.key === '2') {
      e.preventDefault();
      void applyConfigPatch({ layoutMode: 'unified' }, false);
    } else if (mod && (e.key === '=' || e.key === '+')) {
      e.preventDefault();
      void applyConfigPatch({ uiZoom: cfgStore.nextZoomForAction('in') }, false);
    } else if (mod && e.key === '-') {
      e.preventDefault();
      void applyConfigPatch({ uiZoom: cfgStore.nextZoomForAction('out') }, false);
    } else if (mod && e.key === '0') {
      e.preventDefault();
      void applyConfigPatch({ uiZoom: cfgStore.nextZoomForAction('reset') }, false);
    } else if (e.key === 'F1') {
      e.preventDefault();
      showCheatsheet = !showCheatsheet;
    }
  }

  onMount(() => {
    const backend = getBackend();
    const unsubs: (() => void)[] = [
      searchStore.subscribe(),
      dictStore.subscribe(),
      cfgStore.subscribeSystemTheme(),
      backend.onEvent('ui:open-settings', () => {
        showSettings = true;
      }),
      backend.onEvent('ui:open-font-settings', () => {
        showFontSettings = true;
      }),
      backend.onEvent('ui:open-about', () => {
        showAbout = true;
      }),
      backend.onEvent('ui:open-dict-builder', () => {
        showDictBuilder = true;
      }),
      backend.onEvent('ui:open-wordlist-merge', () => {
        showWordListMerger = true;
      }),
      backend.onEvent('ui:set-mode', (nextMode) => {
        if (nextMode === 'word' || nextMode === 'fts') {
          searchStore.mode = nextMode;
          patternBarRef?.focusInput();
          searchStore.scheduleValidation(true);
        }
      }),
      backend.onEvent('ui:set-layout', (nextLayout) => {
        if (nextLayout === 'tiled' || nextLayout === 'unified') {
          void applyConfigPatch({ layoutMode: nextLayout }, false);
        }
      }),
      backend.onEvent('ui:toggle-cheatsheet', () => {
        showCheatsheet = !showCheatsheet;
      }),
      backend.onEvent('ui:toggle-sidebar', () => {
        toggleSidebarCollapsed();
      }),
      backend.onEvent('ui:toggle-definitions', () => {
        toggleDefinitionCollapsed();
      }),
      backend.onEvent('ui:set-theme', (nextTheme) => {
        if (nextTheme === 'dark' || nextTheme === 'light' || nextTheme === 'system') {
          void applyConfigPatch({ theme: nextTheme }, false);
        }
      }),
      backend.onEvent('ui:zoom', (act) => {
        if (act === 'in' || act === 'out' || act === 'reset') {
          void applyConfigPatch({ uiZoom: cfgStore.nextZoomForAction(act) }, false);
        }
      })
    ];

    void (async () => {
      const [, activeDicts] = await Promise.all([cfgStore.load(), dictStore.load()]);
      searchStore.initActiveDicts(activeDicts);
      if (!hasActiveModal()) {
        patternBarRef?.focusInput();
      }
    })();

    return () => {
      for (const off of unsubs) off();
      searchStore.dispose();
      if (cleanupSidebarDrag) cleanupSidebarDrag();
      if (cleanupDefinitionDrag) cleanupDefinitionDrag();
    };
  });

  function handleWindowResize() {
    if (typeof window !== 'undefined') {
      viewportWidth = window.innerWidth || 1320;
      viewportHeight = window.innerHeight || 840;
    }
  }
</script>

<svelte:window onkeydown={handleWindowKeydown} onresize={handleWindowResize} />

<div
  data-testid="app-root"
  style={cfgStore.rootFontVars}
  class="flex h-full w-full flex-col overflow-hidden bg-slate-950 text-slate-100"
>
  <div
    class="flex min-h-0 flex-1 flex-col overflow-hidden"
    inert={anyModalOpen ? true : undefined}
    aria-hidden={anyModalOpen ? 'true' : undefined}
  >
    <a
      href="#results-workspace"
      onclick={handleSkipToResults}
      data-testid="skip-to-results-link"
      class="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50 focus:rounded-lg focus:bg-amber-500 focus:px-3 focus:py-1.5 focus:text-xs focus:font-semibold focus:text-neutral-950 focus:shadow-lg"
    >
      Skip to results
    </a>

    <!-- Top Pattern Bar -->
  <PatternBar
    bind:this={patternBarRef}
    query={searchStore.query}
    mode={searchStore.mode}
    enumeration={searchStore.enumeration}
    isSearching={searchStore.isSearching}
    validation={searchStore.validation}
    recentQueries={cfgStore.value.recentQueries ?? []}
    {showCheatsheet}
    onQueryChange={(q) => {
      searchStore.query = q;
      searchStore.scheduleValidation(false);
    }}
    onModeChange={(m) => {
      searchStore.mode = m;
      searchStore.scheduleValidation(true);
    }}
    onEnumerationChange={(e) => {
      searchStore.enumeration = e;
      searchStore.scheduleValidation(false);
    }}
    onSubmit={() => searchStore.execute()}
    onCancel={() => searchStore.cancel()}
    onToggleCheatsheet={() => (showCheatsheet = !showCheatsheet)}
    onOpenSettings={() => (showSettings = true)}
    onOpenAbout={() => (showAbout = true)}
  />

  <!-- Filter, Length, Sort & Tiled/Unified Layout Toolbar -->
  <FilterToolbar
    minLength={cfgStore.value.minLength}
    maxLength={cfgStore.value.maxLength}
    entryCategories={cfgStore.value.entryFilter}
    suppressDuplicates={cfgStore.value.suppressDuplicates}
    sortMode={cfgStore.value.sortMode}
    layoutMode={cfgStore.value.layoutMode}
    sidebarCollapsed={cfgStore.value.sidebarCollapsed}
    definitionCollapsed={cfgStore.value.definitionCollapsed}
    activeDictCount={dictStore.activeCount}
    totalDictCount={dictStore.list.length}
    onLengthChange={(min, max) => applyConfigPatch({ minLength: min, maxLength: max }, true)}
    onToggleCategory={handleToggleCategory}
    onToggleSuppressDuplicates={() =>
      applyConfigPatch({ suppressDuplicates: !cfgStore.value.suppressDuplicates }, true)}
    onSortChange={(s: SortMode) => applyConfigPatch({ sortMode: s }, true)}
    onLayoutChange={(l: LayoutMode) => applyConfigPatch({ layoutMode: l }, false)}
    onToggleSidebar={toggleSidebarCollapsed}
    onToggleDefinitions={toggleDefinitionCollapsed}
  />

  <!-- Main Workspace: Dictionary Sidebar + Results Workspace + Definition Pane -->
  <div class="relative flex min-h-0 flex-1 overflow-hidden">
    {#if isCompactViewport && !cfgStore.value.sidebarCollapsed}
      <!-- Compact/mobile backdrop when dictionary sidebar is open as an overlay drawer -->
      <button
        type="button"
        aria-label="Close dictionaries panel"
        data-testid="sidebar-drawer-backdrop"
        onclick={() => setSidebarCollapsed(true)}
        class="absolute inset-0 z-20 bg-black/50 backdrop-blur-[1px]"
      ></button>
      <div class="absolute inset-y-0 left-0 z-30 flex h-full shadow-2xl" data-testid="sidebar-drawer-container">
        <DictionarySidebar
          dictionaries={dictStore.list}
          width={effectiveSidebarWidth}
          collapsed={false}
          showBalances={cfgStore.value.showBalances}
          showAnnotations={cfgStore.value.showAnnotations}
          exclusiveVars={cfgStore.value.exclusiveVars}
          ignoreUnusedPunctuation={cfgStore.value.ignoreUnusedPunctuation}
          onToggleEnabled={(id, enabled) => dictStore.setEnabled(id, enabled)}
          onReorder={(ids) => dictStore.reorder(ids)}
          onBrowseAdd={() => dictStore.browseAdd()}
          onBrowseAddDirectory={() => dictStore.browseAddDirectory()}
          onOpenBuilder={() => (showDictBuilder = true)}
          onOpenMerger={() => (showWordListMerger = true)}
          onRemove={(id) => dictStore.remove(id)}
          onCollapse={() => setSidebarCollapsed(true)}
          onOptionChange={(patch) => applyConfigPatch(patch, true)}
        />
      </div>
    {:else}
      <DictionarySidebar
        dictionaries={dictStore.list}
        width={effectiveSidebarWidth}
        collapsed={cfgStore.value.sidebarCollapsed}
        showBalances={cfgStore.value.showBalances}
        showAnnotations={cfgStore.value.showAnnotations}
        exclusiveVars={cfgStore.value.exclusiveVars}
        ignoreUnusedPunctuation={cfgStore.value.ignoreUnusedPunctuation}
        onToggleEnabled={(id, enabled) => dictStore.setEnabled(id, enabled)}
        onReorder={(ids) => dictStore.reorder(ids)}
        onBrowseAdd={() => dictStore.browseAdd()}
        onBrowseAddDirectory={() => dictStore.browseAddDirectory()}
        onOpenBuilder={() => (showDictBuilder = true)}
        onOpenMerger={() => (showWordListMerger = true)}
        onRemove={(id) => dictStore.remove(id)}
        onCollapse={() => setSidebarCollapsed(true)}
        onOptionChange={(patch) => applyConfigPatch(patch, true)}
      />

      {#if !cfgStore.value.sidebarCollapsed}
        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
        <div
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize dictionaries panel"
          aria-valuenow={effectiveSidebarWidth}
          aria-valuemin={MIN_SIDEBAR_WIDTH}
          aria-valuemax={MAX_SIDEBAR_WIDTH}
          tabindex="0"
          data-testid="sidebar-resizer"
          title="Drag to resize dictionaries panel (double-click to reset, drag left to collapse)"
          onmousedown={handleSidebarResizeStart}
          ondblclick={handleSidebarResizeReset}
          onkeydown={handleSidebarResizeKeydown}
          class="group relative z-20 -ml-1 w-2 shrink-0 cursor-col-resize select-none focus:outline-none focus-visible:ring-2 focus-visible:ring-amber-400"
        >
          <div
            class="mx-auto h-full w-0.5 transition-colors {isResizingSidebar
              ? 'bg-amber-400'
              : 'bg-transparent group-hover:bg-amber-400/60 group-focus-visible:bg-amber-400'}"
          ></div>
        </div>
      {/if}
    {/if}

    <main class="flex min-w-0 flex-1 flex-col overflow-hidden">
      <!-- Results Area: Separate Tiled Panes per Dictionary (Default) vs Unified Merged List -->
      <div id="results-workspace" class="@container min-h-0 flex-1 overflow-hidden p-2.5" data-testid="results-workspace">
        {#if cfgStore.value.layoutMode === 'tiled'}
          {#if displayedTiledDicts.length === 0}
            <div class="flex h-full flex-col items-center justify-center gap-2 rounded-lg border border-dashed border-slate-700 text-xs text-slate-400">
              <span>Enable at least one dictionary in the left sidebar to search.</span>
              {#if cfgStore.value.sidebarCollapsed}
                <button
                  type="button"
                  data-testid="empty-expand-sidebar-btn"
                  onclick={toggleSidebarCollapsed}
                  class="rounded bg-amber-500/20 px-2.5 py-1 font-medium text-amber-300 hover:bg-amber-500/30"
                >
                  Show Dictionaries Panel
                </button>
              {/if}
            </div>
          {:else}
            <div
              data-testid="tiled-panes-grid"
              class="grid h-full min-h-0 auto-rows-fr gap-2.5 {tiledGridClass}"
            >
              {#each displayedTiledDicts as dict, paneIdx (dict.id)}
                <VirtualResultPane
                  title={dict.name}
                  subtitle={`${dict.wordCount.toLocaleString()} words`}
                  items={searchStore.matchesByDict[paneIdx] ?? []}
                  selectedWord={defStore.selectedWord}
                  showDictBadge={false}
                  showBalances={cfgStore.value.showBalances}
                  showAnnotations={cfgStore.value.showAnnotations}
                  fontSize={cfgStore.value.fonts.results.size}
                  testId={`tiled-pane-${paneIdx}`}
                  emptyMessage={searchStore.hasCompletedSearch
                    ? `No matches in ${dict.name}`
                    : `Ready · ${dict.name}`}
                  onSelect={handleSelectMatch}
                  onDoubleClick={handleDoubleClickMatch}
                />
              {/each}
            </div>
          {/if}
        {:else}
          <VirtualResultPane
            title="Unified Results"
            subtitle={`${displayedTiledDicts.length} active ${displayedTiledDicts.length === 1 ? 'dictionary' : 'dictionaries'}`}
            items={searchStore.allMatches}
            selectedWord={defStore.selectedWord}
            showDictBadge={true}
            showBalances={cfgStore.value.showBalances}
            showAnnotations={cfgStore.value.showAnnotations}
            fontSize={cfgStore.value.fonts.results.size}
            testId="unified-pane"
            emptyMessage={searchStore.hasCompletedSearch
              ? 'No matching entries found across active dictionaries'
              : 'Enter a crossword, anagram, or full-text pattern above and press Enter'}
            onSelect={handleSelectMatch}
            onDoubleClick={handleDoubleClickMatch}
          />
        {/if}
      </div>

      {#if !cfgStore.value.definitionCollapsed}
        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
        <div
          role="separator"
          aria-orientation="horizontal"
          aria-label="Resize definitions panel"
          aria-valuenow={effectiveDefinitionHeight}
          aria-valuemin={MIN_DEFINITION_HEIGHT}
          aria-valuemax={MAX_DEFINITION_HEIGHT}
          tabindex="0"
          data-testid="definition-resizer"
          title="Drag to resize definitions panel (double-click to reset, drag down to collapse)"
          onmousedown={handleDefinitionResizeStart}
          ondblclick={handleDefinitionResizeReset}
          onkeydown={handleDefinitionResizeKeydown}
          class="group relative z-20 -mt-1 -mb-1 flex h-2 shrink-0 cursor-row-resize items-center select-none focus:outline-none focus-visible:ring-2 focus-visible:ring-amber-400"
        >
          <div
            class="h-0.5 w-full transition-colors {isResizingDefinition
              ? 'bg-amber-400'
              : 'bg-transparent group-hover:bg-amber-400/60 group-focus-visible:bg-amber-400'}"
          ></div>
        </div>
      {/if}

      <!-- Definitions & Web Search Fallback Pane -->
      <DefinitionPane
        height={effectiveDefinitionHeight}
        collapsed={cfgStore.value.definitionCollapsed}
        selectedWord={defStore.selectedWord}
        lookupResult={defStore.lookupResult}
        isLoading={defStore.isLoading}
        includeMentions={defStore.includeMentions}
        canGoBack={defStore.canGoBack}
        canGoForward={defStore.canGoForward}
        defaultWebProvider={cfgStore.value.defaultWebProvider}
        onBack={() => defStore.back()}
        onForward={() => defStore.forward()}
        onToggleIncludeMentions={(v) => defStore.setIncludeMentions(v)}
        onCrossLookupWord={(w) => defStore.load(w, true)}
        onOpenWebLookup={(w, pid) => {
          void defStore.openWebLookup(w, pid);
        }}
        onOpenExternalURL={(u) => {
          void defStore.openExternalURL(u);
        }}
        onCollapse={() => setDefinitionCollapsed(true)}
      />
    </main>
  </div>

  <!-- Bottom Status Bar -->
  <footer
    data-testid="status-bar"
    style="font-family: var(--tisane-font-status-family); font-size: var(--tisane-font-status-size);"
    class="flex items-center justify-between border-t border-slate-800 bg-slate-950 px-4 py-1 text-slate-400 select-none"
  >
    <div class="flex items-center gap-3 truncate">
      {#if searchStore.errorMessage}
        <span role="alert" class="font-medium text-rose-400">Error: {searchStore.errorMessage}</span>
        <button
          type="button"
          data-testid="dismiss-error-btn"
          onclick={() => (searchStore.errorMessage = '')}
          class="rounded px-1 text-[10px] text-rose-300/80 hover:bg-rose-500/20 hover:text-rose-200"
          aria-label="Dismiss error"
        >
          <span aria-hidden="true">✕</span>
        </button>
      {:else if searchStore.isSearching}
        <span class="flex items-center gap-1.5 text-amber-300">
          <span aria-hidden="true" class="inline-block h-1.5 w-1.5 animate-ping rounded-full bg-amber-400"></span>
          Scanning… {searchStore.allMatches.length.toLocaleString()} matches ({searchStore.wordsScanned.toLocaleString()}
          {#if searchStore.totalCandidateWords > 0}/ {searchStore.totalCandidateWords.toLocaleString()}{/if} words)
        </span>
      {:else if searchStore.hasCompletedSearch}
        <span class="text-slate-300">
          <strong class="font-mono text-amber-300">{searchStore.allMatches.length.toLocaleString()}</strong>
          {searchStore.allMatches.length === 1 ? 'match' : 'matches'}
        </span>
        <span aria-hidden="true">·</span>
        <span>
          Scanned {searchStore.wordsScanned.toLocaleString()} words in
          <strong class="font-mono text-slate-200">{searchStore.elapsedMs.toFixed(1)} ms</strong>
        </span>
        {#if searchStore.usedIndexedFTS}
          <span class="rounded bg-sky-500/15 px-1.5 py-0.2 text-[10px] text-sky-300">
            Indexed FTS
          </span>
        {/if}
      {:else}
        <span>Ready · Word Search (Ctrl+W) · Full Text Search (Ctrl+F)</span>
      {/if}
    </div>

    <!-- Screen-reader live announcement on search completion (avoids 25ms streaming progress chatter) -->
    <div role="status" aria-live="polite" aria-atomic="true" class="sr-only" data-testid="sr-search-status">
      {#if searchStore.hasCompletedSearch && !searchStore.isSearching && !searchStore.errorMessage}
        Search complete: {searchStore.allMatches.length.toLocaleString()} {searchStore.allMatches.length === 1 ? 'match' : 'matches'} found across {displayedTiledDicts.length} {displayedTiledDicts.length === 1 ? 'dictionary' : 'dictionaries'}.
      {/if}
    </div>

    <div
      style="font-family: var(--tisane-font-status-family); font-size: var(--tisane-font-status-size);"
      class="hidden shrink-0 items-center gap-3 text-slate-400 sm:flex"
    >
      <span>@={cfgStore.value.atMacro}</span>
      <span>#={cfgStore.value.hashMacro}</span>
      <span>Len {cfgStore.value.minLength}–{cfgStore.value.maxLength}</span>
      <span class="uppercase">{cfgStore.value.layoutMode}</span>
    </div>
  </footer>
  </div>

  <!-- Preferences Modal -->
  <SettingsModal
    open={showSettings}
    config={cfgStore.value}
    onClose={() => (showSettings = false)}
    onSave={(patch) => applyConfigPatch(patch, false)}
    onReset={(resetDicts) => void handleResetSettings(resetDicts)}
  />

  <!-- Direct Font Settings Modal (when opened via View menu) -->
  <FontSettingsModal
    open={showFontSettings}
    fonts={cfgStore.value.fonts}
    onClose={() => (showFontSettings = false)}
    onSave={(nextFonts) => applyConfigPatch({ fonts: nextFonts }, false)}
  />

  <!-- About Tisane Modal (opened by clicking app icon in upper-left corner) -->
  <AboutModal
    open={showAbout}
    onClose={() => (showAbout = false)}
    onOpenExternalURL={(u) => {
      void defStore.openExternalURL(u);
    }}
  />

  <!-- Build Dictionary from Word List Modal -->
  <BuildDictionaryModal
    open={showDictBuilder}
    onClose={() => (showDictBuilder = false)}
  />

  <!-- Merge Word Lists Modal -->
  <MergeWordListsModal
    open={showWordListMerger}
    onClose={() => (showWordListMerger = false)}
  />
</div>
