package app

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"tisane/pkg/pattern"
	"tisane/pkg/search"
)

// ValidateRequest carries live pattern bar parameters for instant validation feedback.
type ValidateRequest struct {
	Query                   string `json:"query"`
	Mode                    string `json:"mode"` // "word" or "fts"
	Enumeration             string `json:"enumeration"`
	MinLength               int    `json:"minLength"`
	MaxLength               int    `json:"maxLength"`
	AtMacro                 string `json:"atMacro"`
	HashMacro               string `json:"hashMacro"`
	ExclusiveVars           bool   `json:"exclusiveVars"`
	IgnoreUnusedPunctuation bool   `json:"ignoreUnusedPunctuation"`
}

// SearchRequest represents a GUI search invocation from Svelte 5.
type SearchRequest struct {
	Query                   string   `json:"query"`
	Mode                    string   `json:"mode"` // "word" or "fts"
	Enumeration             string   `json:"enumeration"`
	MinLength               int      `json:"minLength"`
	MaxLength               int      `json:"maxLength"`
	AtMacro                 string   `json:"atMacro"`
	HashMacro               string   `json:"hashMacro"`
	ExclusiveVars           bool     `json:"exclusiveVars"`
	IgnoreUnusedPunctuation bool     `json:"ignoreUnusedPunctuation"`
	ShowBalances            bool     `json:"showBalances"`
	ShowAnnotations         bool     `json:"showAnnotations"`
	SuppressDuplicates      bool     `json:"suppressDuplicates"`
	EntryCategories         []string `json:"entryCategories"` // ["lowercase", "capitalized", "punctuated", "phrase"]
	SortMode                string   `json:"sortMode"`        // "dict", "alpha", "alpha-desc", "len", "len-desc", "smart"
	Limit                   int      `json:"limit"`
}

// SearchBatchEvent is emitted on "search:batch" with incremental or sorted matches.
type SearchBatchEvent struct {
	SearchID uint64          `json:"searchId"`
	Matches  []search.Match  `json:"matches"`
	Progress search.Progress `json:"progress"`
}

// SearchProgressEvent is emitted on "search:progress" while scanning/sorting.
type SearchProgressEvent struct {
	SearchID uint64          `json:"searchId"`
	Progress search.Progress `json:"progress"`
}

// SearchCompleteEvent is emitted on "search:complete" when a search finishes.
type SearchCompleteEvent struct {
	SearchID            uint64           `json:"searchId"`
	TotalMatches        int              `json:"totalMatches"`
	WordsScanned        uint64           `json:"wordsScanned"`
	TotalCandidateWords uint64           `json:"totalCandidateWords"`
	ElapsedMs           float64          `json:"elapsedMs"`
	UsedIndexedFTS      bool             `json:"usedIndexedFTS"`
	ActiveDicts         []DictionaryInfo `json:"activeDicts"`
}

// SearchErrorEvent is emitted on "search:error" if a search fails asynchronously.
type SearchErrorEvent struct {
	SearchID uint64 `json:"searchId"`
	Message  string `json:"message"`
}

// SearchService coordinates live pattern validation, streaming multi-dictionary search, and cancellation.
type SearchService struct {
	state  *State
	eng    *search.Engine
	seq    atomic.Uint64
	mu     sync.Mutex
	cancel context.CancelFunc
	curID  uint64
}

// NewSearchService creates a SearchService backed by state.
func NewSearchService(state *State) *SearchService {
	return &SearchService{
		state: state,
		eng:   search.NewEngine(),
	}
}

