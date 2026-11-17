export { APP_VERSION, VERSION } from './version';

export interface DictionaryInfo {
  id: string;
  path: string;
  name: string;
  description: string;
  version: string;
  wordCount: number;
  maxWordLength: number;
  hasDefinitions: boolean;
  hasFTS: boolean;
  enabled: boolean;
}

export interface Span {
  start: number;
  end: number;
}

export interface ValidationResult {
  valid: boolean;
  code: number;
  message: string;
  span: Span;
  minMatchedLength: number;
  maxMatchedLength: number;
  hasWarning: boolean;
}

export type SearchMode = 'word' | 'fts';
export type LayoutMode = 'tiled' | 'unified';
export type SortMode = 'dict' | 'alpha' | 'alpha-desc' | 'len' | 'len-desc' | 'smart';
export type EntryCategory = 'lowercase' | 'capitalized' | 'punctuated' | 'phrase';

export interface ValidateRequest {
  query: string;
  mode: SearchMode;
  enumeration: string;
  minLength: number;
  maxLength: number;
  atMacro: string;
  hashMacro: string;
  exclusiveVars: boolean;
  ignoreUnusedPunctuation: boolean;
}

export interface SearchRequest {
  query: string;
  mode: SearchMode;
  enumeration: string;
  minLength: number;
  maxLength: number;
  atMacro: string;
  hashMacro: string;
  exclusiveVars: boolean;
  ignoreUnusedPunctuation: boolean;
  showBalances: boolean;
  showAnnotations: boolean;
  suppressDuplicates: boolean;
  entryCategories: EntryCategory[];
  sortMode: SortMode;
  limit: number;
}

export interface Match {
  word: string;
  annotation?: string;
  balance?: string;
  length: number;
  dictIndex: number;
  dict?: string;
  sources?: number[];
  hasDefinition: boolean;
}

export interface SearchProgress {
  dictIndex: number;
  dictName: string;
  wordsScanned: number;
  totalCandidateWords: number;
}

export interface SearchBatchEvent {
  searchId: number;
  matches: Match[];
  progress: SearchProgress;
}

export interface SearchProgressEvent {
  searchId: number;
  progress: SearchProgress;
}

export interface SearchCompleteEvent {
  searchId: number;
  totalMatches: number;
  wordsScanned: number;
  totalCandidateWords: number;
  elapsedMs: number;
  usedIndexedFTS: boolean;
  activeDicts: DictionaryInfo[];
}

export interface SearchErrorEvent {
  searchId: number;
  message: string;
}

export interface WebProvider {
  id: string;
  name: string;
  urlTemplate: string;
}

export interface WebLink {
  id: string;
  name: string;
  url: string;
}

export interface HeadwordSense {
  headword: string;
  qualifier?: string;
}

export interface DefinitionEntry {
  dictIndex: number;
  dict: string;
  offset: number;
  isAliasRef?: boolean;
  isMention?: boolean;
  isRTF: boolean;
  headwords?: HeadwordSense[];
  html: string;
  text: string;
}

export interface LookupResult {
  word: string;
  entries: DefinitionEntry[];
  webLinks: WebLink[];
}

export interface DictConfig {
  path: string;
  name: string;
  enabled: boolean;
}

export interface ElementFont {
  family: string;
  size: number;
}

export interface FontConfig {
  results: ElementFont;
  definitions: ElementFont;
  patternInput: ElementFont;
  dictionariesList: ElementFont;
  statusBar: ElementFont;
  toolbar: ElementFont;
}

export type FontElementKey = keyof FontConfig;

export interface Config {
  dictDirs: string[];
  dictionaries: DictConfig[];
  atMacro: string;
  hashMacro: string;
  minLength: number;
  maxLength: number;
  exclusiveVars: boolean;
  ignoreUnusedPunctuation: boolean;
  showBalances: boolean;
  showAnnotations: boolean;
  suppressDuplicates: boolean;
  entryFilter: EntryCategory[];
  sortMode: SortMode;
  layoutMode: LayoutMode;
  autoWebFallback: boolean;
  defaultWebProvider: string;
  webProviders: WebProvider[];
  recentQueries: string[];
  fonts: FontConfig;
}

export interface ConfigPatch {
  atMacro?: string;
  hashMacro?: string;
  minLength?: number;
  maxLength?: number;
  exclusiveVars?: boolean;
  ignoreUnusedPunctuation?: boolean;
  showBalances?: boolean;
  showAnnotations?: boolean;
  suppressDuplicates?: boolean;
  entryFilter?: EntryCategory[];
  sortMode?: SortMode;
  layoutMode?: LayoutMode;
  autoWebFallback?: boolean;
  defaultWebProvider?: string;
  webProviders?: WebProvider[];
  fonts?: FontConfig;
}
