import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import { tick } from 'svelte';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import App from './App.svelte';
import { setBackendForTesting, type TisaneBackend, type WailsEventMap } from './lib/bindings';
import { buildFontCssVars, DEFAULT_FONTS, normalizeFontConfig } from './lib/fonts';
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
    addDictionaryPaths: vi.fn(async () => dicts),
    browseAndAddDictionaries: vi.fn(async () => dicts),
    browseAndAddDirectory: vi.fn(async () => dicts),
    scanDirectory: vi.fn(async () => dicts),
    removeDictionary: vi.fn(async (id: string) => {
      dicts = dicts.filter((d) => d.id !== id);
      return dicts;
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

  return { backend, emit, searchRequests, webLookups, externalURLs };
}

describe('Tisane Svelte 5 Desktop UI', () => {
  beforeEach(() => {
    setBackendForTesting(null);
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

    // Collapse via sidebar header button -> sidebar and resizer are removed from DOM
    await fireEvent.click(screen.getByTestId('collapse-sidebar-btn'));
    await waitFor(() => {
      expect(screen.queryByTestId('dictionary-sidebar')).not.toBeInTheDocument();
      expect(screen.queryByTestId('sidebar-resizer')).not.toBeInTheDocument();
      expect(h.backend.updateConfig).toHaveBeenCalledWith({ sidebarCollapsed: true });
    });

    // Toolbar toggle button shows active count and expands sidebar back to remembered 340px width
    const toggleBtn = screen.getByTestId('toggle-sidebar-btn');
    expect(toggleBtn).toHaveAttribute('aria-pressed', 'false');
    expect(toggleBtn).toHaveTextContent('Dictionaries (2/2)');

    await fireEvent.click(toggleBtn);
    await waitFor(() => {
      expect(screen.getByTestId('dictionary-sidebar')).toBeInTheDocument();
      expect(screen.getByTestId('dictionary-sidebar')).toHaveStyle('width: 340px');
      expect(h.backend.updateConfig).toHaveBeenCalledWith({ sidebarCollapsed: false });
    });

    // Dragging the resizer far left past the threshold collapses the sidebar while preserving 340px width
    await fireEvent.mouseDown(screen.getByTestId('sidebar-resizer'), { button: 0, clientX: 340 });
    await fireEvent.mouseMove(window, { clientX: 60 });
    await waitFor(() => {
      expect(screen.queryByTestId('dictionary-sidebar')).not.toBeInTheDocument();
    });

    // Ctrl+B expands it again at 340px
    await fireEvent.keyDown(window, { key: 'b', ctrlKey: true });
    await waitFor(() => {
      expect(screen.getByTestId('dictionary-sidebar')).toBeInTheDocument();
      expect(screen.getByTestId('dictionary-sidebar')).toHaveStyle('width: 340px');
    });

    // Native View menu event ui:toggle-sidebar toggles collapse
    h.emit('ui:toggle-sidebar', null);
    await tick();
    await waitFor(() => {
      expect(screen.queryByTestId('dictionary-sidebar')).not.toBeInTheDocument();
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

    // In desktop WebView mode, Ctrl+B in the webview is ignored so native menu ui:toggle-sidebar does not double-fire.
    const w = window as unknown as { _wails?: { environment?: { OS?: string } } };
    const prevEnv = w._wails?.environment;
    w._wails = { ...(w._wails ?? {}), environment: { OS: 'linux' } };
    try {
      await fireEvent.keyDown(window, { key: 'b', ctrlKey: true });
      await tick();
      expect(screen.getByTestId('dictionary-sidebar')).toBeInTheDocument();

      h.emit('ui:toggle-sidebar', null);
      await waitFor(() => expect(screen.queryByTestId('dictionary-sidebar')).not.toBeInTheDocument());
    } finally {
      if (w._wails) {
        w._wails.environment = prevEnv;
      }
    }
  });
});
