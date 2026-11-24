import type { ElementFont, FontConfig, FontElementKey } from './types';

export const DEFAULT_FONTS: FontConfig = {
  results: { family: 'monospace', size: 13 },
  definitions: { family: 'sans-serif', size: 15 },
  patternInput: { family: 'monospace', size: 14 },
  dictionariesList: { family: 'sans-serif', size: 12 },
  statusBar: { family: 'sans-serif', size: 11 },
  toolbar: { family: 'sans-serif', size: 12 }
};

export interface FontPreset {
  value: string;
  label: string;
  category: 'Generic' | 'Monospace' | 'Sans-Serif' | 'Serif';
}

export const FONT_PRESETS: FontPreset[] = [
  { value: 'sans-serif', label: 'System Sans-Serif', category: 'Generic' },
  { value: 'monospace', label: 'System Monospace', category: 'Generic' },
  { value: 'serif', label: 'System Serif', category: 'Generic' },
  { value: 'JetBrains Mono', label: 'JetBrains Mono', category: 'Monospace' },
  { value: 'Fira Code', label: 'Fira Code', category: 'Monospace' },
  { value: 'Cascadia Code', label: 'Cascadia Code', category: 'Monospace' },
  { value: 'Consolas', label: 'Consolas', category: 'Monospace' },
  { value: 'Courier New', label: 'Courier New', category: 'Monospace' },
  { value: 'Inter', label: 'Inter', category: 'Sans-Serif' },
  { value: 'Segoe UI', label: 'Segoe UI', category: 'Sans-Serif' },
  { value: 'Roboto', label: 'Roboto', category: 'Sans-Serif' },
  { value: 'Verdana', label: 'Verdana', category: 'Sans-Serif' },
  { value: 'Georgia', label: 'Georgia', category: 'Serif' },
  { value: 'Palatino Linotype', label: 'Palatino', category: 'Serif' },
  { value: 'Times New Roman', label: 'Times New Roman', category: 'Serif' }
];

export interface FontElementMeta {
  key: FontElementKey;
  label: string;
  description: string;
  sampleText: string;
  minSize: number;
  maxSize: number;
}

export const FONT_ELEMENTS: FontElementMeta[] = [
  {
    key: 'results',
    label: 'Results Text',
    description: 'Matched headwords, anagram balances, and annotations in tiled/unified panes',
    sampleText: 'matador · drama (-OT) · crossword 📖',
    minSize: 9,
    maxSize: 32
  },
  {
    key: 'definitions',
    label: 'Definitions Text',
    description: 'Dictionary definition articles, senses, and cross-reference body text',
    sampleText: 'CYNIC, n. A blackguard whose faulty vision sees things as they are.',
    minSize: 10,
    maxSize: 32
  },
  {
    key: 'patternInput',
    label: 'Pattern Input Text',
    description: 'Search pattern box, enumeration constraint input, and recent query history',
    sampleText: 'm.....r;aadmort* & ![aeiou]z*',
    minSize: 10,
    maxSize: 32
  },
  {
    key: 'dictionariesList',
    label: 'Dictionaries List',
    description: 'Sidebar dictionary names, word counts, and pattern/output option toggles',
    sampleText: "Core English (241.4k) · The Devil's Dictionary (defs, fts)",
    minSize: 9,
    maxSize: 24
  },
  {
    key: 'statusBar',
    label: 'Status Bar Text',
    description: 'Bottom status bar match counts, scan timing, and active macro indicators',
    sampleText: '42 matches · Scanned 241,350 words in 3.2 ms · @=[aeiou] #=[^aeiou]',
    minSize: 9,
    maxSize: 22
  },
  {
    key: 'toolbar',
    label: 'Filter Toolbar & Headers',
    description: 'Word length filters, entry category buttons, sort order, and pane headers',
    sampleText: 'Length 1–100 · abc Abc a-b a b · ✓ Dedupe · Smart Balance',
    minSize: 9,
    maxSize: 24
  }
];

function clampFontSize(size: unknown, fallback: number): number {
  const num = typeof size === 'number' ? size : Number(size);
  if (!Number.isFinite(num) || num <= 0) return fallback;
  return Math.max(8, Math.min(48, Math.round(num)));
}

