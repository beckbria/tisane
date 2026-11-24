<script lang="ts">
  import type { EntryCategory, LayoutMode, SortMode } from '../types';

  interface Props {
    minLength: number;
    maxLength: number;
    entryCategories: EntryCategory[];
    suppressDuplicates: boolean;
    sortMode: SortMode;
    layoutMode: LayoutMode;
    sidebarCollapsed?: boolean;
    definitionCollapsed?: boolean;
    activeDictCount?: number;
    totalDictCount?: number;
    onLengthChange: (min: number, max: number) => void;
    onToggleCategory: (cat: EntryCategory) => void;
    onToggleSuppressDuplicates: () => void;
    onSortChange: (sort: SortMode) => void;
    onLayoutChange: (layout: LayoutMode) => void;
    onToggleSidebar?: () => void;
    onToggleDefinitions?: () => void;
  }

  let {
    minLength,
    maxLength,
    entryCategories,
    suppressDuplicates,
    sortMode,
    layoutMode,
    sidebarCollapsed = false,
    definitionCollapsed = false,
    activeDictCount = 0,
    totalDictCount = 0,
    onLengthChange,
    onToggleCategory,
    onToggleSuppressDuplicates,
    onSortChange,
    onLayoutChange,
    onToggleSidebar,
    onToggleDefinitions
  }: Props = $props();

  let exactPin = $state(false);

  $effect(() => {
    if (minLength !== maxLength) {
      exactPin = false;
    }
  });

  function clampLen(v: number): number {
    if (Number.isNaN(v)) return 1;
    return Math.max(1, Math.min(100, Math.trunc(v)));
  }

  function updateMin(raw: number) {
    const nextMin = clampLen(raw);
    if (exactPin) {
      onLengthChange(nextMin, nextMin);
    } else {
      onLengthChange(nextMin, Math.max(nextMin, maxLength));
    }
  }

  function updateMax(raw: number) {
    const nextMax = clampLen(raw);
    if (exactPin) {
      onLengthChange(nextMax, nextMax);
    } else {
      onLengthChange(Math.min(minLength, nextMax), nextMax);
    }
  }

  function toggleExactPin() {
    exactPin = !exactPin;
    if (exactPin) {
      onLengthChange(minLength, minLength);
    }
  }

  const categoryButtons: { id: EntryCategory; code: string; title: string }[] = [
    { id: 'lowercase', code: 'abc', title: 'Include all-lowercase entries (e.g. apple)' },
    { id: 'capitalized', code: 'Abc', title: 'Include capitalized / proper-noun entries (e.g. London)' },
    { id: 'punctuated', code: 'a-b', title: 'Include hyphenated & punctuated single words (e.g. by-law)' },
    { id: 'phrase', code: 'a b', title: 'Include multi-word phrases containing spaces' }
  ];
</script>

<div
  data-testid="filter-toolbar"
  style="font-family: var(--tisane-font-toolbar-family); font-size: var(--tisane-font-toolbar-size);"
  class="flex flex-wrap items-center justify-between gap-2 border-b border-slate-800/90 bg-slate-900/60 px-4 py-1.5"
