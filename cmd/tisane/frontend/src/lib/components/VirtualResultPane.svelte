<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import { createVirtualizer } from '@tanstack/svelte-virtual';
  import { copyTextToClipboard } from '../clipboard';
  import type { Match } from '../types';
  import type { ResultContextMenuRequest } from './ResultContextMenu.svelte';

  interface Props {
    title: string;
    subtitle?: string;
    items: Match[];
    count?: number;
    searchKey?: number;
    selectedWord: string | null;
    showDictBadge: boolean;
    showBalances: boolean;
    showAnnotations: boolean;
    fontSize?: number;
    testId: string;
    emptyMessage?: string;
    onSelect: (m: Match) => void;
    onDoubleClick: (m: Match) => void;
    onContextMenu?: (req: ResultContextMenuRequest) => void;
  }

  let {
    title,
    subtitle = '',
    items,
    count,
    searchKey = 0,
    selectedWord,
    showDictBadge,
    showBalances,
    showAnnotations,
    fontSize = 13,
    testId,
    emptyMessage = 'No matches',
    onSelect,
    onDoubleClick,
    onContextMenu
  }: Props = $props();

  const itemCount = $derived(count ?? items.length);
  const rowHeight = $derived(Math.max(24, Math.round(fontSize * 2.15)));
  let scrollEl: HTMLDivElement | null = $state(null);
  let selectedIndex = $state(-1);
  let anchorIndex = $state(-1);
  let selectionInitialized = $state(false);
  let allSelected = $state(false);
  let selectedIndices = $state.raw<Set<number>>(new Set());
  let deselectedIndices = $state.raw<Set<number>>(new Set());
  let lastSearchKey = -1;
  let copyAnnouncement = $state('');
  let copiedBadge = $state(false);
  let copyTimer: ReturnType<typeof setTimeout> | null = null;

  function resetSelection() {
    selectedIndex = -1;
    anchorIndex = -1;
    selectionInitialized = false;
    allSelected = false;
    selectedIndices = new Set();
    deselectedIndices = new Set();
  }

  function showCopiedFeedback(announcement: string) {
    copyAnnouncement = announcement;
    copiedBadge = true;
    if (copyTimer) clearTimeout(copyTimer);
    copyTimer = setTimeout(() => {
      copiedBadge = false;
    }, 1500);
  }

  onDestroy(() => {
    if (copyTimer) clearTimeout(copyTimer);
  });

  async function handleCopyAll() {
    if (itemCount === 0) return;
    const text = items
      .slice(0, itemCount)
      .map((m) => m.word)
      .join('\n');
    const ok = await copyTextToClipboard(text);
    if (ok) {
      showCopiedFeedback(
        `Copied ${itemCount.toLocaleString()} ${itemCount === 1 ? 'word' : 'words'} from ${title}`
      );
    }
  }

  function getActiveMatch(): Match | null {
    if (resolvedActiveIndex >= 0 && resolvedActiveIndex < itemCount && items[resolvedActiveIndex]) {
      return items[resolvedActiveIndex];
    }
    if (selectedWord) {
      for (let i = 0; i < itemCount; i++) {
        if (items[i]?.word === selectedWord) return items[i];
      }
    }
    return null;
  }

  function isRowSelected(idx: number): boolean {
    if (idx < 0 || idx >= itemCount) return false;
    if (allSelected) {
      return !deselectedIndices.has(idx);
    }
    if (selectionInitialized) {
      return selectedIndices.has(idx);
    }
    return resolvedActiveIndex === idx || (resolvedActiveIndex < 0 && selectedWord === items[idx]?.word);
  }

  const selectedCount = $derived.by(() => {
    if (itemCount === 0) return 0;
    if (allSelected) {
      let excluded = 0;
      for (const idx of deselectedIndices) {
        if (idx >= 0 && idx < itemCount) excluded++;
      }
      return Math.max(0, itemCount - excluded);
    }
    if (selectionInitialized) {
      let n = 0;
      for (const idx of selectedIndices) {
        if (idx >= 0 && idx < itemCount) n++;
      }
      return n;
    }
    return resolvedActiveIndex >= 0 ? 1 : 0;
  });

  function getSelectedWords(): string[] {
    if (itemCount === 0) return [];
    if (allSelected) {
      const words: string[] = [];
      for (let i = 0; i < itemCount; i++) {
        if (!deselectedIndices.has(i) && items[i]) {
          words.push(items[i].word);
        }
      }
      return words;
    }
    if (selectionInitialized) {
      const sorted = Array.from(selectedIndices)
        .filter((i) => i >= 0 && i < itemCount && items[i])
        .sort((a, b) => a - b);
      if (sorted.length > 0) {
        return sorted.map((i) => items[i].word);
      }
    }
    const active = getActiveMatch();
    return active ? [active.word] : [];
  }

  async function copySelected(): Promise<void> {
    const words = getSelectedWords();
    if (words.length === 0) return;
    const text = words.join('\n');
    const ok = await copyTextToClipboard(text);
    if (ok) {
      showCopiedFeedback(
        words.length === 1
          ? `Copied ${words[0]}`
          : `Copied ${words.length.toLocaleString()} words from ${title}`
      );
    }
  }

  function handleListboxCopy(e: ClipboardEvent) {
    if ((window.getSelection()?.toString() ?? '').trim() !== '') {
      return;
    }
    const words = getSelectedWords();
    if (words.length === 0) return;
    const text = words.join('\n');
    const announcement =
      words.length === 1
        ? `Copied ${words[0]}`
        : `Copied ${words.length.toLocaleString()} words from ${title}`;
    if (e.clipboardData) {
      e.preventDefault();
      e.clipboardData.setData('text/plain', text);
      showCopiedFeedback(announcement);
    } else {
      e.preventDefault();
      void copyTextToClipboard(text).then((ok) => {
        if (ok) showCopiedFeedback(announcement);
      });
    }
  }

  $effect(() => {
    const key = searchKey;
    if (key !== lastSearchKey) {
      lastSearchKey = key;
      resetSelection();
      if (scrollEl) {
        scrollEl.scrollTop = 0;
      }
    }
    // Reset local keyboard cursor and selection when the result list is cleared or shrinks past it.
    if (itemCount === 0) {
      resetSelection();
    } else if (selectedIndex >= itemCount) {
      selectedIndex = -1;
    }
  });

  const virtualizer = createVirtualizer<HTMLDivElement, HTMLDivElement>({
    count: 0,
    getScrollElement: () => scrollEl,
    estimateSize: () => 28,
    overscan: 10,
    initialRect: { width: 420, height: 480 }
  });

  $effect(() => {
    const total = itemCount;
    const el = scrollEl;
    const rh = rowHeight;
    untrack(() => {
      $virtualizer.setOptions({
        count: total,
        getScrollElement: () => el,
        estimateSize: () => rh
      });
      $virtualizer.measure();
    });
  });

  const resolvedActiveIndex = $derived.by(() => {
    if (
      selectedIndex >= 0 &&
      selectedIndex < itemCount &&
      (!selectedWord || items[selectedIndex]?.word === selectedWord)
    ) {
      return selectedIndex;
    }
    if (selectedWord) {
      for (let i = 0; i < itemCount; i++) {
        if (items[i]?.word === selectedWord) return i;
      }
    }
    return -1;
  });

  const activeDescendantId = $derived.by(() => {
    if (resolvedActiveIndex < 0) return undefined;
    const mounted = $virtualizer.getVirtualItems().some((v) => v.index === resolvedActiveIndex);
    return mounted ? `${testId}-opt-${resolvedActiveIndex}` : undefined;
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

  function buildOptionAriaLabel(
    match: Match,
    bal: { omitted: string; added: string }
  ): string {
    const parts: string[] = [match.word];
    if (showAnnotations && match.annotation) {
      parts.push(match.annotation);
    }
    if (bal.omitted) {
      parts.push(`omits ${bal.omitted.replace(/^-/, '')}`);
    }
    if (bal.added) {
      parts.push(`adds ${bal.added.replace(/^\+/, '')}`);
    }
    if (showDictBadge && match.dict) {
      parts.push(match.dict);
    }
    if (match.hasDefinition) {
      parts.push('has local definition');
    } else {
      parts.push(`${match.length} ${match.length === 1 ? 'letter' : 'letters'}`);
    }
    return parts.join(', ');
  }

  function selectSingle(nextIdx: number, notify = true) {
    const clamped = Math.max(0, Math.min(itemCount - 1, nextIdx));
    const chosen = items[clamped];
    if (!chosen) return;
    selectionInitialized = true;
    allSelected = false;
    deselectedIndices = new Set();
    selectedIndices = new Set([clamped]);
    selectedIndex = clamped;
    anchorIndex = clamped;
    if (notify) {
      onSelect(chosen);
    }
  }

  function toggleIndex(idx: number) {
    const clamped = Math.max(0, Math.min(itemCount - 1, idx));
    const chosen = items[clamped];
    if (!chosen) return;

    let isNowSelected = false;
    if (allSelected) {
      const nextDeselected = new Set(deselectedIndices);
      if (nextDeselected.has(clamped)) {
        nextDeselected.delete(clamped);
        isNowSelected = true;
      } else {
        nextDeselected.add(clamped);
        isNowSelected = false;
      }
      deselectedIndices = nextDeselected;
    } else {
      const nextSelected = new Set(selectedIndices);
      if (!selectionInitialized && resolvedActiveIndex >= 0 && resolvedActiveIndex < itemCount) {
        nextSelected.add(resolvedActiveIndex);
      }
      if (nextSelected.has(clamped)) {
        nextSelected.delete(clamped);
        isNowSelected = false;
      } else {
        nextSelected.add(clamped);
        isNowSelected = true;
      }
      selectedIndices = nextSelected;
    }

    selectionInitialized = true;
    selectedIndex = clamped;
    anchorIndex = clamped;
    if (isNowSelected) {
      onSelect(chosen);
    }
  }

  function selectRange(toIdx: number, additive = false) {
    const clamped = Math.max(0, Math.min(itemCount - 1, toIdx));
    const chosen = items[clamped];
    if (!chosen) return;

    const wasInitialized = selectionInitialized;
    const anchor =
      anchorIndex >= 0 && anchorIndex < itemCount
        ? anchorIndex
        : resolvedActiveIndex >= 0 && resolvedActiveIndex < itemCount
          ? resolvedActiveIndex
          : clamped;
    const start = Math.min(anchor, clamped);
    const end = Math.max(anchor, clamped);

    selectionInitialized = true;
    if (start === 0 && end === itemCount - 1) {
      allSelected = true;
      selectedIndices = new Set();
      deselectedIndices = new Set();
    } else if (additive && allSelected) {
      const nextDeselected = new Set(deselectedIndices);
      for (let i = start; i <= end; i++) {
        nextDeselected.delete(i);
      }
      deselectedIndices = nextDeselected;
    } else {
      const nextSelected = additive ? new Set(selectedIndices) : new Set<number>();
      if (additive && !wasInitialized && resolvedActiveIndex >= 0 && resolvedActiveIndex < itemCount) {
        nextSelected.add(resolvedActiveIndex);
      }
      for (let i = start; i <= end; i++) {
        nextSelected.add(i);
      }
      allSelected = false;
      deselectedIndices = new Set();
      selectedIndices = nextSelected;
    }

    selectedIndex = clamped;
    anchorIndex = anchor;
    onSelect(chosen);
  }

  function selectAll() {
    if (itemCount === 0) return;
    selectionInitialized = true;
    allSelected = true;
    selectedIndices = new Set();
    deselectedIndices = new Set();
    if (selectedIndex < 0) {
      selectedIndex = 0;
      anchorIndex = 0;
    }
  }

  function selectIndex(nextIdx: number, extend = false) {
    const clamped = Math.max(0, Math.min(itemCount - 1, nextIdx));
    if (extend) {
      selectRange(clamped, false);
    } else {
      selectSingle(clamped, true);
    }
    $virtualizer.scrollToIndex(clamped, { align: 'auto' });
  }

  function handleRowClick(e: MouseEvent, idx: number) {
    const mod = e.ctrlKey || e.metaKey;
    if (e.shiftKey) {
      selectRange(idx, mod);
    } else if (mod) {
      toggleIndex(idx);
    } else {
      selectSingle(idx, true);
    }
    scrollEl?.focus({ preventScroll: true });
  }

  function openContextMenuForIndex(idx: number, x: number, y: number) {
    const match = items[idx];
    if (!match || !onContextMenu) return;
    if (!isRowSelected(idx)) {
      selectSingle(idx, false);
    } else {
      selectedIndex = idx;
    }
    onContextMenu({
      x,
      y,
      match,
      selectedCount: Math.max(1, selectedCount),
      paneTitle: title,
      onCopy: () => copySelected(),
      onSelectAll: () => selectAll(),
      restoreFocus: () => scrollEl?.focus({ preventScroll: true })
    });
  }

  function handleRowContextMenu(e: MouseEvent, idx: number) {
    e.preventDefault();
    openContextMenuForIndex(idx, e.clientX, e.clientY);
  }

  function handleKeydown(e: KeyboardEvent) {
    if (itemCount === 0) return;
    const curIdx = resolvedActiveIndex;
    const pageStep = Math.max(1, Math.floor((scrollEl?.clientHeight || 280) / rowHeight) - 1);

    if (e.key === 'ArrowDown') {
      e.preventDefault();
      selectIndex(curIdx + 1, e.shiftKey);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      selectIndex(curIdx <= 0 ? 0 : curIdx - 1, e.shiftKey);
    } else if (e.key === 'Home') {
      e.preventDefault();
      selectIndex(0, e.shiftKey);
    } else if (e.key === 'End') {
      e.preventDefault();
      selectIndex(itemCount - 1, e.shiftKey);
    } else if (e.key === 'PageDown') {
      e.preventDefault();
      selectIndex((curIdx < 0 ? 0 : curIdx) + pageStep, e.shiftKey);
    } else if (e.key === 'PageUp') {
      e.preventDefault();
      selectIndex((curIdx < 0 ? 0 : curIdx) - pageStep, e.shiftKey);
    } else if (e.key === 'Enter' && curIdx >= 0 && items[curIdx]) {
      e.preventDefault();
      onDoubleClick(items[curIdx]);
    } else if (
      (e.ctrlKey || e.metaKey) &&
      !e.shiftKey &&
      !e.altKey &&
      e.key.toLowerCase() === 'a'
    ) {
      e.preventDefault();
      selectAll();
    } else if (
      (e.ctrlKey || e.metaKey) &&
      !e.shiftKey &&
      !e.altKey &&
      e.key.toLowerCase() === 'c' &&
      (window.getSelection()?.toString() ?? '').trim() === ''
    ) {
      if (getSelectedWords().length > 0) {
        e.preventDefault();
        void copySelected();
      }
    } else if (e.key === 'ContextMenu' || (e.shiftKey && e.key === 'F10')) {
      const targetIdx = curIdx >= 0 ? curIdx : 0;
      if (items[targetIdx] && onContextMenu) {
        e.preventDefault();
        const rowEl = document.getElementById(`${testId}-opt-${targetIdx}`);
        const rect = rowEl?.getBoundingClientRect() ?? scrollEl?.getBoundingClientRect();
        const x = rect ? rect.left + Math.min(80, rect.width / 2) : 64;
        const y = rect ? rect.top + rect.height / 2 : 64;
        openContextMenuForIndex(targetIdx, x, y);
      }
    }
  }
</script>

<section
  data-testid={testId}
  aria-label={title}
  class="flex h-full min-h-0 min-w-[220px] flex-1 flex-col overflow-hidden rounded-lg border border-slate-800 bg-slate-950/90"
>
  <!-- Pane Header -->
  <div
    style="font-family: var(--tisane-font-toolbar-family); font-size: var(--tisane-font-toolbar-size);"
    class="flex items-center justify-between gap-2 border-b border-slate-800/90 bg-slate-900/90 px-3 py-1.5 select-none"
  >
    <div class="min-w-0">
      <h3 class="truncate font-semibold text-slate-200">{title}</h3>
      {#if subtitle}
        <p class="truncate text-[0.85em] text-slate-400">{subtitle}</p>
      {/if}
    </div>
    <div class="flex shrink-0 items-center gap-1.5">
      {#if selectedCount > 1}
        <span
          data-testid={`${testId}-selected-count`}
          class="shrink-0 rounded-full bg-amber-500/20 px-2 py-0.5 font-mono text-[0.85em] font-medium text-amber-300"
        >
          {selectedCount.toLocaleString()} selected
        </span>
      {/if}
      {#if itemCount > 0}
        <button
          type="button"
          data-testid={`${testId}-copy-btn`}
          onclick={handleCopyAll}
          aria-label={`Copy all ${itemCount.toLocaleString()} matches from ${title}`}
          title={`Copy all ${itemCount.toLocaleString()} words to clipboard`}
          class="inline-flex min-h-6 items-center gap-1 rounded border border-slate-600 bg-slate-800/80 px-2 py-0.5 text-[0.88em] font-medium text-slate-300 transition hover:bg-slate-800 hover:text-slate-100"
        >
          {#if copiedBadge}
            <span class="text-emerald-300"><span aria-hidden="true">✓ </span>Copied</span>
          {:else}
            <span>Copy</span>
          {/if}
        </button>
      {/if}
      <span
        data-testid={`${testId}-count`}
        class="shrink-0 rounded-full bg-slate-800 px-2 py-0.5 font-mono text-[0.92em] font-medium text-amber-300"
      >
        {itemCount.toLocaleString()}
      </span>
    </div>
    <span role="status" aria-live="polite" aria-atomic="true" class="sr-only">
      {copyAnnouncement}
    </span>
  </div>

  <!-- Virtualized Scroll Container (Single-Tab-Stop WAI-ARIA Listbox) -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div
    bind:this={scrollEl}
    tabindex="0"
    role="listbox"
    aria-multiselectable="true"
    aria-label={`${title} matches`}
    aria-activedescendant={activeDescendantId}
    onkeydown={handleKeydown}
    oncopy={handleListboxCopy}
    class="relative flex-1 overflow-y-auto focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-amber-400"
  >
    {#if itemCount === 0}
      <div class="flex h-full items-center justify-center p-4 text-center text-xs text-slate-400">
        {emptyMessage}
      </div>
    {:else}
      <div
        style="height: {$virtualizer.getTotalSize()}px; width: 100%; position: relative;"
      >
        {#each $virtualizer.getVirtualItems() as vRow (vRow.key)}
          {@const match = items[vRow.index]}
          {#if match}
            {@const isSelected = isRowSelected(vRow.index)}
            {@const isActiveCursor = resolvedActiveIndex === vRow.index}
            {@const bal = showBalances ? splitBalance(match.balance) : { omitted: '', added: '' }}
            <button
              type="button"
              id={`${testId}-opt-${vRow.index}`}
              tabindex="-1"
              role="option"
              aria-selected={isSelected}
              aria-posinset={vRow.index + 1}
              aria-setsize={itemCount}
              aria-label={buildOptionAriaLabel(match, bal)}
              data-testid="result-row"
              onmousedown={(e) => {
                if (e.shiftKey) e.preventDefault();
              }}
              onclick={(e) => handleRowClick(e, vRow.index)}
              ondblclick={() => {
                selectSingle(vRow.index, false);
                onDoubleClick(match);
              }}
              oncontextmenu={(e) => handleRowContextMenu(e, vRow.index)}
              style="position: absolute; top: 0; left: 0; width: 100%; height: {vRow.size}px; transform: translateY({vRow.start}px); font-family: var(--tisane-font-results-family); font-size: var(--tisane-font-results-size);"
              class="flex items-center justify-between gap-2 border-b border-slate-900/70 border-l-2 px-3 text-left transition-colors select-none focus:outline-none {isSelected
                ? 'border-l-amber-400 bg-amber-500/20 text-amber-100'
                : 'border-l-transparent text-slate-200 hover:bg-slate-900/90'} {isActiveCursor && selectedCount > 1
                ? 'ring-1 ring-inset ring-amber-400/50'
                : isActiveCursor && !isSelected
                  ? 'ring-1 ring-inset ring-slate-500/70'
                  : ''}"
            >
              <div class="flex min-w-0 items-center gap-1.5" aria-hidden="true">
                <span class="truncate font-medium">{match.word}</span>
                {#if showAnnotations && match.annotation}
                  <span
                    style="font-size: calc(var(--tisane-font-results-size) * 0.78);"
                    class="truncate rounded bg-slate-800 px-1.5 py-0.5 text-slate-400"
                  >
                    {match.annotation}
                  </span>
                {/if}
              </div>

              <div
                class="flex shrink-0 items-center gap-1.5"
                style="font-size: calc(var(--tisane-font-results-size) * 0.78);"
                aria-hidden="true"
              >
                {#if bal.omitted}
                  <span class="rounded bg-rose-500/15 px-1 font-mono text-rose-300">
                    {bal.omitted}
                  </span>
                {/if}
                {#if bal.added}
                  <span class="rounded bg-emerald-500/15 px-1 font-mono text-emerald-300">
                    {bal.added}
                  </span>
                {/if}
                {#if showDictBadge && match.dict}
                  <span class="max-w-[12ch] truncate rounded bg-slate-800/90 px-1.5 py-0.5 text-slate-400">
                    {match.dict}
                  </span>
                {/if}
                {#if match.hasDefinition}
                  <span
                    class="text-amber-400/90"
                    title="Local definition available"
                  >
                    📖
                  </span>
                {:else}
                  <span class="min-w-[2.2ch] text-center font-mono text-slate-400">{match.length}</span>
                {/if}
              </div>
            </button>
          {/if}
        {/each}
      </div>
    {/if}
  </div>
</section>
