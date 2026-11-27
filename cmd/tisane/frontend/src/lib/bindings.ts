import { Call, Events } from '@wailsio/runtime';
import type {
  BuildDictionaryRequest,
  BuildDictionaryResult,
  Config,
  ConfigPatch,
  DictionaryInfo,
  LayoutMode,
  LookupResult,
  MergeWordListsRequest,
  MergeWordListsResult,
  SearchBatchEvent,
  SearchCancelledEvent,
  SearchCompleteEvent,
  SearchErrorEvent,
  SearchMode,
  SearchProgressEvent,
  SearchRequest,
  ThemeMode,
  ValidateRequest,
  ValidationResult,
  ZoomAction
} from './types';

export type UnsubscribeFn = () => void;

export interface WailsEventMap {
  'search:batch': SearchBatchEvent;
  'search:progress': SearchProgressEvent;
  'search:complete': SearchCompleteEvent;
  'search:error': SearchErrorEvent;
  'search:cancelled': SearchCancelledEvent;
  'dicts:updated': DictionaryInfo[];
  'ui:open-settings': null;
  'ui:open-font-settings': null;
  'ui:open-about': null;
  'ui:open-dict-builder': null;
  'ui:open-wordlist-merge': null;
  'ui:set-mode': SearchMode;
  'ui:set-layout': LayoutMode;
  'ui:toggle-cheatsheet': null;
  'ui:toggle-sidebar': null;
  'ui:toggle-definitions': null;
  'ui:set-theme': ThemeMode;
  'ui:zoom': ZoomAction;
}

export interface TisaneBackend {
  listDictionaries(): Promise<DictionaryInfo[]>;
  setEnabled(id: string, enabled: boolean): Promise<DictionaryInfo[]>;
  reorderDictionaries(orderedIds: string[]): Promise<DictionaryInfo[]>;
  browseAndAddDictionaries(): Promise<DictionaryInfo[]>;
  browseAndAddDirectory(): Promise<DictionaryInfo[]>;
  removeDictionary(id: string): Promise<DictionaryInfo[]>;
  openDictionaryDirectory(id: string): Promise<void>;
  browseWordListFile(): Promise<string>;
  browseSaveDictionaryFile(defaultPath: string): Promise<string>;
  browseSaveWordListFile(defaultPath: string): Promise<string>;
  browseAlphabetMapFile(): Promise<string>;
  buildDictionary(req: BuildDictionaryRequest): Promise<BuildDictionaryResult>;
  mergeWordLists(req: MergeWordListsRequest): Promise<MergeWordListsResult>;

  validatePattern(req: ValidateRequest): Promise<ValidationResult>;
  startSearch(req: SearchRequest): Promise<number>;
  cancelSearch(): Promise<boolean>;

  getDefinitions(word: string, includeMentions: boolean): Promise<LookupResult>;
  openWebLookup(word: string, providerId: string): Promise<string>;
  openExternalURL(rawUrl: string): Promise<void>;

  getConfig(): Promise<Config>;
  updateConfig(patch: ConfigPatch): Promise<Config>;
  resetConfig(resetDictionaries: boolean): Promise<Config>;

  onEvent<K extends keyof WailsEventMap>(
    eventName: K,
    handler: (data: WailsEventMap[K]) => void
  ): UnsubscribeFn;
}

const FQN = {
  listDictionaries: 'tisane/cmd/tisane/app.DictionaryService.ListDictionaries',
  setEnabled: 'tisane/cmd/tisane/app.DictionaryService.SetEnabled',
  reorderDictionaries: 'tisane/cmd/tisane/app.DictionaryService.ReorderDictionaries',
  browseAndAddDictionaries: 'tisane/cmd/tisane/app.DictionaryService.BrowseAndAddDictionaries',
  browseAndAddDirectory: 'tisane/cmd/tisane/app.DictionaryService.BrowseAndAddDirectory',
  removeDictionary: 'tisane/cmd/tisane/app.DictionaryService.RemoveDictionary',
  openDictionaryDirectory: 'tisane/cmd/tisane/app.DictionaryService.OpenDictionaryDirectory',
  browseWordListFile: 'tisane/cmd/tisane/app.ToolsService.BrowseWordListFile',
  browseSaveDictionaryFile: 'tisane/cmd/tisane/app.ToolsService.BrowseSaveDictionaryFile',
  browseSaveWordListFile: 'tisane/cmd/tisane/app.ToolsService.BrowseSaveWordListFile',
  browseAlphabetMapFile: 'tisane/cmd/tisane/app.ToolsService.BrowseAlphabetMapFile',
  buildDictionary: 'tisane/cmd/tisane/app.ToolsService.BuildDictionary',
  mergeWordLists: 'tisane/cmd/tisane/app.ToolsService.MergeWordLists',

  validatePattern: 'tisane/cmd/tisane/app.SearchService.ValidatePattern',
  startSearch: 'tisane/cmd/tisane/app.SearchService.StartSearch',
  cancelSearch: 'tisane/cmd/tisane/app.SearchService.CancelSearch',

  getDefinitions: 'tisane/cmd/tisane/app.LookupService.GetDefinitions',
  openWebLookup: 'tisane/cmd/tisane/app.LookupService.OpenWebLookup',
  openExternalURL: 'tisane/cmd/tisane/app.LookupService.OpenExternalURL',

  getConfig: 'tisane/cmd/tisane/app.ProfileService.GetConfig',
  updateConfig: 'tisane/cmd/tisane/app.ProfileService.UpdateConfig',
  resetConfig: 'tisane/cmd/tisane/app.ProfileService.ResetConfig'
} as const;

