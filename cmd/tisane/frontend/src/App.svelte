<script lang="ts">
  import { onMount } from 'svelte';
  import { getBackend } from './lib/bindings';
  import { buildFontCssVars, DEFAULT_FONTS, normalizeFontConfig } from './lib/fonts';
  import type {
    Config,
    ConfigPatch,
    DictionaryInfo,
    EntryCategory,
    LayoutMode,
    LookupResult,
    Match,
    SearchBatchEvent,
    SearchCompleteEvent,
    SearchErrorEvent,
    SearchMode,
    SearchProgressEvent,
    SortMode,
    ValidationResult
  } from './lib/types';

  import PatternBar from './lib/components/PatternBar.svelte';
  import FilterToolbar from './lib/components/FilterToolbar.svelte';
  import DictionarySidebar from './lib/components/DictionarySidebar.svelte';
  import VirtualResultPane from './lib/components/VirtualResultPane.svelte';
  import DefinitionPane from './lib/components/DefinitionPane.svelte';
  import SettingsModal from './lib/components/SettingsModal.svelte';
  import FontSettingsModal from './lib/components/FontSettingsModal.svelte';
  import AboutModal from './lib/components/AboutModal.svelte';

  const DEFAULT_CONFIG: Config = {
    dictDirs: [],
    dictionaries: [],
    atMacro: '[aeiou]',
    hashMacro: '[^aeiou]',
    minLength: 1,
    maxLength: 100,
    exclusiveVars: false,
    ignoreUnusedPunctuation: true,
    showBalances: true,
    showAnnotations: true,
    suppressDuplicates: true,
    entryFilter: ['lowercase', 'capitalized', 'punctuated', 'phrase'],
    sortMode: 'dict',
    layoutMode: 'tiled',
    autoWebFallback: false,
    defaultWebProvider: 'onelook',
    webProviders: [
      { id: 'onelook', name: 'OneLook', urlTemplate: 'https://www.onelook.com/?w={entry}' },
      { id: 'wiktionary', name: 'Wiktionary', urlTemplate: 'https://en.wiktionary.org/wiki/{entry}' },
      { id: 'wikipedia', name: 'Wikipedia', urlTemplate: 'https://en.wikipedia.org/wiki/Special:Search?search={entry}' },
      { id: 'google', name: 'Google Define', urlTemplate: 'https://www.google.com/search?q=define+{entry}' },
      { id: 'merriam-webster', name: 'Merriam-Webster', urlTemplate: 'https://www.merriam-webster.com/dictionary/{entry}' }
    ],
    recentQueries: [],
    fonts: normalizeFontConfig(DEFAULT_FONTS)
  };

  function normalizeConfig(raw: Partial<Config> | null | undefined): Config {
    if (!raw) return { ...DEFAULT_CONFIG, fonts: normalizeFontConfig(DEFAULT_FONTS) };
    return {
      ...DEFAULT_CONFIG,
      ...raw,
      dictDirs: Array.isArray(raw.dictDirs) ? raw.dictDirs : DEFAULT_CONFIG.dictDirs,
      dictionaries: Array.isArray(raw.dictionaries) ? raw.dictionaries : DEFAULT_CONFIG.dictionaries,
      entryFilter: Array.isArray(raw.entryFilter) ? raw.entryFilter : DEFAULT_CONFIG.entryFilter,
      webProviders: Array.isArray(raw.webProviders) ? raw.webProviders : DEFAULT_CONFIG.webProviders,
      recentQueries: Array.isArray(raw.recentQueries) ? raw.recentQueries : DEFAULT_CONFIG.recentQueries,
      fonts: normalizeFontConfig(raw.fonts)
    };
  }

  // Core reactive state (Svelte 5 runes)
  let config: Config = $state(normalizeConfig(DEFAULT_CONFIG));
  let dictionaries: DictionaryInfo[] = $state([]);

  let query = $state('');
  let mode: SearchMode = $state('word');
  let enumeration = $state('');
  let validation: ValidationResult | null = $state(null);

  let showCheatsheet = $state(false);
  let showSettings = $state(false);
  let showFontSettings = $state(false);
  let showAbout = $state(false);

  const rootFontVars = $derived(buildFontCssVars(config.fonts));

  // Active search & streaming results state
  let currentSearchId = $state(0);
  let isSearching = $state(false);
  let hasCompletedSearch = $state(false);
  let errorMessage = $state('');

  let allMatches: Match[] = $state.raw([]);
  let matchesByDict: Match[][] = $state.raw([]);
  let searchActiveDicts: DictionaryInfo[] = $state([]);

  let wordsScanned = $state(0);
  let totalCandidateWords = $state(0);
  let elapsedMs = $state(0);
  let usedIndexedFTS = $state(false);

  // Definition lookup & history state
  let selectedWord: string | null = $state(null);
  let lookupResult: LookupResult | null = $state(null);
  let isLoadingDef = $state(false);
  let includeMentions = $state(false);
  let historyStack: string[] = $state([]);
  let historyIndex = $state(-1);

  let patternBarRef: ReturnType<typeof PatternBar> | undefined = $state();
  let validateTimer: ReturnType<typeof setTimeout> | null = null;

  const displayedTiledDicts = $derived(
    (isSearching || hasCompletedSearch) && query.trim() !== '' && searchActiveDicts.length > 0
      ? searchActiveDicts
      : (dictionaries ?? []).filter((d) => d.enabled)
  );

  const tiledGridClass = $derived.by(() => {
    const n = displayedTiledDicts.length;
    if (n <= 1) return 'grid-cols-1';
    if (n === 2) return 'grid-cols-1 md:grid-cols-2';
    if (n === 3) return 'grid-cols-1 md:grid-cols-3';
    return 'grid-cols-1 md:grid-cols-2 xl:grid-cols-3';
  });

  function scheduleValidation(immediate = false) {
    if (validateTimer) {
      clearTimeout(validateTimer);
      validateTimer = null;
    }
    const q = query.trim();
    if (!q) {
      validation = null;
      return;
    }
    const run = async () => {
      try {
        const res = await getBackend().validatePattern({
          query: q,
          mode,
          enumeration,
          minLength: config.minLength,
          maxLength: config.maxLength,
          atMacro: config.atMacro,
          hashMacro: config.hashMacro,
          exclusiveVars: config.exclusiveVars,
          ignoreUnusedPunctuation: config.ignoreUnusedPunctuation
        });
        if (query.trim() === q) {
          validation = res;
        }
      } catch {
        // Ignore transient validation transport errors
      }
    };
    if (immediate) {
      void run();
    } else {
      validateTimer = setTimeout(run, 60);
    }
  }

  async function executeSearch() {
    const q = query.trim();
    if (!q) return;

    errorMessage = '';
    const activeSnapshot = (dictionaries ?? []).filter((d) => d.enabled);
    searchActiveDicts = activeSnapshot;
    allMatches = [];
    matchesByDict = activeSnapshot.map(() => []);
    wordsScanned = 0;
    totalCandidateWords = 0;
    elapsedMs = 0;
    usedIndexedFTS = false;
    isSearching = true;
    hasCompletedSearch = false;

    try {
      const sid = await getBackend().startSearch({
        query: q,
        mode,
        enumeration,
        minLength: config.minLength,
        maxLength: config.maxLength,
        atMacro: config.atMacro,
        hashMacro: config.hashMacro,
        exclusiveVars: config.exclusiveVars,
        ignoreUnusedPunctuation: config.ignoreUnusedPunctuation,
        showBalances: config.showBalances,
        showAnnotations: config.showAnnotations,
        suppressDuplicates: config.suppressDuplicates,
        entryCategories: config.entryFilter ?? DEFAULT_CONFIG.entryFilter,
        sortMode: config.sortMode,
        limit: 0
      });
      currentSearchId = sid;

      // Update local recent queries immediately
      const prevRecent = Array.isArray(config.recentQueries) ? config.recentQueries : [];
      config = {
        ...config,
        recentQueries: [q, ...prevRecent.filter((item) => item !== q)].slice(0, 50)
      };
    } catch (err) {
      isSearching = false;
      errorMessage = err instanceof Error ? err.message : String(err);
    }
  }

  async function cancelActiveSearch() {
    isSearching = false;
    try {
      await getBackend().cancelSearch();
    } catch {
      // Ignore
    }
  }

  async function loadDefinition(word: string, pushHistory = true) {
    const clean = word.trim();
    if (!clean) return;

    selectedWord = clean;
    isLoadingDef = true;

    if (pushHistory) {
      const base = historyStack.slice(0, historyIndex + 1);
      if (base[base.length - 1] !== clean) {
        base.push(clean);
      }
      historyStack = base;
      historyIndex = base.length - 1;
    }

    try {
      const res = await getBackend().getDefinitions(clean, includeMentions);
      if (selectedWord === clean) {
        lookupResult = res
          ? {
              word: res.word ?? clean,
              entries: Array.isArray(res.entries) ? res.entries : [],
              webLinks: Array.isArray(res.webLinks) ? res.webLinks : []
            }
          : { word: clean, entries: [], webLinks: [] };
      }
    } catch {
      if (selectedWord === clean) {
        lookupResult = { word: clean, entries: [], webLinks: [] };
      }
    } finally {
      if (selectedWord === clean) {
        isLoadingDef = false;
      }
    }
  }

  function handleSelectMatch(m: Match) {
    void loadDefinition(m.word, true);
  }

  async function handleDoubleClickMatch(m: Match) {
    await loadDefinition(m.word, true);
    if (config.autoWebFallback && !m.hasDefinition && (!lookupResult || (lookupResult.entries ?? []).length === 0)) {
      await getBackend().openWebLookup(m.word, config.defaultWebProvider);
    }
  }

  function handleHistoryBack() {
    if (historyIndex > 0) {
      historyIndex -= 1;
      void loadDefinition(historyStack[historyIndex], false);
    }
  }

  function handleHistoryForward() {
    if (historyIndex < historyStack.length - 1) {
      historyIndex += 1;
      void loadDefinition(historyStack[historyIndex], false);
    }
  }

  async function applyConfigPatch(patch: ConfigPatch, rerunIfSearched = false) {
    // Optimistically update local config for crisp UI responsiveness
    config = normalizeConfig({ ...config, ...patch });
    try {
      const updated = await getBackend().updateConfig(patch);
      if (updated) {
        config = normalizeConfig(updated);
      }
    } catch {
      // Keep optimistic state if headless
    }
    scheduleValidation(true);
    if (rerunIfSearched && query.trim() !== '' && (hasCompletedSearch || isSearching)) {
      void executeSearch();
    }
  }

  async function handleResetSettings(resetDictionaries: boolean) {
    config = normalizeConfig({
      ...DEFAULT_CONFIG,
      dictionaries: resetDictionaries ? [] : config.dictionaries
    });
    let syncedDicts = false;
    try {
      const updated = await getBackend().resetConfig(resetDictionaries);
      if (updated) {
        config = normalizeConfig(updated);
      }
      const nextDicts = await getBackend().listDictionaries();
      if (Array.isArray(nextDicts)) {
        syncUpdatedDictionaries(nextDicts);
        syncedDicts = true;
      }
    } catch {
      // Keep optimistic state if headless
    }
    if (!syncedDicts) {
      scheduleValidation(true);
      if (query.trim() !== '' && (hasCompletedSearch || isSearching)) {
        void executeSearch();
      }
    }
    if (selectedWord) {
      void loadDefinition(selectedWord, false);
    }
  }

  function syncUpdatedDictionaries(next: DictionaryInfo[]) {
    dictionaries = next;
    const active = next.filter((d) => d.enabled);
    searchActiveDicts = active;
    scheduleValidation(true);
    if (query.trim() !== '' && (hasCompletedSearch || isSearching)) {
      void executeSearch();
    } else {
      allMatches = [];
      matchesByDict = active.map(() => []);
      hasCompletedSearch = false;
    }
  }

  async function handleToggleDict(id: string, enabled: boolean) {
    const next = await getBackend().setEnabled(id, enabled);
    if (Array.isArray(next)) {
      syncUpdatedDictionaries(next);
    }
  }

  async function handleReorderDicts(orderedIds: string[]) {
    const next = await getBackend().reorderDictionaries(orderedIds);
    if (Array.isArray(next)) {
      syncUpdatedDictionaries(next);
    }
  }

  async function handleBrowseAddDicts() {
    const next = await getBackend().browseAndAddDictionaries();
    if (Array.isArray(next)) {
      syncUpdatedDictionaries(next);
    }
  }

  async function handleBrowseAddDirectory() {
    const next = await getBackend().browseAndAddDirectory();
    if (Array.isArray(next)) {
      syncUpdatedDictionaries(next);
    }
  }

  async function handleRemoveDict(id: string) {
    const next = await getBackend().removeDictionary(id);
    if (Array.isArray(next)) {
      syncUpdatedDictionaries(next);
    }
  }

  function handleToggleCategory(cat: EntryCategory) {
    const currentFilter = Array.isArray(config.entryFilter) ? config.entryFilter : DEFAULT_CONFIG.entryFilter;
    const exists = currentFilter.includes(cat);
    let next = exists
      ? currentFilter.filter((c) => c !== cat)
      : [...currentFilter, cat];
    if (next.length === 0) {
      // Keep at least one category active so results aren't silently muted
      next = ['lowercase', 'capitalized', 'punctuated', 'phrase'];
    }
    void applyConfigPatch({ entryFilter: next }, true);
  }

  function handleWindowKeydown(e: KeyboardEvent) {
    const mod = e.ctrlKey || e.metaKey;
    if (mod && e.key.toLowerCase() === 'w') {
      e.preventDefault();
      mode = 'word';
      patternBarRef?.focusInput();
      scheduleValidation(true);
    } else if (mod && e.key.toLowerCase() === 'f') {
      e.preventDefault();
      mode = 'fts';
      patternBarRef?.focusInput();
      scheduleValidation(true);
    } else if (mod && e.key === ',') {
      e.preventDefault();
      showSettings = true;
    } else if (mod && e.key === '1') {
      e.preventDefault();
      void applyConfigPatch({ layoutMode: 'tiled' }, false);
    } else if (mod && e.key === '2') {
      e.preventDefault();
      void applyConfigPatch({ layoutMode: 'unified' }, false);
    } else if (e.key === 'F1') {
      e.preventDefault();
      showCheatsheet = !showCheatsheet;
    } else if (e.key === 'Escape') {
      if (showAbout) {
        showAbout = false;
      } else if (showFontSettings) {
        showFontSettings = false;
      } else if (showSettings) {
        showSettings = false;
      } else if (showCheatsheet) {
        showCheatsheet = false;
      } else if (isSearching) {
        void cancelActiveSearch();
      }
    }
  }

  onMount(() => {
    const backend = getBackend();
    const unsubs: (() => void)[] = [];

    unsubs.push(
      backend.onEvent<SearchBatchEvent>('search:batch', (ev) => {
        if (!ev || (currentSearchId !== 0 && ev.searchId !== currentSearchId)) return;
        currentSearchId = ev.searchId;
        if (ev.progress) {
          wordsScanned = ev.progress.wordsScanned;
          totalCandidateWords = ev.progress.totalCandidateWords;
        }
        if (ev.matches && ev.matches.length > 0) {
          const prevEmpty = allMatches.length === 0;
          allMatches = allMatches.concat(ev.matches);

          // Group incoming batch by dictIndex in O(batch) time, then concat once per bucket
          const grouped = new Map<number, Match[]>();
          let maxIdx = matchesByDict.length - 1;
          for (let i = 0; i < ev.matches.length; i++) {
            const m = ev.matches[i];
            const dIdx = m.dictIndex ?? 0;
            if (dIdx > maxIdx) maxIdx = dIdx;
            let arr = grouped.get(dIdx);
            if (!arr) {
              arr = [];
              grouped.set(dIdx, arr);
            }
            arr.push(m);
          }

          const nextBuckets: Match[][] = new Array(maxIdx + 1);
          for (let i = 0; i <= maxIdx; i++) {
            const existing = matchesByDict[i] ?? [];
            const added = grouped.get(i);
            nextBuckets[i] = added ? existing.concat(added) : existing;
          }
          matchesByDict = nextBuckets;

          if (prevEmpty && !selectedWord) {
            void loadDefinition(ev.matches[0].word, true);
          }
        }
      })
    );

    unsubs.push(
      backend.onEvent<SearchProgressEvent>('search:progress', (ev) => {
        if (!ev || (currentSearchId !== 0 && ev.searchId !== currentSearchId)) return;
        wordsScanned = ev.progress.wordsScanned;
        totalCandidateWords = ev.progress.totalCandidateWords;
      })
    );

    unsubs.push(
      backend.onEvent<SearchCompleteEvent>('search:complete', (ev) => {
        if (!ev || (currentSearchId !== 0 && ev.searchId !== currentSearchId)) return;
        isSearching = false;
        hasCompletedSearch = true;
        wordsScanned = ev.wordsScanned;
        totalCandidateWords = ev.totalCandidateWords;
        elapsedMs = ev.elapsedMs;
        usedIndexedFTS = ev.usedIndexedFTS;
        if (ev.activeDicts && ev.activeDicts.length > 0) {
          searchActiveDicts = ev.activeDicts;
        }
      })
    );

    unsubs.push(
      backend.onEvent<SearchErrorEvent>('search:error', (ev) => {
        if (!ev || (currentSearchId !== 0 && ev.searchId !== currentSearchId)) return;
        isSearching = false;
        errorMessage = ev.message;
      })
    );

    unsubs.push(
      backend.onEvent<DictionaryInfo[]>('dicts:updated', (infos) => {
        if (Array.isArray(infos)) {
          dictionaries = infos;
          if (!isSearching && !hasCompletedSearch) {
            const active = infos.filter((d) => d.enabled);
            searchActiveDicts = active;
            matchesByDict = active.map(() => []);
          }
        }
      })
    );

    unsubs.push(
      backend.onEvent('ui:open-settings', () => {
        showSettings = true;
      })
    );
    unsubs.push(
      backend.onEvent('ui:open-font-settings', () => {
        showFontSettings = true;
      })
    );
    unsubs.push(
      backend.onEvent('ui:open-about', () => {
        showAbout = true;
      })
    );
    unsubs.push(
      backend.onEvent<SearchMode>('ui:set-mode', (nextMode) => {
        if (nextMode === 'word' || nextMode === 'fts') {
          mode = nextMode;
          patternBarRef?.focusInput();
          scheduleValidation(true);
        }
      })
    );
    unsubs.push(
      backend.onEvent<LayoutMode>('ui:set-layout', (nextLayout) => {
        if (nextLayout === 'tiled' || nextLayout === 'unified') {
          void applyConfigPatch({ layoutMode: nextLayout }, false);
        }
      })
    );
    unsubs.push(
      backend.onEvent('ui:toggle-cheatsheet', () => {
        showCheatsheet = !showCheatsheet;
      })
    );

    void (async () => {
      try {
        const [loadedCfg, loadedDicts] = await Promise.all([
          backend.getConfig(),
          backend.listDictionaries()
        ]);
        if (loadedCfg) {
          config = normalizeConfig(loadedCfg);
        }
        if (Array.isArray(loadedDicts)) {
          dictionaries = loadedDicts;
          searchActiveDicts = loadedDicts.filter((d) => d.enabled);
          matchesByDict = searchActiveDicts.map(() => []);
        }
      } catch {
        // Fallback defaults already set
      }
      patternBarRef?.focusInput();
    })();

    return () => {
      for (const off of unsubs) off();
      if (validateTimer) clearTimeout(validateTimer);
    };
  });
