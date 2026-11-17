<script lang="ts">
  import type { DictionaryInfo } from '../types';

  interface Props {
    dictionaries: DictionaryInfo[];
    showBalances: boolean;
    showAnnotations: boolean;
    exclusiveVars: boolean;
    ignoreUnusedPunctuation: boolean;
    onToggleEnabled: (id: string, enabled: boolean) => void;
    onReorder: (orderedIds: string[]) => void;
    onBrowseAdd: () => void;
    onBrowseAddDirectory?: () => void;
    onRemove: (id: string) => void;
    onOptionChange: (patch: {
      showBalances?: boolean;
      showAnnotations?: boolean;
      exclusiveVars?: boolean;
      ignoreUnusedPunctuation?: boolean;
    }) => void;
  }

  let {
    dictionaries,
    showBalances,
    showAnnotations,
    exclusiveVars,
    ignoreUnusedPunctuation,
    onToggleEnabled,
    onReorder,
    onBrowseAdd,
    onBrowseAddDirectory,
    onRemove,
    onOptionChange
  }: Props = $props();

  let draggedIndex: number | null = $state(null);
  let dropTargetIndex: number | null = $state(null);
  let addMenuOpen = $state(false);
  let addMenuContainer: HTMLDivElement | undefined = $state();

  function formatCount(n: number): string {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
    if (n >= 1_000) return `${(n / 1_000).toFixed(n >= 100_000 ? 0 : 1)}k`;
    return `${n}`;
  }

  function moveByOffset(index: number, delta: number) {
    const target = index + delta;
    if (target < 0 || target >= dictionaries.length) return;
    const ids = dictionaries.map((d) => d.id);
    const [moved] = ids.splice(index, 1);
    ids.splice(target, 0, moved);
    onReorder(ids);
  }

  function handleDrop(targetIdx: number) {
    if (draggedIndex === null || draggedIndex === targetIdx) {
      draggedIndex = null;
      dropTargetIndex = null;
      return;
    }
    const ids = dictionaries.map((d) => d.id);
    const [moved] = ids.splice(draggedIndex, 1);
    ids.splice(targetIdx, 0, moved);
    draggedIndex = null;
    dropTargetIndex = null;
    onReorder(ids);
  }

  const activeCount = $derived((dictionaries ?? []).filter((d) => d.enabled).length);
</script>

<svelte:window
  onclick={(e) => {
    if (addMenuOpen && addMenuContainer && !addMenuContainer.contains(e.target as Node)) {
      addMenuOpen = false;
    }
  }}
  onkeydown={(e) => {
    if (addMenuOpen && e.key === 'Escape') {
      addMenuOpen = false;
    }
  }}
/>

