import { getBackend } from '../bindings';
import { buildFontCssVars, DEFAULT_FONTS, normalizeFontConfig } from '../fonts';
import type { Config, ConfigPatch, EntryCategory, ThemeMode, ZoomAction } from '../types';

export const BOOT_CACHE_KEY = 'tisane:boot';

export const DEFAULT_SIDEBAR_WIDTH = 272;
export const MIN_SIDEBAR_WIDTH = 180;
export const MAX_SIDEBAR_WIDTH = 560;
export const SIDEBAR_COLLAPSE_THRESHOLD = 100;

export const DEFAULT_DEFINITION_HEIGHT = 256;
export const MIN_DEFINITION_HEIGHT = 120;
export const MAX_DEFINITION_HEIGHT = 640;
export const DEFINITION_COLLAPSE_THRESHOLD = 72;

export const DEFAULT_UI_ZOOM = 100;
export const MIN_UI_ZOOM = 75;
export const MAX_UI_ZOOM = 200;
export const UI_ZOOM_STEP = 10;

export function clampSidebarWidth(w: unknown): number {
  if (typeof w !== 'number' || !Number.isFinite(w) || w <= 0) {
    return DEFAULT_SIDEBAR_WIDTH;
  }
  return Math.max(MIN_SIDEBAR_WIDTH, Math.min(MAX_SIDEBAR_WIDTH, Math.round(w)));
}

export function clampDefinitionHeight(h: unknown): number {
  if (typeof h !== 'number' || !Number.isFinite(h) || h <= 0) {
    return DEFAULT_DEFINITION_HEIGHT;
  }
  return Math.max(MIN_DEFINITION_HEIGHT, Math.min(MAX_DEFINITION_HEIGHT, Math.round(h)));
}

export function clampUIZoom(z: unknown): number {
  if (typeof z !== 'number' || !Number.isFinite(z) || z <= 0) {
    return DEFAULT_UI_ZOOM;
  }
  return Math.max(MIN_UI_ZOOM, Math.min(MAX_UI_ZOOM, Math.round(z)));
}

export function normalizeTheme(raw: unknown): ThemeMode {
  if (typeof raw !== 'string') return 'dark';
  const v = raw.trim().toLowerCase();
  if (v === 'light') return 'light';
  if (v === 'system' || v === 'auto') return 'system';
  return 'dark';
}

export function resolveEffectiveTheme(theme: ThemeMode): 'dark' | 'light' {
  if (theme === 'light') return 'light';
  if (theme === 'system') {
    if (
      typeof window !== 'undefined' &&
      typeof window.matchMedia === 'function' &&
      window.matchMedia('(prefers-color-scheme: light)').matches
    ) {
      return 'light';
    }
    return 'dark';
  }
  return 'dark';
}

export function applyDocumentTheme(theme: ThemeMode): 'dark' | 'light' {
  const effective = resolveEffectiveTheme(theme);
  if (typeof document !== 'undefined' && document.documentElement) {
    document.documentElement.dataset.theme = effective;
    document.documentElement.style.colorScheme = effective;
  }
  return effective;
}

export function applyDocumentZoom(zoom: number): number {
  const clamped = clampUIZoom(zoom);
  if (typeof document !== 'undefined' && document.documentElement) {
    document.documentElement.style.zoom = clamped === 100 ? '' : String(clamped / 100);
  }
  return clamped;
}

interface BootCachePayload {
  theme?: ThemeMode;
  uiZoom?: number;
}