</script>

<svelte:window onkeydown={handleWindowKeydown} />

<div
  data-testid="app-root"
  style={rootFontVars}
  class="flex h-screen w-screen flex-col overflow-hidden bg-slate-950 text-slate-100"
>
  <!-- Top Pattern Bar -->
  <PatternBar
    bind:this={patternBarRef}
    {query}
    {mode}
    {enumeration}
    {isSearching}
    {validation}
    recentQueries={config.recentQueries ?? []}
    {showCheatsheet}
    onQueryChange={(q) => {
      query = q;
      scheduleValidation(false);
    }}
    onModeChange={(m) => {
      mode = m;
      scheduleValidation(true);
    }}
    onEnumerationChange={(e) => {
      enumeration = e;
      scheduleValidation(false);
    }}
    onSubmit={executeSearch}
    onCancel={cancelActiveSearch}
    onToggleCheatsheet={() => (showCheatsheet = !showCheatsheet)}
    onOpenSettings={() => (showSettings = true)}
    onOpenAbout={() => (showAbout = true)}
  />

  <!-- Filter, Length, Sort & Tiled/Unified Layout Toolbar -->
  <FilterToolbar
    minLength={config.minLength}
    maxLength={config.maxLength}
    entryCategories={config.entryFilter}
    suppressDuplicates={config.suppressDuplicates}
    sortMode={config.sortMode}
    layoutMode={config.layoutMode}
    onLengthChange={(min, max) => applyConfigPatch({ minLength: min, maxLength: max }, true)}
    onToggleCategory={handleToggleCategory}
    onToggleSuppressDuplicates={() =>
      applyConfigPatch({ suppressDuplicates: !config.suppressDuplicates }, true)}
    onSortChange={(s: SortMode) => applyConfigPatch({ sortMode: s }, true)}
    onLayoutChange={(l: LayoutMode) => applyConfigPatch({ layoutMode: l }, false)}
  />

  <!-- Main Workspace: Dictionary Sidebar + Results Workspace + Definition Pane -->
  <div class="flex min-h-0 flex-1 overflow-hidden">
    <DictionarySidebar
      {dictionaries}
      showBalances={config.showBalances}
      showAnnotations={config.showAnnotations}
      exclusiveVars={config.exclusiveVars}
      ignoreUnusedPunctuation={config.ignoreUnusedPunctuation}
      onToggleEnabled={handleToggleDict}
      onReorder={handleReorderDicts}
      onBrowseAdd={handleBrowseAddDicts}
      onBrowseAddDirectory={handleBrowseAddDirectory}
      onRemove={handleRemoveDict}
      onOptionChange={(patch) => applyConfigPatch(patch, true)}
    />

    <main class="flex min-w-0 flex-1 flex-col overflow-hidden">
      <!-- Results Area: Separate Tiled Panes per Dictionary (Default) vs Unified Merged List -->
      <div class="min-h-0 flex-1 overflow-hidden p-2.5" data-testid="results-workspace">
        {#if config.layoutMode === 'tiled'}
          {#if displayedTiledDicts.length === 0}
            <div class="flex h-full items-center justify-center rounded-lg border border-dashed border-slate-800 text-xs text-slate-500">
              Enable at least one dictionary in the left sidebar to search.
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
                  items={matchesByDict[paneIdx] ?? []}
                  {selectedWord}
                  showDictBadge={false}
                  showBalances={config.showBalances}
                  showAnnotations={config.showAnnotations}
                  fontSize={config.fonts.results.size}
                  testId={`tiled-pane-${paneIdx}`}
                  emptyMessage={hasCompletedSearch
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
            items={allMatches}
            {selectedWord}
            showDictBadge={true}
            showBalances={config.showBalances}
            showAnnotations={config.showAnnotations}
            fontSize={config.fonts.results.size}
            testId="unified-pane"
            emptyMessage={hasCompletedSearch
              ? 'No matching entries found across active dictionaries'
              : 'Enter a crossword, anagram, or full-text pattern above and press Enter'}
            onSelect={handleSelectMatch}
            onDoubleClick={handleDoubleClickMatch}
          />
        {/if}
      </div>

      <!-- Definitions & Web Search Fallback Pane -->
      <DefinitionPane
        {selectedWord}
        {lookupResult}
        isLoading={isLoadingDef}
        {includeMentions}
        canGoBack={historyIndex > 0}
        canGoForward={historyIndex >= 0 && historyIndex < historyStack.length - 1}
        defaultWebProvider={config.defaultWebProvider}
        onBack={handleHistoryBack}
        onForward={handleHistoryForward}
        onToggleIncludeMentions={(v) => {
          includeMentions = v;
          if (selectedWord) {
            void loadDefinition(selectedWord, false);
          }
        }}
        onCrossLookupWord={(w) => loadDefinition(w, true)}
        onOpenWebLookup={(w, pid) => {
          void getBackend().openWebLookup(w, pid);
        }}
        onOpenExternalURL={(u) => {
          void getBackend().openExternalURL(u);
        }}
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
      {#if errorMessage}
        <span class="font-medium text-rose-400">Error: {errorMessage}</span>
      {:else if isSearching}
        <span class="flex items-center gap-1.5 text-amber-300">
          <span class="inline-block h-1.5 w-1.5 animate-ping rounded-full bg-amber-400"></span>
          Scanning… {allMatches.length.toLocaleString()} matches ({wordsScanned.toLocaleString()}
          {#if totalCandidateWords > 0}/ {totalCandidateWords.toLocaleString()}{/if} words)
        </span>
      {:else if hasCompletedSearch}
        <span class="text-slate-300">
          <strong class="font-mono text-amber-300">{allMatches.length.toLocaleString()}</strong>
          {allMatches.length === 1 ? 'match' : 'matches'}
        </span>
        <span>·</span>
        <span>
          Scanned {wordsScanned.toLocaleString()} words in
          <strong class="font-mono text-slate-200">{elapsedMs.toFixed(1)} ms</strong>
        </span>
        {#if usedIndexedFTS}
          <span class="rounded bg-sky-500/15 px-1.5 py-0.2 text-[10px] text-sky-300">
            Indexed FTS
          </span>
        {/if}
      {:else}
        <span>Ready · Word Search (Ctrl+W) · Full Text Search (Ctrl+F)</span>
      {/if}
    </div>

    <div
      style="font-family: var(--tisane-font-status-family); font-size: var(--tisane-font-status-size);"
      class="flex items-center gap-3 text-slate-500"
    >
      <span>@={config.atMacro}</span>
      <span>#={config.hashMacro}</span>
      <span>Len {config.minLength}–{config.maxLength}</span>
      <span class="uppercase">{config.layoutMode}</span>
    </div>
  </footer>

  <!-- Preferences Modal -->
  <SettingsModal
    open={showSettings}
    {config}
    onClose={() => (showSettings = false)}
    onSave={(patch) => applyConfigPatch(patch, false)}
    onReset={(resetDicts) => void handleResetSettings(resetDicts)}
  />

  <!-- Direct Font Settings Modal (when opened via View menu) -->
  <FontSettingsModal
    open={showFontSettings}
    fonts={config.fonts}
    onClose={() => (showFontSettings = false)}
    onSave={(nextFonts) => applyConfigPatch({ fonts: nextFonts }, false)}
  />

  <!-- About Tisane Modal (opened by clicking app icon in upper-left corner) -->
  <AboutModal
    open={showAbout}
    onClose={() => (showAbout = false)}
    onOpenExternalURL={(u) => {
      void getBackend().openExternalURL(u);
    }}
  />
</div>