// ValidatePattern validates query syntax, optional enumeration, and length compatibility across active dictionaries.
func (ss *SearchService) ValidatePattern(req ValidateRequest) pattern.ValidationResult {
	activeDicts, _, _, cfg := ss.state.SnapshotSearchDicts()

	atMacro := req.AtMacro
	if atMacro == "" {
		atMacro = cfg.AtMacro
	}
	hashMacro := req.HashMacro
	if hashMacro == "" {
		hashMacro = cfg.HashMacro
	}

	var enumParts []pattern.EnumerationPart
	if strings.TrimSpace(req.Enumeration) != "" && !strings.EqualFold(req.Mode, "fts") {
		var err error
		enumParts, err = pattern.ParseEnumeration(req.Enumeration)
		if err != nil {
			return pattern.ValidationResult{
				Valid:   false,
				Code:    pattern.ErrEmpty,
				Message: err.Error(),
			}
		}
	}

	pOpts := pattern.ParseOptions{
		AtMacro:     atMacro,
		HashMacro:   hashMacro,
		Enumeration: enumParts,
	}

	minL, maxL := req.MinLength, req.MaxLength
	if strings.EqualFold(req.Mode, "fts") {
		// In FTS mode, leaf patterns match words of any length 1..100 inside definitions.
		minL, maxL = 1, 100
	}

	cOpts := pattern.CompileOptions{
		MinLength:               minL,
		MaxLength:               maxL,
		ExclusiveVars:           req.ExclusiveVars,
		IgnoreUnusedPunctuation: req.IgnoreUnusedPunctuation,
	}
	return pattern.Validate(req.Query, pOpts, cOpts, activeDicts...)
}

// CancelSearch cancels any currently running search immediately.
func (ss *SearchService) CancelSearch() bool {
	ss.mu.Lock()
	cancel := ss.cancel
	ss.cancel = nil
	ss.mu.Unlock()
	if cancel != nil {
		cancel()
		return true
	}
	return false
}

// StartSearch cancels any prior search, validates inputs synchronously, and launches
// a background streaming search that emits "search:batch", "search:progress", and "search:complete".
func (ss *SearchService) StartSearch(req SearchRequest) (uint64, error) {
	activeDicts, activeInfos, extraDefDicts, cfg := ss.state.SnapshotSearchDicts()

	mode := search.ModeWord
	if strings.EqualFold(req.Mode, "fts") {
		mode = search.ModeFullText
	}

	var enumParts []pattern.EnumerationPart
	if mode == search.ModeWord && strings.TrimSpace(req.Enumeration) != "" {
		var err error
		enumParts, err = pattern.ParseEnumeration(req.Enumeration)
		if err != nil {
			return 0, err
		}
	}

	sortMode, err := search.ParseSortMode(req.SortMode)
	if err != nil {
		return 0, err
	}

	atMacro := req.AtMacro
	if atMacro == "" {
		atMacro = cfg.AtMacro
	}
	hashMacro := req.HashMacro
	if hashMacro == "" {
		hashMacro = cfg.HashMacro
	}

	// Validate pattern syntax up front so syntax errors reject immediately.
	if _, err := pattern.Parse(req.Query, pattern.ParseOptions{
		AtMacro:     atMacro,
		HashMacro:   hashMacro,
		Enumeration: enumParts,
	}); err != nil {
		return 0, err
	}

	var filterMask search.EntryTypeMask
	if len(req.EntryCategories) == 0 {
		filterMask = search.EntryAll
	} else {
		for _, cat := range req.EntryCategories {
			if m, fErr := search.ParseEntryFilter(cat); fErr == nil {
				filterMask |= m
			}
		}
	}

	dedupe := search.DedupeNone
	if req.SuppressDuplicates {
		dedupe = search.DedupeSuppress
	}

	sReq := search.Request{
		Query:                   req.Query,
		Dictionaries:            activeDicts,
		DefinitionDictionaries:  extraDefDicts,
		Mode:                    mode,
		MinLength:               req.MinLength,
		MaxLength:               req.MaxLength,
		Enumeration:             enumParts,
		AtMacro:                 atMacro,
		HashMacro:               hashMacro,
		DisableDefaultMacros:    true,
		ExclusiveVars:           req.ExclusiveVars,
		IgnoreUnusedPunctuation: req.IgnoreUnusedPunctuation,
		ShowBalances:            req.ShowBalances,
		ShowAnnotations:         req.ShowAnnotations,
		DedupeMode:              dedupe,
		EntryFilter:             filterMask,
		Sort:                    sortMode,
		Limit:                   req.Limit,
	}

	ss.RecordQueryAsync(req.Query)

	searchID := ss.seq.Add(1)
	ctx, cancel := context.WithCancel(context.Background())

	ss.mu.Lock()
	if ss.cancel != nil {
		ss.cancel()
	}
	ss.cancel = cancel
	ss.curID = searchID
	ss.mu.Unlock()

	bridge := ss.state.Bridge()
	go ss.runSearchWorker(ctx, searchID, sReq, activeInfos, bridge)
	return searchID, nil
}

