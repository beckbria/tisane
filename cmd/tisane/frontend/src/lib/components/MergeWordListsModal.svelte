<script lang="ts">
  import { getBackend } from '../bindings';
  import type {
    BuildInputEncoding,
    MergeWordListsRequest,
    MergeWordListsResult,
    WordListConversion,
    WordListOperation
  } from '../types';

  interface Props {
    open: boolean;
    onClose: () => void;
    onMerged?: (result: MergeWordListsResult) => void;
  }

  let { open, onClose, onMerged }: Props = $props();

  let primaryPath = $state('');
  let secondaryPath = $state('');
  let outputPath = $state('');
  let lastAutoOutputPath = $state('');
  let conversion: WordListConversion = $state('none');
  let operation: WordListOperation = $state('or');
  let encoding: BuildInputEncoding = $state('auto');

  let isMerging = $state(false);
  let errorMessage = $state('');
  let mergeResult: MergeWordListsResult | null = $state(null);

  const CONVERSION_OPTIONS: { value: WordListConversion; label: string; hint: string }[] = [
    { value: 'none', label: 'None (default)', hint: 'Keep entries unchanged' },
    { value: 'ascii', label: 'To ASCII characters', hint: 'Convert accented letters and symbols to ASCII' },
    { value: 'ascii-letters', label: 'To ASCII characters, letters only', hint: 'Convert to ASCII and keep A–Z, a–z only' },
    { value: 'ascii-lower', label: 'To ASCII lower case letters', hint: 'Convert to ASCII and keep lowercase a–z only' },
    { value: 'ascii-upper', label: 'To ASCII upper case letters', hint: 'Convert to ASCII and keep uppercase A–Z only' }
  ];

  const OPERATION_OPTIONS: { value: WordListOperation; label: string; description: string }[] = [
    { value: 'or', label: 'OR', description: 'Entries in either list' },
    { value: 'and', label: 'AND', description: 'Entries in both lists' },
    { value: 'exclude', label: 'EXCLUDE', description: 'Entries in the primary list and NOT in the secondary list' }
  ];

  function deriveDefaultOutputPath(inPath: string): string {
    const trimmed = inPath.trim();
    if (!trimmed) return '';
    const lastSep = Math.max(trimmed.lastIndexOf('/'), trimmed.lastIndexOf('\\'));
    const lastDot = trimmed.lastIndexOf('.');
    const dir = lastSep >= 0 ? trimmed.slice(0, lastSep + 1) : '';
    const base = lastSep >= 0 ? trimmed.slice(lastSep + 1) : trimmed;
    const stem = lastDot > lastSep ? trimmed.slice(lastSep + 1, lastDot) : base;
    if (!stem) return `${dir}merged.txt`;
    return `${dir}${stem}-merged.txt`;
  }

  function formatBytes(n: number): string {
    if (n >= 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(2)} MB`;
    if (n >= 1024) return `${(n / 1024).toFixed(1)} KB`;
    return `${n} B`;
  }

  const canMerge = $derived(
    !isMerging &&
      primaryPath.trim() !== '' &&
      secondaryPath.trim() !== '' &&
      outputPath.trim() !== ''
  );

  $effect(() => {
    if (!open) {
      errorMessage = '';
      mergeResult = null;
    }
  });

  function applyPrimaryPath(nextPath: string) {
    primaryPath = nextPath;
    const suggested = deriveDefaultOutputPath(nextPath);
    if (outputPath.trim() === '' || outputPath === lastAutoOutputPath) {
      outputPath = suggested;
      lastAutoOutputPath = suggested;
    }
    errorMessage = '';
    mergeResult = null;
  }

  async function handleBrowsePrimary() {
    try {
      const chosen = await getBackend().browseWordListFile();
      if (chosen) {
        applyPrimaryPath(chosen);
      }
    } catch (err) {
      errorMessage = err instanceof Error ? err.message : String(err);
    }
  }

  async function handleBrowseSecondary() {
    try {
      const chosen = await getBackend().browseWordListFile();
      if (chosen) {
        secondaryPath = chosen;
        errorMessage = '';
        mergeResult = null;
      }
    } catch (err) {
      errorMessage = err instanceof Error ? err.message : String(err);
    }
  }

  async function handleBrowseOutput() {
    try {
      const defaultTarget = outputPath.trim() || deriveDefaultOutputPath(primaryPath) || 'merged.txt';
      const chosen = await getBackend().browseSaveWordListFile(defaultTarget);
      if (chosen) {
        outputPath = chosen;
        lastAutoOutputPath = '';
        errorMessage = '';
        mergeResult = null;
      }
    } catch (err) {
      errorMessage = err instanceof Error ? err.message : String(err);
    }
  }

  function handleResetOptions() {
    conversion = 'none';
    operation = 'or';
    encoding = 'auto';
    errorMessage = '';
    mergeResult = null;
  }

  async function handleSubmit(e: Event) {
    e.preventDefault();
    if (!canMerge) return;

    isMerging = true;
    errorMessage = '';
    mergeResult = null;

    const req: MergeWordListsRequest = {
      primaryPath: primaryPath.trim(),
      secondaryPath: secondaryPath.trim(),
      outputPath: outputPath.trim(),
      operation,
      conversion,
      encoding
    };

    try {
      const res = await getBackend().mergeWordLists(req);
      mergeResult = res;
      onMerged?.(res);
    } catch (err) {
      errorMessage = err instanceof Error ? err.message : String(err);
    } finally {
      isMerging = false;
    }
  }
</script>

{#if open}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4 backdrop-blur-xs"
    role="dialog"
    aria-modal="true"
    aria-label="Merge Word Lists"
    data-testid="merge-wordlists-modal"
  >
    <form
      onsubmit={handleSubmit}
      class="flex max-h-[90vh] w-full max-w-2xl flex-col overflow-hidden rounded-xl border border-slate-700 bg-slate-900 shadow-2xl"
    >
      <!-- Header -->
      <div class="flex items-center justify-between border-b border-slate-800 px-5 py-3.5">
        <div>
          <h2 class="text-sm font-semibold text-slate-100">Merge Word Lists</h2>
          <p class="text-[11px] text-slate-400">
            Convert and combine two plain-text word lists into a <code class="text-amber-300">.txt</code> word list
          </p>
        </div>
        <button
          type="button"
          disabled={isMerging}
          onclick={onClose}
          aria-label="Close word list merge tool"
          class="text-xs text-slate-400 hover:text-white disabled:opacity-40"
        >
          ✕
        </button>
      </div>

      <!-- Body -->
      <div class="space-y-5 overflow-y-auto px-5 py-4 text-xs">
        <!-- Input & Output Files -->
        <section class="space-y-3">
          <h3 class="font-semibold tracking-wider text-slate-300 uppercase">Input &amp; Output Word Lists</h3>

          <div class="space-y-1">
            <label for="merge-primary-path" class="block text-slate-300">
              Primary Word List File <span class="text-rose-400">*</span>
            </label>
            <div class="flex items-center gap-2">
              <input
                id="merge-primary-path"
                data-testid="merge-primary-path"
                type="text"
                value={primaryPath}
                oninput={(e) => applyPrimaryPath((e.currentTarget as HTMLInputElement).value)}
                placeholder="/path/to/primary.txt"
                disabled={isMerging}
                class="flex-1 rounded border border-slate-700 bg-slate-950 px-2.5 py-1.5 font-mono text-xs text-slate-100 placeholder-slate-500 focus:border-amber-400 focus:outline-none disabled:opacity-60"
              />
              <button
                type="button"
                data-testid="merge-browse-primary-btn"
                disabled={isMerging}
                onclick={handleBrowsePrimary}
                class="shrink-0 rounded-lg bg-slate-800 px-3 py-1.5 text-xs font-medium text-amber-300 ring-1 ring-slate-700 transition hover:bg-slate-700 disabled:opacity-50"
              >
                Browse…
              </button>
            </div>
          </div>

          <div class="space-y-1">
            <label for="merge-secondary-path" class="block text-slate-300">
              Secondary Word List File <span class="text-rose-400">*</span>
            </label>
            <div class="flex items-center gap-2">
              <input
                id="merge-secondary-path"
                data-testid="merge-secondary-path"
                type="text"
                bind:value={secondaryPath}
                oninput={() => {
                  errorMessage = '';
                  mergeResult = null;
                }}
                placeholder="/path/to/secondary.txt"
                disabled={isMerging}
                class="flex-1 rounded border border-slate-700 bg-slate-950 px-2.5 py-1.5 font-mono text-xs text-slate-100 placeholder-slate-500 focus:border-amber-400 focus:outline-none disabled:opacity-60"
              />
              <button
                type="button"
                data-testid="merge-browse-secondary-btn"
                disabled={isMerging}
                onclick={handleBrowseSecondary}
                class="shrink-0 rounded-lg bg-slate-800 px-3 py-1.5 text-xs font-medium text-amber-300 ring-1 ring-slate-700 transition hover:bg-slate-700 disabled:opacity-50"
              >
                Browse…
              </button>
            </div>
          </div>

          <div class="space-y-1">
            <label for="merge-output-path" class="block text-slate-300">
              Output Word List File (.txt) <span class="text-rose-400">*</span>
            </label>
            <div class="flex items-center gap-2">
              <input
                id="merge-output-path"
                data-testid="merge-output-path"
                type="text"
                bind:value={outputPath}
                oninput={() => {
                  lastAutoOutputPath = '';
                  errorMessage = '';
                  mergeResult = null;
                }}
                placeholder="/path/to/merged.txt"
                disabled={isMerging}
                class="flex-1 rounded border border-slate-700 bg-slate-950 px-2.5 py-1.5 font-mono text-xs text-slate-100 placeholder-slate-500 focus:border-amber-400 focus:outline-none disabled:opacity-60"
              />
              <button
                type="button"
                data-testid="merge-browse-output-btn"
                disabled={isMerging}
                onclick={handleBrowseOutput}
                class="shrink-0 rounded-lg bg-slate-800 px-3 py-1.5 text-xs font-medium text-amber-300 ring-1 ring-slate-700 transition hover:bg-slate-700 disabled:opacity-50"
              >
                Save As…
              </button>
            </div>
          </div>
        </section>

        <!-- Conversion & Encoding -->
        <section class="space-y-3 border-t border-slate-800 pt-4">
          <div class="flex items-center justify-between">
            <h3 class="font-semibold tracking-wider text-slate-300 uppercase">Output Conversion</h3>
            <button
              type="button"
              disabled={isMerging}
              onclick={handleResetOptions}
              class="rounded bg-slate-800 px-2 py-0.5 text-[11px] text-slate-300 hover:bg-slate-700 disabled:opacity-50"
            >
              Reset Options
            </button>
          </div>

          <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <label class="space-y-1">
              <span class="block text-slate-300">Entry Conversion (applied before operation)</span>
              <select
                data-testid="merge-conversion"
                bind:value={conversion}
                disabled={isMerging}
                class="w-full rounded border border-slate-700 bg-slate-950 px-2.5 py-1.5 text-xs text-slate-100 focus:border-amber-400 focus:outline-none disabled:opacity-60"
              >
                {#each CONVERSION_OPTIONS as opt (opt.value)}
                  <option value={opt.value}>{opt.label}</option>
                {/each}
              </select>
              <span class="block text-[10px] text-slate-500">
                {CONVERSION_OPTIONS.find((o) => o.value === conversion)?.hint}
              </span>
            </label>

            <label class="space-y-1">
              <span class="block text-slate-300">Input Text Encoding</span>
              <select
                data-testid="merge-encoding"
                bind:value={encoding}
                disabled={isMerging}
                class="w-full rounded border border-slate-700 bg-slate-950 px-2.5 py-1.5 text-xs text-slate-100 focus:border-amber-400 focus:outline-none disabled:opacity-60"
              >
                <option value="auto">Auto-detect (UTF-8 / Windows-1252)</option>
                <option value="utf8">UTF-8</option>
                <option value="cp1252">Windows-1252 / ANSI</option>
              </select>
            </label>
          </div>
        </section>

        <!-- Merge Operation -->
        <section class="space-y-2.5 border-t border-slate-800 pt-4">
          <div>
            <h3 class="font-semibold tracking-wider text-slate-300 uppercase">Merge Operation</h3>
            <p class="text-[11px] text-slate-500">
              Applied after conversion to the deduplicated entries of both lists.
            </p>
          </div>

          <div class="space-y-2" role="radiogroup" aria-label="Merge operation">
            {#each OPERATION_OPTIONS as opt (opt.value)}
              <label
                class="flex cursor-pointer items-start gap-2.5 rounded-lg border px-3 py-2 transition {operation === opt.value
                  ? 'border-amber-500/60 bg-amber-500/10 text-slate-100'
                  : 'border-slate-800 bg-slate-950/60 text-slate-300 hover:border-slate-700'}"
              >
                <input
                  type="radio"
                  name="merge-operation"
                  value={opt.value}
                  data-testid={`merge-op-${opt.value}`}
                  checked={operation === opt.value}
                  disabled={isMerging}
                  onchange={() => {
                    operation = opt.value;
                    errorMessage = '';
                    mergeResult = null;
                  }}
                  class="mt-0.5 h-3.5 w-3.5 shrink-0 accent-amber-500"
                />
                <span class="min-w-0">
                  <span class="font-mono font-semibold text-amber-300">{opt.label}</span>
                  <span class="ml-1.5 text-slate-300">— {opt.description}</span>
                </span>
              </label>
            {/each}
          </div>
        </section>

        <!-- Error / Success Feedback -->
        {#if errorMessage}
          <div
            data-testid="merge-error"
            class="rounded-lg border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-xs text-rose-200"
          >
            <strong class="font-semibold">Merge failed:</strong> {errorMessage}
          </div>
        {/if}

        {#if mergeResult}
          <div
            data-testid="merge-success"
            class="space-y-2 rounded-lg border border-emerald-500/40 bg-emerald-500/10 p-3 text-xs text-emerald-100"
          >
            <div class="flex items-center justify-between gap-2">
              <span class="font-semibold text-emerald-300">Word lists merged successfully</span>
              <span class="font-mono text-[11px] text-emerald-200">
                {formatBytes(mergeResult.bytesWritten)} · {mergeResult.elapsedMs.toFixed(1)} ms
              </span>
            </div>
            <div class="truncate font-mono text-[11px] text-emerald-200/90" title={mergeResult.outputPath}>
              {mergeResult.outputPath}
            </div>
            <div class="grid grid-cols-2 gap-x-4 gap-y-1 pt-1 text-[11px] text-emerald-100/90 sm:grid-cols-3">
              <div>
                Primary: <strong class="font-mono">{mergeResult.primaryEntries.toLocaleString()}</strong>
                <span class="text-emerald-200/70">({mergeResult.primaryLines.toLocaleString()} lines)</span>
              </div>
              <div>
                Secondary: <strong class="font-mono">{mergeResult.secondaryEntries.toLocaleString()}</strong>
                <span class="text-emerald-200/70">({mergeResult.secondaryLines.toLocaleString()} lines)</span>
              </div>
              <div>
                Output entries: <strong class="font-mono">{mergeResult.outputEntries.toLocaleString()}</strong>
              </div>
            </div>
          </div>
        {/if}
      </div>

      <!-- Footer -->
      <div class="flex items-center justify-end gap-2 border-t border-slate-800 bg-slate-950/60 px-5 py-3">
        <button
          type="button"
          disabled={isMerging}
          onclick={onClose}
          class="rounded-lg border border-slate-700 px-3.5 py-1.5 text-xs text-slate-300 hover:bg-slate-800 disabled:opacity-50"
        >
          {mergeResult ? 'Done' : 'Cancel'}
        </button>
        <button
          type="submit"
          data-testid="merge-submit-btn"
          disabled={!canMerge}
          class="rounded-lg bg-amber-500 px-4 py-1.5 text-xs font-semibold text-slate-950 transition hover:bg-amber-400 disabled:cursor-not-allowed disabled:opacity-40"
        >
          {isMerging ? 'Merging…' : 'Merge Word Lists'}
        </button>
      </div>
    </form>
  </div>
{/if}
