<script lang="ts">
  import { modalTrap } from '../actions/modalTrap';
  import { APP_VERSION } from '../version';

  interface Props {
    open: boolean;
    onClose: () => void;
    onOpenExternalURL: (url: string) => void;
  }

  let { open, onClose, onOpenExternalURL }: Props = $props();

  const REPO_URL = 'https://github.com/beckbria/tisane';
  const REPO_DISPLAY = 'github.com/beckbria/tisane';

  function handleRepoClick(e: MouseEvent) {
    e.preventDefault();
    onOpenExternalURL(REPO_URL);
  }
</script>

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
        onclick={handleRepoClick}
        data-testid="about-github-link"
        class="mt-4 inline-flex items-center gap-1 font-mono text-xs text-amber-300 underline decoration-amber-500/40 underline-offset-2 transition hover:text-amber-200 hover:decoration-amber-300"
      >
        {REPO_DISPLAY}
      </a>
    </div>
  </div>
{/if}
