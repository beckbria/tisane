import { getBackend, type UnsubscribeFn } from '../bindings';
import type {
  Config,
  DictionaryInfo,
  Match,
  SearchMode,
  SearchProgress,
  ValidationResult
} from '../types';
import { DEFAULT_CONFIG } from './config.svelte';

export interface SearchStoreDeps {
  getConfig: () => Config;
  getActiveDictionaries: () => DictionaryInfo[];
  onRecordRecentQuery: (query: string) => void;
  onFirstMatch: (word: string) => void;
}

export class SearchStore {
  query = $state('');
  mode: SearchMode = $state('word');
  enumeration = $state('');
  validation: ValidationResult | null = $state(null);

  currentSearchId = $state(0);
  isSearching = $state(false);
  hasCompletedSearch = $state(false);
  errorMessage = $state('');

  // Backing arrays are populated via amortized O(1) in-place .push() without per-batch .concat() copies;
  // totalMatches and dictMatchCounts drive reactive Svelte / virtualizer updates at <= 1 flush per frame.
  allMatches: Match[] = $state.raw([]);
  matchesByDict: Match[][] = $state.raw([]);
  totalMatches = $state(0);
  dictMatchCounts: number[] = $state.raw([]);
  searchActiveDicts: DictionaryInfo[] = $state([]);

  wordsScanned = $state(0);
  totalCandidateWords = $state(0);
  elapsedMs = $state(0);
  usedIndexedFTS = $state(false);

  private nextClientSearchId = 1;
  private validateTimer: ReturnType<typeof setTimeout> | null = null;
  private rafFlushId: number | null = null;
  private pendingProgress: SearchProgress | null = null;
  private deps: SearchStoreDeps;

  constructor(deps: SearchStoreDeps) {
    this.deps = deps;
  }

  private cancelFrameFlush(): void {
    if (this.rafFlushId !== null) {
      if (typeof cancelAnimationFrame === 'function') {
        cancelAnimationFrame(this.rafFlushId);
      }
      this.rafFlushId = null;
    }
  }

  private resetMatchBuffers(dictCount: number): void {
    this.cancelFrameFlush();
    this.pendingProgress = null;
    this.allMatches = [];
    this.matchesByDict = Array.from({ length: dictCount }, () => []);
    this.totalMatches = 0;
    this.dictMatchCounts = new Array<number>(dictCount).fill(0);
  }

  private flushPendingCounts(): void {
    this.cancelFrameFlush();
    this.totalMatches = this.allMatches.length;
    this.dictMatchCounts = this.matchesByDict.map((b) => b.length);
    if (this.pendingProgress) {
      this.wordsScanned = this.pendingProgress.wordsScanned;
      this.totalCandidateWords = this.pendingProgress.totalCandidateWords;
      this.pendingProgress = null;
    }
  }

  private scheduleFrameFlush(): void {
    if (this.rafFlushId !== null) return;
    if (typeof requestAnimationFrame === 'function') {
      this.rafFlushId = requestAnimationFrame(() => {
        this.rafFlushId = null;
        this.flushPendingCounts();
      });
    } else {
      this.flushPendingCounts();
    }
  }

  initActiveDicts(active: DictionaryInfo[]): void {
    this.searchActiveDicts = active;
    this.resetMatchBuffers(active.length);
  }

  onDictionariesChanged(active: DictionaryInfo[]): void {
    this.searchActiveDicts = active;
    this.scheduleValidation(true);
    if (this.query.trim() !== '' && (this.hasCompletedSearch || this.isSearching)) {
      void this.execute();
    } else {
      this.resetMatchBuffers(active.length);
      this.hasCompletedSearch = false;
    }
  }

  scheduleValidation(immediate = false): void {
    if (this.validateTimer) {
      clearTimeout(this.validateTimer);
      this.validateTimer = null;
    }
    const q = this.query.trim();
    if (!q) {
      this.validation = null;
      return;
    }
    const run = async () => {
      const cfg = this.deps.getConfig();
      try {
        const res = await getBackend().validatePattern({
          query: q,
          mode: this.mode,
          enumeration: this.enumeration,
          minLength: cfg.minLength,
          maxLength: cfg.maxLength,
          atMacro: cfg.atMacro,
          hashMacro: cfg.hashMacro,
          exclusiveVars: cfg.exclusiveVars,
          ignoreUnusedPunctuation: cfg.ignoreUnusedPunctuation
        });
        if (this.query.trim() === q) {
          this.validation = res;
        }
      } catch {
        // Ignore transient validation transport errors
      }
    };
    if (immediate) {
      void run();
    } else {
      this.validateTimer = setTimeout(run, 60);
    }
  }