const wailsBackend: TisaneBackend = {
  listDictionaries: () => Call.ByName(FQN.listDictionaries),
  setEnabled: (id, enabled) => Call.ByName(FQN.setEnabled, id, enabled),
  reorderDictionaries: (orderedIds) => Call.ByName(FQN.reorderDictionaries, orderedIds),
  browseAndAddDictionaries: () => Call.ByName(FQN.browseAndAddDictionaries),
  browseAndAddDirectory: () => Call.ByName(FQN.browseAndAddDirectory),
  removeDictionary: (id) => Call.ByName(FQN.removeDictionary, id),
  openDictionaryDirectory: (id) => Call.ByName(FQN.openDictionaryDirectory, id),
  browseWordListFile: () => Call.ByName(FQN.browseWordListFile),
  browseSaveDictionaryFile: (defaultPath) => Call.ByName(FQN.browseSaveDictionaryFile, defaultPath),
  browseSaveWordListFile: (defaultPath) => Call.ByName(FQN.browseSaveWordListFile, defaultPath),
  browseAlphabetMapFile: () => Call.ByName(FQN.browseAlphabetMapFile),
  buildDictionary: (req) => Call.ByName(FQN.buildDictionary, req),
  mergeWordLists: (req) => Call.ByName(FQN.mergeWordLists, req),

  validatePattern: (req) => Call.ByName(FQN.validatePattern, req),
  startSearch: (req) => Call.ByName(FQN.startSearch, req),
  cancelSearch: () => Call.ByName(FQN.cancelSearch),

  getDefinitions: (word, includeMentions) => Call.ByName(FQN.getDefinitions, word, includeMentions),
  openWebLookup: (word, providerId) => Call.ByName(FQN.openWebLookup, word, providerId),
  openExternalURL: (rawUrl) => Call.ByName(FQN.openExternalURL, rawUrl),

  getConfig: () => Call.ByName(FQN.getConfig),
  updateConfig: (patch) => Call.ByName(FQN.updateConfig, patch),
  resetConfig: (resetDictionaries) => Call.ByName(FQN.resetConfig, resetDictionaries),

  onEvent: <K extends keyof WailsEventMap>(
    eventName: K,
    handler: (data: WailsEventMap[K]) => void
  ): UnsubscribeFn => {
    return Events.On(eventName, (ev: { data: WailsEventMap[K] }) => {
      handler(ev.data);
    });
  }
};

let activeBackend: TisaneBackend = wailsBackend;

export function getBackend(): TisaneBackend {
  return activeBackend;
}

export function setBackendForTesting(mock: TisaneBackend | null): void {
  activeBackend = mock ?? wailsBackend;
}

// Returns true when running inside the native Wails v3 desktop WebView (which owns native menu accelerators),
// returning false in headless "-tags server" browser mode and Vitest.
export function isDesktopWebview(): boolean {
  if (typeof window === 'undefined') return false;
  const w = window as unknown as {
    _wails?: {
      flags?: { server?: boolean };
      environment?: {
        OS?: string;
        PlatformInfo?: { mode?: string };
      };
    };
  };
  if (w._wails?.flags?.server === true || w._wails?.environment?.PlatformInfo?.mode === 'server') {
    return false;
  }
  const os = w._wails?.environment?.OS;
  return os === 'windows' || os === 'linux' || os === 'darwin';
}
