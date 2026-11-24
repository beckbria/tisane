<script lang="ts">
  import { onMount } from 'svelte';
  import type { Match, WebProvider } from '../types';

  export interface ResultContextMenuRequest {
    x: number;
    y: number;
    match: Match;
    selectedCount: number;
    paneTitle: string;
    onCopy: () => void | Promise<void>;
    onSelectAll: () => void;
    restoreFocus: () => void;
  }

  interface Props {
    x: number;
    y: number;
    match: Match;
    selectedCount: number;
    paneTitle: string;
    webProviders: WebProvider[];
    defaultWebProvider: string;
    uiZoom?: number;
    onCopy: () => void | Promise<void>;
    onSelectAll: () => void;
    onLookupInternal: (match: Match) => void;
    onLookupExternal: (word: string, providerId: string) => void;
    onClose: (restoreFocus?: boolean) => void;
  }

  let {
    x,
    y,
    match,
    selectedCount,
    paneTitle,
    webProviders,
    defaultWebProvider,
    uiZoom = 100,
    onCopy,
    onSelectAll,
    onLookupInternal,
    onLookupExternal,
    onClose
  }: Props = $props();

  let menuEl: HTMLDivElement | null = $state(null);
  let posLeft = $state(0);
  let posTop = $state(0);

  const zoomScale = $derived((uiZoom || 100) / 100);

  $effect(() => {
    const scale = zoomScale;
    const rawX = x / scale;
    const rawY = y / scale;
    const vw = (typeof window !== 'undefined' ? window.innerWidth || 1280 : 1280) / scale;
    const vh = (typeof window !== 'undefined' ? window.innerHeight || 800 : 800) / scale;
    const mw = menuEl?.offsetWidth || 232;
    const mh = menuEl?.offsetHeight || 240;
    const margin = 8;

    posLeft = Math.max(margin, Math.min(rawX, vw - mw - margin));
    posTop = Math.max(margin, Math.min(rawY, vh - mh - margin));
  });

  function getMenuItems(): HTMLButtonElement[] {
    if (!menuEl) return [];
    return Array.from(menuEl.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not(:disabled)'));
  }

  function handleKeydown(e: KeyboardEvent) {
    const items = getMenuItems();
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      onClose(true);
      return;
    }
    if (e.key === 'Tab') {
      e.preventDefault();
      onClose(true);
      return;
    }
    if (items.length === 0) return;

    const currentIdx = items.indexOf(document.activeElement as HTMLButtonElement);
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      items[(currentIdx + 1) % items.length]?.focus();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      items[(currentIdx - 1 + items.length) % items.length]?.focus();
    } else if (e.key === 'Home') {
      e.preventDefault();
      items[0]?.focus();
    } else if (e.key === 'End') {
      e.preventDefault();
      items[items.length - 1]?.focus();
    }
  }

  onMount(() => {
    const items = getMenuItems();
    items[0]?.focus({ preventScroll: true });

    const onWindowMouseDown = (e: MouseEvent) => {
      if (menuEl && !menuEl.contains(e.target as Node)) {
        onClose(false);
      }
    };
    const onWindowBlur = () => onClose(false);
    const onWindowResize = () => onClose(false);

    window.addEventListener('mousedown', onWindowMouseDown, true);
    window.addEventListener('blur', onWindowBlur);
    window.addEventListener('resize', onWindowResize);

    return () => {
      window.removeEventListener('mousedown', onWindowMouseDown, true);
      window.removeEventListener('blur', onWindowBlur);
      window.removeEventListener('resize', onWindowResize);
    };
  });

  function triggerCopy() {
    const fn = onCopy;
    onClose(true);
    void fn();
  }

  function triggerSelectAll() {
    const fn = onSelectAll;
    fn();
    onClose(true);
  }

  function triggerInternalLookup() {
    const fn = onLookupInternal;
    const target = match;
    onClose(true);
    fn(target);
  }

  function triggerExternalLookup(providerId: string) {
    const fn = onLookupExternal;
    const word = match.word;
    onClose(true);
    fn(word, providerId);
  }
