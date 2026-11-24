import { getBackend } from '../bindings';
import type { LookupResult } from '../types';

export interface DefinitionStoreCallbacks {
  onError: (message: string) => void;
}

function formatError(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export class DefinitionStore {
  selectedWord: string | null = $state(null);
  lookupResult: LookupResult | null = $state(null);
  isLoading = $state(false);
  includeMentions = $state(false);
  historyStack: string[] = $state([]);
  historyIndex = $state(-1);

  readonly canGoBack = $derived(this.historyIndex > 0);
  readonly canGoForward = $derived(
    this.historyIndex >= 0 && this.historyIndex < this.historyStack.length - 1
  );

  private callbacks: DefinitionStoreCallbacks;
  private loadSeq = 0;

  constructor(callbacks: DefinitionStoreCallbacks) {
    this.callbacks = callbacks;
  }

  async load(word: string, pushHistory = true): Promise<void> {
    const clean = word.trim();
    if (!clean) return;

    const reqId = ++this.loadSeq;
    this.selectedWord = clean;
    this.isLoading = true;

    if (pushHistory) {
      const base = this.historyStack.slice(0, this.historyIndex + 1);
      if (base[base.length - 1] !== clean) {
        base.push(clean);
      }
      this.historyStack = base;
      this.historyIndex = base.length - 1;
    }

    try {
      const res = await getBackend().getDefinitions(clean, this.includeMentions);
      if (reqId === this.loadSeq && this.selectedWord === clean) {
        this.lookupResult = res
          ? {
              word: res.word ?? clean,
              entries: Array.isArray(res.entries) ? res.entries : [],
              webLinks: Array.isArray(res.webLinks) ? res.webLinks : []
            }
          : { word: clean, entries: [], webLinks: [] };
      }
    } catch {
      if (reqId === this.loadSeq && this.selectedWord === clean) {
        this.lookupResult = { word: clean, entries: [], webLinks: [] };
      }
    } finally {
      if (reqId === this.loadSeq && this.selectedWord === clean) {
        this.isLoading = false;
      }
    }
  }

  back(): void {
    if (this.historyIndex > 0) {
      this.historyIndex -= 1;
      void this.load(this.historyStack[this.historyIndex], false);
    }
  }

  forward(): void {
    if (this.historyIndex < this.historyStack.length - 1) {
      this.historyIndex += 1;
      void this.load(this.historyStack[this.historyIndex], false);
    }
  }

  setIncludeMentions(v: boolean): void {
    this.includeMentions = v;
    if (this.selectedWord) {
      void this.load(this.selectedWord, false);
    }
  }

  async openWebLookup(word: string, providerId: string): Promise<void> {
    try {
      await getBackend().openWebLookup(word, providerId);
    } catch (err) {
      this.callbacks.onError(formatError(err));
    }
  }

  async openExternalURL(rawUrl: string): Promise<void> {
    try {
      await getBackend().openExternalURL(rawUrl);
    } catch (err) {
      this.callbacks.onError(formatError(err));
    }
  }
}

export function createDefinitionStore(callbacks: DefinitionStoreCallbacks): DefinitionStore {
  return new DefinitionStore(callbacks);
}
