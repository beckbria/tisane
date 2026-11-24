<script lang="ts">
  import type { SearchMode, ValidationResult } from '../types';

  interface Props {
    query: string;
    mode: SearchMode;
    enumeration: string;
    isSearching: boolean;
    validation: ValidationResult | null;
    recentQueries: string[];
    showCheatsheet: boolean;
    onQueryChange: (q: string) => void;
    onModeChange: (m: SearchMode) => void;
    onEnumerationChange: (e: string) => void;
    onSubmit: () => void;
    onCancel: () => void;
    onToggleCheatsheet: () => void;
    onOpenSettings: () => void;
    onOpenAbout?: () => void;
  }

  let {
    query,
    mode,
    enumeration,
    isSearching,
    validation,
    recentQueries,
    showCheatsheet,
    onQueryChange,
    onModeChange,
    onEnumerationChange,
    onSubmit,
    onCancel,
    onToggleCheatsheet,
    onOpenSettings,
    onOpenAbout
  }: Props = $props();

  let showHistory = $state(false);
  let inputEl: HTMLInputElement | undefined = $state();
  let historyContainerEl: HTMLDivElement | undefined = $state();
  let historyMenuEl: HTMLDivElement | undefined = $state();
  let syntaxBtnEl: HTMLButtonElement | undefined = $state();
  let cheatsheetEl: HTMLDivElement | undefined = $state();

  export function focusInput() {
    inputEl?.focus();
    inputEl?.select();
  }

  function handleWindowClick(e: MouseEvent) {
    const target = e.target as Node | null;
    if (!target) return;
    if (showHistory && historyContainerEl && !historyContainerEl.contains(target)) {
      showHistory = false;
    }
    if (
      showCheatsheet &&
      cheatsheetEl &&
      !cheatsheetEl.contains(target) &&
      syntaxBtnEl &&
      !syntaxBtnEl.contains(target)
    ) {
      onToggleCheatsheet();
    }
  }

  function handleHistoryMenuKeydown(e: KeyboardEvent) {
    if (!historyMenuEl) return;
    const items = Array.from(historyMenuEl.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'));
    if (items.length === 0) return;
    const idx = items.indexOf(document.activeElement as HTMLButtonElement);
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      items[(idx + 1) % items.length]?.focus();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      items[(idx - 1 + items.length) % items.length]?.focus();
    } else if (e.key === 'Home') {
      e.preventDefault();
      items[0]?.focus();
    } else if (e.key === 'End') {
      e.preventDefault();
      items[items.length - 1]?.focus();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      showHistory = false;
      inputEl?.focus();
    }
  }

  const examples = [
    { label: 'Crossword template', pat: 'c.o..w.rd', desc: '. matches any single letter' },
    { label: 'Exact anagram', pat: ';listen', desc: '; rearranges letters exactly' },
    { label: 'Sub-anagram / build', pat: '.....*;aadmort', desc: '5+ letters from a-a-d-m-o-r-t' },
    { label: 'Anagram + known slots', pat: 'm.....r;aadmort*', desc: 'Template intersected with pool' },
    { label: 'Subpattern contain', pat: '*(cat)*', desc: 'Contains contiguous sequence "cat"' },
    { label: 'Variable backreference', pat: '123321', desc: '6-letter palindromes via variables 0–9' },
    { label: 'Vowel / Consonant macros', pat: '@#@#@#', desc: '@ = vowels, # = consonants' },
    { label: 'Boolean intersection', pat: ';retinas* & !s*', desc: '& (AND), | (OR), ! (NOT)' }
  ];

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault();
      showHistory = false;
      onSubmit();
    } else if (e.key === 'Escape') {
      if (showHistory) {
        showHistory = false;
      } else if (isSearching) {
        onCancel();
      }
    } else if (e.key === 'ArrowDown' && e.altKey && recentQueries.length > 0) {
      e.preventDefault();
      showHistory = !showHistory;
      if (showHistory) {
        queueMicrotask(() => {
          historyMenuEl?.querySelector<HTMLButtonElement>('[role="menuitem"]')?.focus();
        });
      }
    }
  }

  const highlightedError = $derived.by(() => {
    if (!validation || validation.valid || !query) return null;
    const start = Math.max(0, Math.min(query.length, validation.span.start));
    const end = Math.max(start, Math.min(query.length, validation.span.end || start + 1));
    return {
      before: query.slice(0, start),
      bad: query.slice(start, end) || '␣',
      after: query.slice(end)
    };
  });
