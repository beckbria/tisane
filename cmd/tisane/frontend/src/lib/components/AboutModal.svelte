<script lang="ts">
  import { modalTrap } from '../actions/modalTrap';
  import { DICTIONARY_CREDITS_MD, parseCreditsMarkdown, type CreditsInline } from '../credits';
  import { APP_VERSION } from '../version';

  interface Props {
    open: boolean;
    onClose: () => void;
    onOpenExternalURL: (url: string) => void;
  }

  let { open, onClose, onOpenExternalURL }: Props = $props();

  const REPO_URL = 'https://github.com/beckbria/tisane';
  const REPO_DISPLAY = 'github.com/beckbria/tisane';
  const creditBlocks = parseCreditsMarkdown(DICTIONARY_CREDITS_MD);

  let showLicenses = $state(false);

  $effect(() => {
    if (!open) {
      showLicenses = false;
    }
  });

  function handleExternalLink(e: MouseEvent, url: string) {
    e.preventDefault();
    onOpenExternalURL(url);
  }
</script>

{#snippet renderInline(segments: CreditsInline[])}
  {#each segments as seg, segIdx (segIdx)}
    {#if seg.kind === 'link'}
      <a
        href={seg.url}
        onclick={(e) => handleExternalLink(e, seg.url)}
        class="break-words text-amber-300 underline decoration-amber-500/40 underline-offset-2 transition hover:text-amber-200 hover:decoration-amber-300"
      >
        {seg.label}
      </a>
    {:else}
      {seg.text}
    {/if}
  {/each}
{/snippet}

{#if open}
  <div
    use:modalTrap={{ onClose }}
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4 backdrop-blur-xs"
    role="dialog"
    aria-modal="true"
    aria-label="About Tisane"
    tabindex="-1"
    data-testid="about-modal"
  >
    <div
      class="relative flex w-full max-w-sm flex-col items-center rounded-xl border border-slate-700 bg-slate-900 px-6 py-6 text-center shadow-2xl"
    >
      <button
        type="button"
        onclick={onClose}
        aria-label="Close About dialog"
        data-testid="close-about-btn"
        class="absolute top-3 right-3 inline-flex min-h-6 min-w-6 items-center justify-center rounded text-xs text-slate-400 hover:bg-slate-800 hover:text-slate-100"
      >
        <span aria-hidden="true">✕</span>
      </button>

      <img
        src="/icon.png"
        alt="Tisane icon"
        data-testid="about-icon"
        class="mb-3 h-16 w-16 rounded-xl object-contain shadow-md ring-1 ring-slate-700/80"
      />

      <h2 class="text-lg font-bold tracking-wide text-slate-100">Tisane</h2>

      <p
        data-testid="about-version"
        class="mt-0.5 font-mono text-xs font-medium text-amber-300"
      >
        Version {APP_VERSION}
      </p>

      <div class="mt-3 space-y-1 text-xs text-slate-300">
        <p>by Brian Beck</p>
        <p class="text-slate-400">Copyright 2026</p>
      </div>

      <a
        href={REPO_URL}
        onclick={(e) => handleExternalLink(e, REPO_URL)}
        data-testid="about-github-link"
        class="mt-4 inline-flex items-center gap-1 font-mono text-xs text-amber-300 underline decoration-amber-500/40 underline-offset-2 transition hover:text-amber-200 hover:decoration-amber-300"
      >
        {REPO_DISPLAY}
      </a>

      <button
        type="button"
        onclick={() => (showLicenses = true)}
        data-testid="about-licenses-btn"
        class="mt-4 rounded-lg border border-slate-600 bg-slate-800/80 px-3.5 py-1.5 text-xs font-medium text-slate-200 transition hover:border-slate-500 hover:bg-slate-800 hover:text-slate-100"
      >
        Dictionary Licenses
      </button>
    </div>
  </div>

  {#if showLicenses}
    <div
      use:modalTrap={{ onClose: () => (showLicenses = false) }}
      class="fixed inset-0 z-60 flex items-center justify-center bg-black/75 p-4 backdrop-blur-xs"
      role="dialog"
      aria-modal="true"
      aria-label="Dictionary Licenses"
      tabindex="-1"
      data-testid="dictionary-licenses-modal"
    >
      <div
        class="flex max-h-[85vh] w-full max-w-xl flex-col overflow-hidden rounded-xl border border-slate-700 bg-slate-900 shadow-2xl"
      >
        <div class="flex shrink-0 items-center justify-between border-b border-slate-800 px-5 py-3.5">
          <h3 class="text-sm font-semibold text-slate-100">Dictionary Licenses</h3>
          <button
            type="button"
            onclick={() => (showLicenses = false)}
            aria-label="Close Dictionary Licenses dialog"
            data-testid="close-licenses-btn"
            class="inline-flex min-h-6 min-w-6 items-center justify-center rounded text-xs text-slate-400 hover:bg-slate-800 hover:text-slate-100"
          >
            <span aria-hidden="true">✕</span>
          </button>
        </div>

        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <div
          data-testid="dictionary-licenses-content"
          role="region"
          tabindex="0"
          aria-label="Dictionary credits and licenses"
          class="min-h-0 flex-1 overflow-y-auto px-5 py-4 text-left text-xs leading-relaxed text-slate-300 select-text focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-amber-400"
        >
          {#each creditBlocks as block, blockIdx (blockIdx)}
            {#if block.kind === 'heading'}
              {#if blockIdx === 0}
                <h4 class="text-sm font-semibold text-slate-100">
                  {@render renderInline(block.segments)}
                </h4>
              {:else}
                <h4 class="mt-4 border-t border-slate-800 pt-3 text-sm font-semibold text-amber-300">
                  {@render renderInline(block.segments)}
                </h4>
              {/if}
            {:else if block.kind === 'paragraph'}
              <p class="mt-2 text-slate-300">
                {#each block.lines as line, lineIdx (lineIdx)}
                  {#if lineIdx > 0}<br />{/if}
                  {@render renderInline(line)}
                {/each}
              </p>
            {:else if block.kind === 'list'}
              <ul class="mt-2 list-disc space-y-1 pl-5 text-slate-300">
                {#each block.items as item, itemIdx (itemIdx)}
                  <li>
                    {@render renderInline(item)}
                  </li>
                {/each}
              </ul>
            {/if}
          {/each}
        </div>

        <div class="flex shrink-0 items-center justify-end border-t border-slate-800 bg-slate-950/60 px-5 py-3">
          <button
            type="button"
            onclick={() => (showLicenses = false)}
            data-testid="close-licenses-footer-btn"
            class="rounded-lg border border-slate-600 px-3.5 py-1.5 text-xs text-slate-200 hover:bg-slate-800"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  {/if}
{/if}