function readBootCache(): BootCachePayload {
  if (typeof window === 'undefined' || !window.localStorage) return {};
  try {
    const raw = window.localStorage.getItem(BOOT_CACHE_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    if (!parsed || typeof parsed !== 'object') return {};
    return {
      theme: normalizeTheme(parsed.theme),
      uiZoom: typeof parsed.uiZoom === 'number' ? parsed.uiZoom : undefined
    };
  } catch {
    return {};
  }
}

function writeBootCache(payload: BootCachePayload): void {
  if (typeof window === 'undefined' || !window.localStorage) return;
  try {
    const existing = readBootCache();
    const merged: BootCachePayload = {
      ...existing,
      ...payload
    };
    window.localStorage.setItem(BOOT_CACHE_KEY, JSON.stringify(merged));
  } catch {
    // Ignore storage quota or security errors
  }
}

function clearBootCache(): void {
  if (typeof window === 'undefined' || !window.localStorage) return;
  try {
    window.localStorage.removeItem(BOOT_CACHE_KEY);
  } catch {
    // Ignore
  }
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
  definitionHeight: DEFAULT_DEFINITION_HEIGHT,
  definitionCollapsed: false,
  theme: 'dark',
  uiZoom: DEFAULT_UI_ZOOM,
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
    definitionHeight: clampDefinitionHeight(raw.definitionHeight),
    definitionCollapsed:
      typeof raw.definitionCollapsed === 'boolean'
        ? raw.definitionCollapsed
        : DEFAULT_CONFIG.definitionCollapsed,
    theme: normalizeTheme(raw.theme),
    uiZoom: clampUIZoom(raw.uiZoom),
    fonts: normalizeFontConfig(raw.fonts)
  };
}

function buildInitialConfig(): Config {
  const cached = readBootCache();
  return normalizeConfig({
    ...DEFAULT_CONFIG,
    theme: cached.theme ?? DEFAULT_CONFIG.theme,
    uiZoom: cached.uiZoom ?? DEFAULT_CONFIG.uiZoom
  });
}

export class ConfigStore {
  value: Config = $state(buildInitialConfig());
  readonly rootFontVars = $derived(buildFontCssVars(this.value.fonts));

  constructor() {
    applyDocumentTheme(this.value.theme);
    applyDocumentZoom(this.value.uiZoom);
  }

  private syncBootAndDocument(): void {
    applyDocumentTheme(this.value.theme);
    applyDocumentZoom(this.value.uiZoom);
    writeBootCache({ theme: this.value.theme, uiZoom: this.value.uiZoom });
  }

  nextZoomForAction(action: ZoomAction): number {
    if (action === 'in') {
      return clampUIZoom(this.value.uiZoom + UI_ZOOM_STEP);
    }
    if (action === 'out') {
      return clampUIZoom(this.value.uiZoom - UI_ZOOM_STEP);
    }
    return DEFAULT_UI_ZOOM;
  }

  subscribeSystemTheme(): () => void {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
      return () => {};
    }
    const mql = window.matchMedia('(prefers-color-scheme: light)');
    const onChange = () => {
      if (this.value.theme === 'system') {
        applyDocumentTheme('system');
      }
    };
    if (typeof mql.addEventListener === 'function') {
      mql.addEventListener('change', onChange);
      return () => mql.removeEventListener('change', onChange);
    }
    return () => {};
  }

  async load(): Promise<void> {
    try {
      const loadedCfg = await getBackend().getConfig();
      if (loadedCfg) {
        this.value = normalizeConfig(loadedCfg);
        this.syncBootAndDocument();
      }
    } catch {
      // Fallback defaults already set
    }
  }

  async applyPatch(patch: ConfigPatch): Promise<void> {
    // Optimistically update local config for crisp UI responsiveness.
    this.value = normalizeConfig({ ...this.value, ...patch });
    this.syncBootAndDocument();
    try {
      const updated = await getBackend().updateConfig(patch);
      if (updated) {
        this.value = normalizeConfig(updated);
        this.syncBootAndDocument();
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
    clearBootCache();
    this.value = normalizeConfig({
      ...DEFAULT_CONFIG,
      dictionaries: resetDictionaries ? [] : this.value.dictionaries
    });
    applyDocumentTheme(this.value.theme);
    applyDocumentZoom(this.value.uiZoom);
    try {
      const updated = await getBackend().resetConfig(resetDictionaries);
      if (updated) {
        this.value = normalizeConfig(updated);
        this.syncBootAndDocument();
      }
    } catch {
      // Keep optimistic state if headless
    }
  }
}

export function createConfigStore(): ConfigStore {
  return new ConfigStore();
}
