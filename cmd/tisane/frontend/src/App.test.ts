import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/svelte';
import { tick } from 'svelte';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import App from './App.svelte';
import {
  isDesktopWebview,
  setBackendForTesting,
  type TisaneBackend,
  type WailsEventMap
} from './lib/bindings';
import { buildFontCssVars, DEFAULT_FONTS, normalizeFontConfig } from './lib/fonts';
import { DEFAULT_CONFIG } from './lib/stores/config.svelte';
import { createSearchStore } from './lib/stores/search.svelte';
import { APP_VERSION } from './lib/version';
import type {
  BuildDictionaryRequest,
  BuildDictionaryResult,
  Config,
  ConfigPatch,
  DictionaryInfo,
  LookupResult,
  Match,
  MergeWordListsRequest,
  MergeWordListsResult,
  SearchRequest,
  ValidateRequest
} from './lib/types';

function createMockHarness() {
  const listeners = new Map<string, Set<(data: any) => void>>();

  let dicts: DictionaryInfo[] = [
    {
      id: '/dicts/core.tsd',
      path: '/dicts/core.tsd',
      name: 'Core English',
      description: 'Core word list',
      version: 'TSD0',
      fileSize: 415696,
      wordCount: 241350,
      minWordLength: 2,
      maxWordLength: 28,
      usedLetters: 26,
      totalLetters: 63,
      ftsWordCount: 0,
      hasDefinitions: false,
      hasFTS: false,
      enabled: true
    },
    {
      id: '/dicts/devils.tsd',
      path: '/dicts/devils.tsd',
      name: "The Devil's Dictionary",
      description: 'Ambrose Bierce definitions',
      version: 'TSD1',
      fileSize: 1207350,
      wordCount: 1580,
      minWordLength: 3,
      maxWordLength: 24,
      usedLetters: 26,
      totalLetters: 63,
      ftsWordCount: 412,
      hasDefinitions: true,
      hasFTS: true,
      enabled: true
    }
  ];

  let config: Config = {
    dictDirs: [],
    dictionaries: dicts.map((d) => ({ path: d.path, name: d.name, enabled: d.enabled })),
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
    sidebarWidth: 272,
    sidebarCollapsed: false,
    definitionHeight: 256,
    definitionCollapsed: false,
    theme: 'dark',
    uiZoom: 100,
    autoWebFallback: false,
    defaultWebProvider: 'onelook',
    webProviders: [
      { id: 'onelook', name: 'OneLook', urlTemplate: 'https://www.onelook.com/?w={entry}' },
      { id: 'wiktionary', name: 'Wiktionary', urlTemplate: 'https://en.wiktionary.org/wiki/{entry}' }
    ],
    recentQueries: [],
    fonts: normalizeFontConfig(DEFAULT_FONTS)
  };

  let searchSeq = 0;
  const searchRequests: SearchRequest[] = [];
  const webLookups: { word: string; providerId: string }[] = [];
  const externalURLs: string[] = [];
  const openedDirectories: string[] = [];

  const definitionsMap = new Map<string, LookupResult>([
    [
      'cynic',
      {
        word: 'cynic',
        entries: [
          {
            dictIndex: 1,
            dict: "The Devil's Dictionary",
            offset: 128,
            isRTF: true,
            headwords: [{ headword: 'Cynic', qualifier: 'n.' }],
            html: '<p><b>CYNIC</b>, <i>n.</i> A blackguard whose faulty vision sees things as they are. See <a href="https://example.org/bierce">Bierce</a>.</p>',
            text: 'CYNIC, n. A blackguard whose faulty vision sees things as they are.'
          }
        ],
        webLinks: [
          { id: 'onelook', name: 'OneLook', url: 'https://www.onelook.com/?w=cynic' },
          { id: 'wiktionary', name: 'Wiktionary', url: 'https://en.wiktionary.org/wiki/cynic' }
        ]
      }
    ]
  ]);

  function emit(eventName: string, payload: unknown) {
    const subs = listeners.get(eventName);
    if (subs) {
      for (const fn of subs) {
        fn(payload);
      }
    }
  }

  const backend: TisaneBackend = {
    listDictionaries: vi.fn(async () => dicts.map((d) => ({ ...d }))),
    setEnabled: vi.fn(async (id: string, enabled: boolean) => {
      dicts = dicts.map((d) => (d.id === id ? { ...d, enabled } : d));
      return dicts.map((d) => ({ ...d }));
    }),
    reorderDictionaries: vi.fn(async (orderedIds: string[]) => {
      const byId = new Map(dicts.map((d) => [d.id, d]));
      dicts = orderedIds.map((id) => byId.get(id)!);
      return dicts.map((d) => ({ ...d }));
    }),
    browseAndAddDictionaries: vi.fn(async () => dicts),
    browseAndAddDirectory: vi.fn(async () => dicts),
    removeDictionary: vi.fn(async (id: string) => {
      dicts = dicts.filter((d) => d.id !== id);
      return dicts;
    }),
    openDictionaryDirectory: vi.fn(async (id: string) => {
      openedDirectories.push(id);
    }),
    browseWordListFile: vi.fn(async () => '/tmp/wordlist.txt'),
    browseSaveDictionaryFile: vi.fn(async (defaultPath: string) => defaultPath || '/tmp/custom.tsd'),
    browseSaveWordListFile: vi.fn(async (defaultPath: string) => defaultPath || '/tmp/merged.txt'),
    browseAlphabetMapFile: vi.fn(async () => '/tmp/ANSI.TSM'),
    buildDictionary: vi.fn(async (req: BuildDictionaryRequest): Promise<BuildDictionaryResult> => {
      const result: BuildDictionaryResult = {
        outputPath: req.outputPath,
        inputLines: 120,
        uniqueWords: 98,
        maxWordLength: Math.min(req.maxLen, 14),
        pass1Definitions: 4,
        pass2Definitions: 25,
        ftsOnlyWords: 12,
        usedLetters: 26,
        totalLetters: 34,
        bytesWritten: 8420,
        elapsedMs: 12.5,
        addedToLibrary: req.addToLibrary
      };
      if (req.addToLibrary) {
        dicts = [
          ...dicts,
          {
            id: req.outputPath,
            path: req.outputPath,
            name: req.outputPath.split('/').pop()?.replace(/\.tsd$/i, '') || 'custom',
            description: req.description,
            version: 'TSD1',
            wordCount: result.uniqueWords,
            maxWordLength: result.maxWordLength,
            hasDefinitions: true,
            hasFTS: req.maxFTSPercent > 0,
            enabled: true
          }
        ];
        emit('dicts:updated', dicts.map((d) => ({ ...d })));
      }
      return result;
    }),
    mergeWordLists: vi.fn(async (req: MergeWordListsRequest): Promise<MergeWordListsResult> => ({
      outputPath: req.outputPath,
      operation: req.operation,
      conversion: req.conversion,
      primaryLines: 50,
      primaryEntries: 48,
      secondaryLines: 30,
      secondaryEntries: 29,
      outputEntries: 20,
      bytesWritten: 256,
      elapsedMs: 3.4
    })),

    validatePattern: vi.fn(async (req: ValidateRequest) => {
      if (req.query.includes('[123]')) {
        const idx = req.query.indexOf('[123]');
        return {
          valid: false,
          code: 2,
          message: 'Choice lists can only include letters, optionally preceded by ^',
          span: { start: idx, end: idx + 5 },
          minMatchedLength: 0,
          maxMatchedLength: 0,
          hasWarning: false
        };
      }
      return {
        valid: true,
        code: 0,
        message: 'Pattern is valid',
        span: { start: 0, end: req.query.length },
        minMatchedLength: 5,
        maxMatchedLength: 7,
        hasWarning: false
      };
    }),

    startSearch: vi.fn(async (req: SearchRequest) => {
      searchRequests.push(req);
      searchSeq = req.searchId ?? searchSeq + 1;
      return searchSeq;
    }),
    cancelSearch: vi.fn(async () => true),

    getDefinitions: vi.fn(async (word: string) => {
      const hit = definitionsMap.get(word.toLowerCase());
      if (hit) return hit;
      return {
        word,
        entries: [],
        webLinks: [
          { id: 'onelook', name: 'OneLook', url: `https://www.onelook.com/?w=${encodeURIComponent(word)}` },
          { id: 'wiktionary', name: 'Wiktionary', url: `https://en.wiktionary.org/wiki/${encodeURIComponent(word)}` }
        ]
      };
    }),
    openWebLookup: vi.fn(async (word: string, providerId: string) => {
      webLookups.push({ word, providerId });
      return `https://en.wiktionary.org/wiki/${word}`;
    }),
    openExternalURL: vi.fn(async (rawUrl: string) => {
      externalURLs.push(rawUrl);
    }),

    getConfig: vi.fn(async () => ({ ...config })),
    updateConfig: vi.fn(async (patch: ConfigPatch) => {
      config = { ...config, ...patch };
      return { ...config };
    }),
    resetConfig: vi.fn(async (resetDictionaries: boolean) => {
      if (resetDictionaries) {
        dicts = [
          {
            id: '/dicts/core.tsd',
            path: '/dicts/core.tsd',
            name: 'Core English',
            description: 'Core word list',
            version: '1.2.0',
            wordCount: 241350,
            maxWordLength: 28,
            hasDefinitions: false,
            hasFTS: false,
            enabled: true
          },
          {
            id: '/dicts/devils.tsd',
            path: '/dicts/devils.tsd',
            name: "The Devil's Dictionary",
            description: 'Ambrose Bierce definitions',
            version: '1.2.0',
            wordCount: 1580,
            maxWordLength: 24,
            hasDefinitions: true,
            hasFTS: true,
            enabled: true
          }
        ];
      }
      config = {
        dictDirs: [],
        dictionaries: dicts.map((d) => ({ path: d.path, name: d.name, enabled: d.enabled })),
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
        sidebarWidth: 272,
        sidebarCollapsed: false,
        definitionHeight: 256,
        definitionCollapsed: false,
        theme: 'dark',
        uiZoom: 100,
        autoWebFallback: false,
        defaultWebProvider: 'onelook',
        webProviders: [
          { id: 'onelook', name: 'OneLook', urlTemplate: 'https://www.onelook.com/?w={entry}' },
          { id: 'wiktionary', name: 'Wiktionary', urlTemplate: 'https://en.wiktionary.org/wiki/{entry}' }
        ],
        recentQueries: [],
        fonts: normalizeFontConfig(DEFAULT_FONTS)
      };
      emit('dicts:updated', dicts.map((d) => ({ ...d })));
      return { ...config };
    }),

    onEvent: <K extends keyof WailsEventMap>(
      eventName: K,
      handler: (data: WailsEventMap[K]) => void
    ) => {
      if (!listeners.has(eventName)) {
        listeners.set(eventName, new Set());
      }
      listeners.get(eventName)!.add(handler as (data: any) => void);
      return () => {
        listeners.get(eventName)?.delete(handler as (data: any) => void);
      };
    }
  };

  return { backend, emit, searchRequests, webLookups, externalURLs, openedDirectories };
}

