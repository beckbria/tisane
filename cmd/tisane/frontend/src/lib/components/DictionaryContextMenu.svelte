<script lang="ts">
  import { onMount } from 'svelte';
  import type { DictionaryInfo } from '../types';

  export interface DictionaryContextMenuRequest {
    x: number;
    y: number;
    dict: DictionaryInfo;
    index: number;
    totalCount: number;
    restoreFocus: () => void;
  }

  interface Props {
    x: number;
    y: number;
    dict: DictionaryInfo;
    index: number;
    totalCount: number;
    uiZoom?: number;
    onToggleEnabled: (id: string, enabled: boolean) => void;
    onShowInfo: (dict: DictionaryInfo) => void;
    onShowWordList: (dict: DictionaryInfo) => void;
    onOpenDirectory: (id: string) => void;
    onMoveToTop: (index: number) => void;
    onMoveToBottom: (index: number) => void;
    onMoveUp: (index: number) => void;
    onMoveDown: (index: number) => void;
    onRemove: (id: string) => void;
    onClose: (restoreFocus?: boolean) => void;
  }

  let {
    x,
    y,
    dict,
    index,
    totalCount,
    uiZoom = 100,
    onToggleEnabled,
    onShowInfo,
    onShowWordList,
    onOpenDirectory,
    onMoveToTop,
    onMoveToBottom,
    onMoveUp,
    onMoveDown,
    onRemove,
    onClose
  }: Props = $props();

  let menuEl: HTMLDivElement | null = $state(null);
  let posLeft = $state(0);
  let posTop = $state(0);

  const zoomScale = $derived((uiZoom || 100) / 100);
  const canMoveUp = $derived(index > 0);
  const canMoveDown = $derived(index >= 0 && index < totalCount - 1);

  $effect(() => {
    const scale = zoomScale;
    const rawX = x / scale;
    const rawY = y / scale;
    const vw = (typeof window !== 'undefined' ? window.innerWidth || 1280 : 1280) / scale;
    const vh = (typeof window !== 'undefined' ? window.innerHeight || 800 : 800) / scale;
    const mw = menuEl?.offsetWidth || 200;
    const mh = menuEl?.offsetHeight || 260;
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

  function triggerToggleEnabled(nextEnabled: boolean) {
    const id = dict.id;
    onClose(true);
    onToggleEnabled(id, nextEnabled);
  }

  function triggerShowInfo() {
    const target = dict;
    // Restore focus to the row before mounting the modal so modalTrap returns focus there on close.
    onClose(true);
    onShowInfo(target);
  }

  function triggerShowWordList() {
    const target = dict;
    onClose(true);
    onShowWordList(target);
  }

  function triggerOpenDirectory() {
    const id = dict.id;
    onClose(true);
    onOpenDirectory(id);
  }

  function triggerMoveToTop() {
    const idx = index;
    onClose(true);
    onMoveToTop(idx);
  }

  function triggerMoveToBottom() {
    const idx = index;
    onClose(true);
    onMoveToBottom(idx);
  }

  function triggerMoveUp() {
    const idx = index;
    onClose(true);
    onMoveUp(idx);
  }

  function triggerMoveDown() {
    const idx = index;
    onClose(true);
    onMoveDown(idx);
  }

  function triggerRemove() {
    const id = dict.id;
    onClose(false);
    onRemove(id);
  }
</script>

<div
  bind:this={menuEl}
  role="menu"
  tabindex="-1"
  data-testid="dict-context-menu"
  aria-label={`Actions for ${dict.name}`}
  style={`left: ${posLeft}px; top: ${posTop}px;`}
  onkeydown={handleKeydown}
  oncontextmenu={(e) => e.preventDefault()}
  class="fixed z-50 max-h-[75vh] min-w-[196px] max-w-[280px] overflow-y-auto rounded-lg border border-slate-600 bg-slate-900 py-1 text-xs text-slate-200 shadow-2xl"
>
  {#if dict.enabled}
    <button
      type="button"
      role="menuitem"
      data-testid="dict-ctx-disable-btn"
      onclick={() => triggerToggleEnabled(false)}
      class="flex w-full items-center justify-between gap-3 px-3 py-1.5 text-left transition hover:bg-slate-800 hover:text-slate-100 focus:bg-slate-800 focus:text-slate-100 focus:outline-none"
    >
      <span class="truncate">Disable</span>
    </button>
  {:else}
    <button
      type="button"
      role="menuitem"
      data-testid="dict-ctx-enable-btn"
      onclick={() => triggerToggleEnabled(true)}
      class="flex w-full items-center justify-between gap-3 px-3 py-1.5 text-left transition hover:bg-slate-800 hover:text-slate-100 focus:bg-slate-800 focus:text-slate-100 focus:outline-none"
    >
      <span class="truncate">Enable</span>
    </button>
  {/if}

  <button
    type="button"
    role="menuitem"
    data-testid="dict-ctx-info-btn"
    onclick={triggerShowInfo}
    class="flex w-full items-center justify-between gap-3 px-3 py-1.5 text-left transition hover:bg-slate-800 hover:text-slate-100 focus:bg-slate-800 focus:text-slate-100 focus:outline-none"
  >
    <span class="truncate">Information</span>
  </button>

  <button
    type="button"
    role="menuitem"
    data-testid="dict-ctx-wordlist-btn"
    onclick={triggerShowWordList}
    class="flex w-full items-center justify-between gap-3 px-3 py-1.5 text-left transition hover:bg-slate-800 hover:text-slate-100 focus:bg-slate-800 focus:text-slate-100 focus:outline-none"
  >
    <span class="truncate">Word List</span>
  </button>

  <button
    type="button"
    role="menuitem"
    data-testid="dict-ctx-open-dir-btn"
    onclick={triggerOpenDirectory}
    class="flex w-full items-center justify-between gap-3 px-3 py-1.5 text-left transition hover:bg-slate-800 hover:text-slate-100 focus:bg-slate-800 focus:text-slate-100 focus:outline-none"
  >
    <span class="truncate">Open Directory</span>
  </button>

  <div role="separator" class="my-1 border-t border-slate-700/80"></div>

  <button
    type="button"
    role="menuitem"
    disabled={!canMoveUp}
    data-testid="dict-ctx-move-top-btn"
    onclick={triggerMoveToTop}
    class="flex w-full items-center justify-between gap-3 px-3 py-1.5 text-left transition enabled:hover:bg-slate-800 enabled:hover:text-slate-100 enabled:focus:bg-slate-800 enabled:focus:text-slate-100 focus:outline-none disabled:cursor-not-allowed disabled:opacity-40"
  >
    <span class="truncate">Move to Top</span>
  </button>

  <button
    type="button"
    role="menuitem"
    disabled={!canMoveDown}
    data-testid="dict-ctx-move-bottom-btn"
    onclick={triggerMoveToBottom}
    class="flex w-full items-center justify-between gap-3 px-3 py-1.5 text-left transition enabled:hover:bg-slate-800 enabled:hover:text-slate-100 enabled:focus:bg-slate-800 enabled:focus:text-slate-100 focus:outline-none disabled:cursor-not-allowed disabled:opacity-40"
  >
    <span class="truncate">Move to Bottom</span>
  </button>

  <button
    type="button"
    role="menuitem"
    disabled={!canMoveUp}
    data-testid="dict-ctx-move-up-btn"
    onclick={triggerMoveUp}
    class="flex w-full items-center justify-between gap-3 px-3 py-1.5 text-left transition enabled:hover:bg-slate-800 enabled:hover:text-slate-100 enabled:focus:bg-slate-800 enabled:focus:text-slate-100 focus:outline-none disabled:cursor-not-allowed disabled:opacity-40"
  >
    <span class="truncate">Move Up</span>
  </button>

  <button
    type="button"
    role="menuitem"
    disabled={!canMoveDown}
    data-testid="dict-ctx-move-down-btn"
    onclick={triggerMoveDown}
    class="flex w-full items-center justify-between gap-3 px-3 py-1.5 text-left transition enabled:hover:bg-slate-800 enabled:hover:text-slate-100 enabled:focus:bg-slate-800 enabled:focus:text-slate-100 focus:outline-none disabled:cursor-not-allowed disabled:opacity-40"
  >
    <span class="truncate">Move Down</span>
  </button>

  <div role="separator" class="my-1 border-t border-slate-700/80"></div>

  <button
    type="button"
    role="menuitem"
    data-testid="dict-ctx-remove-btn"
    onclick={triggerRemove}
    class="flex w-full items-center justify-between gap-3 px-3 py-1.5 text-left text-rose-300 transition hover:bg-rose-500/20 hover:text-rose-200 focus:bg-rose-500/20 focus:text-rose-200 focus:outline-none"
  >
    <span class="truncate">Remove From List</span>
  </button>
</div>