>
  <!-- Left group: Panel Toggles + Min/Max Word Length + Entry Categories -->
  <div class="flex flex-wrap items-center gap-3">
    {#if onToggleSidebar || onToggleDefinitions}
      <div class="flex items-center gap-1.5" role="group" aria-label="Panel visibility">
        {#if onToggleSidebar}
          <button
            type="button"
            data-testid="toggle-sidebar-btn"
            aria-pressed={!sidebarCollapsed}
            aria-label={sidebarCollapsed ? 'Show dictionaries panel' : 'Hide dictionaries panel'}
            title={sidebarCollapsed ? 'Show dictionaries panel (Ctrl+B)' : 'Hide dictionaries panel (Ctrl+B)'}
            onclick={onToggleSidebar}
            class="flex items-center gap-1.5 rounded px-2 py-0.5 text-[11px] font-medium transition {sidebarCollapsed
              ? 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-500/40 hover:bg-amber-500/30'
              : 'bg-slate-800/80 text-slate-300 ring-1 ring-slate-700/80 hover:bg-slate-800 hover:text-slate-100'}"
          >
            <svg
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 16 16"
              fill="none"
              stroke="currentColor"
              stroke-width="1.4"
              class="h-3.5 w-3.5 shrink-0"
              aria-hidden="true"
            >
              <rect x="1.5" y="2.5" width="13" height="11" rx="1.5" />
              <line x1="5.5" y1="2.5" x2="5.5" y2="13.5" />
              {#if !sidebarCollapsed}
                <rect x="2" y="3" width="3" height="10" fill="currentColor" stroke="none" opacity="0.45" />
              {/if}
            </svg>
            <span>
              {#if sidebarCollapsed}
                {totalDictCount > 0 ? `Dictionaries (${activeDictCount}/${totalDictCount})` : 'Dictionaries'}
              {:else}
                Dicts
              {/if}
            </span>
          </button>
        {/if}

        {#if onToggleDefinitions}
          <button
            type="button"
            data-testid="toggle-definitions-btn"
            aria-pressed={!definitionCollapsed}
            aria-label={definitionCollapsed ? 'Show definitions panel' : 'Hide definitions panel'}
            title={definitionCollapsed ? 'Show definitions panel (Ctrl+D)' : 'Hide definitions panel (Ctrl+D)'}
            onclick={onToggleDefinitions}
            class="flex items-center gap-1.5 rounded px-2 py-0.5 text-[11px] font-medium transition {definitionCollapsed
              ? 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-500/40 hover:bg-amber-500/30'
              : 'bg-slate-800/80 text-slate-300 ring-1 ring-slate-700/80 hover:bg-slate-800 hover:text-slate-100'}"
          >
            <svg
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 16 16"
              fill="none"
              stroke="currentColor"
              stroke-width="1.4"
              class="h-3.5 w-3.5 shrink-0"
              aria-hidden="true"
            >
              <rect x="1.5" y="2.5" width="13" height="11" rx="1.5" />
              <line x1="1.5" y1="9.5" x2="14.5" y2="9.5" />
              {#if !definitionCollapsed}
                <rect x="2" y="10" width="12" height="3" fill="currentColor" stroke="none" opacity="0.45" />
              {/if}
            </svg>
            <span>{definitionCollapsed ? 'Definitions' : 'Defs'}</span>
          </button>
        {/if}
      </div>

      <div class="h-4 w-px bg-slate-800"></div>
    {/if}

    <!-- Min / Max Word Length Filter -->
    <div class="flex items-center gap-1.5" role="group" aria-label="Word length filter">
      <span class="text-[11px] font-medium text-slate-400 select-none">Length</span>
      <input
        type="number"
        min="1"
        max="100"
        value={minLength}
        aria-label="Minimum word length"
        data-testid="min-length-input"
        onchange={(e) => updateMin( Number((e.currentTarget as HTMLInputElement).value) )}
        class="w-13 rounded border border-slate-700/80 bg-slate-950 px-1.5 py-0.5 text-center font-mono text-xs text-slate-100 focus:border-amber-400 focus:outline-none"
      />
      <button
        type="button"
        onclick={toggleExactPin}
        data-testid="exact-length-pin"
        class="rounded px-1.5 py-0.5 font-mono text-[11px] transition {exactPin
          ? 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-500/40'
          : 'text-slate-500 hover:text-slate-300'}"
        title={exactPin ? 'Pinned to exact length (click to unpin)' : 'Pin Min = Max for exact word length'}
      >
        {exactPin ? '=' : '–'}
      </button>
      <input
        type="number"
        min="1"
        max="100"
        value={maxLength}
        aria-label="Maximum word length"
        data-testid="max-length-input"
        onchange={(e) => updateMax( Number((e.currentTarget as HTMLInputElement).value) )}
        class="w-13 rounded border border-slate-700/80 bg-slate-950 px-1.5 py-0.5 text-center font-mono text-xs text-slate-100 focus:border-amber-400 focus:outline-none"
      />
      {#if minLength !== 1 || maxLength !== 100}
        <button
          type="button"
          onclick={() => {
            exactPin = false;
            onLengthChange(1, 100);
          }}
          data-testid="reset-length-btn"
          class="text-[11px] text-amber-400 hover:text-amber-300"
          title="Reset length filter to 1–100"
        >
          Reset
        </button>
      {/if}
    </div>

    <div class="h-4 w-px bg-slate-800"></div>

    <!-- 4 Entry Category Toggles -->
    <div class="flex items-center gap-1" role="group" aria-label="Entry categories">
      {#each categoryButtons as cb}
        {@const active = (entryCategories ?? []).includes(cb.id)}
        <button
          type="button"
          data-testid={`cat-${cb.id}`}
          onclick={() => onToggleCategory(cb.id)}
          title={cb.title}
          class="rounded px-2 py-0.5 font-mono text-[11px] font-medium transition {active
            ? 'bg-slate-800 text-amber-300 ring-1 ring-amber-500/40'
            : 'bg-slate-950/60 text-slate-500 line-through hover:text-slate-300'}"
        >
          {cb.code}
        </button>
      {/each}
    </div>

    <div class="h-4 w-px bg-slate-800"></div>

    <!-- Cross-dictionary duplicate suppression -->
    <button
      type="button"
      data-testid="toggle-dedupe-btn"
      onclick={onToggleSuppressDuplicates}
      title="Suppress duplicate matches already found in a higher-priority dictionary"
      class="rounded px-2 py-0.5 text-[11px] font-medium transition {suppressDuplicates
        ? 'bg-slate-800 text-emerald-300 ring-1 ring-emerald-500/35'
        : 'bg-slate-950/60 text-slate-400 hover:text-slate-200'}"
    >
      {suppressDuplicates ? '✓ Dedupe' : 'Dedupe Off'}
    </button>
  </div>

  <!-- Right group: Sort Order + Tiled vs Unified Layout Switch -->
  <div class="flex items-center gap-3">
    <label class="flex items-center gap-1.5">
      <span class="text-[11px] text-slate-400 select-none">Sort</span>
      <select
        value={sortMode}
        data-testid="sort-select"
        onchange={(e) => onSortChange((e.currentTarget as HTMLSelectElement).value as SortMode)}
        class="rounded border border-slate-700/80 bg-slate-950 px-2 py-0.5 text-xs text-slate-200 focus:border-amber-400 focus:outline-none"
      >
        <option value="dict">Dictionary Order</option>
        <option value="smart">Smart Balance</option>
        <option value="alpha">A → Z</option>
        <option value="alpha-desc">Z → A</option>
        <option value="len">Length ↑</option>
        <option value="len-desc">Length ↓</option>
      </select>
    </label>

    <!-- Layout Switch: Separate Tiled Panes (Default) vs Unified Merged List -->
    <div
      class="inline-flex rounded-md bg-slate-950 p-0.5 ring-1 ring-slate-800"
      role="group"
      aria-label="Results layout mode"
    >
      <button
        type="button"
        data-testid="layout-tiled-btn"
        onclick={() => onLayoutChange('tiled')}
        title="Separate tiled pane per active dictionary (Ctrl+1)"
        class="rounded px-2 py-0.5 text-[11px] font-medium transition {layoutMode === 'tiled'
          ? 'bg-amber-500 text-slate-950'
          : 'text-slate-400 hover:text-slate-200'}"
      >
        ⊞ Tiled
      </button>
      <button
        type="button"
        data-testid="layout-unified-btn"
        onclick={() => onLayoutChange('unified')}
        title="Unified merged list across dictionaries (Ctrl+2)"
        class="rounded px-2 py-0.5 text-[11px] font-medium transition {layoutMode === 'unified'
          ? 'bg-amber-500 text-slate-950'
          : 'text-slate-400 hover:text-slate-200'}"
      >
        ≡ Unified
      </button>
    </div>
  </div>
</div>