describe('Tisane Svelte 5 Desktop UI', () => {
  beforeEach(() => {
    setBackendForTesting(null);
    window.localStorage.clear();
    delete document.documentElement.dataset.theme;
    document.documentElement.style.colorScheme = '';
    document.documentElement.style.zoom = '';
  });

  it('defaults to 2 separate tiled panes per dictionary, switches to unified view, and handles RTF definitions + Web Search fallback', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);

    render(App);

    // Wait for initial dictionaries to render in two separate tiled panes by default
    await waitFor(() => {
      expect(screen.getByTestId('tiled-pane-0')).toBeInTheDocument();
      expect(screen.getByTestId('tiled-pane-1')).toBeInTheDocument();
    });

    const logo = screen.getByTestId('app-logo') as HTMLImageElement;
    expect(logo).toBeInTheDocument();
    expect(logo.getAttribute('src')).toBe('/icon.png');

    // Enter pattern and run search
    const input = screen.getByTestId('pattern-input') as HTMLInputElement;
    await fireEvent.input(input, { target: { value: '.....*;aadmort' } });
    await fireEvent.click(screen.getByTestId('run-search-btn'));

    await waitFor(() => {
      expect(h.searchRequests.length).toBe(1);
    });

    // Stream matches into both active dictionary panes (dictIndex 0 and dictIndex 1)
    h.emit('search:batch', {
      searchId: 1,
      matches: [
        { word: 'matador', balance: '', length: 7, dictIndex: 0, dict: 'Core English', hasDefinition: false },
        { word: 'drama', balance: '-OT', length: 5, dictIndex: 0, dict: 'Core English', hasDefinition: false },
        { word: 'cynic', balance: '', length: 5, dictIndex: 1, dict: "The Devil's Dictionary", hasDefinition: true }
      ],
      progress: { dictIndex: 1, dictName: "The Devil's Dictionary", wordsScanned: 242930, totalCandidateWords: 242930 }
    });
    h.emit('search:complete', {
      searchId: 1,
      totalMatches: 3,
      wordsScanned: 242930,
      totalCandidateWords: 242930,
      elapsedMs: 3.8,
      usedIndexedFTS: false,
      activeDicts: await h.backend.listDictionaries()
    });
    await tick();

    expect(screen.getByTestId('tiled-pane-0-count')).toHaveTextContent('2');
    expect(screen.getByTestId('tiled-pane-1-count')).toHaveTextContent('1');

    // First match ("matador") has no local definition -> Web Search fallback card is shown
    await waitFor(() => {
      expect(screen.getByTestId('web-fallback-card')).toBeInTheDocument();
    });
    await fireEvent.click(screen.getByTestId('fallback-web-wiktionary'));
    expect(h.webLookups).toEqual([{ word: 'matador', providerId: 'wiktionary' }]);

    // Select "cynic" in Pane 1 -> renders sanitized RTF HTML definition
    await fireEvent.click(screen.getByText('cynic'));
    await waitFor(() => {
      expect(screen.getByTestId('definition-entries')).toHaveTextContent('faulty vision sees things as they are');
    });

    // Clicking external https link in RTF calls openExternalURL safely
    await fireEvent.click(screen.getByText('Bierce'));
    expect(h.externalURLs).toEqual(['https://example.org/bierce']);

    // History Back button returns to "matador"
    await fireEvent.click(screen.getByTestId('def-back-btn'));
    await waitFor(() => {
      expect(screen.getByTestId('definition-word-heading')).toHaveTextContent('matador');
    });

    // Toggle layout to Unified Merged List -> single pane with all 3 matches
    await fireEvent.click(screen.getByTestId('layout-unified-btn'));
    await waitFor(() => {
      expect(screen.getByTestId('unified-pane-count')).toHaveTextContent('3');
    });
  });

  it('virtualizes 200,000 streamed matches across 2 tiled panes without DOM bloat and ignores stale searchId batches', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);

    const { container } = render(App);
    await waitFor(() => expect(screen.getByTestId('tiled-pane-0')).toBeInTheDocument());

    const input = screen.getByTestId('pattern-input');
    await fireEvent.input(input, { target: { value: '*' } });
    await fireEvent.keyDown(input, { key: 'Enter' }); // searchId = 1
    await fireEvent.keyDown(input, { key: 'Enter' }); // searchId = 2 (supersedes 1)

    await waitFor(() => expect(h.searchRequests.length).toBe(2));

    // Stale batch from searchId=1 must be ignored
    h.emit('search:batch', {
      searchId: 1,
      matches: [{ word: 'staleword', length: 9, dictIndex: 0, hasDefinition: false }],
      progress: { dictIndex: 0, dictName: 'Core English', wordsScanned: 10, totalCandidateWords: 10 }
    });

    // Build 200,000 matches for searchId=2 (100,000 in Pane 0 + 100,000 in Pane 1)
    const bigBatch: Match[] = new Array(200_000);
    for (let i = 0; i < 100_000; i++) {
      bigBatch[i] = { word: `alpha${i}`, length: 6, dictIndex: 0, dict: 'Core English', hasDefinition: false };
    }
    for (let i = 0; i < 100_000; i++) {
      bigBatch[100_000 + i] = { word: `beta${i}`, length: 5, dictIndex: 1, dict: "The Devil's Dictionary", hasDefinition: false };
    }

    h.emit('search:batch', {
      searchId: 2,
      matches: bigBatch,
      progress: { dictIndex: 1, dictName: "The Devil's Dictionary", wordsScanned: 200_000, totalCandidateWords: 200_000 }
    });
    h.emit('search:complete', {
      searchId: 2,
      totalMatches: 200_000,
      wordsScanned: 200_000,
      totalCandidateWords: 200_000,
      elapsedMs: 12.4,
      usedIndexedFTS: false,
      activeDicts: await h.backend.listDictionaries()
    });
    await tick();

    expect(screen.queryByText('staleword')).not.toBeInTheDocument();
    expect(screen.getByTestId('tiled-pane-0-count')).toHaveTextContent('100,000');
    expect(screen.getByTestId('tiled-pane-1-count')).toHaveTextContent('100,000');

    // Verify @tanstack/svelte-virtual keeps actual mounted DOM rows tiny (< 100 for 200,000 items!)
    const renderedRows = container.querySelectorAll('[data-testid="result-row"]');
    expect(renderedRows.length).toBeGreaterThan(10);
    expect(renderedRows.length).toBeLessThan(100);
  });

  it('highlights live syntax error spans and reorders dictionaries in priority order', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);

    render(App);
    await waitFor(() => expect(screen.getByTestId('dict-item-0')).toBeInTheDocument());

    // Live syntax error span highlight
    const input = screen.getByTestId('pattern-input');
    await fireEvent.input(input, { target: { value: 'ab[123]cd' } });

    await waitFor(() => {
      const banner = screen.getByTestId('validation-banner');
      expect(banner).toHaveTextContent('Choice lists can only include letters');
      expect(banner).toHaveTextContent('[123]');
    });

    // Reorder second dictionary above first dictionary
    await fireEvent.click(screen.getByTestId('dict-up-1'));
    expect(h.backend.reorderDictionaries).toHaveBeenCalledWith([
      '/dicts/devils.tsd',
      '/dicts/core.tsd'
    ]);
  });

  it('handles null array fields from backend config without status bar filter error', async () => {
    const h = createMockHarness();
    h.backend.getConfig = vi.fn(async (): Promise<Config> => ({
      dictDirs: null as unknown as string[],
      dictionaries: null as unknown as Config['dictionaries'],
      atMacro: '[aeiou]',
      hashMacro: '[^aeiou]',
      minLength: 1,
      maxLength: 100,
      exclusiveVars: false,
      ignoreUnusedPunctuation: true,
      showBalances: true,
      showAnnotations: true,
      suppressDuplicates: true,
      entryFilter: null as unknown as Config['entryFilter'],
      sortMode: 'dict',
      layoutMode: 'tiled',
      sidebarWidth: 0,
      sidebarCollapsed: false,
      definitionHeight: 0,
      definitionCollapsed: false,
      theme: 'dark',
      uiZoom: 0,
      autoWebFallback: false,
      defaultWebProvider: 'onelook',
      webProviders: null as unknown as Config['webProviders'],
      recentQueries: null as unknown as string[],
      fonts: null as unknown as Config['fonts']
    }));
    setBackendForTesting(h.backend);

    render(App);
    await waitFor(() => expect(screen.getByTestId('tiled-pane-0')).toBeInTheDocument());

    const input = screen.getByTestId('pattern-input') as HTMLInputElement;
    await fireEvent.input(input, { target: { value: 'c.o..w.rd' } });
    await fireEvent.click(screen.getByTestId('run-search-btn'));

    await waitFor(() => {
      expect(h.searchRequests.length).toBe(1);
    });
    expect(screen.getByTestId('status-bar')).not.toHaveTextContent('Error:');
  });

  it('opens the Font Customization dialog through Settings and customizes fonts and sizes across UI elements', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);

    render(App);
    await waitFor(() => expect(screen.getByTestId('tiled-pane-0')).toBeInTheDocument());

    // Open Settings modal -> click "Customize Fonts…" button
    await fireEvent.click(screen.getByTestId('open-settings-btn'));
    expect(screen.getByTestId('settings-modal')).toBeInTheDocument();

    await fireEvent.click(screen.getByTestId('open-font-settings-btn'));
    expect(screen.getByTestId('font-settings-modal')).toBeInTheDocument();

    // Customize Results Text: JetBrains Mono, 18px
    await fireEvent.change(screen.getByTestId('font-family-select-results'), {
      target: { value: 'JetBrains Mono' }
    });
    await fireEvent.change(screen.getByTestId('font-size-input-results'), {
      target: { value: '18' }
    });

    // Customize Definitions Text: Georgia, 19px
    await fireEvent.change(screen.getByTestId('font-family-select-definitions'), {
      target: { value: 'Georgia' }
    });
    await fireEvent.change(screen.getByTestId('font-size-input-definitions'), {
      target: { value: '19' }
    });

    // Customize Pattern Input Text with a custom font family ("Iosevka") and 16px
    await fireEvent.change(screen.getByTestId('font-family-select-patternInput'), {
      target: { value: '__custom__' }
    });
    await fireEvent.input(screen.getByTestId('font-family-custom-patternInput'), {
      target: { value: 'Iosevka' }
    });
    await fireEvent.click(screen.getByTestId('font-size-inc-patternInput')); // 14 -> 15
    await fireEvent.click(screen.getByTestId('font-size-inc-patternInput')); // 15 -> 16

    // Customize Dictionaries List & Status Bar sizes
    await fireEvent.change(screen.getByTestId('font-size-input-dictionariesList'), {
      target: { value: '14' }
    });
    await fireEvent.change(screen.getByTestId('font-size-input-statusBar'), {
      target: { value: '13' }
    });
    await fireEvent.change(screen.getByTestId('font-size-input-toolbar'), {
      target: { value: '14' }
    });

    // Live preview reflects updated font & size
    const resultsPreview = screen.getByTestId('font-preview-results');
    expect(resultsPreview.getAttribute('style')).toContain('18px');
    expect(resultsPreview.getAttribute('style')).toContain('JetBrains Mono');

    // Apply Fonts
    await fireEvent.click(screen.getByTestId('save-font-settings-btn'));

    await waitFor(() => {
      expect(screen.queryByTestId('font-settings-modal')).not.toBeInTheDocument();
    });

    // Verify backend updateConfig received the customized font settings
    expect(h.backend.updateConfig).toHaveBeenCalledWith(
      expect.objectContaining({
        fonts: {
          results: { family: 'JetBrains Mono', size: 18 },
          definitions: { family: 'Georgia', size: 19 },
          patternInput: { family: 'Iosevka', size: 16 },
          dictionariesList: { family: 'sans-serif', size: 14 },
          statusBar: { family: 'sans-serif', size: 13 },
          toolbar: { family: 'sans-serif', size: 14 }
        }
      })
    );

    // Verify root CSS variables on app-root reflect the customized fonts & sizes
    const appRoot = screen.getByTestId('app-root');
    const rootStyle = appRoot.getAttribute('style') ?? '';
    expect(rootStyle).toContain('--tisane-font-results-size: 18px');
    expect(rootStyle).toContain('--tisane-font-definitions-size: 19px');
    expect(rootStyle).toContain('--tisane-font-pattern-size: 16px');
    expect(rootStyle).toContain('--tisane-font-dicts-size: 14px');
    expect(rootStyle).toContain('--tisane-font-status-size: 13px');
    expect(rootStyle).toContain('--tisane-font-toolbar-size: 14px');
    expect(rootStyle).toContain('Iosevka');
  });

  it('opens the About dialog with the root VERSION when clicking the app icon in the upper left corner', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);

    const expectedVersion = readFileSync(resolve(process.cwd(), '../../../VERSION'), 'utf8').trim();

    render(App);
    await waitFor(() => expect(screen.getByTestId('tiled-pane-0')).toBeInTheDocument());

    expect(APP_VERSION).toBe(expectedVersion);
    expect(screen.queryByTestId('about-modal')).not.toBeInTheDocument();

    // Click the app icon in the upper-left corner
    const logo = screen.getByTestId('app-logo');
    await fireEvent.click(logo);

    const modal = screen.getByTestId('about-modal');
    expect(modal).toBeInTheDocument();

    const aboutIcon = screen.getByTestId('about-icon') as HTMLImageElement;
    expect(aboutIcon).toBeInTheDocument();
    expect(aboutIcon.getAttribute('src')).toBe('/icon.png');

    expect(modal).toHaveTextContent('Tisane');
    expect(modal).toHaveTextContent(`Version ${expectedVersion}`);
    expect(modal).toHaveTextContent('by Brian Beck');
    expect(modal).toHaveTextContent('Copyright 2026');
    expect(modal).toHaveTextContent('github.com/beckbria/tisane');
    expect(modal).not.toHaveTextContent('https://github.com/beckbria/tisane');

    // Clicking the GitHub repository link opens it via openExternalURL
    const repoLink = screen.getByTestId('about-github-link');
    await fireEvent.click(repoLink);
    expect(h.externalURLs).toEqual(['https://github.com/beckbria/tisane']);

    // Close the About dialog
    await fireEvent.click(screen.getByTestId('close-about-btn'));
    expect(screen.queryByTestId('about-modal')).not.toBeInTheDocument();
  });

  it('opens the Dictionary Licenses dialog from the About dialog with CREDITS.md contents and clickable links', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);

    render(App);
    await waitFor(() => expect(screen.getByTestId('tiled-pane-0')).toBeInTheDocument());

    await fireEvent.click(screen.getByTestId('app-logo'));
    expect(screen.getByTestId('about-modal')).toBeInTheDocument();
    expect(screen.queryByTestId('dictionary-licenses-modal')).not.toBeInTheDocument();

    // Open Dictionary Licenses from About dialog
    await fireEvent.click(screen.getByTestId('about-licenses-btn'));

    const licensesModal = screen.getByTestId('dictionary-licenses-modal');
    expect(licensesModal).toBeInTheDocument();

    const content = screen.getByTestId('dictionary-licenses-content');
    expect(content).toHaveTextContent('Dictionary credits and licenses');
    expect(content).toHaveTextContent('Core English');
    expect(content).toHaveTextContent('English Wordnet 2025 Plus');
    expect(content).toHaveTextContent('Unedited English');
    expect(content).toHaveTextContent('Wikipedia English 2026');

    // Clicking a license link routes through openExternalURL
    const ccByLink = screen.getByRole('link', {
      name: 'Creative Commons Attribution (CC-BY) 4.0 License'
    });
    await fireEvent.click(ccByLink);
    expect(h.externalURLs).toEqual(['https://creativecommons.org/licenses/by/4.0/']);

    // Pressing Escape closes only the stacked Dictionary Licenses dialog, keeping About open
    await fireEvent.keyDown(window, { key: 'Escape' });
    expect(screen.queryByTestId('dictionary-licenses-modal')).not.toBeInTheDocument();
    expect(screen.getByTestId('about-modal')).toBeInTheDocument();

    // Reopen and close via the footer Close button
    await fireEvent.click(screen.getByTestId('about-licenses-btn'));
    expect(screen.getByTestId('dictionary-licenses-modal')).toBeInTheDocument();
    await fireEvent.click(screen.getByTestId('close-licenses-footer-btn'));
    expect(screen.queryByTestId('dictionary-licenses-modal')).not.toBeInTheDocument();
    expect(screen.getByTestId('about-modal')).toBeInTheDocument();
  });

  it('opens a dropdown from the + Add button with Add dictionary and Add all from directory options', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);

    render(App);
    await waitFor(() => expect(screen.getByTestId('tiled-pane-0')).toBeInTheDocument());

    const addBtn = screen.getByTestId('add-dict-btn');
    expect(screen.queryByTestId('add-dict-menu')).not.toBeInTheDocument();

    // Click "+ Add" to open the dropdown
    await fireEvent.click(addBtn);
    const menu = screen.getByTestId('add-dict-menu');
    expect(menu).toBeInTheDocument();

    const addFileOption = screen.getByTestId('add-dict-file-option');
    const addDirOption = screen.getByTestId('add-dict-dir-option');
    expect(addFileOption).toHaveTextContent('Add dictionary');
    expect(addDirOption).toHaveTextContent('Add all from directory');

    // Click "Add dictionary" -> closes dropdown and calls browseAndAddDictionaries
    await fireEvent.click(addFileOption);
    expect(screen.queryByTestId('add-dict-menu')).not.toBeInTheDocument();
    expect(h.backend.browseAndAddDictionaries).toHaveBeenCalledTimes(1);

    // Reopen dropdown and click "Add all from directory" -> closes dropdown and calls browseAndAddDirectory
    await fireEvent.click(addBtn);
    expect(screen.getByTestId('add-dict-menu')).toBeInTheDocument();
    await fireEvent.click(screen.getByTestId('add-dict-dir-option'));
    expect(screen.queryByTestId('add-dict-menu')).not.toBeInTheDocument();
    expect(h.backend.browseAndAddDirectory).toHaveBeenCalledTimes(1);
  });

  it('updates the results grid when selecting or unselecting dictionaries before any query is performed', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);

    render(App);

    // Initially both dictionaries are enabled and shown in the tiled grid
    await waitFor(() => {
      expect(screen.getByTestId('tiled-pane-0')).toHaveTextContent('Core English');
      expect(screen.getByTestId('tiled-pane-1')).toHaveTextContent("The Devil's Dictionary");
    });

    // Unselect the first dictionary ("Core English") while no query has been run
    const toggle0 = screen.getByTestId('dict-toggle-0') as HTMLInputElement;
    await fireEvent.click(toggle0);

    await waitFor(() => {
      expect(screen.getByTestId('tiled-pane-0')).toHaveTextContent("The Devil's Dictionary");
      expect(screen.queryByTestId('tiled-pane-1')).not.toBeInTheDocument();
    });

    // Unselect the second dictionary ("The Devil's Dictionary") -> shows empty state prompt
    const toggle1 = screen.getByTestId('dict-toggle-1') as HTMLInputElement;
    await fireEvent.click(toggle1);

    await waitFor(() => {
      expect(screen.queryByTestId('tiled-pane-0')).not.toBeInTheDocument();
      expect(screen.getByTestId('results-workspace')).toHaveTextContent(
        'Enable at least one dictionary in the left sidebar to search.'
      );
    });

    // Re-select "Core English" -> grid immediately shows Core English pane again
    await fireEvent.click(toggle0);

    await waitFor(() => {
      expect(screen.getByTestId('tiled-pane-0')).toHaveTextContent('Core English');
      expect(screen.queryByTestId('tiled-pane-1')).not.toBeInTheDocument();
    });

    expect(h.searchRequests.length).toBe(0);
  });

  it('prompts for confirmation when resetting all settings to defaults and supports keeping or resetting dictionaries', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);

    render(App);
    await waitFor(() => expect(screen.getByTestId('tiled-pane-0')).toBeInTheDocument());

    // Switch layout to unified and unselect the first dictionary
    await fireEvent.click(screen.getByTestId('layout-unified-btn'));
    await waitFor(() => expect(screen.getByTestId('unified-pane')).toBeInTheDocument());

    const toggle0 = screen.getByTestId('dict-toggle-0') as HTMLInputElement;
    await fireEvent.click(toggle0);
    await waitFor(() => expect(toggle0.checked).toBe(false));

    // Open Settings and change @ macro to Include Y
    await fireEvent.click(screen.getByTestId('open-settings-btn'));
    expect(screen.getByTestId('settings-modal')).toBeInTheDocument();
    await fireEvent.click(screen.getByText('Include Y'));
    await fireEvent.click(screen.getByTestId('save-settings-btn'));

    await waitFor(() => {
      expect(screen.getByTestId('status-bar')).toHaveTextContent('@=[aeiouy]');
      expect(screen.getByTestId('status-bar')).toHaveTextContent('unified');
    });

    // Reopen Settings and click "Reset All Settings to Defaults"
    await fireEvent.click(screen.getByTestId('open-settings-btn'));
    await fireEvent.click(screen.getByTestId('reset-all-settings-btn'));

    // Confirmation prompt appears; checkbox for resetting dictionaries defaults to unchecked
    const confirmModal = screen.getByTestId('reset-settings-confirm-modal');
    expect(confirmModal).toBeInTheDocument();
    const resetDictsCheckbox = screen.getByTestId('reset-dicts-checkbox') as HTMLInputElement;
    expect(resetDictsCheckbox.checked).toBe(false);

    // Cancel confirmation -> keeps modal open without resetting
    await fireEvent.click(screen.getByTestId('cancel-reset-settings-btn'));
    expect(screen.queryByTestId('reset-settings-confirm-modal')).not.toBeInTheDocument();
    expect(h.backend.resetConfig).not.toHaveBeenCalled();

    // Click "Reset All Settings to Defaults" again and confirm WITHOUT resetting dictionaries
    await fireEvent.click(screen.getByTestId('reset-all-settings-btn'));
    await fireEvent.click(screen.getByTestId('confirm-reset-settings-btn'));

    await waitFor(() => {
      expect(h.backend.resetConfig).toHaveBeenCalledWith(false);
      expect(screen.queryByTestId('settings-modal')).not.toBeInTheDocument();
      expect(screen.getByTestId('status-bar')).toHaveTextContent('@=[aeiou]');
      expect(screen.getByTestId('status-bar')).toHaveTextContent('tiled');
    });

    // Dictionary selection was preserved (Core English remains unselected)
    expect((screen.getByTestId('dict-toggle-0') as HTMLInputElement).checked).toBe(false);
    expect(screen.getByTestId('tiled-pane-0')).toHaveTextContent("The Devil's Dictionary");
    expect(screen.queryByTestId('tiled-pane-1')).not.toBeInTheDocument();

    // Now reopen Settings, click "Reset All Settings to Defaults", check "Also reset loaded and selected dictionaries", and confirm
    await fireEvent.click(screen.getByTestId('open-settings-btn'));
    await fireEvent.click(screen.getByTestId('reset-all-settings-btn'));
    await fireEvent.click(screen.getByTestId('reset-dicts-checkbox'));
    expect((screen.getByTestId('reset-dicts-checkbox') as HTMLInputElement).checked).toBe(true);
    await fireEvent.click(screen.getByTestId('confirm-reset-settings-btn'));

    await waitFor(() => {
      expect(h.backend.resetConfig).toHaveBeenCalledWith(true);
      expect((screen.getByTestId('dict-toggle-0') as HTMLInputElement).checked).toBe(true);
      expect((screen.getByTestId('dict-toggle-1') as HTMLInputElement).checked).toBe(true);
      expect(screen.getByTestId('tiled-pane-0')).toHaveTextContent('Core English');
      expect(screen.getByTestId('tiled-pane-1')).toHaveTextContent("The Devil's Dictionary");
    });
  });

  it('resizes the dictionary sidebar via drag, keyboard, and double-click reset', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);

    render(App);
    await waitFor(() => expect(h.backend.listDictionaries).toHaveBeenCalled());

    const sidebar = screen.getByTestId('dictionary-sidebar');
    const resizer = screen.getByTestId('sidebar-resizer');
    expect(sidebar).toHaveStyle('width: 272px');

    // Drag right by +80px -> 352px
    await fireEvent.mouseDown(resizer, { button: 0, clientX: 272 });
    await fireEvent.mouseMove(window, { clientX: 352 });
    expect(sidebar).toHaveStyle('width: 352px');
    await fireEvent.mouseUp(window);

    await waitFor(() => {
      expect(h.backend.updateConfig).toHaveBeenCalledWith({ sidebarWidth: 352 });
    });

    // Keyboard ArrowLeft shrinks by 16px -> 336px
    await fireEvent.keyDown(resizer, { key: 'ArrowLeft' });
    await waitFor(() => {
      expect(sidebar).toHaveStyle('width: 336px');
      expect(h.backend.updateConfig).toHaveBeenCalledWith({ sidebarWidth: 336 });
    });

    // Double-clicking the resizer resets to default 272px
    await fireEvent.dblClick(resizer);
    await waitFor(() => {
      expect(sidebar).toHaveStyle('width: 272px');
      expect(h.backend.updateConfig).toHaveBeenCalledWith({ sidebarWidth: 272 });
    });
  });

  it('collapses and expands the dictionary sidebar via button, drag-to-collapse, Ctrl+B, and menu event', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);

    render(App);
    await waitFor(() => expect(h.backend.listDictionaries).toHaveBeenCalled());

    // Resize first so we can verify expanded width is remembered across collapse/expand
    const resizer = screen.getByTestId('sidebar-resizer');
    await fireEvent.mouseDown(resizer, { button: 0, clientX: 272 });
    await fireEvent.mouseMove(window, { clientX: 340 });
    await fireEvent.mouseUp(window);
    await waitFor(() => expect(screen.getByTestId('dictionary-sidebar')).toHaveStyle('width: 340px'));

    // Collapse via sidebar header button -> narrow 32px bar remains with expand button, resizer removed
    await fireEvent.click(screen.getByTestId('collapse-sidebar-btn'));
    await waitFor(() => {
      const sidebar = screen.getByTestId('dictionary-sidebar');
      expect(sidebar).toHaveAttribute('data-collapsed', 'true');
      expect(sidebar).toHaveStyle('width: 32px');
      expect(sidebar.className).toContain('transition-[width]');
      expect(screen.getByTestId('expand-sidebar-btn')).toBeInTheDocument();
      expect(screen.queryByTestId('sidebar-resizer')).not.toBeInTheDocument();
      expect(h.backend.updateConfig).toHaveBeenCalledWith({ sidebarCollapsed: true });
    });

    // Clicking the narrow bar's expand button restores the remembered 340px width
    await fireEvent.click(screen.getByTestId('expand-sidebar-btn'));
    await waitFor(() => {
      const sidebar = screen.getByTestId('dictionary-sidebar');
      expect(sidebar).toHaveAttribute('data-collapsed', 'false');
      expect(sidebar).toHaveStyle('width: 340px');
      expect(screen.queryByTestId('expand-sidebar-btn')).not.toBeInTheDocument();
      expect(h.backend.updateConfig).toHaveBeenCalledWith({ sidebarCollapsed: false });
    });

    // Collapse again and verify toolbar toggle button shows active count and expands back to 340px
    await fireEvent.click(screen.getByTestId('collapse-sidebar-btn'));
    await waitFor(() => {
      expect(screen.getByTestId('dictionary-sidebar')).toHaveAttribute('data-collapsed', 'true');
    });

    const toggleBtn = screen.getByTestId('toggle-sidebar-btn');
    expect(toggleBtn).toHaveAttribute('aria-pressed', 'false');
    expect(toggleBtn).toHaveTextContent('Dictionaries (2/2)');

    await fireEvent.click(toggleBtn);
    await waitFor(() => {
      expect(screen.getByTestId('dictionary-sidebar')).toHaveAttribute('data-collapsed', 'false');
      expect(screen.getByTestId('dictionary-sidebar')).toHaveStyle('width: 340px');
      expect(h.backend.updateConfig).toHaveBeenCalledWith({ sidebarCollapsed: false });
    });

    // Dragging the resizer far left past the threshold collapses the sidebar while preserving 340px width
    await fireEvent.mouseDown(screen.getByTestId('sidebar-resizer'), { button: 0, clientX: 340 });
    expect(screen.getByTestId('dictionary-sidebar').className).not.toContain('transition-[width]');
    await fireEvent.mouseMove(window, { clientX: 60 });
    await waitFor(() => {
      expect(screen.getByTestId('dictionary-sidebar')).toHaveAttribute('data-collapsed', 'true');
      expect(screen.getByTestId('dictionary-sidebar')).toHaveStyle('width: 32px');
    });

    // Ctrl+B expands it again at 340px
    await fireEvent.keyDown(window, { key: 'b', ctrlKey: true });
    await waitFor(() => {
      expect(screen.getByTestId('dictionary-sidebar')).toHaveAttribute('data-collapsed', 'false');
      expect(screen.getByTestId('dictionary-sidebar')).toHaveStyle('width: 340px');
    });

    // Native View menu event ui:toggle-sidebar toggles collapse
    h.emit('ui:toggle-sidebar', null);
    await tick();
    await waitFor(() => {
      expect(screen.getByTestId('dictionary-sidebar')).toHaveAttribute('data-collapsed', 'true');
    });
  });

  it('resizes the definition pane via drag, keyboard, and double-click reset', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);

    render(App);
    await waitFor(() => expect(h.backend.listDictionaries).toHaveBeenCalled());

    const defPane = screen.getByTestId('definition-pane');
    const resizer = screen.getByTestId('definition-resizer');
    expect(defPane).toHaveStyle('height: 256px');

    // Drag upward by 80px (clientY 500 -> 420) -> increases height to 336px
    await fireEvent.mouseDown(resizer, { button: 0, clientY: 500 });
    await fireEvent.mouseMove(window, { clientY: 420 });
    expect(defPane).toHaveStyle('height: 336px');
    await fireEvent.mouseUp(window);

    await waitFor(() => {
      expect(h.backend.updateConfig).toHaveBeenCalledWith({ definitionHeight: 336 });
    });

    // Keyboard ArrowDown shrinks by 16px -> 320px; ArrowUp grows by 16px -> 336px
    await fireEvent.keyDown(resizer, { key: 'ArrowDown' });
    await waitFor(() => {
      expect(defPane).toHaveStyle('height: 320px');
      expect(h.backend.updateConfig).toHaveBeenCalledWith({ definitionHeight: 320 });
    });

    await fireEvent.keyDown(resizer, { key: 'ArrowUp' });
    await waitFor(() => {
      expect(defPane).toHaveStyle('height: 336px');
      expect(h.backend.updateConfig).toHaveBeenCalledWith({ definitionHeight: 336 });
    });

    // Double-clicking the resizer resets to default 256px
    await fireEvent.dblClick(resizer);
    await waitFor(() => {
      expect(defPane).toHaveStyle('height: 256px');
      expect(h.backend.updateConfig).toHaveBeenCalledWith({ definitionHeight: 256 });
    });
  });

  it('collapses and expands the definition pane via button, drag-to-collapse, Ctrl+D, menu event, and result double-click', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);

    render(App);
    await waitFor(() => expect(h.backend.listDictionaries).toHaveBeenCalled());

    // Resize first so we can verify expanded height is remembered across collapse/expand
    const resizer = screen.getByTestId('definition-resizer');
    await fireEvent.mouseDown(resizer, { button: 0, clientY: 500 });
    await fireEvent.mouseMove(window, { clientY: 436 });
    await fireEvent.mouseUp(window);
    await waitFor(() => expect(screen.getByTestId('definition-pane')).toHaveStyle('height: 320px'));

    // Collapse via definition header button -> narrow 32px bar remains with expand button, resizer removed
    await fireEvent.click(screen.getByTestId('collapse-definition-btn'));
    await waitFor(() => {
      const defPane = screen.getByTestId('definition-pane');
      expect(defPane).toHaveAttribute('data-collapsed', 'true');
      expect(defPane).toHaveStyle('height: 32px');
      expect(defPane.className).toContain('transition-[height]');
      expect(screen.getByTestId('expand-definition-btn')).toBeInTheDocument();
      expect(screen.queryByTestId('definition-resizer')).not.toBeInTheDocument();
      expect(h.backend.updateConfig).toHaveBeenCalledWith({ definitionCollapsed: true });
    });

    // Clicking the narrow bar's expand button restores the remembered 320px height
    await fireEvent.click(screen.getByTestId('expand-definition-btn'));
    await waitFor(() => {
      const defPane = screen.getByTestId('definition-pane');
      expect(defPane).toHaveAttribute('data-collapsed', 'false');
      expect(defPane).toHaveStyle('height: 320px');
      expect(screen.queryByTestId('expand-definition-btn')).not.toBeInTheDocument();
      expect(h.backend.updateConfig).toHaveBeenCalledWith({ definitionCollapsed: false });
    });

    // Collapse again and verify toolbar toggle button expands back to remembered 320px height
    await fireEvent.click(screen.getByTestId('collapse-definition-btn'));
    await waitFor(() => {
      expect(screen.getByTestId('definition-pane')).toHaveAttribute('data-collapsed', 'true');
    });

    const toggleBtn = screen.getByTestId('toggle-definitions-btn');
    expect(toggleBtn).toHaveAttribute('aria-pressed', 'false');
    expect(toggleBtn).toHaveTextContent('Definitions');

    await fireEvent.click(toggleBtn);
    await waitFor(() => {
      expect(screen.getByTestId('definition-pane')).toHaveAttribute('data-collapsed', 'false');
      expect(screen.getByTestId('definition-pane')).toHaveStyle('height: 320px');
      expect(h.backend.updateConfig).toHaveBeenCalledWith({ definitionCollapsed: false });
    });

    // Dragging the resizer far down past the threshold collapses the pane while preserving 320px height
    await fireEvent.mouseDown(screen.getByTestId('definition-resizer'), { button: 0, clientY: 440 });
    expect(screen.getByTestId('definition-pane').className).not.toContain('transition-[height]');
    await fireEvent.mouseMove(window, { clientY: 720 });
    await waitFor(() => {
      expect(screen.getByTestId('definition-pane')).toHaveAttribute('data-collapsed', 'true');
      expect(screen.getByTestId('definition-pane')).toHaveStyle('height: 32px');
    });

    // Ctrl+D expands it again at 320px
    await fireEvent.keyDown(window, { key: 'd', ctrlKey: true });
    await waitFor(() => {
      expect(screen.getByTestId('definition-pane')).toHaveAttribute('data-collapsed', 'false');
      expect(screen.getByTestId('definition-pane')).toHaveStyle('height: 320px');
    });

    // Native View menu event ui:toggle-definitions toggles collapse
    h.emit('ui:toggle-definitions', null);
    await tick();
    await waitFor(() => {
      expect(screen.getByTestId('definition-pane')).toHaveAttribute('data-collapsed', 'true');
    });

    // Double-clicking a match while collapsed re-opens the definition pane for that word
    const input = screen.getByTestId('pattern-input');
    await fireEvent.input(input, { target: { value: 'cynic' } });
    await fireEvent.keyDown(input, { key: 'Enter' });
    await waitFor(() => expect(h.searchRequests.length).toBe(1));

    h.emit('search:batch', {
      searchId: 1,
      matches: [{ word: 'cynic', length: 5, dictIndex: 1, dict: "The Devil's Dictionary", hasDefinition: true }],
      progress: { dictIndex: 1, dictName: "The Devil's Dictionary", wordsScanned: 1580, totalCandidateWords: 1580 }
    });
    h.emit('search:complete', {
      searchId: 1,
      totalMatches: 1,
      wordsScanned: 1580,
      totalCandidateWords: 1580,
      elapsedMs: 1.0,
      usedIndexedFTS: false,
      activeDicts: await h.backend.listDictionaries()
    });
    await tick();

    // Single-click does not force-expand while collapsed, but shows the word in the collapsed bar
    const matchEl = within(screen.getByTestId('results-workspace')).getByText('cynic');
    await fireEvent.click(matchEl);
    await tick();
    expect(screen.getByTestId('definition-pane')).toHaveAttribute('data-collapsed', 'true');
    expect(screen.getByTestId('expand-definition-btn')).toHaveTextContent('cynic');

    // Double-click expands the definition pane at its remembered 320px height
    await fireEvent.dblClick(matchEl);
    await waitFor(() => {
      expect(screen.getByTestId('definition-pane')).toHaveAttribute('data-collapsed', 'false');
      expect(screen.getByTestId('definition-pane')).toHaveStyle('height: 320px');
      expect(screen.getByTestId('definition-entries')).toHaveTextContent('faulty vision sees things as they are');
    });
  });

  it('opens the Dictionary Builder modal, exposes all tisane-build options, validates inputs, and builds a dictionary', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);

    render(App);
    await waitFor(() => expect(screen.getByTestId('add-dict-btn')).toBeInTheDocument());

    // Open via + Add dropdown -> Build dictionary…
    await fireEvent.click(screen.getByTestId('add-dict-btn'));
    await waitFor(() => expect(screen.getByTestId('build-dict-option')).toBeInTheDocument());
    await fireEvent.click(screen.getByTestId('build-dict-option'));

    await waitFor(() => expect(screen.getByTestId('build-dict-modal')).toBeInTheDocument());

    // Submit is disabled until input & output paths are set
    expect(screen.getByTestId('builder-submit-btn')).toBeDisabled();

    // Browse for word list file -> auto-derives output .tsd path
    await fireEvent.click(screen.getByTestId('builder-browse-input-btn'));
    await waitFor(() => {
      expect(screen.getByTestId('builder-input-path')).toHaveValue('/tmp/wordlist.txt');
      expect(screen.getByTestId('builder-output-path')).toHaveValue('/tmp/wordlist.tsd');
    });

    // Change output path via Save As button
    (h.backend.browseSaveDictionaryFile as ReturnType<typeof vi.fn>).mockResolvedValueOnce('/tmp/my_dict.tsd');
    await fireEvent.click(screen.getByTestId('builder-browse-output-btn'));
    await waitFor(() => {
      expect(screen.getByTestId('builder-output-path')).toHaveValue('/tmp/my_dict.tsd');
    });

    // Set description, word length min/max, encoding, max FTS %, alphabet map, no-sort, and TSV mode
    await fireEvent.input(screen.getByTestId('builder-description'), {
      target: { value: 'My custom crossword list' }
    });
    await fireEvent.input(screen.getByTestId('builder-min-len'), { target: { value: '3' } });
    await fireEvent.input(screen.getByTestId('builder-max-len'), { target: { value: '15' } });
    await fireEvent.change(screen.getByTestId('builder-encoding'), { target: { value: 'utf8' } });
    await fireEvent.input(screen.getByTestId('builder-max-fts-pct'), { target: { value: '20' } });
    await fireEvent.click(screen.getByTestId('builder-browse-map-btn'));
    await waitFor(() => expect(screen.getByTestId('builder-map-path')).toHaveValue('/tmp/ANSI.TSM'));

    await fireEvent.click(screen.getByTestId('builder-sort-alpha')); // uncheck -> preserve input order
    await fireEvent.click(screen.getByTestId('builder-tsv-mode')); // check -> TSV mode

    // Invalid min > max disables submit and shows validation message
    await fireEvent.input(screen.getByTestId('builder-min-len'), { target: { value: '20' } });
    await waitFor(() => {
      expect(screen.getByTestId('builder-validation-error')).toBeInTheDocument();
      expect(screen.getByTestId('builder-submit-btn')).toBeDisabled();
    });
    await fireEvent.input(screen.getByTestId('builder-min-len'), { target: { value: '3' } });
    await waitFor(() => expect(screen.getByTestId('builder-submit-btn')).not.toBeDisabled());

    // Submit build
    await fireEvent.click(screen.getByTestId('builder-submit-btn'));

    await waitFor(() => expect(screen.getByTestId('builder-success')).toBeInTheDocument());
    expect(h.backend.buildDictionary).toHaveBeenCalledWith({
      inputPath: '/tmp/wordlist.txt',
      outputPath: '/tmp/my_dict.tsd',
      mapPath: '/tmp/ANSI.TSM',
      description: 'My custom crossword list',
      minLen: 3,
      maxLen: 15,
      maxFTSPercent: 20,
      sortAlphabetically: false,
      tabSeparatedAnnotations: true,
      encoding: 'utf8',
      addToLibrary: true
    });
    expect(screen.getByTestId('builder-success')).toHaveTextContent('Dictionary built successfully');
    expect(screen.getByTestId('builder-success')).toHaveTextContent('Added to Tisane');

    // Close via Escape, then reopen via native File menu event ui:open-dict-builder
    await fireEvent.keyDown(window, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByTestId('build-dict-modal')).not.toBeInTheDocument());

    h.emit('ui:open-dict-builder', null);
    await tick();
    await waitFor(() => expect(screen.getByTestId('build-dict-modal')).toBeInTheDocument());
  });

  it('opens the Merge Word Lists modal, supports all conversions and operations, suggests .txt output, and merges lists', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);

    render(App);
    await waitFor(() => expect(screen.getByTestId('add-dict-btn')).toBeInTheDocument());

    // Open via + Add dropdown -> Merge word lists…
    await fireEvent.click(screen.getByTestId('add-dict-btn'));
    await waitFor(() => expect(screen.getByTestId('merge-wordlists-option')).toBeInTheDocument());
    await fireEvent.click(screen.getByTestId('merge-wordlists-option'));

    await waitFor(() => expect(screen.getByTestId('merge-wordlists-modal')).toBeInTheDocument());

    // Defaults: conversion = none, operation = or, submit disabled until paths are set
    expect(screen.getByTestId('merge-conversion')).toHaveValue('none');
    expect(screen.getByTestId('merge-op-or')).toBeChecked();
    expect(screen.getByTestId('merge-submit-btn')).toBeDisabled();

    // Browse for primary list -> auto-derives .txt output path
    (h.backend.browseWordListFile as ReturnType<typeof vi.fn>).mockResolvedValueOnce('/tmp/primary.txt');
    await fireEvent.click(screen.getByTestId('merge-browse-primary-btn'));
    await waitFor(() => {
      expect(screen.getByTestId('merge-primary-path')).toHaveValue('/tmp/primary.txt');
      expect(screen.getByTestId('merge-output-path')).toHaveValue('/tmp/primary-merged.txt');
    });

    // Browse for secondary list
    (h.backend.browseWordListFile as ReturnType<typeof vi.fn>).mockResolvedValueOnce('/tmp/secondary.txt');
    await fireEvent.click(screen.getByTestId('merge-browse-secondary-btn'));
    await waitFor(() => {
      expect(screen.getByTestId('merge-secondary-path')).toHaveValue('/tmp/secondary.txt');
    });

    // Browse for output path via Save As
    (h.backend.browseSaveWordListFile as ReturnType<typeof vi.fn>).mockResolvedValueOnce('/tmp/result.txt');
    await fireEvent.click(screen.getByTestId('merge-browse-output-btn'));
    await waitFor(() => {
      expect(screen.getByTestId('merge-output-path')).toHaveValue('/tmp/result.txt');
    });

    // Select conversion and operation
    await fireEvent.change(screen.getByTestId('merge-conversion'), { target: { value: 'ascii-lower' } });
    await fireEvent.click(screen.getByTestId('merge-op-exclude'));
    expect(screen.getByTestId('merge-op-exclude')).toBeChecked();

    // Submit merge
    expect(screen.getByTestId('merge-submit-btn')).not.toBeDisabled();
    await fireEvent.click(screen.getByTestId('merge-submit-btn'));

    await waitFor(() => expect(screen.getByTestId('merge-success')).toBeInTheDocument());
    expect(h.backend.mergeWordLists).toHaveBeenCalledWith({
      primaryPath: '/tmp/primary.txt',
      secondaryPath: '/tmp/secondary.txt',
      outputPath: '/tmp/result.txt',
      operation: 'exclude',
      conversion: 'ascii-lower',
      encoding: 'auto'
    });
    expect(screen.getByTestId('merge-success')).toHaveTextContent('Word lists merged successfully');
    expect(screen.getByTestId('merge-success')).toHaveTextContent('Output entries: 20');

    // Close via Escape, then reopen via native File menu event ui:open-wordlist-merge
    await fireEvent.keyDown(window, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByTestId('merge-wordlists-modal')).not.toBeInTheDocument());

    h.emit('ui:open-wordlist-merge', null);
    await tick();
    await waitFor(() => expect(screen.getByTestId('merge-wordlists-modal')).toBeInTheDocument());
  });

  it('sanitizes custom font CSS values and rejects non-http(s) web provider URLs in Settings', async () => {
    const cssVars = buildFontCssVars({
      ...normalizeFontConfig(DEFAULT_FONTS),
      results: { family: 'Arial, sans-serif; background: url(https://evil.example/x)', size: 14 },
      definitions: { family: 'Evil"; color: red; --x: "', size: 15 }
    });
    expect(cssVars).not.toContain('background:');
    expect(cssVars).not.toContain('url(');
    expect(cssVars).not.toContain('color:');

    const h = createMockHarness();
    setBackendForTesting(h.backend);
    render(App);
    await waitFor(() => expect(h.backend.getConfig).toHaveBeenCalled());

    h.emit('ui:open-settings', null);
    await tick();
    await waitFor(() => expect(screen.getByTestId('settings-modal')).toBeInTheDocument());

    const nameInput = screen.getByPlaceholderText('Name (e.g. Chambers)');
    const tmplInput = screen.getByPlaceholderText('https://example.com/search?q={entry}');
    await fireEvent.input(nameInput, { target: { value: 'Evil' } });
    await fireEvent.input(tmplInput, { target: { value: 'file:///etc/passwd?{entry}' } });
    await fireEvent.click(screen.getByRole('button', { name: '+ Add' }));

    await waitFor(() => expect(screen.getByTestId('settings-provider-error')).toBeInTheDocument());
    expect(screen.getByTestId('settings-provider-error')).toHaveTextContent('http:// or https://');
  });

  it('correlates fast search events emitted before startSearch resolves, refreshes active search on dicts:updated, and steps past duplicate words with ArrowDown', async () => {
    const h = createMockHarness();
    // Emit batch + complete synchronously BEFORE the startSearch Promise resolves.
    (h.backend.startSearch as ReturnType<typeof vi.fn>).mockImplementation((req: SearchRequest) => {
      const sid = req.searchId ?? 1;
      h.emit('search:batch', {
        searchId: sid,
        matches: [
          { word: 'echo', length: 4, dictIndex: 0, dict: 'Core English', hasDefinition: false },
          { word: 'echo', length: 4, dictIndex: 1, dict: "The Devil's Dictionary", hasDefinition: true },
          { word: 'fable', length: 5, dictIndex: 0, dict: 'Core English', hasDefinition: false }
        ],
        progress: { dictIndex: 0, dictName: 'Core English', wordsScanned: 100, totalCandidateWords: 100 }
      });
      h.emit('search:complete', {
        searchId: sid,
        totalMatches: 3,
        wordsScanned: 100,
        totalCandidateWords: 100,
        elapsedMs: 1.0,
        usedIndexedFTS: false,
        activeDicts: []
      });
      return new Promise<number>((resolve) => setTimeout(() => resolve(sid), 10));
    });

    setBackendForTesting(h.backend);
    render(App);
    await waitFor(() => expect(h.backend.getConfig).toHaveBeenCalled());

    // Switch to Unified view so duplicate words appear together.
    await fireEvent.click(screen.getByTestId('layout-unified-btn'));

    // First search
    const input = screen.getByTestId('pattern-input');
    await fireEvent.input(input, { target: { value: 'e*' } });
    await fireEvent.keyDown(input, { key: 'Enter' });
    await waitFor(() => expect(screen.getByTestId('unified-pane-count')).toHaveTextContent('3'));

    // Second search: fast events arrive while startSearch Promise is still pending
    await fireEvent.input(input, { target: { value: 'f*' } });
    await fireEvent.keyDown(input, { key: 'Enter' });
    await waitFor(() => expect(screen.getByTestId('status-bar')).toHaveTextContent('3 matches'));
    expect(screen.getByTestId('status-bar')).not.toHaveTextContent('Scanning');

    // Keyboard navigation steps past duplicate "echo" to "fable"
    const listbox = screen.getByRole('listbox', { name: 'Unified Results matches' });
    await fireEvent.keyDown(listbox, { key: 'ArrowDown' });
    await fireEvent.keyDown(listbox, { key: 'ArrowDown' });
    await waitFor(() => expect(screen.getByTestId('definition-word-heading')).toHaveTextContent('fable'));

    // External dicts:updated (e.g. from native File -> Add Dictionary menu) re-runs the active search
    const callsBefore = (h.backend.startSearch as ReturnType<typeof vi.fn>).mock.calls.length;
    h.emit('dicts:updated', [
      {
        id: '/dicts/core.tsd',
        path: '/dicts/core.tsd',
        name: 'Core English',
        description: 'Core word list',
        version: '1.2.0',
        wordCount: 241350,
        maxWordLength: 28,
        hasDefinitions: false,
        hasFTS: false,
        enabled: true
      }
    ]);
    await waitFor(() =>
      expect((h.backend.startSearch as ReturnType<typeof vi.fn>).mock.calls.length).toBe(callsBefore + 1)
    );
  });

  it('surfaces backend errors from dictionary actions in the status bar and allows dismissing them', async () => {
    const h = createMockHarness();
    (h.backend.setEnabled as ReturnType<typeof vi.fn>).mockRejectedValueOnce(new Error('disk full'));
    setBackendForTesting(h.backend);
    render(App);
    await waitFor(() => expect(h.backend.listDictionaries).toHaveBeenCalled());

    const checkbox = screen.getByTestId('dict-toggle-0');
    await fireEvent.click(checkbox);
    await waitFor(() => expect(screen.getByTestId('status-bar')).toHaveTextContent('Error: disk full'));

    await fireEvent.click(screen.getByTestId('dismiss-error-btn'));
    await waitFor(() => expect(screen.getByTestId('status-bar')).not.toHaveTextContent('Error: disk full'));
  });

  it('prevents desktop menu shortcut double-firing and closes modals on Escape without cancelling search', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);
    render(App);
    await waitFor(() => expect(h.backend.getConfig).toHaveBeenCalled());

    // Start a search that stays active, then open Settings and press Escape.
    const input = screen.getByTestId('pattern-input');
    await fireEvent.input(input, { target: { value: 'c*' } });
    await fireEvent.keyDown(input, { key: 'Enter' });
    await waitFor(() => expect(h.backend.startSearch).toHaveBeenCalled());

    h.emit('ui:open-settings', null);
    await tick();
    await waitFor(() => expect(screen.getByTestId('settings-modal')).toBeInTheDocument());

    await fireEvent.keyDown(window, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByTestId('settings-modal')).not.toBeInTheDocument());
    expect(h.backend.cancelSearch).not.toHaveBeenCalled();

    // Pressing Escape again with no modal open cancels the active search.
    await fireEvent.keyDown(window, { key: 'Escape' });
    await waitFor(() => expect(h.backend.cancelSearch).toHaveBeenCalledTimes(1));

    // Backend-emitted search:cancelled (e.g. from native Search -> Cancel Active Search menu) clears isSearching.
    await fireEvent.keyDown(input, { key: 'Enter' });
    await waitFor(() => expect(screen.getByTestId('status-bar')).toHaveTextContent('Scanning'));
    h.emit('search:cancelled', { searchId: 2 });
    await waitFor(() => expect(screen.getByTestId('status-bar')).not.toHaveTextContent('Scanning'));

    // In desktop WebView mode, Ctrl+B and Ctrl+D in the webview are ignored so native menu accelerators do not double-fire.
    const w = window as unknown as { _wails?: { environment?: { OS?: string } } };
    const prevEnv = w._wails?.environment;
    w._wails = { ...(w._wails ?? {}), environment: { OS: 'linux' } };
    try {
      await fireEvent.keyDown(window, { key: 'b', ctrlKey: true });
      await fireEvent.keyDown(window, { key: 'd', ctrlKey: true });
      await tick();
      expect(screen.getByTestId('dictionary-sidebar')).toHaveAttribute('data-collapsed', 'false');
      expect(screen.getByTestId('definition-pane')).toHaveAttribute('data-collapsed', 'false');

      h.emit('ui:toggle-sidebar', null);
      h.emit('ui:toggle-definitions', null);
      await waitFor(() => {
        expect(screen.getByTestId('dictionary-sidebar')).toHaveAttribute('data-collapsed', 'true');
        expect(screen.getByTestId('definition-pane')).toHaveAttribute('data-collapsed', 'true');
      });
    } finally {
      if (w._wails) {
        w._wails.environment = prevEnv;
      }
    }
  });

  it('switches between Dark, Light, and System themes, mirrors theme in boot cache, and styles .tsd-indent', async () => {
    // Seed boot cache to verify synchronous pre-paint initialization before async getConfig resolves
    window.localStorage.setItem('tisane:boot', JSON.stringify({ theme: 'light' }));

    const h = createMockHarness();
    let resolveInitialConfig!: (cfg: Config) => void;
    (h.backend.getConfig as ReturnType<typeof vi.fn>).mockImplementationOnce(
      () =>
        new Promise<Config>((resolve) => {
          resolveInitialConfig = resolve;
        })
    );
    setBackendForTesting(h.backend);

    render(App);
    // Boot cache applied light theme immediately before getConfig resolved
    expect(document.documentElement.dataset.theme).toBe('light');

    // Now resolve canonical profile (which has theme='dark') -> overrides boot cache
    const baseCfg = await h.backend.updateConfig({});
    resolveInitialConfig({ ...baseCfg, theme: 'dark' });
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe('dark'));
    expect(JSON.parse(window.localStorage.getItem('tisane:boot') || '{}').theme).toBe('dark');

    // Switch to Light theme via Settings modal
    await fireEvent.click(screen.getByTestId('open-settings-btn'));
    const lightBtn = screen.getByTestId('theme-light-btn');
    await fireEvent.click(lightBtn);

    await waitFor(() => {
      expect(document.documentElement.dataset.theme).toBe('light');
      expect(document.documentElement.style.colorScheme).toBe('light');
      expect(h.backend.updateConfig).toHaveBeenCalledWith({ theme: 'light' });
      expect(JSON.parse(window.localStorage.getItem('tisane:boot') || '{}').theme).toBe('light');
    });

    // Switch via native View -> Theme menu event
    h.emit('ui:set-theme', 'system');
    await waitFor(() => {
      expect(h.backend.updateConfig).toHaveBeenCalledWith({ theme: 'system' });
      expect(JSON.parse(window.localStorage.getItem('tisane:boot') || '{}').theme).toBe('system');
    });

    // Resetting all settings clears boot cache and restores dark theme
    await fireEvent.click(screen.getByTestId('reset-all-settings-btn'));
    await fireEvent.click(screen.getByTestId('confirm-reset-settings-btn'));
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe('dark'));

    // Verify app.css defines RTF .tsd-indent rule and [data-theme="light"] tokens
    const cssSource = readFileSync(resolve(__dirname, 'app.css'), 'utf8');
    expect(cssSource).toContain('.tisane-def .tsd-indent');
    expect(cssSource).toContain('[data-theme="light"]');
    expect(cssSource).toContain('prefers-reduced-motion: reduce');
  });

  it('provides WAI-ARIA single-tab-stop listbox navigation, aria-pressed states, live regions, and stacked modal focus trapping', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);

    render(App);
    await waitFor(() => expect(screen.getByTestId('tiled-pane-0')).toBeInTheDocument());

    // Mode, category, dedupe, and layout buttons expose aria-pressed
    expect(screen.getByTestId('mode-word-btn')).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByTestId('mode-fts-btn')).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByTestId('cat-lowercase')).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByTestId('cat-lowercase')).toHaveAttribute('aria-label', 'Include lowercase entries');
    expect(screen.getByTestId('toggle-dedupe-btn')).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByTestId('layout-tiled-btn')).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByTestId('layout-unified-btn')).toHaveAttribute('aria-pressed', 'false');

    // Invalid syntax sets aria-invalid and role="alert" on validation banner
    (h.backend.validatePattern as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      valid: false,
      code: 2,
      message: 'Unclosed character class',
      span: { start: 0, end: 2 },
      minMatchedLength: 0,
      maxMatchedLength: 0,
      hasWarning: false
    });
    const patternInput = screen.getByTestId('pattern-input');
    await fireEvent.input(patternInput, { target: { value: '[a' } });
    await waitFor(() => {
      expect(patternInput).toHaveAttribute('aria-invalid', 'true');
      expect(screen.getByTestId('validation-banner')).toHaveAttribute('role', 'alert');
    });

    // Stream matches into Unified view and verify single-tab-stop listbox + Home/End + setsize
    await fireEvent.click(screen.getByTestId('layout-unified-btn'));
    await fireEvent.input(patternInput, { target: { value: 'c*' } });
    await fireEvent.keyDown(patternInput, { key: 'Enter' });
    await waitFor(() => expect(h.backend.startSearch).toHaveBeenCalled());

    const sid = 1;
    h.emit('search:batch', {
      searchId: sid,
      matches: [
        { word: 'alpha', length: 5, dictIndex: 0, dict: 'Core English', hasDefinition: false },
        { word: 'bravo', annotation: 'n.', balance: '-Z', length: 5, dictIndex: 0, dict: 'Core English', hasDefinition: false },
        { word: 'cynic', length: 5, dictIndex: 1, dict: "The Devil's Dictionary", hasDefinition: true }
      ],
      progress: { dictIndex: 1, dictName: "The Devil's Dictionary", wordsScanned: 100, totalCandidateWords: 100 }
    });
    h.emit('search:complete', {
      searchId: sid,
      totalMatches: 3,
      wordsScanned: 100,
      totalCandidateWords: 100,
      elapsedMs: 1.5,
      usedIndexedFTS: false,
      activeDicts: []
    });

    await waitFor(() => {
      expect(screen.getByTestId('sr-search-status')).toHaveTextContent('Search complete: 3 matches');
    });

    const rows = screen.getAllByTestId('result-row');
    expect(rows[0]).toHaveAttribute('tabindex', '-1');
    expect(rows[0]).toHaveAttribute('aria-posinset', '1');
    expect(rows[0]).toHaveAttribute('aria-setsize', '3');
    expect(rows[1]).toHaveAttribute('aria-label', 'bravo, n., omits Z, Core English, 5 letters');

    // Skip-to-results link focuses the listbox, and End/Home navigate to last/first match
    await fireEvent.click(screen.getByTestId('skip-to-results-link'));
    const listbox = screen.getByRole('listbox', { name: 'Unified Results matches' });
    expect(document.activeElement).toBe(listbox);

    await fireEvent.keyDown(listbox, { key: 'End' });
    await waitFor(() => {
      expect(listbox).toHaveAttribute('aria-activedescendant', 'unified-pane-opt-2');
      expect(screen.getByTestId('definition-word-heading')).toHaveTextContent('cynic');
    });

    await fireEvent.keyDown(listbox, { key: 'Home' });
    await waitFor(() => {
      expect(listbox).toHaveAttribute('aria-activedescendant', 'unified-pane-opt-0');
    });

    // Stacked modal focus trap & Escape isolation:
    // Focus Settings trigger button, open Settings -> opens Font Settings -> Escape closes ONLY Font Settings
    const settingsBtn = screen.getByTestId('open-settings-btn');
    settingsBtn.focus();
    await fireEvent.click(settingsBtn);

    const settingsModal = screen.getByTestId('settings-modal');
    expect(settingsModal).toBeInTheDocument();
    await tick();
    expect(settingsModal.contains(document.activeElement)).toBe(true);

    // Edit @ macro inside Settings, then open nested Customize Fonts dialog
    const macroInputs = settingsModal.querySelectorAll<HTMLInputElement>('input[type="text"]');
    await fireEvent.input(macroInputs[0], { target: { value: '[aeiouy]' } });

    const openFontBtn = screen.getByTestId('open-font-settings-btn');
    openFontBtn.focus();
    await fireEvent.click(openFontBtn);
    const fontModal = screen.getByTestId('font-settings-modal');
    expect(fontModal).toBeInTheDocument();
    await tick();
    expect(fontModal.contains(document.activeElement)).toBe(true);

    // Tab wraps within FontSettingsModal (from last focusable back to first)
    const saveFontBtn = screen.getByTestId('save-font-settings-btn');
    saveFontBtn.focus();
    await fireEvent.keyDown(fontModal, { key: 'Tab' });
    expect(document.activeElement).toBe(screen.getByTestId('scale-all-down-btn'));

    // Pressing Escape closes ONLY FontSettingsModal, keeping SettingsModal and its unsaved edit intact
    await fireEvent.keyDown(window, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByTestId('font-settings-modal')).not.toBeInTheDocument());
    expect(screen.getByTestId('settings-modal')).toBeInTheDocument();
    expect(macroInputs[0].value).toBe('[aeiouy]');
    await waitFor(() => expect(document.activeElement).toBe(openFontBtn));

    // Pressing Escape again closes SettingsModal and restores focus to open-settings-btn
    await fireEvent.keyDown(window, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByTestId('settings-modal')).not.toBeInTheDocument());
    await waitFor(() => expect(document.activeElement).toBe(settingsBtn));
  });

  it('supports global UI zoom via shortcuts, menu events, Settings controls, and boot cache', async () => {
    window.localStorage.setItem('tisane:boot', JSON.stringify({ theme: 'dark', uiZoom: 130 }));

    const h = createMockHarness();
    let resolveInitialConfig!: (cfg: Config) => void;
    (h.backend.getConfig as ReturnType<typeof vi.fn>).mockImplementationOnce(
      () =>
        new Promise<Config>((resolve) => {
          resolveInitialConfig = resolve;
        })
    );
    setBackendForTesting(h.backend);

    render(App);
    expect(document.documentElement.style.zoom).toBe('1.3');

    const baseCfg = await h.backend.updateConfig({});
    resolveInitialConfig({ ...baseCfg, uiZoom: 100 });
    await waitFor(() => expect(document.documentElement.style.zoom).toBe(''));

    // Keyboard shortcuts Ctrl+=, Ctrl+-, Ctrl+0
    await fireEvent.keyDown(window, { key: '=', ctrlKey: true });
    await waitFor(() => {
      expect(document.documentElement.style.zoom).toBe('1.1');
      expect(h.backend.updateConfig).toHaveBeenCalledWith({ uiZoom: 110 });
      expect(JSON.parse(window.localStorage.getItem('tisane:boot') || '{}').uiZoom).toBe(110);
    });

    // Native View -> Zoom In menu event
    h.emit('ui:zoom', 'in');
    await waitFor(() => {
      expect(document.documentElement.style.zoom).toBe('1.2');
      expect(h.backend.updateConfig).toHaveBeenCalledWith({ uiZoom: 120 });
    });

    // Settings modal UI Scale controls
    await fireEvent.click(screen.getByTestId('open-settings-btn'));
    expect(screen.getByTestId('zoom-value')).toHaveTextContent('120%');
    await fireEvent.click(screen.getByTestId('zoom-out-btn'));
    await waitFor(() => {
      expect(screen.getByTestId('zoom-value')).toHaveTextContent('110%');
      expect(document.documentElement.style.zoom).toBe('1.1');
    });

    await fireEvent.click(screen.getByTestId('zoom-reset-btn'));
    await waitFor(() => {
      expect(screen.getByTestId('zoom-value')).toHaveTextContent('100%');
      expect(document.documentElement.style.zoom).toBe('');
    });
  });

  it('adapts to compact split-screen and mobile viewports with container queries, drawer sidebar, and clamped definition height', async () => {
    const origInnerWidth = window.innerWidth;
    const origInnerHeight = window.innerHeight;
    try {
      const h = createMockHarness();
      await h.backend.updateConfig({ definitionHeight: 400 });
      setBackendForTesting(h.backend);

      render(App);
      await waitFor(() => expect(screen.getByTestId('results-workspace')).toBeInTheDocument());

      // Results workspace uses CSS container queries for tiled columns
      expect(screen.getByTestId('results-workspace').className).toContain('@container');
      expect(screen.getByTestId('tiled-panes-grid').className).toContain('@md:grid-cols-2');

      // Resize viewport down to compact 640x480 split-screen / narrow browser size
      Object.defineProperty(window, 'innerWidth', { configurable: true, writable: true, value: 640 });
      Object.defineProperty(window, 'innerHeight', { configurable: true, writable: true, value: 480 });
      await fireEvent(window, new Event('resize'));

      // Sidebar switches to an overlay drawer and definition height clamps to 55% of 480px (264px)
      await waitFor(() => {
        expect(screen.getByTestId('sidebar-drawer-container')).toBeInTheDocument();
        expect(screen.getByTestId('sidebar-drawer-backdrop')).toBeInTheDocument();
        expect(screen.getByTestId('definition-pane')).toHaveStyle('height: 264px');
      });

      // Clicking the drawer backdrop dismisses the sidebar drawer back to the narrow collapsed bar
      await fireEvent.click(screen.getByTestId('sidebar-drawer-backdrop'));
      await waitFor(() => {
        expect(screen.queryByTestId('sidebar-drawer-container')).not.toBeInTheDocument();
        expect(screen.getByTestId('dictionary-sidebar')).toHaveAttribute('data-collapsed', 'true');
        expect(screen.getByTestId('expand-sidebar-btn')).toBeInTheDocument();
        expect(h.backend.updateConfig).toHaveBeenCalledWith({ sidebarCollapsed: true });
      });

      // Restoring full desktop viewport restores full 400px configured definition height
      Object.defineProperty(window, 'innerWidth', { configurable: true, writable: true, value: 1320 });
      Object.defineProperty(window, 'innerHeight', { configurable: true, writable: true, value: 840 });
      await fireEvent(window, new Event('resize'));
      await waitFor(() => {
        expect(screen.getByTestId('definition-pane')).toHaveStyle('height: 400px');
      });
    } finally {
      Object.defineProperty(window, 'innerWidth', {
        configurable: true,
        writable: true,
        value: origInnerWidth
      });
      Object.defineProperty(window, 'innerHeight', {
        configurable: true,
        writable: true,
        value: origInnerHeight
      });
    }
  });

  it('copies selected headwords, full pane match lists, and plain-text definitions to clipboard', async () => {
    const writeTextMock = vi.fn(async () => {});
    const origClipboard = navigator.clipboard;
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: writeTextMock }
    });

    try {
      const h = createMockHarness();
      setBackendForTesting(h.backend);

      render(App);
      await waitFor(() => expect(screen.getByTestId('tiled-pane-0')).toBeInTheDocument());

      await fireEvent.click(screen.getByTestId('layout-unified-btn'));
      const patternInput = screen.getByTestId('pattern-input');
      await fireEvent.input(patternInput, { target: { value: 'c*' } });
      await fireEvent.keyDown(patternInput, { key: 'Enter' });
      await waitFor(() => expect(h.backend.startSearch).toHaveBeenCalled());

      const sid = 1;
      h.emit('search:batch', {
        searchId: sid,
        matches: [
          { word: 'cynic', length: 5, dictIndex: 1, dict: "The Devil's Dictionary", hasDefinition: true },
          { word: 'crossword', length: 9, dictIndex: 0, dict: 'Core English', hasDefinition: false }
        ],
        progress: { dictIndex: 1, dictName: "The Devil's Dictionary", wordsScanned: 50, totalCandidateWords: 50 }
      });
      h.emit('search:complete', {
        searchId: sid,
        totalMatches: 2,
        wordsScanned: 50,
        totalCandidateWords: 50,
        elapsedMs: 1.1,
        usedIndexedFTS: false,
        activeDicts: []
      });

      await waitFor(() => {
        expect(screen.getByTestId('unified-pane-copy-btn')).toBeInTheDocument();
        expect(screen.getByTestId('copy-definition-btn')).toBeInTheDocument();
      });

      // 1. Copy all matches in the pane
      await fireEvent.click(screen.getByTestId('unified-pane-copy-btn'));
      await waitFor(() => {
        expect(writeTextMock).toHaveBeenCalledWith('cynic\ncrossword');
        expect(screen.getByTestId('unified-pane-copy-btn')).toHaveTextContent('Copied');
      });

      // 2. Ctrl+C on focused listbox copies the active word ("crossword")
      const listbox = screen.getByRole('listbox', { name: 'Unified Results matches' });
      await fireEvent.keyDown(listbox, { key: 'End' });
      await fireEvent.keyDown(listbox, { key: 'c', ctrlKey: true });
      await waitFor(() => {
        expect(writeTextMock).toHaveBeenCalledWith('crossword');
      });

      // 3. Copy definition button copies headword + plain-text definition
      await fireEvent.keyDown(listbox, { key: 'Home' });
      await waitFor(() => expect(screen.getByTestId('definition-word-heading')).toHaveTextContent('cynic'));
      await fireEvent.click(screen.getByTestId('copy-definition-btn'));
      await waitFor(() => {
        expect(writeTextMock).toHaveBeenLastCalledWith(
          "cynic\n\n[The Devil's Dictionary]\nCYNIC, n. A blackguard whose faulty vision sees things as they are."
        );
        expect(screen.getByTestId('copy-definition-btn')).toHaveTextContent('Copied');
      });
    } finally {
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: origClipboard
      });
    }
  });

  it('enables keyboard shortcuts in server mode, aligns UTF-8 error spans, guards async definition races, refreshes in-place rebuilds, and isolates dropdown Escape', async () => {
    const h = createMockHarness();
    // Simulate Go UTF-8 byte offsets in validatePattern
    (h.backend.validatePattern as ReturnType<typeof vi.fn>).mockImplementation(async (req: ValidateRequest) => {
      const enc = new TextEncoder();
      const idx = req.query.indexOf('[123]');
      if (idx >= 0) {
        const byteStart = enc.encode(req.query.slice(0, idx)).length;
        const byteEnd = byteStart + enc.encode('[123]').length;
        return {
          valid: false,
          code: 2,
          message: 'Invalid choice list',
          span: { start: byteStart, end: byteEnd },
          minMatchedLength: 0,
          maxMatchedLength: 0,
          hasWarning: false
        };
      }
      return {
        valid: true,
        code: 0,
        message: 'Pattern is valid',
        span: { start: 0, end: enc.encode(req.query).length },
        minMatchedLength: 4,
        maxMatchedLength: 4,
        hasWarning: false
      };
    });

    setBackendForTesting(h.backend);

    // 1. Server-mode detection keeps window shortcuts active even when OS is "linux".
    const w = window as unknown as Record<string, unknown>;
    const prevWails = w._wails;
    w._wails = {
      environment: { OS: 'linux', PlatformInfo: { mode: 'server' } },
      flags: { server: true }
    };
    try {
      expect(isDesktopWebview()).toBe(false);
      render(App);
      await waitFor(() => expect(screen.getByTestId('tiled-pane-0')).toBeInTheDocument());

      await fireEvent.keyDown(window, { key: 'f', ctrlKey: true });
      expect(screen.getByTestId('mode-fts-btn')).toHaveAttribute('aria-pressed', 'true');
      await fireEvent.keyDown(window, { key: 'w', ctrlKey: true });
      expect(screen.getByTestId('mode-word-btn')).toHaveAttribute('aria-pressed', 'true');

      // 2. Multi-byte UTF-8 query ("café[123]") highlights "[123]" accurately.
      const input = screen.getByTestId('pattern-input') as HTMLInputElement;
      await fireEvent.input(input, { target: { value: 'café[123]' } });
      await waitFor(() => {
        const banner = screen.getByTestId('validation-banner');
        expect(banner.querySelector('code > span')?.textContent).toBe('[123]');
      });

      // 3. Dropdown Escape closes "+ Add" menu without cancelling an active search.
      await fireEvent.input(input, { target: { value: 'c.t*' } });
      await fireEvent.keyDown(input, { key: 'Enter' });
      await waitFor(() => expect(h.searchRequests.length).toBe(1));

      await fireEvent.click(screen.getByTestId('add-dict-btn'));
      expect(screen.getByTestId('add-dict-menu')).toBeInTheDocument();
      await fireEvent.keyDown(window, { key: 'Escape' });
      await waitFor(() => expect(screen.queryByTestId('add-dict-menu')).not.toBeInTheDocument());
      expect(h.backend.cancelSearch).not.toHaveBeenCalled();

      // Finish the search so we can verify in-place dictionary rebuild refresh.
      const sid = h.searchRequests[0].searchId!;
      h.emit('search:complete', {
        searchId: sid,
        totalMatches: 0,
        wordsScanned: 100,
        totalCandidateWords: 100,
        elapsedMs: 1.0,
        usedIndexedFTS: false,
        activeDicts: []
      });
      await waitFor(() => expect(h.searchRequests.length).toBe(1));

      // 4. Emitting dicts:updated with same id/enabled but changed wordCount triggers refresh.
      h.emit('dicts:updated', [
        {
          id: '/dicts/core.tsd',
          path: '/dicts/core.tsd',
          name: 'Core English',
          description: 'Core word list',
          version: 'TSD1',
          wordCount: 300000,
          maxWordLength: 28,
          hasDefinitions: false,
          hasFTS: false,
          enabled: true
        },
        {
          id: '/dicts/devils.tsd',
          path: '/dicts/devils.tsd',
          name: "The Devil's Dictionary",
          description: 'Ambrose Bierce definitions',
          version: '1.2.0',
          wordCount: 1580,
          maxWordLength: 24,
          hasDefinitions: true,
          hasFTS: true,
          enabled: true
        }
      ]);
      await waitFor(() => {
        expect(screen.getByTestId('tiled-pane-0')).toHaveTextContent('300,000 words');
        expect(h.searchRequests.length).toBe(2);
      });

      // 5. Out-of-order getDefinitions response (slow includeMentions=true) cannot clobber newer result.
      let resolveSlowMentions!: (val: LookupResult) => void;
      (h.backend.getDefinitions as ReturnType<typeof vi.fn>).mockImplementation(
        (word: string, includeMentions: boolean) => {
          if (includeMentions) {
            return new Promise<LookupResult>((resolve) => {
              resolveSlowMentions = resolve;
            });
          }
          return Promise.resolve({
            word,
            entries: [
              {
                dictIndex: 1,
                dict: "The Devil's Dictionary",
                offset: 1,
                isRTF: false,
                headwords: [{ headword: word, qualifier: '' }],
                html: '<p>Direct headword definition only</p>',
                text: 'Direct headword definition only'
              }
            ],
            webLinks: []
          });
        }
      );

      // Emit a match to select "cynic", then toggle Include Mentions ON (slow) -> OFF (fast).
      const sid2 = h.searchRequests[1].searchId!;
      h.emit('search:batch', {
        searchId: sid2,
        matches: [{ word: 'cynic', length: 5, dictIndex: 1, dict: "The Devil's Dictionary", hasDefinition: true }],
        progress: { dictIndex: 1, dictName: "The Devil's Dictionary", wordsScanned: 10, totalCandidateWords: 10, matchesFound: 1 }
      });
      h.emit('search:complete', {
        searchId: sid2,
        totalMatches: 1,
        wordsScanned: 10,
        totalCandidateWords: 10,
        elapsedMs: 0.5,
        usedIndexedFTS: false,
        activeDicts: []
      });
      await waitFor(() => expect(screen.getByTestId('definition-word-heading')).toHaveTextContent('cynic'));

      const mentionsCheckbox = screen.getByTestId('include-mentions-toggle');
      await fireEvent.click(mentionsCheckbox); // includeMentions = true (pending)
      await fireEvent.click(mentionsCheckbox); // includeMentions = false (resolves fast)
      await waitFor(() =>
        expect(screen.getByTestId('definition-pane')).toHaveTextContent('Direct headword definition only')
      );

      // Now resolve the stale slow includeMentions=true call; UI must keep the newer result.
      resolveSlowMentions({
        word: 'cynic',
        entries: [
          {
            dictIndex: 1,
            dict: "The Devil's Dictionary",
            offset: 2,
            isRTF: false,
            isMention: true,
            headwords: [{ headword: 'Other', qualifier: '' }],
            html: '<p>Stale mention entry</p>',
            text: 'Stale mention entry'
          }
        ],
        webLinks: []
      });
      await tick();
      expect(screen.getByTestId('definition-pane')).toHaveTextContent('Direct headword definition only');
      expect(screen.getByTestId('definition-pane')).not.toHaveTextContent('Stale mention entry');
    } finally {
      w._wails = prevWails;
    }
  });

  it('accumulates multi-batch streaming matches in O(N) without array reallocations and coalesces reactive frame flushes', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);

    const activeDicts = await h.backend.listDictionaries();
    const firstWords: string[] = [];
    const store = createSearchStore({
      getConfig: () => DEFAULT_CONFIG,
      getActiveDictionaries: () => activeDicts,
      onRecordRecentQuery: () => {},
      onFirstMatch: (w) => firstWords.push(w)
    });
    store.initActiveDicts(activeDicts);
    const unsub = store.subscribe();

    try {
      store.query = '*';
      await store.execute();
      const sid = store.currentSearchId;

      // Capture initial backing array references before any batches arrive.
      const initialAllRef = store.allMatches;
      const initialBucket0Ref = store.matchesByDict[0];
      const initialBucket1Ref = store.matchesByDict[1];

      // Stream 40 batches of 1,250 matches (50,000 total across 2 dictionaries).
      for (let b = 0; b < 40; b++) {
        const batch: Match[] = new Array(1250);
        for (let i = 0; i < 1250; i++) {
          const idx = b * 1250 + i;
          const dIdx = idx % 2;
          batch[i] = {
            word: `word_${idx}`,
            length: 6,
            dictIndex: dIdx,
            dict: activeDicts[dIdx].name,
            hasDefinition: false
          };
        }
        h.emit('search:batch', {
          searchId: sid,
          matches: batch,
          progress: {
            dictIndex: b % 2,
            dictName: activeDicts[b % 2].name,
            wordsScanned: (b + 1) * 1250,
            totalCandidateWords: 50_000,
            matchesFound: (b + 1) * 1250
          }
        });
        if (b === 0) {
          // First batch flushes synchronously for immediate first-paint responsiveness.
          expect(store.totalMatches).toBe(1250);
          expect(firstWords).toEqual(['word_0']);
        }
      }

      // Backing array references must be strictly identical (zero .concat() copies across 40 batches).
      expect(store.allMatches).toBe(initialAllRef);
      expect(store.matchesByDict[0]).toBe(initialBucket0Ref);
      expect(store.matchesByDict[1]).toBe(initialBucket1Ref);
      expect(store.allMatches.length).toBe(50_000);
      // Intermediate bursts after batch 0 are frame-coalesced until rAF or search:complete.
      expect(store.totalMatches).toBe(1250);

      h.emit('search:complete', {
        searchId: sid,
        totalMatches: 50_000,
        wordsScanned: 50_000,
        totalCandidateWords: 50_000,
        elapsedMs: 9.2,
        usedIndexedFTS: false,
        activeDicts
      });

      // Terminal completion flushes counts synchronously.
      expect(store.totalMatches).toBe(50_000);
      expect(store.dictMatchCounts).toEqual([25_000, 25_000]);
    } finally {
      unsub();
      store.dispose();
    }
  });

  it('supports Ctrl+Click, Shift+Click, Shift+Arrow, and Ctrl+A multi-selection in result panes', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);

    const writeTextMock = vi.fn(async (_text: string) => {});
    const originalClipboard = navigator.clipboard;
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: writeTextMock },
      configurable: true
    });

    try {
      render(App);
      await waitFor(() => expect(screen.getByTestId('tiled-pane-0')).toBeInTheDocument());

      const patternInput = screen.getByTestId('pattern-input') as HTMLInputElement;
      await fireEvent.input(patternInput, { target: { value: '*****' } });
      await fireEvent.keyDown(patternInput, { key: 'Enter' });
      await waitFor(() => expect(h.backend.startSearch).toHaveBeenCalledTimes(1));

      const sid = h.searchRequests[0].searchId ?? 1;
      const pane0Matches: Match[] = ['alpha', 'bravo', 'charlie', 'delta', 'echo'].map((word) => ({
        word,
        length: word.length,
        dictIndex: 0,
        dict: 'Core English',
        hasDefinition: false
      }));
      h.emit('search:batch', {
        searchId: sid,
        matches: pane0Matches,
        progress: { dictIndex: 0, dictName: 'Core English', wordsScanned: 500, totalCandidateWords: 500 }
      });
      h.emit('search:complete', {
        searchId: sid,
        totalMatches: 5,
        wordsScanned: 500,
        totalCandidateWords: 500,
        elapsedMs: 1.5,
        usedIndexedFTS: false,
        activeDicts: await h.backend.listDictionaries()
      });

      const pane0 = screen.getByTestId('tiled-pane-0');
      await waitFor(() => expect(within(pane0).getAllByTestId('result-row')).toHaveLength(5));

      const rows = within(pane0).getAllByTestId('result-row');
      const listbox = within(pane0).getByRole('listbox');
      expect(listbox).toHaveAttribute('aria-multiselectable', 'true');

      // 1. Plain click selects row 1 ("bravo"), Shift+Click row 3 ("delta") selects range 1..3
      await fireEvent.click(rows[1]);
      await fireEvent.click(rows[3], { shiftKey: true });
      await waitFor(() => {
        expect(rows[0]).toHaveAttribute('aria-selected', 'false');
        expect(rows[1]).toHaveAttribute('aria-selected', 'true');
        expect(rows[2]).toHaveAttribute('aria-selected', 'true');
        expect(rows[3]).toHaveAttribute('aria-selected', 'true');
        expect(rows[4]).toHaveAttribute('aria-selected', 'false');
        expect(screen.getByTestId('tiled-pane-0-selected-count')).toHaveTextContent('3 selected');
      });

      // 2. Ctrl+Click toggles row 2 ("charlie") off and row 0 ("alpha") on -> {0, 1, 3}
      await fireEvent.click(rows[2], { ctrlKey: true });
      await fireEvent.click(rows[0], { ctrlKey: true });
      await waitFor(() => {
        expect(rows[0]).toHaveAttribute('aria-selected', 'true');
        expect(rows[1]).toHaveAttribute('aria-selected', 'true');
        expect(rows[2]).toHaveAttribute('aria-selected', 'false');
        expect(rows[3]).toHaveAttribute('aria-selected', 'true');
      });

      // Ctrl+C copies selected words in ascending list order
      await fireEvent.keyDown(listbox, { key: 'c', ctrlKey: true });
      await waitFor(() => {
        expect(writeTextMock).toHaveBeenLastCalledWith('alpha\nbravo\ndelta');
      });

      // 3. Shift+ArrowDown from row 0 extends selection to {0, 1}
      await fireEvent.click(rows[0]);
      await fireEvent.keyDown(listbox, { key: 'ArrowDown', shiftKey: true });
      await waitFor(() => {
        expect(rows[0]).toHaveAttribute('aria-selected', 'true');
        expect(rows[1]).toHaveAttribute('aria-selected', 'true');
        expect(rows[2]).toHaveAttribute('aria-selected', 'false');
      });

      // 4. Ctrl+A selects all 5 results
      await fireEvent.keyDown(listbox, { key: 'a', ctrlKey: true });
      await waitFor(() => {
        expect(screen.getByTestId('tiled-pane-0-selected-count')).toHaveTextContent('5 selected');
        for (const row of rows) {
          expect(row).toHaveAttribute('aria-selected', 'true');
        }
      });
    } finally {
      Object.defineProperty(navigator, 'clipboard', {
        value: originalClipboard,
        configurable: true
      });
    }
  });

  it('provides right-click context menu to copy only current dictionary selections, select all, and trigger internal or external lookup', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);

    const writeTextMock = vi.fn(async (_text: string) => {});
    const originalClipboard = navigator.clipboard;
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: writeTextMock },
      configurable: true
    });

    try {
      render(App);
      await waitFor(() => {
        expect(screen.getByTestId('tiled-pane-0')).toBeInTheDocument();
        expect(screen.getByTestId('tiled-pane-1')).toBeInTheDocument();
      });

      const patternInput = screen.getByTestId('pattern-input') as HTMLInputElement;
      await fireEvent.input(patternInput, { target: { value: 'c*' } });
      await fireEvent.keyDown(patternInput, { key: 'Enter' });
      await waitFor(() => expect(h.backend.startSearch).toHaveBeenCalledTimes(1));

      const sid = h.searchRequests[0].searchId ?? 1;
      h.emit('search:batch', {
        searchId: sid,
        matches: [
          { word: 'candle', length: 6, dictIndex: 0, dict: 'Core English', hasDefinition: false },
          { word: 'castle', length: 6, dictIndex: 0, dict: 'Core English', hasDefinition: false },
          { word: 'crane', length: 5, dictIndex: 0, dict: 'Core English', hasDefinition: false },
          { word: 'cynic', length: 5, dictIndex: 1, dict: "The Devil's Dictionary", hasDefinition: true },
          { word: 'cabbage', length: 7, dictIndex: 1, dict: "The Devil's Dictionary", hasDefinition: true }
        ],
        progress: { dictIndex: 1, dictName: "The Devil's Dictionary", wordsScanned: 200, totalCandidateWords: 200 }
      });
      h.emit('search:complete', {
        searchId: sid,
        totalMatches: 5,
        wordsScanned: 200,
        totalCandidateWords: 200,
        elapsedMs: 2.0,
        usedIndexedFTS: false,
        activeDicts: await h.backend.listDictionaries()
      });

      const pane0 = screen.getByTestId('tiled-pane-0');
      const pane1 = screen.getByTestId('tiled-pane-1');
      await waitFor(() => {
        expect(within(pane0).getAllByTestId('result-row')).toHaveLength(3);
        expect(within(pane1).getAllByTestId('result-row')).toHaveLength(2);
      });

      const pane0Rows = within(pane0).getAllByTestId('result-row');
      const pane1Rows = within(pane1).getAllByTestId('result-row');

      // Select "candle" and "crane" in Pane 0, AND select "cynic" and "cabbage" in Pane 1
      await fireEvent.click(pane0Rows[0]);
      await fireEvent.click(pane0Rows[2], { ctrlKey: true });
      await fireEvent.click(pane1Rows[0]);
      await fireEvent.click(pane1Rows[1], { ctrlKey: true });

      // Right-click a selected row in Pane 0 -> Copy must ONLY copy Pane 0's selections, not Pane 1's
      await fireEvent.contextMenu(pane0Rows[0], { clientX: 120, clientY: 160 });
      await waitFor(() => expect(screen.getByTestId('result-context-menu')).toBeInTheDocument());
      expect(screen.getByTestId('ctx-copy-btn')).toHaveTextContent('Copy (2 selected)');

      await fireEvent.click(screen.getByTestId('ctx-copy-btn'));
      await waitFor(() => {
        expect(writeTextMock).toHaveBeenLastCalledWith('candle\ncrane');
        expect(screen.queryByTestId('result-context-menu')).not.toBeInTheDocument();
      });

      // Right-click an unselected row ("castle") in Pane 0 -> selects only "castle" and copies only "castle"
      await fireEvent.contextMenu(pane0Rows[1], { clientX: 120, clientY: 190 });
      await waitFor(() => expect(screen.getByTestId('result-context-menu')).toBeInTheDocument());
      expect(screen.getByTestId('ctx-copy-btn')).toHaveTextContent('Copy');
      expect(screen.getByTestId('ctx-copy-btn')).not.toHaveTextContent('selected');
      await fireEvent.click(screen.getByTestId('ctx-copy-btn'));
      await waitFor(() => {
        expect(writeTextMock).toHaveBeenLastCalledWith('castle');
      });

      // Right-click in Pane 0 -> Select All selects all 3 rows in Pane 0, then Ctrl-deselecting row 1 leaves {0, 2}
      await fireEvent.contextMenu(pane0Rows[0], { clientX: 120, clientY: 160 });
      await fireEvent.click(screen.getByTestId('ctx-select-all-btn'));
      await waitFor(() => {
        expect(screen.getByTestId('tiled-pane-0-selected-count')).toHaveTextContent('3 selected');
      });
      await fireEvent.click(pane0Rows[1], { ctrlKey: true });
      await fireEvent.contextMenu(pane0Rows[0], { clientX: 120, clientY: 160 });
      await fireEvent.click(screen.getByTestId('ctx-copy-btn'));
      await waitFor(() => {
        expect(writeTextMock).toHaveBeenLastCalledWith('candle\ncrane');
      });

      // Collapse definition pane, then right-click "cynic" in Pane 1 -> Local definitions expands pane & loads "cynic"
      await fireEvent.click(screen.getByTestId('collapse-definition-btn'));
      await waitFor(() => {
        expect(screen.getByTestId('definition-pane')).toHaveAttribute('data-collapsed', 'true');
      });

      await fireEvent.contextMenu(pane1Rows[0], { clientX: 420, clientY: 160 });
      await waitFor(() => expect(screen.getByTestId('ctx-lookup-internal-btn')).toBeInTheDocument());
      await fireEvent.click(screen.getByTestId('ctx-lookup-internal-btn'));
      await waitFor(() => {
        expect(screen.getByTestId('definition-pane')).toHaveAttribute('data-collapsed', 'false');
        expect(screen.getByTestId('definition-word-heading')).toHaveTextContent('cynic');
      });

      // Right-click "cabbage" in Pane 1 -> External lookup choosing Wiktionary
      await fireEvent.contextMenu(pane1Rows[1], { clientX: 420, clientY: 190 });
      await waitFor(() => expect(screen.getByTestId('ctx-lookup-web-wiktionary')).toBeInTheDocument());
      await fireEvent.click(screen.getByTestId('ctx-lookup-web-wiktionary'));
      await waitFor(() => {
        expect(h.webLookups).toContainEqual({ word: 'cabbage', providerId: 'wiktionary' });
        expect(screen.queryByTestId('result-context-menu')).not.toBeInTheDocument();
      });

      // Escape dismisses the context menu cleanly
      await fireEvent.contextMenu(pane1Rows[0], { clientX: 420, clientY: 160 });
      await waitFor(() => expect(screen.getByTestId('result-context-menu')).toBeInTheDocument());
      await fireEvent.keyDown(screen.getByTestId('result-context-menu'), { key: 'Escape' });
      await waitFor(() => {
        expect(screen.queryByTestId('result-context-menu')).not.toBeInTheDocument();
      });
    } finally {
      Object.defineProperty(navigator, 'clipboard', {
        value: originalClipboard,
        configurable: true
      });
    }
  });

  it('suppresses default right-click menu on general UI elements while allowing text inputs and the status bar', async () => {
    const h = createMockHarness();
    setBackendForTesting(h.backend);

    render(App);
    await waitFor(() => expect(screen.getByTestId('status-bar')).toBeInTheDocument());

    const dispatchContextMenu = (target: Element): boolean => {
      const ev = new MouseEvent('contextmenu', { bubbles: true, cancelable: true });
      target.dispatchEvent(ev);
      return ev.defaultPrevented;
    };

    // 1. Text boxes allow native right-click context menu (not prevented)
    const patternInput = screen.getByTestId('pattern-input');
    expect(dispatchContextMenu(patternInput)).toBe(false);

    // 2. Status bar (and its children) allow native WebView context menu (not prevented)
    const statusBar = screen.getByTestId('status-bar');
    expect(dispatchContextMenu(statusBar)).toBe(false);
    const statusChild = statusBar.querySelector('span');
    expect(statusChild).not.toBeNull();
    expect(dispatchContextMenu(statusChild!)).toBe(false);

    // 3. Other UI elements (buttons, checkboxes, panes) suppress default right-click menu
    expect(dispatchContextMenu(screen.getByTestId('layout-unified-btn'))).toBe(true);
    expect(dispatchContextMenu(screen.getByTestId('include-mentions-toggle'))).toBe(true);
    expect(dispatchContextMenu(screen.getByTestId('definition-pane'))).toBe(true);
    expect(dispatchContextMenu(screen.getByTestId('tiled-pane-0'))).toBe(true);
    expect(screen.queryByTestId('result-context-menu')).not.toBeInTheDocument();
  });

  it('supports right-clicking dictionaries for Enable/Disable, Information dialog, Open Directory, Move Top/Bottom/Up/Down, and Remove From List', async () => {
    const h = createMockHarness();
    const threeDicts: DictionaryInfo[] = [
      {
        id: '/dicts/core.tsd',
        path: '/dicts/core.tsd',
        name: 'Core English',
        description: 'Core word list\r\nSecond line of notes',
        version: 'TSD0',
        fileSize: 415696,
        wordCount: 67057,
        minWordLength: 2,
        maxWordLength: 23,
        usedLetters: 26,
        totalLetters: 63,
        ftsWordCount: 0,
        hasDefinitions: false,
        hasFTS: false,
        enabled: true
      },
      {
        id: '/dicts/devils.tsd',
        path: '/dicts/devils.tsd',
        name: "The Devil's Dictionary",
        description: 'Ambrose Bierce definitions',
        version: 'TSD1',
        fileSize: 1207350,
        wordCount: 1580,
        minWordLength: 3,
        maxWordLength: 24,
        usedLetters: 26,
        totalLetters: 63,
        ftsWordCount: 412,
        hasDefinitions: true,
        hasFTS: true,
        enabled: true
      },
      {
        id: '/dicts/wiki.tsd',
        path: '/dicts/wiki.tsd',
        name: 'Wikipedia English',
        description: '',
        version: 'TSD1',
        fileSize: 148308572,
        wordCount: 14538963,
        minWordLength: 1,
        maxWordLength: 100,
        usedLetters: 26,
        totalLetters: 63,
        ftsWordCount: 0,
        hasDefinitions: false,
        hasFTS: false,
        enabled: false
      }
    ];
    let currentDicts = threeDicts.map((d) => ({ ...d }));
    h.backend.listDictionaries = vi.fn(async () => currentDicts.map((d) => ({ ...d })));
    h.backend.setEnabled = vi.fn(async (id: string, enabled: boolean) => {
      currentDicts = currentDicts.map((d) => (d.id === id ? { ...d, enabled } : d));
      return currentDicts.map((d) => ({ ...d }));
    });
    h.backend.reorderDictionaries = vi.fn(async (orderedIds: string[]) => {
      const byId = new Map(currentDicts.map((d) => [d.id, d]));
      currentDicts = orderedIds.map((id) => byId.get(id)!);
      return currentDicts.map((d) => ({ ...d }));
    });
    h.backend.removeDictionary = vi.fn(async (id: string) => {
      currentDicts = currentDicts.filter((d) => d.id !== id);
      return currentDicts.map((d) => ({ ...d }));
    });

    setBackendForTesting(h.backend);
    render(App);

    await waitFor(() => {
      expect(screen.getByTestId('dict-item-0')).toHaveTextContent('Core English');
      expect(screen.getByTestId('dict-item-2')).toHaveTextContent('Wikipedia English');
    });

    // 1. Right-click enabled top item (index 0): shows Disable (not Enable), Move to Top & Move Up disabled
    await fireEvent.contextMenu(screen.getByTestId('dict-item-0'), { clientX: 80, clientY: 120 });
    const menu0 = await screen.findByTestId('dict-context-menu');
    expect(within(menu0).getByTestId('dict-ctx-disable-btn')).toHaveTextContent('Disable');
    expect(within(menu0).queryByTestId('dict-ctx-enable-btn')).not.toBeInTheDocument();
    expect(within(menu0).getByTestId('dict-ctx-move-top-btn')).toBeDisabled();
    expect(within(menu0).getByTestId('dict-ctx-move-up-btn')).toBeDisabled();
    expect(within(menu0).getByTestId('dict-ctx-move-bottom-btn')).not.toBeDisabled();
    expect(within(menu0).getByTestId('dict-ctx-move-down-btn')).not.toBeDisabled();

    // Click Disable -> toggles enabled to false
    await fireEvent.click(within(menu0).getByTestId('dict-ctx-disable-btn'));
    await waitFor(() => {
      expect(h.backend.setEnabled).toHaveBeenCalledWith('/dicts/core.tsd', false);
      expect(screen.queryByTestId('dict-context-menu')).not.toBeInTheDocument();
    });

    // Right-click same item now that it is disabled -> shows Enable (not Disable)
    await fireEvent.contextMenu(screen.getByTestId('dict-item-0'), { clientX: 80, clientY: 120 });
    const menu0Disabled = await screen.findByTestId('dict-context-menu');
    expect(within(menu0Disabled).getByTestId('dict-ctx-enable-btn')).toHaveTextContent('Enable');
    expect(within(menu0Disabled).queryByTestId('dict-ctx-disable-btn')).not.toBeInTheDocument();
    await fireEvent.click(within(menu0Disabled).getByTestId('dict-ctx-enable-btn'));
    await waitFor(() => {
      expect(h.backend.setEnabled).toHaveBeenCalledWith('/dicts/core.tsd', true);
    });

    // 2. Information dialog shows name, description, and metadata, and closes cleanly on Escape
    await fireEvent.contextMenu(screen.getByTestId('dict-item-1'), { clientX: 80, clientY: 160 });
    await fireEvent.click(await screen.findByTestId('dict-ctx-info-btn'));

    const infoModal = await screen.findByTestId('dict-info-modal');
    expect(within(infoModal).getByTestId('dict-info-heading')).toHaveTextContent(
      "The Devil's Dictionary"
    );
    expect(within(infoModal).getByTestId('dict-info-description')).toHaveTextContent(
      'Ambrose Bierce definitions'
    );
    expect(within(infoModal).getByTestId('dict-info-name')).toHaveTextContent(
      "The Devil's Dictionary"
    );
    expect(within(infoModal).getByTestId('dict-info-format')).toHaveTextContent('TSD1');
    expect(within(infoModal).getByTestId('dict-info-status')).toHaveTextContent(
      'Enabled · Priority #2 of 3'
    );
    expect(within(infoModal).getByTestId('dict-info-words')).toHaveTextContent('1,580 words');
    expect(within(infoModal).getByTestId('dict-info-word-length')).toHaveTextContent('3–24 letters');
    expect(within(infoModal).getByTestId('dict-info-file-size')).toHaveTextContent('1.15 MB');
    expect(within(infoModal).getByTestId('dict-info-definitions')).toHaveTextContent('Available');
    expect(within(infoModal).getByTestId('dict-info-fts')).toHaveTextContent(
      'Indexed (412 FTS-only keys)'
    );
    expect(within(infoModal).getByTestId('dict-info-alphabet')).toHaveTextContent(
      '26 active / 63 total classes'
    );
    expect(within(infoModal).getByTestId('dict-info-path')).toHaveTextContent('/dicts/devils.tsd');

    // Open Directory from inside Information dialog
    await fireEvent.click(within(infoModal).getByTestId('dict-info-open-dir-btn'));
    await waitFor(() => {
      expect(h.openedDirectories).toEqual(['/dicts/devils.tsd']);
    });

    // Dismiss Information dialog with Escape and verify focus restores to row 1
    await fireEvent.keyDown(window, { key: 'Escape' });
    await waitFor(() => {
      expect(screen.queryByTestId('dict-info-modal')).not.toBeInTheDocument();
      expect(screen.getByTestId('dict-toggle-1')).toHaveFocus();
    });

    // 3. Open Directory directly from context menu
    await fireEvent.contextMenu(screen.getByTestId('dict-item-0'), { clientX: 80, clientY: 120 });
    await fireEvent.click(await screen.findByTestId('dict-ctx-open-dir-btn'));
    await waitFor(() => {
      expect(h.openedDirectories).toEqual(['/dicts/devils.tsd', '/dicts/core.tsd']);
    });

    // 4. Move to Bottom, Move to Top, Move Up, Move Down
    // Move index 0 (Core English) to Bottom -> [devils, wiki, core]
    await fireEvent.contextMenu(screen.getByTestId('dict-item-0'), { clientX: 80, clientY: 120 });
    await fireEvent.click(await screen.findByTestId('dict-ctx-move-bottom-btn'));
    await waitFor(() => {
      expect(h.backend.reorderDictionaries).toHaveBeenLastCalledWith([
        '/dicts/devils.tsd',
        '/dicts/wiki.tsd',
        '/dicts/core.tsd'
      ]);
      expect(screen.getByTestId('dict-item-2')).toHaveTextContent('Core English');
    });

    // Right-click bottom item (index 2): Move to Bottom & Move Down are disabled
    await fireEvent.contextMenu(screen.getByTestId('dict-item-2'), { clientX: 80, clientY: 200 });
    const menuBottom = await screen.findByTestId('dict-context-menu');
    expect(within(menuBottom).getByTestId('dict-ctx-move-bottom-btn')).toBeDisabled();
    expect(within(menuBottom).getByTestId('dict-ctx-move-down-btn')).toBeDisabled();
    // Click Move to Top -> [core, devils, wiki]
    await fireEvent.click(within(menuBottom).getByTestId('dict-ctx-move-top-btn'));
    await waitFor(() => {
      expect(h.backend.reorderDictionaries).toHaveBeenLastCalledWith([
        '/dicts/core.tsd',
        '/dicts/devils.tsd',
        '/dicts/wiki.tsd'
      ]);
    });

    // Move index 0 Down -> [devils, core, wiki]
    await fireEvent.contextMenu(screen.getByTestId('dict-item-0'), { clientX: 80, clientY: 120 });
    await fireEvent.click(await screen.findByTestId('dict-ctx-move-down-btn'));
    await waitFor(() => {
      expect(h.backend.reorderDictionaries).toHaveBeenLastCalledWith([
        '/dicts/devils.tsd',
        '/dicts/core.tsd',
        '/dicts/wiki.tsd'
      ]);
    });

    // Move index 1 Up -> [core, devils, wiki]
    await fireEvent.contextMenu(screen.getByTestId('dict-item-1'), { clientX: 80, clientY: 160 });
    await fireEvent.click(await screen.findByTestId('dict-ctx-move-up-btn'));
    await waitFor(() => {
      expect(h.backend.reorderDictionaries).toHaveBeenLastCalledWith([
        '/dicts/core.tsd',
        '/dicts/devils.tsd',
        '/dicts/wiki.tsd'
      ]);
    });

    // 5. Keyboard Shift+F10 opens context menu on dictionary row, and Remove From List removes it
    const wikiRow = screen.getByTestId('dict-item-2');
    await fireEvent.keyDown(wikiRow, { key: 'F10', shiftKey: true });
    const menuWiki = await screen.findByTestId('dict-context-menu');
    expect(within(menuWiki).getByTestId('dict-ctx-enable-btn')).toBeInTheDocument();
    await fireEvent.click(within(menuWiki).getByTestId('dict-ctx-remove-btn'));
    await waitFor(() => {
      expect(h.backend.removeDictionary).toHaveBeenCalledWith('/dicts/wiki.tsd');
      expect(screen.queryByTestId('dict-item-2')).not.toBeInTheDocument();
    });
  });
});
