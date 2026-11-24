<script lang="ts">
  import { modalTrap } from '../actions/modalTrap';
  import {
    DEFAULT_FONTS,
    FONT_ELEMENTS,
    FONT_PRESETS,
    normalizeFontConfig,
    resolveFontFamily
  } from '../fonts';
  import type { FontConfig, FontElementKey } from '../types';

  interface Props {
    open: boolean;
    fonts: FontConfig;
    onClose: () => void;
    onSave: (fonts: FontConfig) => void;
  }

  let { open, fonts, onClose, onSave }: Props = $props();

  let draft: FontConfig = $state(normalizeFontConfig(DEFAULT_FONTS));
  let customMode: Record<FontElementKey, boolean> = $state({
    results: false,
    definitions: false,
    patternInput: false,
    dictionariesList: false,
    statusBar: false,
    toolbar: false
  });

  function isPresetFamily(family: string): boolean {
    return FONT_PRESETS.some((p) => p.value === family);
  }

  $effect(() => {
    if (open) {
      const norm = normalizeFontConfig(fonts);
      draft = norm;
      customMode = {
        results: !isPresetFamily(norm.results.family),
        definitions: !isPresetFamily(norm.definitions.family),
        patternInput: !isPresetFamily(norm.patternInput.family),
        dictionariesList: !isPresetFamily(norm.dictionariesList.family),
        statusBar: !isPresetFamily(norm.statusBar.family),
        toolbar: !isPresetFamily(norm.toolbar.family)
      };
    }
  });

  function updateFamily(key: FontElementKey, family: string) {
    draft = {
      ...draft,
      [key]: {
        ...draft[key],
        family
      }
    };
  }

  function handlePresetSelect(key: FontElementKey, val: string) {
    if (val === '__custom__') {
      customMode = { ...customMode, [key]: true };
      return;
    }
    customMode = { ...customMode, [key]: false };
    updateFamily(key, val);
  }

  function updateSize(key: FontElementKey, rawSize: number, min: number, max: number) {
    if (Number.isNaN(rawSize)) return;
    const clamped = Math.max(min, Math.min(max, Math.round(rawSize)));
    draft = {
      ...draft,
      [key]: {
        ...draft[key],
        size: clamped
      }
    };
  }

  function scaleAll(delta: number) {
    const next = { ...draft };
    for (const meta of FONT_ELEMENTS) {
      const cur = next[meta.key];
      const clamped = Math.max(meta.minSize, Math.min(meta.maxSize, cur.size + delta));
      next[meta.key] = { ...cur, size: clamped };
    }
    draft = next;
  }

  function resetElement(key: FontElementKey) {
    const def = DEFAULT_FONTS[key];
    draft = {
      ...draft,
      [key]: { ...def }
    };
    customMode = {
      ...customMode,
      [key]: false
    };
  }

  function resetAll() {
    draft = normalizeFontConfig(DEFAULT_FONTS);
    customMode = {
      results: false,
      definitions: false,
      patternInput: false,
      dictionariesList: false,
      statusBar: false,
      toolbar: false
    };
  }

  function handleSubmit(e: Event) {
    e.preventDefault();
    onSave(normalizeFontConfig(draft));
    onClose();
  }
</script>

