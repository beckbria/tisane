import { getBackend, type UnsubscribeFn } from '../bindings';
import type { DictionaryInfo } from '../types';

export interface DictionaryStoreCallbacks {
  onChanged: (active: DictionaryInfo[]) => void;
  onError: (message: string) => void;
  onClearError: () => void;
}

function formatError(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

function dictListSignature(list: DictionaryInfo[]): string {
  return list.map((d) => `${d.id}:${d.enabled ? '1' : '0'}`).join('|');
}

export class DictionaryStore {
  list: DictionaryInfo[] = $state([]);
  readonly active = $derived((this.list ?? []).filter((d) => d.enabled));
  readonly activeCount = $derived(this.active.length);

  private callbacks: DictionaryStoreCallbacks;

  constructor(callbacks: DictionaryStoreCallbacks) {
    this.callbacks = callbacks;
  }

  async load(): Promise<DictionaryInfo[]> {
    try {
      const loaded = await getBackend().listDictionaries();
      if (Array.isArray(loaded)) {
        this.list = loaded;
        return this.active;
      }
    } catch {
      // Fallback empty list already set
    }
    return this.active;
  }

  sync(next: DictionaryInfo[]): boolean {
    const changed = dictListSignature(next) !== dictListSignature(this.list);
    this.list = next;
    if (changed) {
      this.callbacks.onChanged(this.active);
    }
    return changed;
  }

  subscribe(): UnsubscribeFn {
    return getBackend().onEvent('dicts:updated', (infos) => {
      if (Array.isArray(infos)) {
        this.sync(infos);
      }
    });
  }

  async setEnabled(id: string, enabled: boolean): Promise<void> {
    try {
      const next = await getBackend().setEnabled(id, enabled);
      if (Array.isArray(next)) {
        this.sync(next);
      }
      this.callbacks.onClearError();
    } catch (err) {
      this.callbacks.onError(formatError(err));
    }
  }

  async reorder(orderedIds: string[]): Promise<void> {
    try {
      const next = await getBackend().reorderDictionaries(orderedIds);
      if (Array.isArray(next)) {
        this.sync(next);
      }
      this.callbacks.onClearError();
    } catch (err) {
      this.callbacks.onError(formatError(err));
    }
  }

  async browseAdd(): Promise<void> {
    try {
      const next = await getBackend().browseAndAddDictionaries();
      if (Array.isArray(next)) {
        this.sync(next);
      }
      this.callbacks.onClearError();
    } catch (err) {
      this.callbacks.onError(formatError(err));
    }
  }

  async browseAddDirectory(): Promise<void> {
    try {
      const next = await getBackend().browseAndAddDirectory();
      if (Array.isArray(next)) {
        this.sync(next);
      }
      this.callbacks.onClearError();
    } catch (err) {
      this.callbacks.onError(formatError(err));
    }
  }

  async remove(id: string): Promise<void> {
    try {
      const next = await getBackend().removeDictionary(id);
      if (Array.isArray(next)) {
        this.sync(next);
      }
      this.callbacks.onClearError();
    } catch (err) {
      this.callbacks.onError(formatError(err));
    }
  }
}

export function createDictionaryStore(callbacks: DictionaryStoreCallbacks): DictionaryStore {
  return new DictionaryStore(callbacks);
}