  async execute(): Promise<void> {
    const q = this.query.trim();
    if (!q) return;

    const cfg = this.deps.getConfig();
    const activeSnapshot = this.deps.getActiveDictionaries();
    this.errorMessage = '';
    this.searchActiveDicts = activeSnapshot;
    this.resetMatchBuffers(activeSnapshot.length);
    this.wordsScanned = 0;
    this.totalCandidateWords = 0;
    this.elapsedMs = 0;
    this.usedIndexedFTS = false;

    const clientSearchId = this.nextClientSearchId++;
    this.currentSearchId = clientSearchId;
    this.isSearching = true;
    this.hasCompletedSearch = false;

    try {
      const sid = await getBackend().startSearch({
        searchId: clientSearchId,
        query: q,
        mode: this.mode,
        enumeration: this.enumeration,
        minLength: cfg.minLength,
        maxLength: cfg.maxLength,
        atMacro: cfg.atMacro,
        hashMacro: cfg.hashMacro,
        exclusiveVars: cfg.exclusiveVars,
        ignoreUnusedPunctuation: cfg.ignoreUnusedPunctuation,
        showBalances: cfg.showBalances,
        showAnnotations: cfg.showAnnotations,
        suppressDuplicates: cfg.suppressDuplicates,
        entryCategories: cfg.entryFilter ?? DEFAULT_CONFIG.entryFilter,
        sortMode: cfg.sortMode,
        limit: 0
      });
      if (sid && sid !== this.currentSearchId && this.isSearching) {
        this.currentSearchId = sid;
      }
      this.deps.onRecordRecentQuery(q);
    } catch (err) {
      this.isSearching = false;
      this.errorMessage = err instanceof Error ? err.message : String(err);
    }
  }

  async cancel(): Promise<void> {
    this.isSearching = false;
    try {
      await getBackend().cancelSearch();
    } catch {
      // Ignore
    }
  }

  subscribe(): UnsubscribeFn {
    const backend = getBackend();
    const unsubs: UnsubscribeFn[] = [];

    unsubs.push(
      backend.onEvent('search:batch', (ev) => {
        if (!ev || (this.currentSearchId !== 0 && ev.searchId !== this.currentSearchId)) return;
        this.currentSearchId = ev.searchId;
        if (ev.progress) {
          this.pendingProgress = ev.progress;
        }
        if (ev.matches && ev.matches.length > 0) {
          const prevEmpty = this.allMatches.length === 0;
          let grewBuckets = false;
          for (let i = 0; i < ev.matches.length; i++) {
            const m = ev.matches[i];
            this.allMatches.push(m);
            const dIdx = m.dictIndex ?? 0;
            while (this.matchesByDict.length <= dIdx) {
              this.matchesByDict.push([]);
              grewBuckets = true;
            }
            this.matchesByDict[dIdx].push(m);
          }
          if (grewBuckets) {
            this.matchesByDict = this.matchesByDict.slice();
          }

          if (prevEmpty) {
            this.flushPendingCounts();
            this.deps.onFirstMatch(ev.matches[0].word);
          } else {
            this.scheduleFrameFlush();
          }
        } else if (ev.progress) {
          this.scheduleFrameFlush();
        }
      })
    );

    unsubs.push(
      backend.onEvent('search:progress', (ev) => {
        if (!ev || (this.currentSearchId !== 0 && ev.searchId !== this.currentSearchId)) return;
        this.pendingProgress = ev.progress;
        this.scheduleFrameFlush();
      })
    );

    unsubs.push(
      backend.onEvent('search:complete', (ev) => {
        if (!ev || (this.currentSearchId !== 0 && ev.searchId !== this.currentSearchId)) return;
        this.flushPendingCounts();
        this.isSearching = false;
        this.hasCompletedSearch = true;
        this.wordsScanned = ev.wordsScanned;
        this.totalCandidateWords = ev.totalCandidateWords;
        this.elapsedMs = ev.elapsedMs;
        this.usedIndexedFTS = ev.usedIndexedFTS;
        if (ev.activeDicts && ev.activeDicts.length > 0) {
          this.searchActiveDicts = ev.activeDicts;
        }
      })
    );

    unsubs.push(
      backend.onEvent('search:error', (ev) => {
        if (!ev || (this.currentSearchId !== 0 && ev.searchId !== this.currentSearchId)) return;
        this.flushPendingCounts();
        this.isSearching = false;
        this.errorMessage = ev.message;
      })
    );

    unsubs.push(
      backend.onEvent('search:cancelled', (ev) => {
        if (!ev || (this.currentSearchId !== 0 && ev.searchId !== this.currentSearchId)) return;
        this.flushPendingCounts();
        this.isSearching = false;
      })
    );

    return () => {
      for (const off of unsubs) off();
    };
  }

  dispose(): void {
    this.cancelFrameFlush();
    if (this.validateTimer) {
      clearTimeout(this.validateTimer);
      this.validateTimer = null;
    }
  }
}

export function createSearchStore(deps: SearchStoreDeps): SearchStore {
  return new SearchStore(deps);
}
