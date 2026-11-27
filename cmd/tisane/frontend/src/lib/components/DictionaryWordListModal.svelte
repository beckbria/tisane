<script lang="ts">
  import { onDestroy } from 'svelte';
  import { modalTrap } from '../actions/modalTrap';
  import { getBackend } from '../bindings';
  import { copyTextToClipboard } from '../clipboard';
  import type { DictionaryInfo } from '../types';

  const PAGE_SIZE = 500;
  const MAX_CACHED_PAGES = 24;
  const MAX_SCROLL_HEIGHT_PX = 8_000_000;
  const OVERSCAN_ROWS = 12;

  interface Props {
    open: boolean;
    dict: DictionaryInfo | null;
    fontSize?: number;
    onClose: () => void;
    onLookupWord?: (word: string) => void;
  }

  let {
    open,
    dict,
    fontSize = 13,
    onClose,
    onLookupWord
  }: Props = $props();

  let scrollEl: HTMLDivElement | null = $state(null);
  let scrollTop = $state(0);
  let viewportHeight = $state(420);

  let isLoadingInitial = $state(false);
  let errorMessage = $state('');
  let totalWords = $state(0);
  let selectedIndex = $state(-1);
  let jumpText = $state('');
  let copiedWord = $state(false);
  let pageCache = $state.raw<Map<number, string[]>>(new Map());

  let sessionSeq = 0;
  let activeDictId: string | null = null;
  let wasOpen = false;
  let inFlightPages = new Set<number>();
  let jumpTimer: ReturnType<typeof setTimeout> | null = null;
  let copyTimer: ReturnType<typeof setTimeout> | null = null;

  const rowHeight = $derived(Math.max(24, Math.round(fontSize * 1.95)));
  const rawTotalHeight = $derived(totalWords * rowHeight);
  const isScaledScroll = $derived(rawTotalHeight > MAX_SCROLL_HEIGHT_PX);
  const scrollHeight = $derived(
    totalWords === 0 ? 0 : Math.max(viewportHeight, Math.min(rawTotalHeight, MAX_SCROLL_HEIGHT_PX))
  );

  const visibleRows = $derived(Math.max(1, Math.floor(viewportHeight / rowHeight)));

  const scrollPosition = $derived.by(() => {
    if (totalWords <= 0) {
      return { firstIndex: 0, subRowOffset: 0 };
    }
    if (!isScaledScroll) {
      const firstIndex = Math.min(
        Math.max(0, totalWords - 1),
        Math.max(0, Math.floor(scrollTop / rowHeight))
      );
      const subRowOffset = Math.max(0, scrollTop - firstIndex * rowHeight);
      return { firstIndex, subRowOffset };
    }
    const maxScrollTop = Math.max(1, scrollHeight - viewportHeight);
    const maxFirstIndex = Math.max(0, totalWords - visibleRows);
    const clampedTop = Math.min(maxScrollTop, Math.max(0, scrollTop));
    const exactIndex = (clampedTop / maxScrollTop) * maxFirstIndex;
    const firstIndex = Math.floor(exactIndex);
    const subRowOffset = (exactIndex - firstIndex) * rowHeight;
    return { firstIndex, subRowOffset };
  });

  const renderRange = $derived.by(() => {
    if (totalWords <= 0) {
      return { start: 0, end: 0 };
    }
    const start = Math.max(0, scrollPosition.firstIndex - OVERSCAN_ROWS);
    const capacity = Math.ceil(viewportHeight / rowHeight) + 1 + OVERSCAN_ROWS;
    const end = Math.min(totalWords, scrollPosition.firstIndex + capacity);
    return { start, end };
  });

  const renderedIndices = $derived.by(() => {
    const { start, end } = renderRange;
    const out: number[] = [];
    for (let i = start; i < end; i++) {
      out.push(i);
    }
    return out;
  });

  function getWordAt(index: number): string | undefined {
    if (index < 0 || index >= totalWords) return undefined;
    const pageIdx = Math.floor(index / PAGE_SIZE);
    const page = pageCache.get(pageIdx);
    return page?.[index - pageIdx * PAGE_SIZE];
  }

  const selectedWord = $derived(selectedIndex >= 0 ? (getWordAt(selectedIndex) ?? null) : null);

  function storePageInCache(pageIdx: number, words: string[]) {
    const next = new Map(pageCache);
    if (next.has(pageIdx)) {
      next.delete(pageIdx);
    }
    next.set(pageIdx, words);
    while (next.size > MAX_CACHED_PAGES) {
      const oldest = next.keys().next().value;
      if (oldest === undefined) break;
      next.delete(oldest);
    }
    pageCache = next;
  }

  async function fetchPage(seq: number, dictId: string, pageIdx: number) {
    if (pageIdx < 0 || inFlightPages.has(pageIdx) || pageCache.has(pageIdx)) {
      return;
    }
    inFlightPages.add(pageIdx);
    try {
      const res = await getBackend().getDictionaryWords({
        id: dictId,
        offset: pageIdx * PAGE_SIZE,
        limit: PAGE_SIZE
      });
      if (seq !== sessionSeq) return;
      totalWords = res.totalWords;
      storePageInCache(pageIdx, res.words ?? []);
    } catch (err) {
      if (seq !== sessionSeq) return;
      errorMessage = err instanceof Error ? err.message : String(err);
    } finally {
      if (seq === sessionSeq) {
        inFlightPages.delete(pageIdx);
      }
    }
  }

  function ensureRangeLoaded(seq: number, dictId: string, startIdx: number, endIdx: number) {
    if (endIdx <= startIdx || totalWords <= 0) return;
    const firstPage = Math.max(0, Math.floor(startIdx / PAGE_SIZE));
    const lastPage = Math.max(0, Math.floor((endIdx - 1) / PAGE_SIZE));
    for (let p = firstPage; p <= lastPage; p++) {
      void fetchPage(seq, dictId, p);
    }
  }

  async function startSession(targetDict: DictionaryInfo) {
    const seq = ++sessionSeq;
    activeDictId = targetDict.id;
    inFlightPages = new Set([0]);
    if (jumpTimer) {
      clearTimeout(jumpTimer);
      jumpTimer = null;
    }
    isLoadingInitial = true;
    errorMessage = '';
    totalWords = 0;
    selectedIndex = -1;
    jumpText = '';
    copiedWord = false;
    scrollTop = 0;
    pageCache = new Map();
    if (scrollEl) {
      scrollEl.scrollTop = 0;
    }

    try {
      const res = await getBackend().getDictionaryWords({
        id: targetDict.id,
        offset: 0,
        limit: PAGE_SIZE
      });
      if (seq !== sessionSeq) return;
      inFlightPages.delete(0);
      totalWords = res.totalWords;
      const words = res.words ?? [];
      const initialMap = new Map<number, string[]>();
      initialMap.set(0, words);
      pageCache = initialMap;
      selectedIndex = res.totalWords > 0 ? 0 : -1;
      isLoadingInitial = false;
    } catch (err) {
      if (seq !== sessionSeq) return;
      inFlightPages.delete(0);
      errorMessage = err instanceof Error ? err.message : String(err);
      isLoadingInitial = false;
    }
  }

  function closeSession() {
    sessionSeq++;
    activeDictId = null;
    inFlightPages.clear();
    if (jumpTimer) {
      clearTimeout(jumpTimer);
      jumpTimer = null;
    }
    if (copyTimer) {
      clearTimeout(copyTimer);
      copyTimer = null;
    }
    pageCache = new Map();
    totalWords = 0;
    selectedIndex = -1;
    isLoadingInitial = false;
    errorMessage = '';
    void getBackend().closeDictionaryWordList().catch(() => {});
  }

  $effect(() => {
    const isOpen = open && dict !== null;
    const target = dict;
    if (isOpen && target) {
      wasOpen = true;
      if (activeDictId !== target.id) {
        void startSession(target);
      }
    } else if (wasOpen) {
      wasOpen = false;
      closeSession();
    }
  });

  $effect(() => {
    const el = scrollEl;
    if (!el || typeof window === 'undefined') return;
    const measure = () => {
      const h = el.clientHeight;
      if (h > 0) {
        viewportHeight = h;
      }
    };
    measure();
    if (typeof ResizeObserver !== 'undefined') {
      const ro = new ResizeObserver(measure);
      ro.observe(el);
      return () => ro.disconnect();
    }
  });

  onDestroy(() => {
    if (wasOpen) {
      wasOpen = false;
      closeSession();
    } else {
      if (jumpTimer) clearTimeout(jumpTimer);
      if (copyTimer) clearTimeout(copyTimer);
    }
  });

  function scrollToIndex(targetIdx: number, align: 'nearest' | 'top' = 'nearest') {
    if (!scrollEl || totalWords <= 0) return;
    const clamped = Math.max(0, Math.min(totalWords - 1, targetIdx));
    let nextTop: number | null = null;

    if (!isScaledScroll) {
      const itemTop = clamped * rowHeight;
      const itemBottom = itemTop + rowHeight;
      const curTop = scrollEl.scrollTop;
      const curBottom = curTop + viewportHeight;
      if (align === 'top' || itemTop < curTop) {
        nextTop = itemTop;
      } else if (itemBottom > curBottom) {
        nextTop = Math.max(0, itemBottom - viewportHeight);
      }
    } else {
      const maxScrollTop = Math.max(1, scrollHeight - viewportHeight);
      const maxFirstIndex = Math.max(0, totalWords - visibleRows);
      const first = scrollPosition.firstIndex;
      if (align === 'top' || clamped < first) {
        const targetFirst = Math.min(maxFirstIndex, clamped);
        nextTop = maxFirstIndex > 0 ? (targetFirst / maxFirstIndex) * maxScrollTop : 0;
      } else if (clamped >= first + visibleRows) {
        const targetFirst = Math.min(maxFirstIndex, Math.max(0, clamped - visibleRows + 1));
        nextTop = maxFirstIndex > 0 ? (targetFirst / maxFirstIndex) * maxScrollTop : 0;
      }
    }

    if (nextTop !== null) {
      scrollEl.scrollTop = nextTop;
      scrollTop = nextTop;
    }

    if (activeDictId) {
      const start = Math.max(0, clamped - OVERSCAN_ROWS);
      const end = Math.min(totalWords, clamped + visibleRows + OVERSCAN_ROWS);
      ensureRangeLoaded(sessionSeq, activeDictId, start, end);
    }
  }

  function handleScroll(e: Event) {
    const el = e.currentTarget as HTMLDivElement;
    scrollTop = el.scrollTop;
    if (el.clientHeight > 0 && el.clientHeight !== viewportHeight) {
      viewportHeight = el.clientHeight;
    }
    if (activeDictId && totalWords > 0) {
      ensureRangeLoaded(sessionSeq, activeDictId, renderRange.start, renderRange.end);
    }
  }

  async function executeJump(prefix: string) {
    if (!dict || !open) return;
    const seq = sessionSeq;
    const trimmed = prefix.trim();
    if (trimmed === '') {
      selectedIndex = totalWords > 0 ? 0 : -1;
      scrollToIndex(0, 'top');
      return;
    }
    try {
      const res = await getBackend().getDictionaryWords({
        id: dict.id,
        offset: -1,
        limit: PAGE_SIZE,
        jumpPrefix: trimmed
      });
      if (seq !== sessionSeq) return;
      totalWords = res.totalWords;
      if (res.offset >= 0 && res.offset % PAGE_SIZE === 0) {
        storePageInCache(res.offset / PAGE_SIZE, res.words ?? []);
      }
      if (res.jumpIndex >= 0 && res.totalWords > 0) {
        selectedIndex = res.jumpIndex;
        scrollToIndex(res.jumpIndex, 'top');
      }
    } catch (err) {
      if (seq !== sessionSeq) return;
      errorMessage = err instanceof Error ? err.message : String(err);
    }
  }

  function handleJumpInput(nextValue: string) {
    jumpText = nextValue;
    if (jumpTimer) clearTimeout(jumpTimer);
    jumpTimer = setTimeout(() => {
      void executeJump(nextValue);
    }, 80);
  }

  async function handleCopySelected() {
    if (!selectedWord) return;
    const ok = await copyTextToClipboard(selectedWord);
    if (ok) {
      copiedWord = true;
      if (copyTimer) clearTimeout(copyTimer);
      copyTimer = setTimeout(() => {
        copiedWord = false;
      }, 1500);
    }
  }

  function selectWordIndex(idx: number) {
    if (totalWords <= 0) return;
    const clamped = Math.max(0, Math.min(totalWords - 1, idx));
    selectedIndex = clamped;
    scrollToIndex(clamped, 'nearest');
  }

  function handleListboxKeydown(e: KeyboardEvent) {
    if (totalWords <= 0) return;
    const cur = selectedIndex >= 0 ? selectedIndex : scrollPosition.firstIndex;
    const pageStep = Math.max(1, visibleRows - 1);

    if (e.key === 'ArrowDown') {
      e.preventDefault();
      selectWordIndex(cur + 1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      selectWordIndex(cur - 1);
    } else if (e.key === 'PageDown') {
      e.preventDefault();
      selectWordIndex(cur + pageStep);
    } else if (e.key === 'PageUp') {
      e.preventDefault();
      selectWordIndex(cur - pageStep);
    } else if (e.key === 'Home') {
      e.preventDefault();
      selectWordIndex(0);
    } else if (e.key === 'End') {
      e.preventDefault();
      selectWordIndex(totalWords - 1);
    } else if (e.key === 'Enter') {
      const w = getWordAt(cur);
      if (w && onLookupWord) {
        e.preventDefault();
        onLookupWord(w);
      }
    } else if (
      (e.ctrlKey || e.metaKey) &&
      !e.shiftKey &&
      !e.altKey &&
      e.key.toLowerCase() === 'c' &&
      (window.getSelection()?.toString() ?? '').trim() === ''
    ) {
      if (selectedWord) {
        e.preventDefault();
        void handleCopySelected();
      }
    }
  }

  function computeRowTop(index: number): number {
    if (!isScaledScroll) {
      return index * rowHeight;
    }
    return scrollTop + (index - scrollPosition.firstIndex) * rowHeight - scrollPosition.subRowOffset;
  }

  const activeDescendantId = $derived(
    selectedIndex >= renderRange.start && selectedIndex < renderRange.end
      ? `dict-wl-opt-${selectedIndex}`
      : undefined
  );
</script>

{#if open && dict}
  <div
    use:modalTrap={{ onClose }}
    class="fixed inset-0 z-60 flex items-center justify-center bg-black/75 p-4 backdrop-blur-xs"
    role="dialog"
    aria-modal="true"
    aria-label={`Word List: ${dict.name}`}
    tabindex="-1"
    data-testid="dict-wordlist-modal"
  >
    <div
      class="flex h-[78vh] max-h-[680px] min-h-[340px] w-full max-w-lg flex-col overflow-hidden rounded-xl border border-slate-700 bg-slate-900 shadow-2xl"
    >
      <!-- Header -->
      <div class="flex items-center justify-between gap-3 border-b border-slate-800 px-5 py-3">
        <div class="flex min-w-0 items-center gap-2.5">
          <h2
            data-testid="dict-wordlist-heading"
            class="truncate text-sm font-semibold text-slate-100"
          >
            {dict.name} — Word List
          </h2>
          {#if !isLoadingInitial && !errorMessage}
            <span
              data-testid="dict-wordlist-count"
              class="shrink-0 rounded-full bg-slate-800 px-2 py-0.5 font-mono text-[11px] font-medium text-amber-300"
            >
              {totalWords.toLocaleString()} {totalWords === 1 ? 'word' : 'words'}
            </span>
          {/if}
        </div>
        <button
          type="button"
          onclick={onClose}
          aria-label="Close Word List dialog"
          data-testid="close-dict-wordlist-btn"
          class="inline-flex min-h-6 min-w-6 shrink-0 items-center justify-center rounded text-xs text-slate-400 hover:bg-slate-800 hover:text-slate-100"
        >
          <span aria-hidden="true">✕</span>
        </button>
      </div>

      <!-- Jump & Copy Bar -->
      <div class="flex items-center gap-2 border-b border-slate-800/90 bg-slate-950/50 px-4 py-2">
        <input
          type="search"
          data-autofocus
          value={jumpText}
          oninput={(e) => handleJumpInput((e.currentTarget as HTMLInputElement).value)}
          onkeydown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              if (jumpTimer) clearTimeout(jumpTimer);
              void executeJump(jumpText);
            } else if (e.key === 'ArrowDown' && totalWords > 0) {
              e.preventDefault();
              scrollEl?.focus();
            }
          }}
          placeholder="Jump to word or prefix…"
          aria-label="Jump to word or prefix"
          data-testid="dict-wordlist-jump-input"
          class="min-w-0 flex-1 rounded-md border border-slate-600 bg-slate-900 px-2.5 py-1 text-xs text-slate-100 placeholder-slate-400 focus:border-amber-400 focus:outline-none"
        />
        <button
          type="button"
          disabled={!selectedWord}
          onclick={handleCopySelected}
          data-testid="dict-wordlist-copy-btn"
          aria-label={selectedWord ? `Copy ${selectedWord}` : 'Copy selected word'}
          class="shrink-0 rounded-md border border-slate-600 bg-slate-800/80 px-2.5 py-1 text-xs font-medium text-slate-200 transition enabled:hover:bg-slate-800 enabled:hover:text-slate-100 disabled:opacity-40"
        >
          {#if copiedWord}
            <span class="text-emerald-300"><span aria-hidden="true">✓ </span>Copied</span>
          {:else}
            Copy
          {/if}
        </button>
      </div>

      <!-- Scrollable Virtualized Alphabetical Word List -->
      {#if isLoadingInitial}
        <div
          data-testid="dict-wordlist-loading"
          class="flex flex-1 items-center justify-center gap-2 p-6 text-xs text-slate-300"
        >
          <span aria-hidden="true" class="inline-block h-2 w-2 animate-ping rounded-full bg-amber-400"></span>
          <span>Loading alphabetical word list…</span>
        </div>
      {:else if errorMessage}
        <div
          role="alert"
          data-testid="dict-wordlist-error"
          class="flex flex-1 items-center justify-center p-6 text-center text-xs text-rose-400"
        >
          Error: {errorMessage}
        </div>
      {:else if totalWords === 0}
        <div
          data-testid="dict-wordlist-empty"
          class="flex flex-1 items-center justify-center p-6 text-center text-xs text-slate-400"
        >
          This dictionary contains no words.
        </div>
      {:else}
        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
        <div
          bind:this={scrollEl}
          tabindex="0"
          role="listbox"
          aria-label={`Alphabetical words in ${dict.name}`}
          aria-activedescendant={activeDescendantId}
          data-testid="dict-wordlist-scroll"
          onscroll={handleScroll}
          onkeydown={handleListboxKeydown}
          class="relative flex-1 overflow-y-auto bg-slate-950/80 focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-amber-400"
        >
          <div style={`height: ${scrollHeight}px; width: 100%; position: relative;`}>
            {#each renderedIndices as itemIdx (itemIdx)}
              {@const word = getWordAt(itemIdx)}
              {@const isSelected = selectedIndex === itemIdx}
              <button
                type="button"
                id={`dict-wl-opt-${itemIdx}`}
                tabindex="-1"
                role="option"
                aria-selected={isSelected}
                aria-posinset={itemIdx + 1}
                aria-setsize={totalWords}
                data-testid="dict-wordlist-row"
                onclick={() => {
                  selectedIndex = itemIdx;
                }}
                ondblclick={() => {
                  selectedIndex = itemIdx;
                  if (word && onLookupWord) {
                    onLookupWord(word);
                  }
                }}
                style={`position: absolute; top: 0; left: 0; width: 100%; height: ${rowHeight}px; transform: translateY(${computeRowTop(itemIdx)}px); font-family: var(--tisane-font-results-family); font-size: var(--tisane-font-results-size);`}
                class="flex items-center justify-between gap-3 border-b border-slate-900/70 border-l-2 px-4 text-left transition-colors select-none focus:outline-none {isSelected
                  ? 'border-l-amber-400 bg-amber-500/20 text-amber-100'
                  : 'border-l-transparent text-slate-200 hover:bg-slate-900/90'}"
              >
                {#if word !== undefined}
                  <span class="truncate font-medium">{word}</span>
                {:else}
                  <span class="text-slate-400">…</span>
                {/if}
                <span class="shrink-0 font-mono text-[10px] text-slate-400">
                  #{(itemIdx + 1).toLocaleString()}
                </span>
              </button>
            {/each}
          </div>
        </div>
      {/if}

      <!-- Footer -->
      <div class="flex items-center justify-between gap-2 border-t border-slate-800 bg-slate-950/60 px-5 py-3">
        <div class="truncate text-[11px] text-slate-400">
          {#if !isLoadingInitial && !errorMessage && totalWords > 0}
            Alphabetical order · {totalWords.toLocaleString()} {totalWords === 1 ? 'entry' : 'entries'}
          {/if}
        </div>
        <button
          type="button"
          onclick={onClose}
          data-testid="dict-wordlist-close-footer-btn"
          class="rounded-lg border border-slate-600 px-3.5 py-1.5 text-xs font-medium text-slate-200 transition hover:bg-slate-800 hover:text-slate-100"
        >
          Close
        </button>
      </div>
    </div>
  </div>
{/if}