{#if open}
  <div
    use:modalTrap={{ onClose }}
    class="fixed inset-0 z-60 flex items-center justify-center bg-black/75 p-4 backdrop-blur-xs"
    role="dialog"
    aria-modal="true"
    aria-label="Customize Fonts"
    data-testid="font-settings-modal"
  >
    <form
      onsubmit={handleSubmit}
      class="flex max-h-[90vh] w-full max-w-2xl flex-col overflow-hidden rounded-xl border border-slate-700 bg-slate-900 shadow-2xl"
    >
      <!-- Header -->
      <div class="flex items-center justify-between border-b border-slate-800 px-5 py-3.5">
        <div>
          <h2 class="text-sm font-semibold text-slate-100">Customize UI Fonts & Sizes</h2>
          <p class="text-[11px] text-slate-400">
            Configure font families and pixel sizes for individual Tisane workspace elements
          </p>
        </div>
        <div class="flex items-center gap-2">
          <div class="flex items-center rounded-lg border border-slate-600 bg-slate-950 p-0.5 text-[11px]">
            <button
              type="button"
              data-testid="scale-all-down-btn"
              onclick={() => scaleAll(-1)}
              class="rounded px-2 py-0.5 text-slate-300 hover:bg-slate-800 hover:text-slate-100"
              title="Decrease all font sizes by 1px"
            >
              A−
            </button>
            <span class="px-1 text-slate-400">|</span>
            <button
              type="button"
              data-testid="scale-all-up-btn"
              onclick={() => scaleAll(1)}
              class="rounded px-2 py-0.5 text-slate-300 hover:bg-slate-800 hover:text-slate-100"
              title="Increase all font sizes by 1px"
            >
              A+
            </button>
          </div>
          <button
            type="button"
            onclick={onClose}
            aria-label="Close font settings"
            class="ml-1 text-xs text-slate-400 hover:text-slate-100"
          >
            ✕
          </button>
        </div>
      </div>

      <!-- Body: Per-Element Font & Size Controls -->
      <div class="space-y-3 overflow-y-auto px-5 py-4 text-xs">
        {#each FONT_ELEMENTS as item (item.key)}
          {@const current = draft[item.key]}
          {@const def = DEFAULT_FONTS[item.key]}
          {@const isModified = current.family !== def.family || current.size !== def.size}
          <div
            data-testid={`font-row-${item.key}`}
            class="rounded-lg border border-slate-800 bg-slate-950/60 p-3"
          >
            <div class="flex flex-wrap items-start justify-between gap-2">
              <div class="min-w-[180px] flex-1">
                <div class="flex items-center gap-2">
                  <span class="font-semibold text-slate-200">{item.label}</span>
                  {#if isModified}
                    <button
                      type="button"
                      data-testid={`font-reset-${item.key}`}
                      onclick={() => resetElement(item.key)}
                      class="rounded bg-slate-800 px-1.5 py-0.2 text-[10px] text-amber-300 hover:bg-slate-700"
                      title={`Reset ${item.label} to ${def.family} (${def.size}px)`}
                    >
                      Reset
                    </button>
                  {/if}
                </div>
                <p class="text-[11px] text-slate-400">{item.description}</p>
              </div>

              <!-- Font Family + Size Controls -->
              <div class="flex flex-wrap items-center gap-2.5">
                <!-- Font Family Select -->
                <div class="flex flex-col gap-1">
                  <label class="text-[10px] text-slate-400" for={`font-family-${item.key}`}>
                    Font Family
                  </label>
                  <div class="flex items-center gap-1.5">
                    <select
                      id={`font-family-${item.key}`}
                      aria-label={`${item.label} font family`}
                      data-testid={`font-family-select-${item.key}`}
                      value={customMode[item.key] ? '__custom__' : current.family}
                      onchange={(e) =>
                        handlePresetSelect(item.key, (e.currentTarget as HTMLSelectElement).value)}
                      class="rounded border border-slate-600 bg-slate-900 px-2 py-1 text-xs text-slate-100 focus:border-amber-400 focus:outline-none"
                    >
                      {#each FONT_PRESETS as preset}
                        <option value={preset.value}>{preset.label}</option>
                      {/each}
                      <option value="__custom__">Custom Font…</option>
                    </select>

                    {#if customMode[item.key]}
                      <input
                        type="text"
                        value={current.family}
                        aria-label={`${item.label} custom font family`}
                        data-testid={`font-family-custom-${item.key}`}
                        placeholder="e.g. Iosevka, Ubuntu Mono"
                        oninput={(e) =>
                          updateFamily(item.key, (e.currentTarget as HTMLInputElement).value)}
                        class="w-36 rounded border border-amber-500/60 bg-slate-950 px-2 py-1 text-xs text-slate-100 focus:border-amber-400 focus:outline-none"
                      />
                    {/if}
                  </div>
                </div>

                <!-- Font Size Controls -->
                <div class="flex flex-col gap-1">
                  <label class="text-[10px] text-slate-400" for={`font-size-${item.key}`}>
                    Size (px)
                  </label>
                  <div class="flex items-center gap-1.5">
                    <button
                      type="button"
                      data-testid={`font-size-dec-${item.key}`}
                      disabled={current.size <= item.minSize}
                      onclick={() => updateSize(item.key, current.size - 1, item.minSize, item.maxSize)}
                      class="rounded border border-slate-600 bg-slate-900 px-1.5 py-0.5 font-mono text-xs text-slate-300 hover:bg-slate-800 disabled:opacity-35"
                      aria-label={`Decrease ${item.label} size`}
                    >
                      −
                    </button>
                    <input
                      type="range"
                      min={item.minSize}
                      max={item.maxSize}
                      value={current.size}
                      aria-label={`${item.label} size slider`}
                      data-testid={`font-size-slider-${item.key}`}
                      oninput={(e) =>
                        updateSize(
                          item.key,
                          Number((e.currentTarget as HTMLInputElement).value),
                          item.minSize,
                          item.maxSize
                        )}
                      class="h-1.5 w-20 cursor-pointer accent-amber-500"
                    />
                    <input
                      id={`font-size-${item.key}`}
                      type="number"
                      min={item.minSize}
                      max={item.maxSize}
                      value={current.size}
                      aria-label={`${item.label} font size`}
                      data-testid={`font-size-input-${item.key}`}
                      onchange={(e) =>
                        updateSize(
                          item.key,
                          Number((e.currentTarget as HTMLInputElement).value),
                          item.minSize,
                          item.maxSize
                        )}
                      class="w-13 rounded border border-slate-600 bg-slate-900 px-1.5 py-0.5 text-center font-mono text-xs text-slate-100 focus:border-amber-400 focus:outline-none"
                    />
                    <button
                      type="button"
                      data-testid={`font-size-inc-${item.key}`}
                      disabled={current.size >= item.maxSize}
                      onclick={() => updateSize(item.key, current.size + 1, item.minSize, item.maxSize)}
                      class="rounded border border-slate-600 bg-slate-900 px-1.5 py-0.5 font-mono text-xs text-slate-300 hover:bg-slate-800 disabled:opacity-35"
                      aria-label={`Increase ${item.label} size`}
                    >
                      +
                    </button>
                  </div>
                </div>
              </div>
            </div>

            <!-- Live Sample Preview -->
            <div
              data-testid={`font-preview-${item.key}`}
              style="font-family: {resolveFontFamily(current.family)}; font-size: {current.size}px;"
              class="mt-2 truncate rounded border border-slate-800/90 bg-slate-900/90 px-3 py-1.5 text-slate-200"
            >
              {item.sampleText}
            </div>
          </div>
        {/each}
      </div>

      <!-- Footer -->
      <div class="flex items-center justify-between gap-2 border-t border-slate-800 bg-slate-950/60 px-5 py-3">
        <button
          type="button"
          data-testid="reset-fonts-btn"
          onclick={resetAll}
          class="rounded-lg border border-slate-600 px-3 py-1.5 text-xs text-slate-300 hover:bg-slate-800 hover:text-slate-100"
        >
          Reset All to Defaults
        </button>

        <div class="flex items-center gap-2">
          <button
            type="button"
            data-testid="cancel-font-settings-btn"
            onclick={onClose}
            class="rounded-lg border border-slate-600 px-3.5 py-1.5 text-xs text-slate-300 hover:bg-slate-800"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid="save-font-settings-btn"
            class="rounded-lg bg-amber-500 px-4 py-1.5 text-xs font-semibold text-neutral-950 hover:brightness-105"
          >
            Apply Fonts
          </button>
        </div>
      </div>
    </form>
  </div>
{/if}
