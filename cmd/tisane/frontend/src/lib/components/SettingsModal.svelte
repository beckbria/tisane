<script lang="ts">
  import {
    DEFAULT_FONTS,
    FONT_ELEMENTS,
    formatFontSummary,
    normalizeFontConfig
  } from '../fonts';
  import type { Config, ConfigPatch, FontConfig, WebProvider } from '../types';
  import FontSettingsModal from './FontSettingsModal.svelte';

  interface Props {
    open: boolean;
    config: Config;
    onClose: () => void;
    onSave: (patch: ConfigPatch) => void;
  }

  let { open, config, onClose, onSave }: Props = $props();

  let atMacro = $state('');
  let hashMacro = $state('');
  let autoWebFallback = $state(false);
  let defaultWebProvider = $state('onelook');
  let providers: WebProvider[] = $state([]);
  let fonts: FontConfig = $state(normalizeFontConfig(DEFAULT_FONTS));
  let showFontDialog = $state(false);

  let newId = $state('');
  let newName = $state('');
  let newTemplate = $state('');

  $effect(() => {
    if (open) {
      atMacro = config.atMacro;
      hashMacro = config.hashMacro;
      autoWebFallback = config.autoWebFallback;
      defaultWebProvider = config.defaultWebProvider;
      providers = (config.webProviders ?? []).map((p) => ({ ...p }));
      fonts = normalizeFontConfig(config.fonts);
    } else {
      showFontDialog = false;
    }
  });

  function addProvider() {
    const name = newName.trim();
    const urlTemplate = newTemplate.trim();
    if (!name || !urlTemplate.includes('{entry}')) return;
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
    if (next.length > 0 && !next.some((p) => p.id === defaultWebProvider)) {
      defaultWebProvider = next[0].id;
    }
  }

  function handleSaveFonts(nextFonts: FontConfig) {
    fonts = normalizeFontConfig(nextFonts);
    onSave({ fonts });
  }

  function handleSubmit(e: Event) {
    e.preventDefault();
    onSave({
      atMacro,
      hashMacro,
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
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4 backdrop-blur-xs"
    role="dialog"
    aria-modal="true"
    aria-label="Tisane Settings"
    data-testid="settings-modal"
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
          class="text-xs text-slate-400 hover:text-white"
        >
          ✕
        </button>
      </div>

      <div class="space-y-5 overflow-y-auto px-5 py-4 text-xs">
        <!-- Appearance & Typography -->
        <section class="space-y-2.5">
          <div class="flex items-center justify-between gap-2">
            <div>
              <h3 class="font-semibold tracking-wider text-slate-300 uppercase">
                Appearance & Typography
              </h3>
              <p class="text-[11px] text-slate-400">
                Customize fonts and font sizes for results, definitions, pattern bar, sidebar, and status bar
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
                class="w-full rounded border border-slate-700 bg-slate-950 px-2.5 py-1.5 font-mono text-xs text-slate-100 focus:border-amber-400 focus:outline-none"
              />
            </label>
            <label class="space-y-1">
              <span class="block text-slate-400"><code class="text-amber-300">#</code> Macro (Consonants)</span>
              <input
                type="text"
                bind:value={hashMacro}
                class="w-full rounded border border-slate-700 bg-slate-950 px-2.5 py-1.5 font-mono text-xs text-slate-100 focus:border-amber-400 focus:outline-none"
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
              class="h-3.5 w-3.5 rounded accent-amber-500"
            />
            <span>Double-clicking a word with no local definition opens Default Web Search automatically</span>
          </label>

          <label class="block space-y-1">
            <span class="text-slate-400">Default Web Provider</span>
            <select
              bind:value={defaultWebProvider}
              class="w-full rounded border border-slate-700 bg-slate-950 px-2.5 py-1.5 text-xs text-slate-100 focus:border-amber-400 focus:outline-none"
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
                  class="w-32 rounded border border-slate-800 bg-slate-950 px-2 py-1 text-xs text-slate-200"
                />
                <input
                  type="text"
                  bind:value={p.urlTemplate}
                  aria-label="Provider URL template"
                  class="flex-1 rounded border border-slate-800 bg-slate-950 px-2 py-1 font-mono text-[11px] text-slate-300"
                />
                <button
                  type="button"
                  disabled={providers.length <= 1}
                  onclick={() => removeProvider(idx)}
                  class="rounded px-2 py-1 text-slate-400 hover:bg-rose-500/20 hover:text-rose-300 disabled:opacity-30"
                  title="Remove provider"
                >
                  ✕
                </button>
              </div>
            {/each}

            <!-- Add Custom Provider -->
            <div class="mt-2 flex items-center gap-2 rounded-lg border border-dashed border-slate-800 p-2">
              <input
                type="text"
                bind:value={newName}
                placeholder="Name (e.g. Chambers)"
                class="w-32 rounded border border-slate-700 bg-slate-950 px-2 py-1 text-xs text-slate-200"
              />
              <input
                type="text"
                bind:value={newTemplate}
                placeholder={'https://example.com/search?q={entry}'}
                class="flex-1 rounded border border-slate-700 bg-slate-950 px-2 py-1 font-mono text-[11px] text-slate-200"
              />
              <button
                type="button"
                onclick={addProvider}
                class="rounded bg-slate-800 px-2.5 py-1 text-xs font-medium text-amber-300 hover:bg-slate-700"
              >
                + Add
              </button>
            </div>
          </div>
        </section>
      </div>

      <div class="flex items-center justify-end gap-2 border-t border-slate-800 bg-slate-950/60 px-5 py-3">
        <button
          type="button"
          onclick={onClose}
          class="rounded-lg border border-slate-700 px-3.5 py-1.5 text-xs text-slate-300 hover:bg-slate-800"
        >
          Cancel
        </button>
        <button
          type="submit"
          data-testid="save-settings-btn"
          class="rounded-lg bg-amber-500 px-4 py-1.5 text-xs font-semibold text-slate-950 hover:bg-amber-400"
        >
          Save Settings
        </button>
      </div>
    </form>
  </div>

  <FontSettingsModal
    open={showFontDialog}
    {fonts}
    onClose={() => (showFontDialog = false)}
    onSave={handleSaveFonts}
  />
{/if}