</script>

<div
  bind:this={menuEl}
  role="menu"
  tabindex="-1"
  data-testid="result-context-menu"
  aria-label={`Actions for ${match.word} in ${paneTitle}`}
  style={`left: ${posLeft}px; top: ${posTop}px;`}
  onkeydown={handleKeydown}
  oncontextmenu={(e) => e.preventDefault()}
  class="fixed z-50 max-h-[70vh] min-w-[220px] max-w-[300px] overflow-y-auto rounded-lg border border-slate-600 bg-slate-900 py-1 text-xs text-slate-200 shadow-2xl"
>
  <button
    type="button"
    role="menuitem"
    data-testid="ctx-copy-btn"
    onclick={triggerCopy}
    class="flex w-full items-center justify-between gap-3 px-3 py-1.5 text-left transition hover:bg-slate-800 hover:text-slate-100 focus:bg-slate-800 focus:text-slate-100 focus:outline-none"
  >
    <span class="truncate font-medium">
      {#if selectedCount > 1}
        Copy ({selectedCount.toLocaleString()} selected)
      {:else}
        Copy
      {/if}
    </span>
    <span class="shrink-0 font-mono text-[10px] text-slate-400">Ctrl+C</span>
  </button>

  <button
    type="button"
    role="menuitem"
    data-testid="ctx-select-all-btn"
    onclick={triggerSelectAll}
    class="flex w-full items-center justify-between gap-3 px-3 py-1.5 text-left transition hover:bg-slate-800 hover:text-slate-100 focus:bg-slate-800 focus:text-slate-100 focus:outline-none"
  >
    <span class="truncate">Select All</span>
    <span class="shrink-0 font-mono text-[10px] text-slate-400">Ctrl+A</span>
  </button>

  <div role="separator" class="my-1 border-t border-slate-700/80"></div>

  <div class="truncate px-3 py-1 text-[10px] font-semibold tracking-wider text-slate-400 uppercase select-none">
    Look up “{match.word}”
  </div>

  <button
    type="button"
    role="menuitem"
    data-testid="ctx-lookup-internal-btn"
    onclick={triggerInternalLookup}
    class="flex w-full items-center justify-between gap-2 px-3 py-1.5 text-left transition hover:bg-slate-800 hover:text-slate-100 focus:bg-slate-800 focus:text-slate-100 focus:outline-none"
  >
    <span class="flex min-w-0 items-center gap-2">
      <span aria-hidden="true" class="text-amber-400">📖</span>
      <span class="truncate">Local definitions</span>
    </span>
    {#if match.hasDefinition}
      <span class="shrink-0 rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium text-amber-300">
        Available
      </span>
    {/if}
  </button>

  {#if webProviders.length > 0}
    <div role="separator" class="my-1 border-t border-slate-700/60"></div>
    {#each webProviders as provider (provider.id)}
      <button
        type="button"
        role="menuitem"
        data-testid={`ctx-lookup-web-${provider.id}`}
        onclick={() => triggerExternalLookup(provider.id)}
        class="flex w-full items-center justify-between gap-2 px-3 py-1.5 text-left transition hover:bg-slate-800 hover:text-slate-100 focus:bg-slate-800 focus:text-slate-100 focus:outline-none"
      >
        <span class="flex min-w-0 items-center gap-2">
          <span aria-hidden="true" class="text-slate-400">↗</span>
          <span class="truncate">{provider.name}</span>
        </span>
        {#if provider.id === defaultWebProvider}
          <span class="shrink-0 rounded bg-slate-800 px-1.5 py-0.5 text-[10px] text-slate-400">
            Default
          </span>
        {/if}
      </button>
    {/each}
  {/if}
</div>
