import { getBackend } from '../bindings';
import { buildFontCssVars, DEFAULT_FONTS, normalizeFontConfig } from '../fonts';
import type { Config, ConfigPatch, EntryCategory } from '../types';

export const DEFAULT_SIDEBAR_WIDTH = 272;
export const MIN_SIDEBAR_WIDTH = 180;
export const MAX_SIDEBAR_WIDTH = 560;
export const SIDEBAR_COLLAPSE_THRESHOLD = 100;

export function clampSidebarWidth(w: unknown): number {
  if (typeof w !== 'number' || !Number.isFinite(w) || w <= 0) {
    return DEFAULT_SIDEBAR_WIDTH;
  }
  return Math.max(MIN_SIDEBAR_WIDTH, Math.min(MAX_SIDEBAR_WIDTH, Math.round(w)));
}

export const DEFAULT_CONFIG: Config = {
  dictDirs: [],
  dictionaries: [],
  atMacro: '[aeiou]',
  hashMacro: '[^aeiou]',
  minLength: 1,
  maxLength: 100,
  exclusiveVars: false,
  ignoreUnusedPunctuation: true,
  showBalances: true,
  showAnnotations: true,
  suppressDuplicates: true,
  entryFilter: ['lowercase', 'capitalized', 'punctuated', 'phrase'],
  sortMode: 'dict',
  layoutMode: 'tiled',
  sidebarWidth: DEFAULT_SIDEBAR_WIDTH,
  sidebarCollapsed: false,
  autoWebFallback: false,
  defaultWebProvider: 'onelook',
  webProviders: [
    { id: 'onelook', name: 'OneLook', urlTemplate: 'https://www.onelook.com/?w={entry}' },
    { id: 'wiktionary', name: 'Wiktionary', urlTemplate: 'https://en.wiktionary.org/wiki/{entry}' },
    {
      id: 'wikipedia',
      name: 'Wikipedia',
      urlTemplate: 'https://en.wikipedia.org/wiki/Special:Search?search={entry}'
    },
    { id: 'google', name: 'Google Define', urlTemplate: 'https://www.google.com/search?q=define+{entry}' },
    {
      id: 'merriam-webster',
      name: 'Merriam-Webster',
      urlTemplate: 'https://www.merriam-webster.com/dictionary/{entry}'
    }
  ],
  recentQueries: [],
  fonts: normalizeFontConfig(DEFAULT_FONTS)
};

export function normalizeConfig(raw: Partial<Config> | null | undefined): Config {
  if (!raw) return { ...DEFAULT_CONFIG, fonts: normalizeFontConfig(DEFAULT_FONTS) };
  return {
    ...DEFAULT_CONFIG,
    ...raw,
    dictDirs: Array.isArray(raw.dictDirs) ? raw.dictDirs : DEFAULT_CONFIG.dictDirs,
    dictionaries: Array.isArray(raw.dictionaries) ? raw.dictionaries : DEFAULT_CONFIG.dictionaries,
    entryFilter: Array.isArray(raw.entryFilter) ? raw.entryFilter : DEFAULT_CONFIG.entryFilter,
    webProviders: Array.isArray(raw.webProviders) ? raw.webProviders : DEFAULT_CONFIG.webProviders,
    recentQueries: Array.isArray(raw.recentQueries) ? raw.recentQueries : DEFAULT_CONFIG.recentQueries,
    sidebarWidth: clampSidebarWidth(raw.sidebarWidth),
    sidebarCollapsed:
      typeof raw.sidebarCollapsed === 'boolean' ? raw.sidebarCollapsed : DEFAULT_CONFIG.sidebarCollapsed,
    fonts: normalizeFontConfig(raw.fonts)
  };
}

export class ConfigStore {
  value: Config = $state(normalizeConfig(DEFAULT_CONFIG));
  readonly rootFontVars = $derived(buildFontCssVars(this.value.fonts));

  async load(): Promise<void> {
    try {
      const loadedCfg = await getBackend().getConfig();
      if (loadedCfg) {
        this.value = normalizeConfig(loadedCfg);
      }
    } catch {
      // Fallback defaults already set
    }
  }

  async applyPatch(patch: ConfigPatch): Promise<void> {
    // Optimistically update local config for crisp UI responsiveness.
    this.value = normalizeConfig({ ...this.value, ...patch });
    try {
      const updated = await getBackend().updateConfig(patch);
      if (updated) {
        this.value = normalizeConfig(updated);
      }
    } catch {
      // Keep optimistic state if headless
    }
  }

  recordRecentQuery(query: string): void {
    const prevRecent = Array.isArray(this.value.recentQueries) ? this.value.recentQueries : [];
    this.value = {
      ...this.value,
      recentQueries: [query, ...prevRecent.filter((item) => item !== query)].slice(0, 50)
    };
  }

  nextEntryFilterForToggle(cat: EntryCategory): EntryCategory[] {
    const currentFilter = Array.isArray(this.value.entryFilter)
      ? this.value.entryFilter
      : DEFAULT_CONFIG.entryFilter;
    const exists = currentFilter.includes(cat);
    const next = exists ? currentFilter.filter((c) => c !== cat) : [...currentFilter, cat];
    if (next.length === 0) {
      // Keep at least one category active so results aren't silently muted.
      return ['lowercase', 'capitalized', 'punctuated', 'phrase'];
    }
    return next;
  }

  async reset(resetDictionaries: boolean): Promise<void> {
    this.value = normalizeConfig({
      ...DEFAULT_CONFIG,
      dictionaries: resetDictionaries ? [] : this.value.dictionaries
    });
    try {
      const updated = await getBackend().resetConfig(resetDictionaries);
      if (updated) {
        this.value = normalizeConfig(updated);
      }
    } catch {
      // Keep optimistic state if headless
    }
  }
}

export function createConfigStore(): ConfigStore {
  return new ConfigStore();
}
