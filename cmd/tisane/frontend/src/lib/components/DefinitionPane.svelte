<script lang="ts">
  import type { LookupResult } from '../types';

  interface Props {
    height?: number;
    collapsed?: boolean;
    selectedWord: string | null;
    lookupResult: LookupResult | null;
    isLoading: boolean;
    includeMentions: boolean;
    canGoBack: boolean;
    canGoForward: boolean;
    defaultWebProvider: string;
    onBack: () => void;
    onForward: () => void;
    onToggleIncludeMentions: (v: boolean) => void;
    onCrossLookupWord: (word: string) => void;
    onOpenWebLookup: (word: string, providerId: string) => void;
    onOpenExternalURL: (url: string) => void;
    onCollapse?: () => void;
  }

  let {
    height = 256,
    collapsed = false,
    selectedWord,
    lookupResult,
    isLoading,
    includeMentions,
    canGoBack,
    canGoForward,
    defaultWebProvider,
    onBack,
    onForward,
    onToggleIncludeMentions,
    onCrossLookupWord,
    onOpenWebLookup,
    onOpenExternalURL,
    onCollapse
  }: Props = $props();

  function handleDefClick(e: MouseEvent) {
    const target = e.target as HTMLElement | null;
    const anchor = target?.closest('a');
    if (anchor) {
      e.preventDefault();
      const href = anchor.getAttribute('href') || '';
      if (/^(https?:|mailto:)/i.test(href)) {
        onOpenExternalURL(href);
      }
    }
  }

  function handleDefDoubleClick() {
    const sel = window.getSelection()?.toString().trim() ?? '';
    const cleaned = sel.replace(/^[^A-Za-z0-9À-ÿ]+|[^A-Za-z0-9À-ÿ'-]+$/g, '');
    if (cleaned.length >= 2 && cleaned.length <= 64) {
      onCrossLookupWord(cleaned);
    }
  }
</script>

{#if !collapsed}
<section
  aria-label="Word definitions"
  data-testid="definition-pane"
  style={`height: ${height}px;`}
  class="flex max-h-[65%] shrink-0 flex-col overflow-hidden border-t border-slate-800 bg-slate-900/90"
>
  <!-- Definition Header Bar -->
  <div class="flex shrink-0 items-center justify-between gap-3 border-b border-slate-800 px-4 py-1.5 select-none">
    <div class="flex items-center gap-2 min-w-0">
      <div class="inline-flex rounded bg-slate-950 ring-1 ring-slate-800">
        <button
          type="button"
          disabled={!canGoBack}
          data-testid="def-back-btn"
          onclick={onBack}
          class="px-2 py-0.5 text-xs text-slate-300 hover:bg-slate-800 disabled:opacity-30"
          title="Previous looked-up word"
          aria-label="Back"
        >
          <span aria-hidden="true">←</span>
        </button>
        <button
          type="button"
          disabled={!canGoForward}
          data-testid="def-forward-btn"
          onclick={onForward}
          class="border-l border-slate-800 px-2 py-0.5 text-xs text-slate-300 hover:bg-slate-800 disabled:opacity-30"
          title="Next looked-up word"
          aria-label="Forward"
        >
          <span aria-hidden="true">→</span>
        </button>
      </div>

      {#if selectedWord}
        <h3 class="truncate font-mono text-sm font-bold text-amber-300" data-testid="definition-word-heading">
          {selectedWord}
        </h3>
        {#if lookupResult && (lookupResult.entries ?? []).length > 0}
          <span class="rounded bg-slate-800 px-1.5 py-0.5 text-[10px] text-slate-400">
            {lookupResult.entries.length} {lookupResult.entries.length === 1 ? 'entry' : 'entries'}
          </span>
        {/if}
      {:else}
        <span class="text-xs text-slate-400">Definitions & Web Lookup</span>
      {/if}
    </div>

    <div class="flex shrink-0 items-center gap-3 text-xs">
      <label class="flex cursor-pointer items-center gap-1.5 text-[11px] text-slate-400 hover:text-slate-200" title="Also show dictionary entries that mention this word inside their definition body">
        <input
          type="checkbox"
          checked={includeMentions}
          data-testid="include-mentions-toggle"
          onchange={(e) => onToggleIncludeMentions((e.currentTarget as HTMLInputElement).checked)}
          class="h-3.5 w-3.5 rounded border-slate-600 accent-amber-500"
        />
        <span>Include mentions</span>
      </label>

      {#if selectedWord && lookupResult && (lookupResult.webLinks ?? []).length > 0}
        <div class="hidden items-center gap-1 sm:flex">
          <span class="text-[11px] text-slate-400">Web:</span>
          {#each lookupResult.webLinks.slice(0, 4) as wl}
            <button
              type="button"
              data-testid={`header-web-${wl.id}`}
              onclick={() => onOpenWebLookup(selectedWord!, wl.id)}
              class="rounded bg-slate-800/90 px-2 py-0.5 text-[11px] text-slate-300 transition hover:bg-amber-500/20 hover:text-amber-200"
              title={`Look up "${selectedWord}" on ${wl.name}`}
            >
              {wl.name} <span aria-hidden="true">↗</span>
            </button>
          {/each}
        </div>
      {/if}

      {#if onCollapse}
        <button
          type="button"
          data-testid="collapse-definition-btn"
          onclick={onCollapse}
          aria-label="Collapse definitions panel"
          title="Collapse definitions panel (Ctrl+D)"
          class="flex h-6 w-6 items-center justify-center rounded-md text-slate-400 transition hover:bg-slate-800 hover:text-slate-100"
        >
          <svg
            xmlns="http://www.w3.org/2000/svg"
            viewBox="0 0 16 16"
            fill="currentColor"
            class="h-3.5 w-3.5"
            aria-hidden="true"
          >
            <path
              fill-rule="evenodd"
              d="M4.22 6.22a.75.75 0 0 1 1.06 0L8 8.94l2.72-2.72a.75.75 0 1 1 1.06 1.06l-3.25 3.25a.75.75 0 0 1-1.06 0L4.22 7.28a.75.75 0 0 1 0-1.06Z"
              clip-rule="evenodd"
            />
            <path
              fill-rule="evenodd"
              d="M4.22 9.72a.75.75 0 0 1 1.06 0L8 12.44l2.72-2.72a.75.75 0 1 1 1.06 1.06l-3.25 3.25a.75.75 0 0 1-1.06 0L4.22 10.78a.75.75 0 0 1 0-1.06Z"
              clip-rule="evenodd"
            />
          </svg>
        </button>
      {/if}
    </div>
  </div>

  <!-- Definition Body -->
  <div class="min-h-0 flex-1 overflow-y-auto px-4 py-3" aria-busy={isLoading}>
    {#if !selectedWord}
      <div class="flex h-full items-center justify-center text-xs text-slate-400">
        Select any matched word above to view local definitions or launch a web search. Double-click any word inside a definition to cross-lookup.
      </div>
    {:else if isLoading}
      <div class="flex h-full items-center justify-center text-xs text-slate-400">
        Loading definitions for <strong class="ml-1 font-mono text-slate-200">{selectedWord}</strong>…
      </div>
    {:else if lookupResult && (lookupResult.entries ?? []).length > 0}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="space-y-3"
        style="font-family: var(--tisane-font-definitions-family); font-size: var(--tisane-font-definitions-size);"
        onclick={handleDefClick}
        ondblclick={handleDefDoubleClick}
        data-testid="definition-entries"
      >
        {#each lookupResult.entries as entry}
          <article class="rounded-lg border border-slate-800 bg-slate-950/75 p-3 shadow-xs">
            <div class="mb-1.5 flex flex-wrap items-center justify-between gap-2 border-b border-slate-800/80 pb-1">
              <div class="flex flex-wrap items-center gap-2">
                <span class="rounded bg-amber-500/15 px-1.5 py-0.5 font-sans text-[11px] font-semibold text-amber-300">
                  {entry.dict}
                </span>
                {#if entry.headwords && entry.headwords.length > 0}
                  <span class="font-semibold text-slate-100">
                    {#each entry.headwords as hw, idx}
                      {#if idx > 0}<span class="text-slate-400"> · </span>{/if}
                      {hw.headword}{#if hw.qualifier}
                        <em class="ml-1 font-sans text-[11px] font-normal text-slate-400">({hw.qualifier})</em>
                      {/if}
                    {/each}
                  </span>
                {/if}
              </div>
              <div class="flex items-center gap-1.5 font-sans">
                {#if entry.isMention}
                  <span class="rounded bg-sky-500/15 px-1.5 py-0.5 text-[10px] text-sky-300">
                    Text mention
                  </span>
                {/if}
                {#if entry.isAliasRef}
                  <span class="rounded bg-purple-500/15 px-1.5 py-0.5 text-[10px] text-purple-300">
                    Cross-ref
                  </span>
                {/if}
              </div>
            </div>

            <div
              class="tisane-def"
              style="font-family: var(--tisane-font-definitions-family); font-size: var(--tisane-font-definitions-size);"
            >
              {@html entry.html}
            </div>
          </article>
        {/each}
      </div>
    {:else}
      <!-- Seamless Web Search Fallback when no local definition exists -->
      <div
        data-testid="web-fallback-card"
        class="flex h-full flex-col items-center justify-center rounded-lg border border-dashed border-slate-800 bg-slate-950/50 p-4 text-center"
      >
        <p class="text-xs text-slate-300">
          No local definition found for
          <strong class="font-mono text-amber-300">“{selectedWord}”</strong>
          in your loaded dictionaries.
        </p>
        <p class="mt-0.5 text-[11px] text-slate-400">
          Look it up online with one click:
        </p>
        {#if lookupResult && (lookupResult.webLinks ?? []).length > 0}
          <div class="mt-3 flex flex-wrap items-center justify-center gap-2">
            {#each lookupResult.webLinks as wl}
              <button
                type="button"
                data-testid={`fallback-web-${wl.id}`}
                onclick={() => onOpenWebLookup(selectedWord!, wl.id)}
                class="inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs font-medium transition {wl.id === defaultWebProvider
                  ? 'border-amber-500/50 bg-amber-500/20 text-amber-200 hover:bg-amber-500/30'
                  : 'border-slate-700 bg-slate-800/80 text-slate-200 hover:border-slate-600 hover:bg-slate-800'}"
              >
                <span>{wl.name}</span>
                <span aria-hidden="true" class="text-[10px] opacity-75">↗</span>
              </button>
            {/each}
          </div>
        {/if}
      </div>
    {/if}
  </div>
</section>
{/if}