<aside class="flex h-full w-68 shrink-0 flex-col border-r border-slate-800 bg-slate-900/75 select-none">
  <!-- Header -->
  <div class="flex items-center justify-between border-b border-slate-800 px-3 py-2">
    <div>
      <h2 class="text-xs font-semibold tracking-wider text-slate-300 uppercase">
        Dictionaries
      </h2>
      <p class="text-[11px] text-slate-500">
        {activeCount} of {dictionaries.length} active · priority order
      </p>
    </div>
    <div class="relative" bind:this={addMenuContainer}>
      <button
        type="button"
        data-testid="add-dict-btn"
        aria-haspopup="menu"
        aria-expanded={addMenuOpen}
        onclick={() => (addMenuOpen = !addMenuOpen)}
        class="flex items-center gap-1 rounded-md bg-slate-800 px-2 py-1 text-xs font-medium text-amber-300 ring-1 ring-slate-700 transition hover:bg-slate-700"
        title="Add .tsd dictionary files or directory (Ctrl+O)"
      >
        <span>+ Add</span>
        <span class="text-[10px] text-amber-300/80">▾</span>
      </button>

      {#if addMenuOpen}
        <div
          role="menu"
          data-testid="add-dict-menu"
          class="absolute right-0 z-40 mt-1 w-44 rounded-lg border border-slate-700 bg-slate-900 py-1 shadow-xl"
        >
          <button
            type="button"
            role="menuitem"
            data-testid="add-dict-file-option"
            onclick={() => {
              addMenuOpen = false;
              onBrowseAdd();
            }}
            class="flex w-full items-center px-3 py-1.5 text-left text-xs text-slate-200 hover:bg-slate-800 hover:text-amber-300"
          >
            Add dictionary
          </button>
          <button
            type="button"
            role="menuitem"
            data-testid="add-dict-dir-option"
            onclick={() => {
              addMenuOpen = false;
              onBrowseAddDirectory?.();
            }}
            class="flex w-full items-center px-3 py-1.5 text-left text-xs text-slate-200 hover:bg-slate-800 hover:text-amber-300"
          >
            Add all from directory
          </button>
        </div>
      {/if}
    </div>
  </div>

  <!-- Ordered Dictionary List -->
  <div
    class="flex-1 overflow-y-auto p-2"
    style="font-family: var(--tisane-font-dicts-family); font-size: var(--tisane-font-dicts-size);"
    data-testid="dictionary-list"
  >
    {#if dictionaries.length === 0}
      <div class="rounded-lg border border-dashed border-slate-800 p-4 text-center text-slate-400">
        <p>No <code class="text-slate-300">.tsd</code> dictionaries configured.</p>
        <button
          type="button"
          onclick={onBrowseAdd}
          class="mt-2 rounded bg-amber-500/20 px-2.5 py-1 font-medium text-amber-300 hover:bg-amber-500/30"
        >
          Browse for .tsd files...
        </button>
      </div>
    {:else}
      <ul class="space-y-1">
        {#each dictionaries as dict, idx (dict.id)}
          <li
            draggable="true"
            ondragstart={() => (draggedIndex = idx)}
            ondragover={(e) => {
              e.preventDefault();
              dropTargetIndex = idx;
            }}
            ondragleave={() => {
              if (dropTargetIndex === idx) dropTargetIndex = null;
            }}
            ondrop={(e) => {
              e.preventDefault();
              handleDrop(idx);
            }}
            ondragend={() => {
              draggedIndex = null;
              dropTargetIndex = null;
            }}
            data-testid={`dict-item-${idx}`}
            style="font-family: var(--tisane-font-dicts-family); font-size: var(--tisane-font-dicts-size);"
            class="group flex items-center gap-1.5 rounded-lg border px-2 py-1.5 transition {dropTargetIndex === idx
              ? 'border-amber-400 bg-amber-500/10'
              : dict.enabled
                ? 'border-slate-800 bg-slate-950/75 text-slate-100'
                : 'border-transparent bg-slate-900/40 text-slate-400 opacity-75 hover:bg-slate-900'}"
          >
            <!-- Drag Handle -->
            <span
              class="cursor-grab text-slate-500 hover:text-slate-300"
              title="Drag to reorder priority"
            >
              ≡
            </span>

            <!-- Enable Checkbox -->
            <input
              type="checkbox"
              checked={dict.enabled}
              aria-label={`Enable ${dict.name}`}
              data-testid={`dict-toggle-${idx}`}
              onchange={(e) => onToggleEnabled(dict.id, (e.currentTarget as HTMLInputElement).checked)}
              class="h-3.5 w-3.5 rounded border-slate-600 bg-slate-900 accent-amber-500"
            />

            <!-- Name & Capability Badges -->
            <div class="min-w-0 flex-1" title={`${dict.name}\n${dict.path}\n${dict.wordCount.toLocaleString()} words (max length ${dict.maxWordLength})`}>
              <div class="truncate font-medium">{dict.name}</div>
              <div class="flex items-center gap-1.5 text-[10px] text-slate-400">
                <span class="font-mono">{formatCount(dict.wordCount)}</span>
                {#if dict.hasDefinitions}
                  <span class="rounded bg-amber-500/15 px-1 text-amber-300" title="Contains local definitions">defs</span>
                {/if}
                {#if dict.hasFTS}
                  <span class="rounded bg-sky-500/15 px-1 text-sky-300" title="Contains Full-Text Search index">fts</span>
                {/if}
              </div>
            </div>

            <!-- Explicit Up / Down + Remove Controls -->
            <div class="flex items-center gap-0.5 opacity-60 transition-opacity group-hover:opacity-100">
              <button
                type="button"
                disabled={idx === 0}
                data-testid={`dict-up-${idx}`}
                onclick={() => moveByOffset(idx, -1)}
                class="rounded px-1 py-0.5 text-[10px] text-slate-300 hover:bg-slate-800 disabled:opacity-25"
                title="Move dictionary higher in priority"
                aria-label={`Move ${dict.name} up`}
              >
                ↑
              </button>
              <button
                type="button"
                disabled={idx === dictionaries.length - 1}
                data-testid={`dict-down-${idx}`}
                onclick={() => moveByOffset(idx, 1)}
                class="rounded px-1 py-0.5 text-[10px] text-slate-300 hover:bg-slate-800 disabled:opacity-25"
                title="Move dictionary lower in priority"
                aria-label={`Move ${dict.name} down`}
              >
                ↓
              </button>
              <button
                type="button"
                onclick={() => onRemove(dict.id)}
                class="rounded px-1 py-0.5 text-[10px] text-slate-400 hover:bg-rose-500/20 hover:text-rose-300"
                title="Remove from list"
                aria-label={`Remove ${dict.name}`}
              >
                ×
              </button>
            </div>
          </li>
        {/each}
      </ul>
    {/if}
  </div>

  <!-- Search & Output Options Footer -->
  <div
    style="font-family: var(--tisane-font-dicts-family); font-size: var(--tisane-font-dicts-size);"
    class="border-t border-slate-800 bg-slate-950/60 px-3 py-2.5"
  >
    <div class="mb-1.5 text-[10px] font-semibold tracking-wider text-slate-400 uppercase">
      Pattern & Output Options
    </div>
    <div class="space-y-1.5 text-slate-300">
      <label class="flex cursor-pointer items-center gap-2">
        <input
          type="checkbox"
          checked={showBalances}
          onchange={(e) => onOptionChange({ showBalances: (e.currentTarget as HTMLInputElement).checked })}
          class="h-3.5 w-3.5 rounded accent-amber-500"
        />
        <span>Show anagram balances</span>
      </label>
      <label class="flex cursor-pointer items-center gap-2">
        <input
          type="checkbox"
          checked={showAnnotations}
          onchange={(e) => onOptionChange({ showAnnotations: (e.currentTarget as HTMLInputElement).checked })}
          class="h-3.5 w-3.5 rounded accent-amber-500"
        />
        <span>Show word annotations</span>
      </label>
      <label class="flex cursor-pointer items-center gap-2" title="Require variables 0–9 to bind to distinct letters">
        <input
          type="checkbox"
          checked={exclusiveVars}
          onchange={(e) => onOptionChange({ exclusiveVars: (e.currentTarget as HTMLInputElement).checked })}
          class="h-3.5 w-3.5 rounded accent-amber-500"
        />
        <span>Exclusive variables (0–9)</span>
      </label>
      <label class="flex cursor-pointer items-center gap-2" title="Allow spaces, hyphens, and apostrophes anywhere unless explicitly constrained">
        <input
          type="checkbox"
          checked={ignoreUnusedPunctuation}
          onchange={(e) => onOptionChange({ ignoreUnusedPunctuation: (e.currentTarget as HTMLInputElement).checked })}
          class="h-3.5 w-3.5 rounded accent-amber-500"
        />
        <span>Ignore unused punctuation</span>
      </label>
    </div>
  </div>
</aside>