function normalizeElementFont(raw: Partial<ElementFont> | null | undefined, def: ElementFont): ElementFont {
  if (!raw || typeof raw !== 'object') {
    return { ...def };
  }
  const family = typeof raw.family === 'string' && raw.family.trim() !== '' ? raw.family.trim() : def.family;
  const size = clampFontSize(raw.size, def.size);
  return { family, size };
}

export function normalizeFontConfig(raw?: Partial<FontConfig> | null): FontConfig {
  if (!raw || typeof raw !== 'object') {
    return {
      results: { ...DEFAULT_FONTS.results },
      definitions: { ...DEFAULT_FONTS.definitions },
      patternInput: { ...DEFAULT_FONTS.patternInput },
      dictionariesList: { ...DEFAULT_FONTS.dictionariesList },
      statusBar: { ...DEFAULT_FONTS.statusBar },
      toolbar: { ...DEFAULT_FONTS.toolbar }
    };
  }
  return {
    results: normalizeElementFont(raw.results, DEFAULT_FONTS.results),
    definitions: normalizeElementFont(raw.definitions, DEFAULT_FONTS.definitions),
    patternInput: normalizeElementFont(raw.patternInput, DEFAULT_FONTS.patternInput),
    dictionariesList: normalizeElementFont(raw.dictionariesList, DEFAULT_FONTS.dictionariesList),
    statusBar: normalizeElementFont(raw.statusBar, DEFAULT_FONTS.statusBar),
    toolbar: normalizeElementFont(raw.toolbar, DEFAULT_FONTS.toolbar)
  };
}

function sanitizeFontToken(raw: string): string {
  return raw.replace(/[^A-Za-z0-9 \-_'"]/g, '').replace(/"/g, '').trim();
}

export function resolveFontFamily(family: string): string {
  const clean = (family ?? '').trim();
  if (!clean || clean === 'sans-serif') {
    return 'ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif';
  }
  if (clean === 'monospace') {
    return 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace';
  }
  if (clean === 'serif') {
    return 'ui-serif, Georgia, Cambria, "Times New Roman", Times, serif';
  }
  if (clean.includes(',')) {
    const parts = clean
      .split(',')
      .map((part) => sanitizeFontToken(part))
      .filter((part) => part.length > 0);
    if (parts.length === 0) {
      return 'ui-sans-serif, system-ui, sans-serif';
    }
    return parts.join(', ');
  }
  const escaped = sanitizeFontToken(clean);
  if (!escaped) {
    return 'ui-sans-serif, system-ui, sans-serif';
  }
  return `"${escaped}", ui-sans-serif, system-ui, sans-serif`;
}

export function formatFontSummary(ef: ElementFont): string {
  const preset = FONT_PRESETS.find((p) => p.value.toLowerCase() === ef.family.toLowerCase());
  const label = preset ? preset.label : ef.family;
  return `${label} · ${ef.size}px`;
}

export function buildFontCssVars(fonts: FontConfig): string {
  const f = normalizeFontConfig(fonts);
  return [
    `--tisane-font-results-family: ${resolveFontFamily(f.results.family)}`,
    `--tisane-font-results-size: ${f.results.size}px`,
    `--tisane-font-definitions-family: ${resolveFontFamily(f.definitions.family)}`,
    `--tisane-font-definitions-size: ${f.definitions.size}px`,
    `--tisane-font-pattern-family: ${resolveFontFamily(f.patternInput.family)}`,
    `--tisane-font-pattern-size: ${f.patternInput.size}px`,
    `--tisane-font-dicts-family: ${resolveFontFamily(f.dictionariesList.family)}`,
    `--tisane-font-dicts-size: ${f.dictionariesList.size}px`,
    `--tisane-font-status-family: ${resolveFontFamily(f.statusBar.family)}`,
    `--tisane-font-status-size: ${f.statusBar.size}px`,
    `--tisane-font-toolbar-family: ${resolveFontFamily(f.toolbar.family)}`,
    `--tisane-font-toolbar-size: ${f.toolbar.size}px`
  ].join('; ');
}
