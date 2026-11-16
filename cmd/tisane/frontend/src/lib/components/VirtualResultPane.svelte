<script lang="ts">
  import { untrack } from 'svelte';
  import { createVirtualizer } from '@tanstack/svelte-virtual';
  import type { Match } from '../types';

  interface Props {
    title: string;
    subtitle?: string;
    items: Match[];
    selectedWord: string | null;
    showDictBadge: boolean;
    showBalances: boolean;
    showAnnotations: boolean;
    testId: string;
    emptyMessage?: string;
    onSelect: (m: Match) => void;
    onDoubleClick: (m: Match) => void;
  }

  let {
    title,
    subtitle = '',
    items,
    selectedWord,
    showDictBadge,
    showBalances,
    showAnnotations,
    testId,
    emptyMessage = 'No matches',
    onSelect,
    onDoubleClick
  }: Props = $props();

  const ROW_HEIGHT = 28;
  let scrollEl: HTMLDivElement | null = $state(null);

  const virtualizer = createVirtualizer<HTMLDivElement, HTMLDivElement>({
    count: 0,
    getScrollElement: () => scrollEl,
    estimateSize: () => ROW_HEIGHT,
    overscan: 10,
    initialRect: { width: 420, height: 480 }
  });

  $effect(() => {
    const count = items.length;
    const el = scrollEl;
    untrack(() => {
      $virtualizer.setOptions({
        count,
        getScrollElement: () => el
      });
    });
  });

  function splitBalance(bal?: string): { omitted: string; added: string } {
    if (!bal) return { omitted: '', added: '' };
    const plusIdx = bal.indexOf('+');
    if (bal.startsWith('-')) {
      if (plusIdx > 0) {
        return { omitted: bal.slice(0, plusIdx), added: bal.slice(plusIdx) };
      }
      return { omitted: bal, added: '' };
    }
    if (plusIdx === 0) {
      return { omitted: '', added: bal };
    }
    return { omitted: bal, added: '' };
  }

  function handleKeydown(e: KeyboardEvent) {
    if (items.length === 0) return;
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
    e.preventDefault();
    const curIdx = selectedWord ? items.findIndex((m) => m.word === selectedWord) : -1;
    const nextIdx =
      e.key === 'ArrowDown'
        ? Math.min(items.length - 1, curIdx + 1)
        : Math.max(0, curIdx <= 0 ? 0 : curIdx - 1);
    const chosen = items[nextIdx];
    if (chosen) {
      onSelect(chosen);
      $virtualizer.scrollToIndex(nextIdx, { align: 'auto' });
    }
  }
</script>

<section
  data-testid={testId}
  class="flex h-full min-h-0 min-w-[220px] flex-1 flex-col overflow-hidden rounded-lg border border-slate-800 bg-slate-950/90"
>
  <!-- Pane Header -->
  <div class="flex items-center justify-between gap-2 border-b border-slate-800/90 bg-slate-900/90 px-3 py-1.5 select-none">
    <div class="min-w-0">
      <h3 class="truncate text-xs font-semibold text-slate-200">{title}</h3>
      {#if subtitle}
        <p class="truncate text-[10px] text-slate-500">{subtitle}</p>
      {/if}
    </div>
    <span
      data-testid={`${testId}-count`}
      class="shrink-0 rounded-full bg-slate-800 px-2 py-0.5 font-mono text-[11px] font-medium text-amber-300"
    >
      {items.length.toLocaleString()}
    </span>
  </div>

  <!-- Virtualized Scroll Container -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div
    bind:this={scrollEl}
    tabindex="0"
    role="listbox"
    aria-label={`${title} matches`}
    onkeydown={handleKeydown}
    class="relative flex-1 overflow-y-auto focus:outline-none"
  >
    {#if items.length === 0}
      <div class="flex h-full items-center justify-center p-4 text-center text-xs text-slate-500">
        {emptyMessage}
      </div>
    {:else}
      <div
        style="height: {$virtualizer.getTotalSize()}px; width: 100%; position: relative;"
      >
        {#each $virtualizer.getVirtualItems() as vRow (vRow.key)}
          {@const match = items[vRow.index]}
          {#if match}
            {@const isSelected = selectedWord === match.word}
            {@const bal = showBalances ? splitBalance(match.balance) : { omitted: '', added: '' }}
            <button
              type="button"
              role="option"
              aria-selected={isSelected}
              data-testid="result-row"
              onclick={() => onSelect(match)}
              ondblclick={() => onDoubleClick(match)}
              style="position: absolute; top: 0; left: 0; width: 100%; height: {vRow.size}px; transform: translateY({vRow.start}px);"
              class="flex items-center justify-between gap-2 border-b border-slate-900/70 px-3 text-left text-xs transition-colors {isSelected
                ? 'bg-amber-500/20 text-amber-100'
                : 'text-slate-200 hover:bg-slate-900/90'}"
            >
              <div class="flex min-w-0 items-center gap-1.5">
                <span class="truncate font-mono text-[13px] font-medium">{match.word}</span>
                {#if showAnnotations && match.annotation}
                  <span class="truncate rounded bg-slate-800 px-1.5 py-0.2 text-[10px] text-slate-400">
                    {match.annotation}
                  </span>
                {/if}
              </div>

              <div class="flex shrink-0 items-center gap-1.5">
                {#if bal.omitted}
                  <span class="rounded bg-rose-500/15 px-1 font-mono text-[10px] text-rose-300">
                    {bal.omitted}
                  </span>
                {/if}
                {#if bal.added}
                  <span class="rounded bg-emerald-500/15 px-1 font-mono text-[10px] text-emerald-300">
                    {bal.added}
                  </span>
                {/if}
                {#if showDictBadge && match.dict}
                  <span class="max-w-[110px] truncate rounded bg-slate-800/90 px-1.5 py-0.2 text-[10px] text-slate-400">
                    {match.dict}
                  </span>
                {/if}
                {#if match.hasDefinition}
                  <span
                    class="text-[11px] text-amber-400/90"
                    title="Local definition available"
                    aria-label="Has local definition"
                  >
                    📖
                  </span>
                {:else}
                  <span class="w-3.5 text-center font-mono text-[10px] text-slate-600">{match.length}</span>
                {/if}
              </div>
            </button>
          {/if}
        {/each}
      </div>
    {/if}
  </div>
</section>
