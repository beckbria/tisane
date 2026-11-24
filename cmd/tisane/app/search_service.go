package app

import (
	"context"
	"slices"
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
	SearchID                uint64   `json:"searchId,omitempty"`
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

// SearchCancelledEvent is emitted on "search:cancelled" when an active search is explicitly cancelled.
type SearchCancelledEvent struct {
	SearchID uint64 `json:"searchId"`
}

// SearchService coordinates live pattern validation, streaming multi-dictionary search, and cancellation.
type SearchService struct {
	state  *State
	eng    *search.Engine
	seq    atomic.Uint64
	mu     sync.Mutex
	cancel context.CancelFunc
	curID  uint64
	wg     sync.WaitGroup
}

// ShutdownSearch cancels any active search and waits for its worker goroutine to exit.
// Kept as a package function rather than a method on SearchService so Wails does not bind it as an RPC.
func ShutdownSearch(ss *SearchService) {
	if ss == nil {
		return
	}
	ss.CancelSearch()
	ss.wg.Wait()
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
	activeDicts, _, _, cfg, release := ss.state.SnapshotSearchDicts()
	defer release()

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

// CancelSearch cancels any currently running search immediately and notifies the frontend.
func (ss *SearchService) CancelSearch() bool {
	ss.mu.Lock()
	cancel := ss.cancel
	cancelledID := ss.curID
	ss.cancel = nil
	ss.mu.Unlock()
	if cancel != nil {
		cancel()
		ss.state.Bridge().EmitEvent(EventSearchCancelled, SearchCancelledEvent{SearchID: cancelledID})
		return true
	}
	return false
}

// StartSearch validates inputs synchronously, cancels any prior running search, and launches
// a background streaming search that emits "search:batch", "search:progress", and "search:complete".
func (ss *SearchService) StartSearch(req SearchRequest) (uint64, error) {
	activeDicts, activeInfos, extraDefDicts, cfg, release := ss.state.SnapshotSearchDicts()
	released := false
	defer func() {
		if !released {
			release()
		}
	}()

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

	filterMask := search.EntryFilterFromProfile(req.EntryCategories)

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

	ctx, cancel := context.WithCancel(context.Background())

	ss.mu.Lock()
	searchID := req.SearchID
	if searchID == 0 {
		searchID = ss.seq.Add(1)
	}
	prevCancel := ss.cancel
	ss.cancel = cancel
	ss.curID = searchID
	ss.mu.Unlock()

	if prevCancel != nil {
		prevCancel()
	}

	bridge := ss.state.Bridge()
	released = true
	ss.wg.Add(1)
	go ss.runSearchWorker(ctx, searchID, sReq, activeInfos, bridge, release)
	return searchID, nil
}

func (ss *SearchService) recordQuery(q string) {
	ss.state.RecordRecentQuery(q)
}

func (ss *SearchService) runSearchWorker(
	ctx context.Context,
	searchID uint64,
	sReq search.Request,
	activeInfos []DictionaryInfo,
	bridge HostBridge,
	release func(),
) {
	defer ss.wg.Done()
	defer release()
	start := time.Now()
	ss.recordQuery(sReq.Query)
	defer func() {
		ss.mu.Lock()
		if ss.curID == searchID {
			ss.cancel = nil
		}
		ss.mu.Unlock()
	}()

	if len(sReq.Dictionaries) == 0 {
		bridge.EmitEvent(EventSearchComplete, SearchCompleteEvent{
			SearchID:    searchID,
			ElapsedMs:   0,
			ActiveDicts: activeInfos,
		})
		return
	}

	// Streaming path for SortDict: emit incremental match batches every ~25ms or ~2500 matches.
	if sReq.Sort == search.SortDict {
		totalMatches := 0
		var usedIndexedFTS bool
		var lastProg search.Progress
		var accMatches []search.Match
		lastFlush := time.Now()

		for batch, err := range ss.eng.SearchStreamWithStats(ctx, sReq, &usedIndexedFTS) {
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				bridge.EmitEvent(EventSearchError, SearchErrorEvent{
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
					bridge.EmitEvent(EventSearchBatch, SearchBatchEvent{
						SearchID: searchID,
						Matches:  accMatches,
						Progress: lastProg,
					})
					accMatches = nil
				} else {
					bridge.EmitEvent(EventSearchProgress, SearchProgressEvent{
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
			bridge.EmitEvent(EventSearchBatch, SearchBatchEvent{
				SearchID: searchID,
				Matches:  accMatches,
				Progress: lastProg,
			})
		}
		bridge.EmitEvent(EventSearchComplete, SearchCompleteEvent{
			SearchID:            searchID,
			TotalMatches:        totalMatches,
			WordsScanned:        lastProg.WordsScanned,
			TotalCandidateWords: lastProg.TotalCandidateWords,
			ElapsedMs:           float64(time.Since(start).Microseconds()) / 1000.0,
			UsedIndexedFTS:      usedIndexedFTS,
			ActiveDicts:         activeInfos,
		})
		return
	}

	// Sorted path (Smart, Alpha, Length): stream scan progress while collecting matches,
	// then sort and emit chunked batches so both Tiled Panes and Unified View stay in sorted order.
	streamReq := sReq
	streamReq.Limit = 0
	var usedIndexedFTS bool
	var lastProg search.Progress
	var allMatches []search.Match
	lastProgressEmit := time.Now()

	for batch, err := range ss.eng.SearchStreamWithStats(ctx, streamReq, &usedIndexedFTS) {
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			bridge.EmitEvent(EventSearchError, SearchErrorEvent{
				SearchID: searchID,
				Message:  err.Error(),
			})
			return
		}
		lastProg = batch.Progress
		if len(batch.Matches) > 0 {
			allMatches = append(allMatches, batch.Matches...)
		}
		if time.Since(lastProgressEmit) >= 25*time.Millisecond {
			bridge.EmitEvent(EventSearchProgress, SearchProgressEvent{
				SearchID: searchID,
				Progress: lastProg,
			})
			lastProgressEmit = time.Now()
		}
	}
	if ctx.Err() != nil {
		return
	}

	search.SortMatches(allMatches, sReq.Sort)
	if sReq.Limit > 0 && len(allMatches) > sReq.Limit {
		allMatches = slices.Clone(allMatches[:sReq.Limit])
	}

	const emitChunk = 5000
	for off := 0; off < len(allMatches); off += emitChunk {
		if ctx.Err() != nil {
			return
		}
		end := min(off+emitChunk, len(allMatches))
		bridge.EmitEvent(EventSearchBatch, SearchBatchEvent{
			SearchID: searchID,
			Matches:  allMatches[off:end],
			Progress: lastProg,
		})
	}

	bridge.EmitEvent(EventSearchComplete, SearchCompleteEvent{
		SearchID:            searchID,
		TotalMatches:        len(allMatches),
		WordsScanned:        lastProg.WordsScanned,
		TotalCandidateWords: lastProg.TotalCandidateWords,
		ElapsedMs:           float64(time.Since(start).Microseconds()) / 1000.0,
		UsedIndexedFTS:      usedIndexedFTS,
		ActiveDicts:         activeInfos,
	})
}