</script>

<svelte:window onclick={handleWindowClick} />

<header class="relative z-20 border-b border-slate-800 bg-slate-900/95 px-4 py-2.5 shadow-sm">
  <div class="flex flex-wrap items-center gap-2.5">
    <!-- App Brand -->
    <button
      type="button"
      onclick={() => onOpenAbout?.()}
      data-testid="app-brand-btn"
      title="About Tisane"
      aria-label="About Tisane"
      class="mr-1 flex cursor-pointer items-center gap-2 rounded-lg px-1.5 py-1 text-left transition select-none hover:bg-slate-800/80 focus:outline-none focus-visible:ring-2 focus-visible:ring-amber-400/80"
    >
      <img
        src="/icon.png"
        alt="Tisane logo"
        data-testid="app-logo"
        class="h-7 w-7 rounded-md object-contain shadow-xs ring-1 ring-slate-700/60"
      />
      <span class="text-sm font-semibold tracking-wide text-slate-100">Tisane</span>
    </button>

    <!-- Search Mode Segmented Switch -->
    <div
      class="inline-flex rounded-lg bg-slate-950 p-0.5 ring-1 ring-slate-600"
      role="group"
      aria-label="Search mode"
    >
      <button
        type="button"
        data-testid="mode-word-btn"
        aria-pressed={mode === 'word'}
        class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors {mode === 'word'
          ? 'bg-amber-500 text-neutral-950 shadow-xs'
          : 'text-slate-400 hover:text-slate-200'}"
        onclick={() => onModeChange('word')}
        title="Search dictionary headwords and phrases (Ctrl+W)"
      >
        Word Search
      </button>
      <button
        type="button"
        data-testid="mode-fts-btn"
        aria-pressed={mode === 'fts'}
        class="rounded-md px-2.5 py-1 text-xs font-medium transition-colors {mode === 'fts'
          ? 'bg-amber-500 text-neutral-950 shadow-xs'
          : 'text-slate-400 hover:text-slate-200'}"
        onclick={() => onModeChange('fts')}
        title="Full-text search inside dictionary definitions (Ctrl+F)"
      >
        Full Text (FTS)
      </button>
    </div>

    <!-- Main Pattern Input + History Dropdown -->
    <div class="relative min-w-[260px] flex-1" bind:this={historyContainerEl}>
      <div class="flex items-center rounded-lg bg-slate-950 ring-1 transition-shadow {validation && !validation.valid && query.trim() !== ''
        ? 'ring-rose-500/70 focus-within:ring-2 focus-within:ring-rose-400'
        : 'ring-slate-600 focus-within:ring-2 focus-within:ring-amber-400'}">
        <input
          bind:this={inputEl}
          type="text"
          value={query}
          oninput={(e) => onQueryChange((e.currentTarget as HTMLInputElement).value)}
          onkeydown={handleKeydown}
          placeholder={mode === 'word'
            ? 'Enter pattern (e.g. c.o..w.rd, ;anagram, *(sub)*, 123321)...'
            : 'Full-text query in definitions (e.g. eagle & !bald, myth*)...'}
          aria-label="Search pattern"
          aria-invalid={validation !== null && !validation.valid && query.trim() !== '' ? 'true' : undefined}
          aria-describedby={query.trim() !== '' && validation ? 'pattern-validation-banner' : undefined}
          data-testid="pattern-input"
          spellcheck="false"
          autocomplete="off"
          style="font-family: var(--tisane-font-pattern-family); font-size: var(--tisane-font-pattern-size);"
          class="w-full bg-transparent px-3 py-1.5 text-slate-100 placeholder-slate-400 focus:outline-none"
        />

        {#if query}
          <button
            type="button"
            onclick={() => {
              onQueryChange('');
              inputEl?.focus();
            }}
            class="px-1.5 text-slate-400 hover:text-slate-200"
            title="Clear pattern"
            aria-label="Clear pattern"
          >
            <span aria-hidden="true">×</span>
          </button>
        {/if}

        {#if recentQueries.length > 0}
          <button
            type="button"
            aria-haspopup="menu"
            aria-expanded={showHistory}
            aria-controls="recent-queries-menu"
            onclick={() => {
              showHistory = !showHistory;
              if (showHistory) {
                queueMicrotask(() => {
                  historyMenuEl?.querySelector<HTMLButtonElement>('[role="menuitem"]')?.focus();
                });
              }
            }}
            class="border-l border-slate-700 px-2 py-1.5 text-xs text-slate-400 hover:text-slate-200"
            title="Recent queries (Alt+Down)"
            aria-label="Recent queries"
          >
            <span aria-hidden="true">▾</span>
          </button>
        {/if}
      </div>

      {#if showHistory && recentQueries.length > 0}
        <div
          bind:this={historyMenuEl}
          id="recent-queries-menu"
          role="menu"
          aria-label="Recent queries"
          tabindex="-1"
          onkeydown={handleHistoryMenuKeydown}
          class="absolute right-0 left-0 z-40 mt-1 max-h-64 overflow-y-auto rounded-lg border border-slate-700 bg-slate-900 py-1 shadow-xl"
          data-testid="recent-queries-menu"
        >
          <div class="px-3 py-1 text-[11px] font-semibold tracking-wider text-slate-400 uppercase" aria-hidden="true">
            Recent Queries
          </div>
          {#each recentQueries as rq}
            <button
              type="button"
              role="menuitem"
              style="font-family: var(--tisane-font-pattern-family); font-size: var(--tisane-font-pattern-size);"
              class="flex w-full items-center justify-between px-3 py-1.5 text-left text-slate-200 hover:bg-slate-800 focus:bg-slate-800 focus:outline-none"
              onclick={() => {
                onQueryChange(rq);
                showHistory = false;
                onSubmit();
              }}
            >
              <span class="truncate">{rq}</span>
            </button>
          {/each}
        </div>
      {/if}
    </div>

    <!-- Enumeration Shorthand Input (Word mode) -->
    {#if mode === 'word'}
      <div class="flex items-center gap-1.5 rounded-lg bg-slate-950 px-2.5 py-1 ring-1 ring-slate-600 focus-within:ring-2 focus-within:ring-amber-400" title="Optional word/hyphen enumeration constraint (e.g. 4,3 or 3-5)">
        <span class="text-[11px] font-medium text-slate-400 select-none">Enum</span>
        <input
          type="text"
          value={enumeration}
          oninput={(e) => onEnumerationChange((e.currentTarget as HTMLInputElement).value)}
          onkeydown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              onSubmit();
            }
          }}
          placeholder="e.g. 4,3"
          aria-label="Enumeration"
          data-testid="enum-input"
          style="font-family: var(--tisane-font-pattern-family); font-size: var(--tisane-font-pattern-size);"
          class="w-20 bg-transparent text-slate-100 placeholder-slate-400 focus:outline-none"
        />
      </div>
    {/if}

    <!-- Search / Stop Action -->
    {#if isSearching}
      <button
        type="button"
        data-testid="cancel-search-btn"
        onclick={onCancel}
        class="inline-flex items-center gap-1.5 rounded-lg bg-rose-600 px-3.5 py-1.5 text-xs font-semibold text-white shadow-xs hover:bg-rose-700"
      >
        <span class="inline-block h-2 w-2 animate-pulse rounded-full bg-white"></span>
        Stop
      </button>
    {:else}
      <button
        type="button"
        data-testid="run-search-btn"
        onclick={onSubmit}
        disabled={!query.trim() || (validation !== null && !validation.valid)}
        class="inline-flex items-center gap-1.5 rounded-lg bg-amber-500 px-4 py-1.5 text-xs font-semibold text-neutral-950 shadow-xs transition hover:brightness-105 disabled:cursor-not-allowed disabled:opacity-40"
      >
        Search
      </button>
    {/if}

    <!-- Syntax Help & Settings Buttons -->
    <button
      bind:this={syntaxBtnEl}
      type="button"
      aria-expanded={showCheatsheet}
      aria-controls="syntax-cheatsheet-popover"
      onclick={onToggleCheatsheet}
      data-testid="syntax-help-btn"
      class="rounded-lg border border-slate-600 bg-slate-800/70 px-2.5 py-1.5 text-xs font-medium text-slate-300 hover:bg-slate-800 hover:text-slate-100"
      title="Pattern syntax reference (F1)"
    >
      <span aria-hidden="true">?</span> Syntax
    </button>

    <button
      type="button"
      onclick={onOpenSettings}
      data-testid="open-settings-btn"
      class="rounded-lg border border-slate-600 bg-slate-800/70 px-2.5 py-1.5 text-xs font-medium text-slate-300 hover:bg-slate-800 hover:text-slate-100"
      title="Macros & Web Search Settings (Ctrl+,)"
    >
      <span aria-hidden="true">⚙</span> Settings
    </button>
  </div>

  <!-- Live Validation Status Strip -->
  {#if query.trim() !== '' && validation}
    <div
      id="pattern-validation-banner"
      role={!validation.valid ? 'alert' : 'status'}
      aria-live="polite"
      class="mt-1.5 flex items-center justify-between text-xs"
      data-testid="validation-banner"
    >
      {#if !validation.valid}
        <div class="flex flex-wrap items-center gap-2 text-rose-400">
          <span class="font-medium"><span aria-hidden="true">⚠</span> {validation.message}</span>
          {#if highlightedError}
            <code class="rounded bg-slate-950 px-1.5 py-0.5 font-mono text-[11px] text-slate-300 ring-1 ring-rose-500/40">
              {highlightedError.before}<span class="rounded bg-rose-500/30 px-0.5 font-bold text-rose-200 underline decoration-rose-400">{highlightedError.bad}</span>{highlightedError.after}
            </code>
          {/if}
        </div>
      {:else if validation.hasWarning}
        <div class="text-amber-400">
          <span aria-hidden="true">⚠</span> {validation.message}
        </div>
      {:else}
        <div class="flex items-center gap-3 text-slate-400">
          <span class="text-emerald-400"><span aria-hidden="true">✓</span> Valid pattern</span>
          {#if mode === 'word' && validation.minMatchedLength > 0}
            <span>
              Matches length
              <strong class="font-mono text-slate-200">
                {validation.minMatchedLength === validation.maxMatchedLength
                  ? `${validation.minMatchedLength}`
                  : `${validation.minMatchedLength}–${validation.maxMatchedLength}`}
              </strong>
            </span>
          {/if}
        </div>
      {/if}
    </div>
  {/if}

  <!-- Syntax Reference Popover -->
  {#if showCheatsheet}
    <div
      bind:this={cheatsheetEl}
      id="syntax-cheatsheet-popover"
      role="region"
      aria-label="Tisane pattern syntax quick reference"
      class="absolute right-4 z-50 mt-2 w-full max-w-xl rounded-xl border border-slate-700 bg-slate-900 p-4 shadow-2xl"
      data-testid="syntax-cheatsheet"
    >
      <div class="mb-2.5 flex items-center justify-between border-b border-slate-800 pb-2">
        <h3 class="text-sm font-semibold text-slate-100">Tisane Pattern Syntax Quick Reference</h3>
        <button
          type="button"
          onclick={onToggleCheatsheet}
          class="text-xs text-slate-400 hover:text-slate-100"
        >
          Close ✕
        </button>
      </div>
      <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
        {#each examples as ex}
          <button
            type="button"
            class="flex flex-col rounded-lg border border-slate-800 bg-slate-950/70 p-2 text-left transition hover:border-amber-500/50 hover:bg-slate-950"
            onclick={() => {
              onQueryChange(ex.pat);
              onToggleCheatsheet();
              inputEl?.focus();
            }}
          >
            <div class="flex items-center justify-between gap-2">
              <span class="text-xs font-medium text-slate-200">{ex.label}</span>
              <code class="rounded bg-amber-500/15 px-1.5 py-0.5 font-mono text-xs text-amber-300">{ex.pat}</code>
            </div>
            <span class="mt-0.5 text-[11px] text-slate-400">{ex.desc}</span>
          </button>
        {/each}
      </div>
      <p class="mt-3 text-[11px] text-slate-400">
        Wildcards: <code class="text-slate-200">.</code> any letter · <code class="text-slate-200">?</code> optional letter (0 or 1) · <code class="text-slate-200">*</code> 0+ letters · <code class="text-slate-200">[abc]</code> / <code class="text-slate-200">[^abc]</code> letter sets · <code class="text-slate-200">\</code> literal escape. Click any card above to try it.
      </p>
    </div>
  {/if}
</header>
