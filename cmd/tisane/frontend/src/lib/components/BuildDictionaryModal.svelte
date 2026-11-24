<script lang="ts">
  import { modalTrap } from '../actions/modalTrap';
  import { getBackend } from '../bindings';
  import type {
    BuildDictionaryRequest,
    BuildDictionaryResult,
    BuildInputEncoding
  } from '../types';

  interface Props {
    open: boolean;
    onClose: () => void;
    onBuilt?: (result: BuildDictionaryResult) => void;
  }

  let { open, onClose, onBuilt }: Props = $props();

  let inputPath = $state('');
  let outputPath = $state('');
  let lastAutoOutputPath = $state('');
  let mapPath = $state('');
  let description = $state('');
  let minLen = $state(1);
  let maxLen = $state(100);
  let maxFTSPercent = $state(10);
  let sortAlphabetically = $state(true);
  let tabSeparatedAnnotations = $state(false);
  let encoding: BuildInputEncoding = $state('auto');
  let addToLibrary = $state(true);

  let isBuilding = $state(false);
  let errorMessage = $state('');
  let buildResult: BuildDictionaryResult | null = $state(null);

  function deriveDefaultOutputPath(inPath: string): string {
    const trimmed = inPath.trim();
    if (!trimmed) return '';
    const lastSep = Math.max(trimmed.lastIndexOf('/'), trimmed.lastIndexOf('\\'));
    const lastDot = trimmed.lastIndexOf('.');
    if (lastDot > lastSep) {
      const ext = trimmed.slice(lastDot);
      if (ext.toLowerCase() !== '.tsd') {
        return `${trimmed.slice(0, lastDot)}.tsd`;
      }
    }
    return `${trimmed}.tsd`;
  }

  function formatBytes(n: number): string {
    if (n >= 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(2)} MB`;
    if (n >= 1024) return `${(n / 1024).toFixed(1)} KB`;
    return `${n} B`;
  }

  const descriptionBytes = $derived(new TextEncoder().encode(description).length);

  const validationError = $derived.by(() => {
    if (!Number.isInteger(minLen) || minLen < 1 || minLen > 100) {
      return 'Minimum word length must be an integer between 1 and 100.';
    }
    if (!Number.isInteger(maxLen) || maxLen < 1 || maxLen > 100) {
      return 'Maximum word length must be an integer between 1 and 100.';
    }
    if (minLen > maxLen) {
      return 'Minimum word length cannot exceed maximum word length.';
    }
    if (!Number.isInteger(maxFTSPercent) || maxFTSPercent < 0 || maxFTSPercent > 100) {
      return 'Max FTS % must be an integer between 0 and 100.';
    }
    return '';
  });

  const canBuild = $derived(
    !isBuilding &&
      inputPath.trim() !== '' &&
      outputPath.trim() !== '' &&
      validationError === ''
  );

  $effect(() => {
    if (!open) {
      errorMessage = '';
      buildResult = null;
    }
  });

  function applyInputPath(nextPath: string) {
    inputPath = nextPath;
    const suggested = deriveDefaultOutputPath(nextPath);
    if (outputPath.trim() === '' || outputPath === lastAutoOutputPath) {
      outputPath = suggested;
      lastAutoOutputPath = suggested;
    }
    errorMessage = '';
    buildResult = null;
  }

  async function handleBrowseWordList() {
    try {
      const chosen = await getBackend().browseWordListFile();
      if (chosen) {
        applyInputPath(chosen);
      }
    } catch (err) {
      errorMessage = err instanceof Error ? err.message : String(err);
    }
  }

  async function handleBrowseOutput() {
    try {
      const defaultTarget = outputPath.trim() || deriveDefaultOutputPath(inputPath);
      const chosen = await getBackend().browseSaveDictionaryFile(defaultTarget);
      if (chosen) {
        outputPath = chosen;
        lastAutoOutputPath = '';
        errorMessage = '';
        buildResult = null;
      }
    } catch (err) {
      errorMessage = err instanceof Error ? err.message : String(err);
    }
  }

  async function handleBrowseMap() {
    try {
      const chosen = await getBackend().browseAlphabetMapFile();
      if (chosen) {
        mapPath = chosen;
        errorMessage = '';
        buildResult = null;
      }
    } catch (err) {
      errorMessage = err instanceof Error ? err.message : String(err);
    }
  }

  function handleResetOptions() {
    mapPath = '';
    description = '';
    minLen = 1;
    maxLen = 100;
    maxFTSPercent = 10;
    sortAlphabetically = true;
    tabSeparatedAnnotations = false;
    encoding = 'auto';
    addToLibrary = true;
    errorMessage = '';
    buildResult = null;
  }

  async function handleSubmit(e: Event) {
    e.preventDefault();
    if (!canBuild) return;

    isBuilding = true;
    errorMessage = '';
    buildResult = null;

    const req: BuildDictionaryRequest = {
      inputPath: inputPath.trim(),
      outputPath: outputPath.trim(),
      mapPath: mapPath.trim(),
      description,
      minLen: Number(minLen),
      maxLen: Number(maxLen),
      maxFTSPercent: Number(maxFTSPercent),
      sortAlphabetically,
      tabSeparatedAnnotations,
      encoding,
      addToLibrary
    };

    try {
      const res = await getBackend().buildDictionary(req);
      buildResult = res;
      onBuilt?.(res);
    } catch (err) {
      errorMessage = err instanceof Error ? err.message : String(err);
    } finally {
      isBuilding = false;
    }
  }
</script>

{#if open}
  <div
    use:modalTrap={{ onClose, disabled: isBuilding }}
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4 backdrop-blur-xs"
    role="dialog"
    aria-modal="true"
    aria-label="Build Dictionary from Word List"
    data-testid="build-dict-modal"
  >
    <form
      onsubmit={handleSubmit}
      class="flex max-h-[90vh] w-full max-w-2xl flex-col overflow-hidden rounded-xl border border-slate-700 bg-slate-900 shadow-2xl"
    >
      <!-- Header -->
      <div class="flex items-center justify-between border-b border-slate-800 px-5 py-3.5">
        <div>
          <h2 class="text-sm font-semibold text-slate-100">Build Dictionary from Word List</h2>
          <p class="text-[11px] text-slate-400">
            Compile a plain-text word or definition list into a <code class="text-amber-300">.tsd</code> dictionary
          </p>
        </div>
        <button
          type="button"
          disabled={isBuilding}
          onclick={onClose}
          aria-label="Close dictionary builder"
          class="text-xs text-slate-400 hover:text-slate-100 disabled:opacity-40"
        >
          ✕
        </button>
      </div>

      <!-- Body -->
      <div class="space-y-5 overflow-y-auto px-5 py-4 text-xs">
        <!-- Input & Output Files -->
        <section class="space-y-3">
          <h3 class="font-semibold tracking-wider text-slate-300 uppercase">Source &amp; Output Files</h3>

          <div class="space-y-1">
            <label for="builder-input-path" class="block text-slate-300">
              Word List File <span class="text-rose-400">*</span>
            </label>
            <div class="flex items-center gap-2">
              <input
                id="builder-input-path"
                data-testid="builder-input-path"
                type="text"
                value={inputPath}
                oninput={(e) => applyInputPath((e.currentTarget as HTMLInputElement).value)}
                placeholder="/path/to/wordlist.txt"
                disabled={isBuilding}
                class="flex-1 rounded border border-slate-600 bg-slate-950 px-2.5 py-1.5 font-mono text-xs text-slate-100 placeholder-slate-400 focus:border-amber-400 focus:outline-none disabled:opacity-60"
              />
              <button
                type="button"
                data-testid="builder-browse-input-btn"
                disabled={isBuilding}
                onclick={handleBrowseWordList}
                class="shrink-0 rounded-lg bg-slate-800 px-3 py-1.5 text-xs font-medium text-amber-300 ring-1 ring-slate-600 transition hover:bg-slate-700 disabled:opacity-50"
              >
                Browse…
              </button>
            </div>
            <p class="text-[11px] text-slate-400">
              Same schema as <code class="text-slate-300">tisane-build</code>: one word per line, or
              <code class="text-slate-300">word+annotation:alias;hidden|definition</code>.
            </p>
          </div>

          <div class="space-y-1">
            <label for="builder-output-path" class="block text-slate-300">
              Output Dictionary File (.tsd) <span class="text-rose-400">*</span>
            </label>
            <div class="flex items-center gap-2">
              <input
                id="builder-output-path"
                data-testid="builder-output-path"
                type="text"
                bind:value={outputPath}
                oninput={() => {
                  lastAutoOutputPath = '';
                  errorMessage = '';
                  buildResult = null;
                }}
                placeholder="/path/to/output.tsd"
                disabled={isBuilding}
                class="flex-1 rounded border border-slate-600 bg-slate-950 px-2.5 py-1.5 font-mono text-xs text-slate-100 placeholder-slate-400 focus:border-amber-400 focus:outline-none disabled:opacity-60"
              />
              <button
                type="button"
                data-testid="builder-browse-output-btn"
                disabled={isBuilding}
                onclick={handleBrowseOutput}
                class="shrink-0 rounded-lg bg-slate-800 px-3 py-1.5 text-xs font-medium text-amber-300 ring-1 ring-slate-600 transition hover:bg-slate-700 disabled:opacity-50"
              >
                Save As…
              </button>
            </div>
          </div>
        </section>

        <!-- Description & Word Lengths -->
        <section class="space-y-3 border-t border-slate-800 pt-4">
          <h3 class="font-semibold tracking-wider text-slate-300 uppercase">Metadata &amp; Word Lengths</h3>

          <div class="space-y-1">
            <div class="flex items-center justify-between">
              <label for="builder-description" class="text-slate-300">Description</label>
              <span
                data-testid="builder-description-bytes"
                class="font-mono text-[11px] {descriptionBytes > 999 ? 'text-amber-400' : 'text-slate-400'}"
              >
                {descriptionBytes} / 999 bytes{descriptionBytes > 999 ? ' (will be truncated)' : ''}
              </span>
            </div>
            <textarea
              id="builder-description"
              data-testid="builder-description"
              rows={3}
              bind:value={description}
              disabled={isBuilding}
              placeholder="Optional description stored in the dictionary header…"
              class="w-full resize-y rounded border border-slate-600 bg-slate-950 px-2.5 py-1.5 text-xs text-slate-100 placeholder-slate-400 focus:border-amber-400 focus:outline-none disabled:opacity-60"
            ></textarea>
          </div>

          <div class="grid grid-cols-2 gap-3">
            <label class="space-y-1">
              <span class="block text-slate-300">Minimum Word Length (1–100)</span>
              <input
                type="number"
                data-testid="builder-min-len"
                min={1}
                max={100}
                bind:value={minLen}
                disabled={isBuilding}
                class="w-full rounded border border-slate-600 bg-slate-950 px-2.5 py-1.5 font-mono text-xs text-slate-100 focus:border-amber-400 focus:outline-none disabled:opacity-60"
              />
            </label>
            <label class="space-y-1">
              <span class="block text-slate-300">Maximum Word Length (1–100)</span>
              <input
                type="number"
                data-testid="builder-max-len"
                min={1}
                max={100}
                bind:value={maxLen}
                disabled={isBuilding}
                class="w-full rounded border border-slate-600 bg-slate-950 px-2.5 py-1.5 font-mono text-xs text-slate-100 focus:border-amber-400 focus:outline-none disabled:opacity-60"
              />
            </label>
          </div>
        </section>

        <!-- Advanced Builder Options (matching tisane-build CLI) -->
        <section class="space-y-3 border-t border-slate-800 pt-4">
          <div class="flex items-center justify-between">
            <h3 class="font-semibold tracking-wider text-slate-300 uppercase">Builder Options</h3>
            <button
              type="button"
              disabled={isBuilding}
              onclick={handleResetOptions}
              class="rounded bg-slate-800 px-2 py-0.5 text-[11px] text-slate-300 hover:bg-slate-700 disabled:opacity-50"
            >
              Reset Options
            </button>
          </div>

          <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <label class="space-y-1">
              <span class="block text-slate-300">Input Text Encoding</span>
              <select
                data-testid="builder-encoding"
                bind:value={encoding}
                disabled={isBuilding}
                class="w-full rounded border border-slate-600 bg-slate-950 px-2.5 py-1.5 text-xs text-slate-100 focus:border-amber-400 focus:outline-none disabled:opacity-60"
              >
                <option value="auto">Auto-detect (UTF-8 / Windows-1252)</option>
                <option value="utf8">UTF-8</option>
                <option value="cp1252">Windows-1252 / ANSI</option>
              </select>
            </label>

            <label class="space-y-1">
              <span class="block text-slate-300">Max FTS Document Frequency (%)</span>
              <input
                type="number"
                data-testid="builder-max-fts-pct"
                min={0}
                max={100}
                bind:value={maxFTSPercent}
                disabled={isBuilding}
                class="w-full rounded border border-slate-600 bg-slate-950 px-2.5 py-1.5 font-mono text-xs text-slate-100 focus:border-amber-400 focus:outline-none disabled:opacity-60"
              />
              <span class="block text-[10px] text-slate-400">
                0 disables FTS occurrence lists; default 10%
              </span>
            </label>
          </div>

          <div class="space-y-1">
            <label for="builder-map-path" class="block text-slate-300">
              Alphabet Map File (.tsm) <span class="text-slate-400">(optional)</span>
            </label>
            <div class="flex items-center gap-2">
              <input
                id="builder-map-path"
                data-testid="builder-map-path"
                type="text"
                bind:value={mapPath}
                disabled={isBuilding}
                placeholder="Built-in Default.TSM"
                class="flex-1 rounded border border-slate-600 bg-slate-950 px-2.5 py-1.5 font-mono text-xs text-slate-100 placeholder-slate-400 focus:border-amber-400 focus:outline-none disabled:opacity-60"
              />
              <button
                type="button"
                data-testid="builder-browse-map-btn"
                disabled={isBuilding}
                onclick={handleBrowseMap}
                class="shrink-0 rounded-lg bg-slate-800 px-3 py-1.5 text-xs font-medium text-slate-200 ring-1 ring-slate-600 transition hover:bg-slate-700 disabled:opacity-50"
              >
                Browse…
              </button>
              {#if mapPath.trim() !== ''}
                <button
                  type="button"
                  data-testid="builder-clear-map-btn"
                  disabled={isBuilding}
                  onclick={() => (mapPath = '')}
                  class="shrink-0 rounded-lg border border-slate-600 px-2.5 py-1.5 text-xs text-slate-400 hover:bg-slate-800 hover:text-slate-200"
                >
                  Clear
                </button>
              {/if}
            </div>
          </div>

          <div class="space-y-2 pt-1">
            <label class="flex cursor-pointer items-center gap-2 text-slate-200">
              <input
                type="checkbox"
                data-testid="builder-sort-alpha"
                bind:checked={sortAlphabetically}
                disabled={isBuilding}
                class="h-3.5 w-3.5 rounded accent-amber-500"
              />
              <span>Sort words alphabetically within each length bucket</span>
            </label>

            <label class="flex cursor-pointer items-center gap-2 text-slate-200">
              <input
                type="checkbox"
                data-testid="builder-tsv-mode"
                bind:checked={tabSeparatedAnnotations}
                disabled={isBuilding}
                class="h-3.5 w-3.5 rounded accent-amber-500"
              />
              <span>Treat tab (<code class="text-amber-300">\t</code>) as annotation delimiter (TSV word lists)</span>
            </label>

            <label class="flex cursor-pointer items-center gap-2 text-slate-200">
              <input
                type="checkbox"
                data-testid="builder-add-to-library"
                bind:checked={addToLibrary}
                disabled={isBuilding}
                class="h-3.5 w-3.5 rounded accent-amber-500"
              />
              <span>Add built dictionary to Tisane after building</span>
            </label>
          </div>
        </section>

        <!-- Validation / Error / Success Feedback -->
        {#if validationError}
          <div
            role="alert"
            data-testid="builder-validation-error"
            class="rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-200"
          >
            {validationError}
          </div>
        {/if}

        {#if errorMessage}
          <div
            role="alert"
            data-testid="builder-error"
            class="rounded-lg border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-xs text-rose-200"
          >
            <strong class="font-semibold">Build failed:</strong> {errorMessage}
          </div>
        {/if}

        {#if buildResult}
          <div
            role="status"
            aria-live="polite"
            data-testid="builder-success"
            class="space-y-2 rounded-lg border border-emerald-500/40 bg-emerald-500/10 p-3 text-xs text-emerald-100"
          >
            <div class="flex items-center justify-between gap-2">
              <span class="font-semibold text-emerald-300">Dictionary built successfully</span>
              <span class="font-mono text-[11px] text-emerald-200">
                {formatBytes(buildResult.bytesWritten)} · {buildResult.elapsedMs.toFixed(1)} ms
              </span>
            </div>
            <div class="truncate font-mono text-[11px] text-emerald-200/90" title={buildResult.outputPath}>
              {buildResult.outputPath}
            </div>
            <div class="grid grid-cols-2 gap-x-4 gap-y-1 pt-1 text-[11px] text-emerald-100/90 sm:grid-cols-3">
              <div>Input lines: <strong class="font-mono">{buildResult.inputLines.toLocaleString()}</strong></div>
              <div>Unique words: <strong class="font-mono">{buildResult.uniqueWords.toLocaleString()}</strong></div>
              <div>Max length: <strong class="font-mono">{buildResult.maxWordLength}</strong></div>
              <div>Headword defs: <strong class="font-mono">{buildResult.pass2Definitions.toLocaleString()}</strong></div>
              <div>Alias-only defs: <strong class="font-mono">{buildResult.pass1Definitions.toLocaleString()}</strong></div>
              <div>FTS-only keys: <strong class="font-mono">{buildResult.ftsOnlyWords.toLocaleString()}</strong></div>
              <div class="col-span-2 sm:col-span-3">
                Alphabet: <strong class="font-mono">{buildResult.usedLetters}/{buildResult.totalLetters}</strong> active letter classes
                {#if buildResult.addedToLibrary}
                  <span class="ml-2 rounded bg-emerald-500/20 px-1.5 py-0.5 text-[10px] font-medium text-emerald-200">
                    Added to Tisane
                  </span>
                {/if}
              </div>
            </div>
          </div>
        {/if}
      </div>

      <!-- Footer -->
      <div class="flex items-center justify-end gap-2 border-t border-slate-800 bg-slate-950/60 px-5 py-3">
        <button
          type="button"
          disabled={isBuilding}
          onclick={onClose}
          class="rounded-lg border border-slate-600 px-3.5 py-1.5 text-xs text-slate-300 hover:bg-slate-800 disabled:opacity-50"
        >
          {buildResult ? 'Done' : 'Cancel'}
        </button>
        <button
          type="submit"
          data-testid="builder-submit-btn"
          disabled={!canBuild}
          class="rounded-lg bg-amber-500 px-4 py-1.5 text-xs font-semibold text-neutral-950 transition hover:brightness-105 disabled:cursor-not-allowed disabled:opacity-40"
        >
          {isBuilding ? 'Building…' : 'Build Dictionary'}
        </button>
      </div>
    </form>
  </div>
{/if}
