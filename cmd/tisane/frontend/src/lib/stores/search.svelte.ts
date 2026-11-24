import { getBackend, type UnsubscribeFn } from '../bindings';
import type { Config, DictionaryInfo, Match, SearchMode, ValidationResult } from '../types';
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

  allMatches: Match[] = $state.raw([]);
  matchesByDict: Match[][] = $state.raw([]);
  searchActiveDicts: DictionaryInfo[] = $state([]);

  wordsScanned = $state(0);
  totalCandidateWords = $state(0);
  elapsedMs = $state(0);
  usedIndexedFTS = $state(false);

  private nextClientSearchId = 1;
  private validateTimer: ReturnType<typeof setTimeout> | null = null;
  private deps: SearchStoreDeps;

  constructor(deps: SearchStoreDeps) {
    this.deps = deps;
  }

  initActiveDicts(active: DictionaryInfo[]): void {
    this.searchActiveDicts = active;
    this.matchesByDict = active.map(() => []);
  }

  onDictionariesChanged(active: DictionaryInfo[]): void {
    this.searchActiveDicts = active;
    this.scheduleValidation(true);
    if (this.query.trim() !== '' && (this.hasCompletedSearch || this.isSearching)) {
      void this.execute();
    } else {
      this.allMatches = [];
      this.matchesByDict = active.map(() => []);
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
    this.allMatches = [];
    this.matchesByDict = activeSnapshot.map(() => []);
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
          this.wordsScanned = ev.progress.wordsScanned;
          this.totalCandidateWords = ev.progress.totalCandidateWords;
        }
        if (ev.matches && ev.matches.length > 0) {
          const prevEmpty = this.allMatches.length === 0;
          this.allMatches = this.allMatches.concat(ev.matches);

          const grouped = new Map<number, Match[]>();
          let maxIdx = this.matchesByDict.length - 1;
          for (let i = 0; i < ev.matches.length; i++) {
            const m = ev.matches[i];
            const dIdx = m.dictIndex ?? 0;
            if (dIdx > maxIdx) maxIdx = dIdx;
            let arr = grouped.get(dIdx);
            if (!arr) {
              arr = [];
              grouped.set(dIdx, arr);
            }
            arr.push(m);
          }

          const nextBuckets: Match[][] = new Array(maxIdx + 1);
          for (let i = 0; i <= maxIdx; i++) {
            const existing = this.matchesByDict[i] ?? [];
            const added = grouped.get(i);
            nextBuckets[i] = added ? existing.concat(added) : existing;
          }
          this.matchesByDict = nextBuckets;

          if (prevEmpty) {
            this.deps.onFirstMatch(ev.matches[0].word);
          }
        }
      })
    );

    unsubs.push(
      backend.onEvent('search:progress', (ev) => {
        if (!ev || (this.currentSearchId !== 0 && ev.searchId !== this.currentSearchId)) return;
        this.wordsScanned = ev.progress.wordsScanned;
        this.totalCandidateWords = ev.progress.totalCandidateWords;
      })
    );

    unsubs.push(
      backend.onEvent('search:complete', (ev) => {
        if (!ev || (this.currentSearchId !== 0 && ev.searchId !== this.currentSearchId)) return;
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
        this.isSearching = false;
        this.errorMessage = ev.message;
      })
    );

    return () => {
      for (const off of unsubs) off();
    };
  }

  dispose(): void {
    if (this.validateTimer) {
      clearTimeout(this.validateTimer);
      this.validateTimer = null;
    }
  }
}

export function createSearchStore(deps: SearchStoreDeps): SearchStore {
  return new SearchStore(deps);
}