func (ss *SearchService) RecordQueryAsync(q string) {
	ss.state.RecordRecentQuery(q)
}

func (ss *SearchService) runSearchWorker(
	ctx context.Context,
	searchID uint64,
	sReq search.Request,
	activeInfos []DictionaryInfo,
	bridge HostBridge,
) {
	start := time.Now()
	defer func() {
		ss.mu.Lock()
		if ss.curID == searchID {
			ss.cancel = nil
		}
		ss.mu.Unlock()
	}()

	if len(sReq.Dictionaries) == 0 {
		bridge.EmitEvent("search:complete", SearchCompleteEvent{
			SearchID:    searchID,
			ElapsedMs:   0,
			ActiveDicts: activeInfos,
		})
		return
	}

	// Streaming path for SortDict: emit incremental match batches every ~25ms or ~4000 matches.
	if sReq.Sort == search.SortDict {
		totalMatches := 0
		var lastProg search.Progress
		var accMatches []search.Match
		lastFlush := time.Now()

		for batch, err := range ss.eng.SearchStream(ctx, sReq) {
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				bridge.EmitEvent("search:error", SearchErrorEvent{
					SearchID: searchID,
					Message:  err.Error(),
				})
				return
			}
			lastProg = batch.Progress
			if len(batch.Matches) > 0 {
				accMatches = append(accMatches, batch.Matches...)
				totalMatches += len(batch.Matches)
			}

			if len(accMatches) >= 2500 || time.Since(lastFlush) >= 25*time.Millisecond {
				if len(accMatches) > 0 {
					bridge.EmitEvent("search:batch", SearchBatchEvent{
						SearchID: searchID,
						Matches:  accMatches,
						Progress: lastProg,
					})
					accMatches = nil
				} else {
					bridge.EmitEvent("search:progress", SearchProgressEvent{
						SearchID: searchID,
						Progress: lastProg,
					})
				}
				lastFlush = time.Now()
			}
		}

		if ctx.Err() != nil {
			return
		}
		if len(accMatches) > 0 {
			bridge.EmitEvent("search:batch", SearchBatchEvent{
				SearchID: searchID,
				Matches:  accMatches,
				Progress: lastProg,
			})
		}
		bridge.EmitEvent("search:complete", SearchCompleteEvent{
			SearchID:            searchID,
			TotalMatches:        totalMatches,
			WordsScanned:        lastProg.WordsScanned,
			TotalCandidateWords: lastProg.TotalCandidateWords,
			ElapsedMs:           float64(time.Since(start).Microseconds()) / 1000.0,
			ActiveDicts:         activeInfos,
		})
		return
	}

	// Sorted path (Smart, Alpha, Length): stream progress while scanning, then sort & emit chunked batches
	// so both Tiled Panes (filtered by DictIndex) and Unified View display in exact sorted order!
	sum, err := ss.eng.Search(ctx, sReq)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		bridge.EmitEvent("search:error", SearchErrorEvent{
			SearchID: searchID,
			Message:  err.Error(),
		})
		return
	}
	if ctx.Err() != nil {
		return
	}

	const emitChunk = 5000
	prog := search.Progress{
		WordsScanned:        sum.WordsScanned,
		TotalCandidateWords: sum.TotalCandidateWords,
	}
	for off := 0; off < len(sum.Matches); off += emitChunk {
		if ctx.Err() != nil {
			return
		}
		end := off + emitChunk
		if end > len(sum.Matches) {
			end = len(sum.Matches)
		}
		bridge.EmitEvent("search:batch", SearchBatchEvent{
			SearchID: searchID,
			Matches:  sum.Matches[off:end],
			Progress: prog,
		})
	}

	bridge.EmitEvent("search:complete", SearchCompleteEvent{
		SearchID:            searchID,
		TotalMatches:        len(sum.Matches),
		WordsScanned:        sum.WordsScanned,
		TotalCandidateWords: sum.TotalCandidateWords,
		ElapsedMs:           float64(time.Since(start).Microseconds()) / 1000.0,
		UsedIndexedFTS:      sum.UsedIndexedFTS,
		ActiveDicts:         activeInfos,
	})
}
