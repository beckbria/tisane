<script lang="ts">
  import { onDestroy } from 'svelte';
  import { modalTrap } from '../actions/modalTrap';
  import { copyTextToClipboard } from '../clipboard';
  import type { DictionaryInfo } from '../types';

  interface Props {
    open: boolean;
    dict: DictionaryInfo | null;
    priorityIndex?: number;
    totalCount?: number;
    onClose: () => void;
    onShowWordList?: (dict: DictionaryInfo) => void;
    onOpenDirectory?: (id: string) => void;
  }

  let {
    open,
    dict,
    priorityIndex = -1,
    totalCount = 0,
    onClose,
    onShowWordList,
    onOpenDirectory
  }: Props = $props();

  let copiedPath = $state(false);
  let copyTimer: ReturnType<typeof setTimeout> | null = null;

  onDestroy(() => {
    if (copyTimer) clearTimeout(copyTimer);
  });

  $effect(() => {
    if (!open) {
      copiedPath = false;
      if (copyTimer) {
        clearTimeout(copyTimer);
        copyTimer = null;
      }
    }
  });

  function sanitizeDescription(raw: string): string {
    const normalized = raw.replace(/\r\n/g, '\n').replace(/\r/g, '\n');
    let out = '';
    for (const ch of normalized) {
      const code = ch.codePointAt(0) ?? 0;
      if (ch === '\n' || ch === '\t') {
        out += ch;
      } else if (code < 0x20 || code === 0x7f || (code >= 0x80 && code <= 0x9f)) {
        continue;
      } else {
        out += ch;
      }
    }
    return out.trim();
  }

  function formatFileSize(bytes: number): string {
    if (bytes >= 1024 * 1024) {
      return `${(bytes / (1024 * 1024)).toFixed(2)} MB (${bytes.toLocaleString()} bytes)`;
    }
    if (bytes >= 1024) {
      return `${(bytes / 1024).toFixed(1)} KB (${bytes.toLocaleString()} bytes)`;
    }
    return `${bytes.toLocaleString()} bytes`;
  }

  function formatWordLength(d: DictionaryInfo): string {
    if (d.wordCount === 0 || d.maxWordLength <= 0) {
      return '—';
    }
    const min = d.minWordLength ?? 0;
    if (min > 0) {
      if (min === d.maxWordLength) {
        return `${d.maxWordLength} ${d.maxWordLength === 1 ? 'letter' : 'letters'}`;
      }
      return `${min}–${d.maxWordLength} letters`;
    }
    return `Up to ${d.maxWordLength} ${d.maxWordLength === 1 ? 'letter' : 'letters'}`;
  }

  async function handleCopyPath() {
    if (!dict) return;
    const ok = await copyTextToClipboard(dict.path);
    if (ok) {
      copiedPath = true;
      if (copyTimer) clearTimeout(copyTimer);
      copyTimer = setTimeout(() => {
        copiedPath = false;
      }, 1500);
    }
  }

  const cleanedDescription = $derived(dict ? sanitizeDescription(dict.description ?? '') : '');
</script>

