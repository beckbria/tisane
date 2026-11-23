import { Call, Events } from '@wailsio/runtime';
import type {
  BuildDictionaryRequest,
  BuildDictionaryResult,
  Config,
  ConfigPatch,
  DictionaryInfo,
  LookupResult,
  SearchRequest,
  ValidateRequest,
  ValidationResult
} from './types';

export type UnsubscribeFn = () => void;

export interface TisaneBackend {
  listDictionaries(): Promise<DictionaryInfo[]>;
  setEnabled(id: string, enabled: boolean): Promise<DictionaryInfo[]>;
  reorderDictionaries(orderedIds: string[]): Promise<DictionaryInfo[]>;
  addDictionaryPaths(paths: string[]): Promise<DictionaryInfo[]>;
  browseAndAddDictionaries(): Promise<DictionaryInfo[]>;
  browseAndAddDirectory(): Promise<DictionaryInfo[]>;
  scanDirectory(dir: string): Promise<DictionaryInfo[]>;
  removeDictionary(id: string): Promise<DictionaryInfo[]>;
  browseWordListFile(): Promise<string>;
  browseSaveDictionaryFile(defaultPath: string): Promise<string>;
  browseAlphabetMapFile(): Promise<string>;
  buildDictionary(req: BuildDictionaryRequest): Promise<BuildDictionaryResult>;

  validatePattern(req: ValidateRequest): Promise<ValidationResult>;
  startSearch(req: SearchRequest): Promise<number>;
  cancelSearch(): Promise<boolean>;

  getDefinitions(word: string, includeMentions: boolean): Promise<LookupResult>;
  openWebLookup(word: string, providerId: string): Promise<string>;
  openExternalURL(rawUrl: string): Promise<void>;

  getConfig(): Promise<Config>;
  updateConfig(patch: ConfigPatch): Promise<Config>;
  resetConfig(resetDictionaries: boolean): Promise<Config>;

  onEvent<T = unknown>(eventName: string, handler: (data: T) => void): UnsubscribeFn;
}

const FQN = {
  listDictionaries: 'tisane/cmd/tisane/app.DictionaryService.ListDictionaries',
  setEnabled: 'tisane/cmd/tisane/app.DictionaryService.SetEnabled',
  reorderDictionaries: 'tisane/cmd/tisane/app.DictionaryService.ReorderDictionaries',
  addDictionaryPaths: 'tisane/cmd/tisane/app.DictionaryService.AddDictionaryPaths',
  browseAndAddDictionaries: 'tisane/cmd/tisane/app.DictionaryService.BrowseAndAddDictionaries',
  browseAndAddDirectory: 'tisane/cmd/tisane/app.DictionaryService.BrowseAndAddDirectory',
  scanDirectory: 'tisane/cmd/tisane/app.DictionaryService.ScanDirectory',
  removeDictionary: 'tisane/cmd/tisane/app.DictionaryService.RemoveDictionary',
  browseWordListFile: 'tisane/cmd/tisane/app.DictionaryService.BrowseWordListFile',
  browseSaveDictionaryFile: 'tisane/cmd/tisane/app.DictionaryService.BrowseSaveDictionaryFile',
  browseAlphabetMapFile: 'tisane/cmd/tisane/app.DictionaryService.BrowseAlphabetMapFile',
  buildDictionary: 'tisane/cmd/tisane/app.DictionaryService.BuildDictionary',

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
  addDictionaryPaths: (paths) => Call.ByName(FQN.addDictionaryPaths, paths),
  browseAndAddDictionaries: () => Call.ByName(FQN.browseAndAddDictionaries),
  browseAndAddDirectory: () => Call.ByName(FQN.browseAndAddDirectory),
  scanDirectory: (dir) => Call.ByName(FQN.scanDirectory, dir),
  removeDictionary: (id) => Call.ByName(FQN.removeDictionary, id),
  browseWordListFile: () => Call.ByName(FQN.browseWordListFile),
  browseSaveDictionaryFile: (defaultPath) => Call.ByName(FQN.browseSaveDictionaryFile, defaultPath),
  browseAlphabetMapFile: () => Call.ByName(FQN.browseAlphabetMapFile),
  buildDictionary: (req) => Call.ByName(FQN.buildDictionary, req),

  validatePattern: (req) => Call.ByName(FQN.validatePattern, req),
  startSearch: (req) => Call.ByName(FQN.startSearch, req),
  cancelSearch: () => Call.ByName(FQN.cancelSearch),

  getDefinitions: (word, includeMentions) => Call.ByName(FQN.getDefinitions, word, includeMentions),
  openWebLookup: (word, providerId) => Call.ByName(FQN.openWebLookup, word, providerId),
  openExternalURL: (rawUrl) => Call.ByName(FQN.openExternalURL, rawUrl),

  getConfig: () => Call.ByName(FQN.getConfig),
  updateConfig: (patch) => Call.ByName(FQN.updateConfig, patch),
  resetConfig: (resetDictionaries) => Call.ByName(FQN.resetConfig, resetDictionaries),

  onEvent: <T = unknown>(eventName: string, handler: (data: T) => void): UnsubscribeFn => {
    return Events.On(eventName, (ev: { data: T }) => {
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
