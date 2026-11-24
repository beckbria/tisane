<script lang="ts">
  import { modalTrap } from '../actions/modalTrap';
  import {
    clampUIZoom,
    DEFAULT_UI_ZOOM,
    MAX_UI_ZOOM,
    MIN_UI_ZOOM,
    UI_ZOOM_STEP
  } from '../stores/config.svelte';
  import {
    DEFAULT_FONTS,
    FONT_ELEMENTS,
    formatFontSummary,
    normalizeFontConfig
  } from '../fonts';
  import type { Config, ConfigPatch, FontConfig, ThemeMode, WebProvider } from '../types';
  import FontSettingsModal from './FontSettingsModal.svelte';

  interface Props {
    open: boolean;
    config: Config;
    onClose: () => void;
    onSave: (patch: ConfigPatch) => void;
    onReset?: (resetDictionaries: boolean) => void;
  }

  let { open, config, onClose, onSave, onReset }: Props = $props();

  let atMacro = $state('');
  let hashMacro = $state('');
  let theme: ThemeMode = $state('dark');
  let uiZoom = $state(DEFAULT_UI_ZOOM);
  let autoWebFallback = $state(false);
  let defaultWebProvider = $state('onelook');
  let providers: WebProvider[] = $state([]);
  let fonts: FontConfig = $state(normalizeFontConfig(DEFAULT_FONTS));
  let showFontDialog = $state(false);
  let showResetConfirm = $state(false);
  let resetIncludeDicts = $state(false);

  let newId = $state('');
  let newName = $state('');
  let newTemplate = $state('');
  let providerError = $state('');

  function isValidWebProviderTemplate(tmpl: string): boolean {
    const trimmed = tmpl.trim();
    if (!/^https?:\/\//i.test(trimmed)) return false;
    try {
      const parsed = new URL(trimmed.replace(/\{entry\}/g, 'test'));
      return (parsed.protocol === 'http:' || parsed.protocol === 'https:') && parsed.host.length > 0;
    } catch {
      return false;
    }
  }

  $effect(() => {
    if (open) {
      atMacro = config.atMacro;
      hashMacro = config.hashMacro;
      theme = config.theme ?? 'dark';
      uiZoom = clampUIZoom(config.uiZoom);
      autoWebFallback = config.autoWebFallback;
      defaultWebProvider = config.defaultWebProvider;
      providers = (config.webProviders ?? []).map((p) => ({ ...p }));
      fonts = normalizeFontConfig(config.fonts);
      providerError = '';
    } else {
      showFontDialog = false;
      showResetConfirm = false;
      resetIncludeDicts = false;
      providerError = '';
    }
  });

  function addProvider() {
    const name = newName.trim();
    const urlTemplate = newTemplate.trim();
    if (!name || !urlTemplate.includes('{entry}')) {
      providerError = 'Provider name and a URL template containing {entry} are required.';
      return;
    }
    if (!isValidWebProviderTemplate(urlTemplate)) {
      providerError = 'Web provider URL templates must use http:// or https:// with a valid host.';
      return;
    }
    providerError = '';
    const id =
      (newId.trim() || name.toLowerCase().replace(/[^a-z0-9]+/g, '-')).replace(/^-|-$/g, '') ||
      `custom-${providers.length + 1}`;
    providers = [...providers, { id, name, urlTemplate }];
    newId = '';
    newName = '';
    newTemplate = '';
  }

  function removeProvider(idx: number) {
    const next = providers.filter((_, i) => i !== idx);
    providers = next;
    providerError = '';
    if (next.length > 0 && !next.some((p) => p.id === defaultWebProvider)) {
      defaultWebProvider = next[0].id;
    }
  }

  const THEME_OPTIONS: { value: ThemeMode; label: string }[] = [
    { value: 'dark', label: 'Dark' },
    { value: 'light', label: 'Light' },
    { value: 'system', label: 'System' }
  ];

  function handleSelectTheme(nextTheme: ThemeMode) {
    theme = nextTheme;
    onSave({ theme: nextTheme });
  }

  function handleStepZoom(nextZoom: number) {
    const clamped = clampUIZoom(nextZoom);
    uiZoom = clamped;
    onSave({ uiZoom: clamped });
  }

  function handleSaveFonts(nextFonts: FontConfig) {
    fonts = normalizeFontConfig(nextFonts);
    onSave({ fonts });
  }

  function handleConfirmReset() {
    const includeDicts = resetIncludeDicts;
    showResetConfirm = false;
    resetIncludeDicts = false;
    onReset?.(includeDicts);
    onClose();
  }

  function handleSubmit(e: Event) {
    e.preventDefault();
    for (const p of providers) {
      if (!isValidWebProviderTemplate(p.urlTemplate)) {
        providerError = `Invalid URL template for "${p.name || p.id}": only http:// and https:// URLs are allowed.`;
        return;
      }
    }
    providerError = '';
    onSave({
      atMacro,
      hashMacro,
      theme,
      uiZoom,
      autoWebFallback,
      defaultWebProvider,
      webProviders: providers,
      fonts
    });
    onClose();
  }
</script>

{#if open}
  <div
    use:modalTrap={{ onClose }}
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4 backdrop-blur-xs"
    role="dialog"
    aria-modal="true"
    aria-label="Tisane Settings"
    data-testid="settings-modal"
    inert={showFontDialog || showResetConfirm ? true : undefined}
    aria-hidden={showFontDialog || showResetConfirm ? 'true' : undefined}
  >
    <form
      onsubmit={handleSubmit}
      class="flex max-h-[88vh] w-full max-w-xl flex-col overflow-hidden rounded-xl border border-slate-700 bg-slate-900 shadow-2xl"
    >
      <div class="flex items-center justify-between border-b border-slate-800 px-5 py-3.5">
        <h2 class="text-sm font-semibold text-slate-100">Tisane Preferences</h2>
        <button
          type="button"
          onclick={onClose}
          aria-label="Close preferences"
          class="inline-flex min-h-6 min-w-6 items-center justify-center rounded text-xs text-slate-400 hover:bg-slate-800 hover:text-slate-100"
        >
          <span aria-hidden="true">✕</span>
        </button>
      </div>

      <div class="space-y-5 overflow-y-auto px-5 py-4 text-xs">
        <!-- Appearance & Typography -->
        <section class="space-y-3">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div>
              <h3 class="font-semibold tracking-wider text-slate-300 uppercase">
                Appearance & Typography
              </h3>
              <p class="text-[11px] text-slate-400">
                Choose a color theme and customize workspace fonts and sizes
              </p>
            </div>
            <button
              type="button"
              data-testid="open-font-settings-btn"
              onclick={() => (showFontDialog = true)}
              class="shrink-0 rounded-lg bg-amber-500/20 px-3 py-1.5 text-xs font-semibold text-amber-300 ring-1 ring-amber-500/40 transition hover:bg-amber-500/30"
            >
              Customize Fonts…
            </button>
          </div>

          <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
            <div class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-slate-800 bg-slate-950/70 px-3 py-2">
              <span class="font-medium text-slate-200">Color Theme</span>
              <div
                class="inline-flex rounded-lg bg-slate-900 p-0.5 ring-1 ring-slate-600"
                role="group"
                aria-label="Color theme"
              >
                {#each THEME_OPTIONS as opt (opt.value)}
                  <button
                    type="button"
                    data-testid={`theme-${opt.value}-btn`}
                    aria-pressed={theme === opt.value}
                    onclick={() => handleSelectTheme(opt.value)}
                    class="rounded-md px-2.5 py-1 text-xs font-medium transition {theme === opt.value
                      ? 'bg-amber-500 text-neutral-950 shadow-xs'
                      : 'text-slate-300 hover:text-slate-100'}"
                  >
                    {opt.label}
                  </button>
                {/each}
              </div>
            </div>

            <div class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-slate-800 bg-slate-950/70 px-3 py-2">
              <span class="font-medium text-slate-200">UI Scale</span>
              <div class="flex items-center gap-1.5" role="group" aria-label="Interface zoom">
                <button
                  type="button"
                  data-testid="zoom-out-btn"
                  disabled={uiZoom <= MIN_UI_ZOOM}
                  onclick={() => handleStepZoom(uiZoom - UI_ZOOM_STEP)}
                  aria-label="Zoom out"
                  title="Zoom out (Ctrl+-)"
                  class="inline-flex min-h-6 min-w-6 items-center justify-center rounded border border-slate-600 bg-slate-900 font-mono text-xs text-slate-200 hover:bg-slate-800 disabled:opacity-35"
                >
                  <span aria-hidden="true">−</span>
                </button>
                <span
                  data-testid="zoom-value"
                  class="min-w-[4.5ch] text-center font-mono text-xs font-semibold text-slate-100"
                >
                  {uiZoom}%
                </span>
                <button
                  type="button"
                  data-testid="zoom-in-btn"
                  disabled={uiZoom >= MAX_UI_ZOOM}
                  onclick={() => handleStepZoom(uiZoom + UI_ZOOM_STEP)}
                  aria-label="Zoom in"
                  title="Zoom in (Ctrl+=)"
                  class="inline-flex min-h-6 min-w-6 items-center justify-center rounded border border-slate-600 bg-slate-900 font-mono text-xs text-slate-200 hover:bg-slate-800 disabled:opacity-35"
                >
                  <span aria-hidden="true">+</span>
                </button>
                {#if uiZoom !== DEFAULT_UI_ZOOM}
                  <button
                    type="button"
                    data-testid="zoom-reset-btn"
                    onclick={() => handleStepZoom(DEFAULT_UI_ZOOM)}
                    title="Reset UI zoom to 100% (Ctrl+0)"
                    class="rounded px-1.5 py-0.5 text-[11px] text-amber-400 hover:text-amber-300"
                  >
                    100%
                  </button>
                {/if}
              </div>
            </div>
          </div>

          <div
            data-testid="settings-font-summary"
            class="grid grid-cols-2 gap-1.5 rounded-lg border border-slate-800 bg-slate-950/70 p-2.5 text-[11px] sm:grid-cols-3"
          >
            {#each FONT_ELEMENTS as item (item.key)}
              <div class="truncate">
                <span class="text-slate-400">{item.label}:</span>
                <span class="ml-1 font-mono text-slate-200">{formatFontSummary(fonts[item.key])}</span>
              </div>
            {/each}
          </div>
        </section>

        <!-- Macro Configuration -->
        <section class="space-y-2.5 border-t border-slate-800 pt-4">
          <div class="flex items-center justify-between">
            <h3 class="font-semibold tracking-wider text-slate-300 uppercase">Pattern Macros</h3>
            <div class="flex gap-2">
              <button
                type="button"
                onclick={() => {
                  atMacro = '[aeiou]';
                  hashMacro = '[^aeiou]';
                }}
                class="rounded bg-slate-800 px-2 py-0.5 text-[11px] text-slate-300 hover:bg-slate-700"
              >
                Standard AEIOU
              </button>
              <button
                type="button"
                onclick={() => {
                  atMacro = '[aeiouy]';
                  hashMacro = '[^aeiouy]';
                }}
                class="rounded bg-slate-800 px-2 py-0.5 text-[11px] text-slate-300 hover:bg-slate-700"
              >
                Include Y
              </button>
            </div>
          </div>

          <div class="grid grid-cols-2 gap-3">
            <label class="space-y-1">
              <span class="block text-slate-400"><code class="text-amber-300">@</code> Macro (Vowels)</span>
              <input
                type="text"
                bind:value={atMacro}
                class="w-full rounded border border-slate-600 bg-slate-950 px-2.5 py-1.5 font-mono text-xs text-slate-100 focus:border-amber-400 focus:outline-none"
              />
            </label>
            <label class="space-y-1">
              <span class="block text-slate-400"><code class="text-amber-300">#</code> Macro (Consonants)</span>
              <input
                type="text"
                bind:value={hashMacro}
                class="w-full rounded border border-slate-600 bg-slate-950 px-2.5 py-1.5 font-mono text-xs text-slate-100 focus:border-amber-400 focus:outline-none"
              />
            </label>
          </div>
        </section>

        <!-- Web Lookup Fallback -->
        <section class="space-y-2.5 border-t border-slate-800 pt-4">
          <h3 class="font-semibold tracking-wider text-slate-300 uppercase">Web Search Fallback</h3>

          <label class="flex cursor-pointer items-center gap-2 text-slate-200">
            <input
              type="checkbox"
              bind:checked={autoWebFallback}
              class="h-3.5 w-3.5 rounded border-slate-600 accent-amber-500"
            />
            <span>Double-clicking a word with no local definition opens Default Web Search automatically</span>
          </label>

          <label class="block space-y-1">
            <span class="text-slate-400">Default Web Provider</span>
            <select
              bind:value={defaultWebProvider}
              class="w-full rounded border border-slate-600 bg-slate-950 px-2.5 py-1.5 text-xs text-slate-100 focus:border-amber-400 focus:outline-none"
            >
              {#each providers as p}
                <option value={p.id}>{p.name} ({p.urlTemplate})</option>
              {/each}
            </select>
          </label>

          <div class="space-y-1.5 pt-1">
            <div class="text-[11px] font-medium text-slate-400">Configured URL Templates (<code class="text-amber-300">{'{entry}'}</code> placeholder)</div>
            {#each providers as p, idx}
              <div class="flex items-center gap-2">
                <input
                  type="text"
                  bind:value={p.name}
                  aria-label="Provider name"
                  class="w-32 rounded border border-slate-600 bg-slate-950 px-2 py-1 text-xs text-slate-200 focus:border-amber-400 focus:outline-none"
                />
                <input
                  type="text"
                  bind:value={p.urlTemplate}
                  aria-label="Provider URL template"
                  class="flex-1 rounded border border-slate-600 bg-slate-950 px-2 py-1 font-mono text-[11px] text-slate-300 focus:border-amber-400 focus:outline-none"
                />
                <button
                  type="button"
                  disabled={providers.length <= 1}
                  onclick={() => removeProvider(idx)}
                  aria-label={`Remove ${p.name || 'provider'}`}
                  class="inline-flex min-h-6 min-w-6 items-center justify-center rounded px-1.5 text-slate-400 hover:bg-rose-500/20 hover:text-rose-300 disabled:opacity-35"
                  title="Remove provider"
                >
                  <span aria-hidden="true">✕</span>
                </button>
              </div>
            {/each}

            <!-- Add Custom Provider -->
            <div class="mt-2 flex items-center gap-2 rounded-lg border border-dashed border-slate-600 p-2">
              <input
                type="text"
                bind:value={newName}
                placeholder="Name (e.g. Chambers)"
                class="w-32 rounded border border-slate-600 bg-slate-950 px-2 py-1 text-xs text-slate-200 placeholder-slate-400 focus:border-amber-400 focus:outline-none"
              />
              <input
                type="text"
                bind:value={newTemplate}
                placeholder={'https://example.com/search?q={entry}'}
                class="flex-1 rounded border border-slate-600 bg-slate-950 px-2 py-1 font-mono text-[11px] text-slate-200 placeholder-slate-400 focus:border-amber-400 focus:outline-none"
              />
              <button
                type="button"
                onclick={addProvider}
                class="rounded bg-slate-800 px-2.5 py-1 text-xs font-medium text-amber-300 hover:bg-slate-700"
              >
                + Add
              </button>
            </div>
            {#if providerError}
              <p role="alert" data-testid="settings-provider-error" class="text-[11px] text-rose-400">
                {providerError}
              </p>
            {/if}
          </div>
        </section>
      </div>

      <div class="flex items-center justify-between gap-2 border-t border-slate-800 bg-slate-950/60 px-5 py-3">
        <button
          type="button"
          data-testid="reset-all-settings-btn"
          onclick={() => {
            resetIncludeDicts = false;
            showResetConfirm = true;
          }}
          class="rounded-lg border border-slate-600 px-3 py-1.5 text-xs text-slate-300 transition hover:border-rose-500/50 hover:bg-rose-500/10 hover:text-rose-300"
        >
          Reset All Settings to Defaults
        </button>

        <div class="flex items-center gap-2">
          <button
            type="button"
            onclick={onClose}
            class="rounded-lg border border-slate-600 px-3.5 py-1.5 text-xs text-slate-300 hover:bg-slate-800"
          >
            Cancel
          </button>
          <button
            type="submit"
            data-testid="save-settings-btn"
            class="rounded-lg bg-amber-500 px-4 py-1.5 text-xs font-semibold text-neutral-950 hover:brightness-105"
          >
            Save Settings
          </button>
        </div>
      </div>
    </form>
  </div>

  {#if showResetConfirm}
    <div
      use:modalTrap={{ onClose: () => (showResetConfirm = false) }}
      class="fixed inset-0 z-60 flex items-center justify-center bg-black/75 p-4 backdrop-blur-xs"
      role="dialog"
      aria-modal="true"
      aria-label="Reset All Settings to Defaults"
      data-testid="reset-settings-confirm-modal"
    >
      <div
        class="flex w-full max-w-md flex-col overflow-hidden rounded-xl border border-slate-700 bg-slate-900 shadow-2xl"
      >
        <div class="flex items-center justify-between border-b border-slate-800 px-5 py-3.5">
          <h3 class="text-sm font-semibold text-slate-100">Reset All Settings to Defaults?</h3>
          <button
            type="button"
            onclick={() => (showResetConfirm = false)}
            aria-label="Close confirmation dialog"
            class="inline-flex min-h-6 min-w-6 items-center justify-center rounded text-xs text-slate-400 hover:bg-slate-800 hover:text-slate-100"
          >
            <span aria-hidden="true">✕</span>
          </button>
        </div>

        <div class="space-y-3.5 px-5 py-4 text-xs text-slate-300">
          <p>
            This will clear all persisted settings (macros, filters, sort &amp; layout modes, theme, custom fonts, web providers, and recent queries) and restore Tisane's factory defaults.
          </p>

          <label
            class="flex cursor-pointer items-start gap-2.5 rounded-lg border border-slate-600 bg-slate-950/70 p-3 text-slate-200 transition hover:border-slate-500"
          >
            <input
              type="checkbox"
              bind:checked={resetIncludeDicts}
              data-testid="reset-dicts-checkbox"
              class="mt-0.5 h-3.5 w-3.5 shrink-0 rounded border-slate-600 accent-amber-500"
            />
            <div class="space-y-0.5">
              <span class="block font-medium text-slate-100">
                Also reset loaded and selected dictionaries
              </span>
              <span class="block text-[11px] text-slate-400">
                Clears your current dictionary list and re-adds all dictionaries found in the executable's directory and the <code class="text-amber-300">dictionaries</code> subdirectory.
              </span>
            </div>
          </label>
        </div>

        <div class="flex items-center justify-end gap-2 border-t border-slate-800 bg-slate-950/60 px-5 py-3">
          <button
            type="button"
            data-testid="cancel-reset-settings-btn"
            onclick={() => (showResetConfirm = false)}
            class="rounded-lg border border-slate-600 px-3.5 py-1.5 text-xs text-slate-300 hover:bg-slate-800"
          >
            Cancel
          </button>
          <button
            type="button"
            data-testid="confirm-reset-settings-btn"
            onclick={handleConfirmReset}
            class="rounded-lg bg-rose-600 px-4 py-1.5 text-xs font-semibold text-white hover:bg-rose-700"
          >
            Reset to Defaults
          </button>
        </div>
      </div>
    </div>
  {/if}

  <FontSettingsModal
    open={showFontDialog}
    {fonts}
    onClose={() => (showFontDialog = false)}
    onSave={handleSaveFonts}
  />
{/if}