{#if open && dict}
  <div
    use:modalTrap={{ onClose }}
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4 backdrop-blur-xs"
    role="dialog"
    aria-modal="true"
    aria-label={`Dictionary Information: ${dict.name}`}
    tabindex="-1"
    data-testid="dict-info-modal"
  >
    <div
      class="flex max-h-[85vh] w-full max-w-lg flex-col overflow-hidden rounded-xl border border-slate-700 bg-slate-900 shadow-2xl"
    >
      <!-- Header -->
      <div class="flex items-center justify-between gap-3 border-b border-slate-800 px-5 py-3.5">
        <div class="flex min-w-0 items-center gap-2.5">
          <h2
            data-testid="dict-info-heading"
            class="truncate text-sm font-semibold text-slate-100"
          >
            {dict.name}
          </h2>
          {#if dict.enabled}
            <span
              class="shrink-0 rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-medium text-emerald-300"
            >
              Enabled
            </span>
          {:else}
            <span
              class="shrink-0 rounded bg-slate-800 px-1.5 py-0.5 text-[10px] font-medium text-slate-400"
            >
              Disabled
            </span>
          {/if}
        </div>
        <button
          type="button"
          onclick={onClose}
          aria-label="Close Dictionary Information dialog"
          data-testid="close-dict-info-btn"
          class="inline-flex min-h-6 min-w-6 shrink-0 items-center justify-center rounded text-xs text-slate-400 hover:bg-slate-800 hover:text-slate-100"
        >
          <span aria-hidden="true">✕</span>
        </button>
      </div>

      <!-- Body -->
      <div class="space-y-4 overflow-y-auto px-5 py-4 text-xs text-slate-200 select-text">
        <!-- Description -->
        <div>
          <h3 class="mb-1.5 text-[10px] font-semibold tracking-wider text-slate-400 uppercase select-none">
            Description
          </h3>
          {#if cleanedDescription}
            <div
              data-testid="dict-info-description"
              class="max-h-40 overflow-y-auto rounded-lg border border-slate-800 bg-slate-950/70 px-3 py-2.5 leading-relaxed whitespace-pre-wrap text-slate-200"
            >
              {cleanedDescription}
            </div>
          {:else}
            <div
              data-testid="dict-info-description"
              class="rounded-lg border border-slate-800 bg-slate-950/50 px-3 py-2 italic text-slate-400"
            >
              No description provided.
            </div>
          {/if}
        </div>

        <!-- Metadata -->
        <div>
          <h3 class="mb-1.5 text-[10px] font-semibold tracking-wider text-slate-400 uppercase select-none">
            Metadata
          </h3>
          <dl class="divide-y divide-slate-800/80 rounded-lg border border-slate-800 bg-slate-950/60">
            <div class="grid grid-cols-[9rem_1fr] gap-3 px-3 py-2">
              <dt class="font-medium text-slate-400 select-none">Name</dt>
              <dd data-testid="dict-info-name" class="break-words font-medium text-slate-100">
                {dict.name}
              </dd>
            </div>

            <div class="grid grid-cols-[9rem_1fr] gap-3 px-3 py-2">
              <dt class="font-medium text-slate-400 select-none">Format</dt>
              <dd data-testid="dict-info-format" class="font-mono text-slate-200">
                {dict.version || 'TSD'}
              </dd>
            </div>

            <div class="grid grid-cols-[9rem_1fr] gap-3 px-3 py-2">
              <dt class="font-medium text-slate-400 select-none">Status</dt>
              <dd data-testid="dict-info-status" class="text-slate-200">
                {dict.enabled ? 'Enabled' : 'Disabled'}{#if priorityIndex >= 0 && totalCount > 0}<span class="text-slate-400">{' · '}Priority #{priorityIndex + 1} of {totalCount}</span>{/if}
              </dd>
            </div>

            <div class="grid grid-cols-[9rem_1fr] gap-3 px-3 py-2">
              <dt class="font-medium text-slate-400 select-none">Entries</dt>
              <dd data-testid="dict-info-words" class="font-mono text-amber-300">
                {dict.wordCount.toLocaleString()} {dict.wordCount === 1 ? 'word' : 'words'}
              </dd>
            </div>

            <div class="grid grid-cols-[9rem_1fr] gap-3 px-3 py-2">
              <dt class="font-medium text-slate-400 select-none">Word Length</dt>
              <dd data-testid="dict-info-word-length" class="font-mono text-slate-200">
                {formatWordLength(dict)}
              </dd>
            </div>

            {#if (dict.fileSize ?? 0) > 0}
              <div class="grid grid-cols-[9rem_1fr] gap-3 px-3 py-2">
                <dt class="font-medium text-slate-400 select-none">File Size</dt>
                <dd data-testid="dict-info-file-size" class="font-mono text-slate-200">
                  {formatFileSize(dict.fileSize!)}
                </dd>
              </div>
            {/if}

            <div class="grid grid-cols-[9rem_1fr] gap-3 px-3 py-2">
              <dt class="font-medium text-slate-400 select-none">Definitions</dt>
              <dd data-testid="dict-info-definitions" class="text-slate-200">
                {#if dict.hasDefinitions}
                  <span class="text-amber-300">Available</span>
                {:else}
                  <span class="text-slate-400">None</span>
                {/if}
              </dd>
            </div>

            <div class="grid grid-cols-[9rem_1fr] gap-3 px-3 py-2">
              <dt class="font-medium text-slate-400 select-none">Full-Text Search</dt>
              <dd data-testid="dict-info-fts" class="text-slate-200">
                {#if dict.hasFTS}
                  <span class="text-sky-300">Indexed</span>{#if (dict.ftsWordCount ?? 0) > 0}<span class="font-mono text-slate-400">{' '}({dict.ftsWordCount!.toLocaleString()} FTS-only {dict.ftsWordCount === 1 ? 'key' : 'keys'})</span>{/if}
                {:else}
                  <span class="text-slate-400">Not indexed</span>
                {/if}
              </dd>
            </div>

            {#if (dict.totalLetters ?? 0) > 0}
              <div class="grid grid-cols-[9rem_1fr] gap-3 px-3 py-2">
                <dt class="font-medium text-slate-400 select-none">Alphabet</dt>
                <dd data-testid="dict-info-alphabet" class="font-mono text-slate-200">
                  {dict.usedLetters ?? 0} active / {dict.totalLetters} total classes
                </dd>
              </div>
            {/if}

            <div class="grid grid-cols-[9rem_1fr] items-start gap-3 px-3 py-2">
              <dt class="pt-0.5 font-medium text-slate-400 select-none">File Path</dt>
              <dd class="flex items-start justify-between gap-2">
                <span data-testid="dict-info-path" class="break-all font-mono text-[11px] text-slate-300">
                  {dict.path}
                </span>
                <button
                  type="button"
                  onclick={handleCopyPath}
                  data-testid="dict-info-copy-path-btn"
                  aria-label="Copy dictionary file path"
                  class="shrink-0 rounded border border-slate-600 bg-slate-800/80 px-2 py-0.5 text-[11px] font-medium text-slate-300 transition select-none hover:bg-slate-800 hover:text-slate-100"
                >
                  {#if copiedPath}
                    <span class="text-emerald-300"><span aria-hidden="true">✓ </span>Copied</span>
                  {:else}
                    Copy
                  {/if}
                </button>
              </dd>
            </div>
          </dl>
        </div>
      </div>

      <!-- Footer -->
      <div class="flex items-center justify-between gap-2 border-t border-slate-800 bg-slate-950/60 px-5 py-3">
        <div class="flex items-center gap-2">
          {#if onShowWordList}
            <button
              type="button"
              onclick={() => onShowWordList(dict)}
              data-testid="dict-info-wordlist-btn"
              class="rounded-lg border border-slate-600 bg-slate-800/80 px-3 py-1.5 text-xs font-medium text-slate-200 transition hover:bg-slate-800 hover:text-slate-100"
            >
              Word List
            </button>
          {/if}
          {#if onOpenDirectory}
            <button
              type="button"
              onclick={() => onOpenDirectory(dict.id)}
              data-testid="dict-info-open-dir-btn"
              class="rounded-lg border border-slate-600 bg-slate-800/80 px-3 py-1.5 text-xs font-medium text-slate-200 transition hover:bg-slate-800 hover:text-slate-100"
            >
              Open Directory
            </button>
          {/if}
        </div>
        <button
          type="button"
          data-autofocus
          onclick={onClose}
          data-testid="dict-info-close-footer-btn"
          class="rounded-lg border border-slate-600 px-3.5 py-1.5 text-xs font-medium text-slate-200 transition hover:bg-slate-800 hover:text-slate-100"
        >
          Close
        </button>
      </div>
    </div>
  </div>
{/if}
