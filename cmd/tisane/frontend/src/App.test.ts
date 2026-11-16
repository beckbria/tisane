import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import { tick } from 'svelte';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import App from './App.svelte';
import { setBackendForTesting, type TisaneBackend } from './lib/bindings';
import type {
  Config,
  ConfigPatch,
  DictionaryInfo,
  LookupResult,
  Match,
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
    autoWebFallback: false,
    defaultWebProvider: 'onelook',
    webProviders: [
      { id: 'onelook', name: 'OneLook', urlTemplate: 'https://www.onelook.com/?w={entry}' },
      { id: 'wiktionary', name: 'Wiktionary', urlTemplate: 'https://en.wiktionary.org/wiki/{entry}' }
    ],
    recentQueries: []
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
    scanDirectory: vi.fn(async () => dicts),
    removeDictionary: vi.fn(async (id: string) => {
      dicts = dicts.filter((d) => d.id !== id);
      return dicts;
    }),

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
      searchSeq += 1;
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

    onEvent: <T = unknown>(eventName: string, handler: (data: T) => void) => {
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
});
