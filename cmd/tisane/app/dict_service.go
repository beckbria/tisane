package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"tisane/pkg/profile"
	"tisane/pkg/tsd"
)

const (
	defaultWordListPageLimit = 500
	maxWordListPageLimit     = 5000
)

// WordListRequest queries a paginated window of alphabetically sorted words from a dictionary.
type WordListRequest struct {
	ID         string `json:"id"`
	Offset     int    `json:"offset"`
	Limit      int    `json:"limit"`
	JumpPrefix string `json:"jumpPrefix,omitempty"`
}

// WordListPage contains a slice of alphabetically sorted words from a dictionary.
type WordListPage struct {
	DictID     string   `json:"dictId"`
	DictName   string   `json:"dictName"`
	TotalWords int      `json:"totalWords"`
	Offset     int      `json:"offset"`
	Words      []string `json:"words"`
	JumpIndex  int      `json:"jumpIndex"`
}

type wordListCacheEntry struct {
	dictPtr *tsd.Dictionary
	dictID  string
	list    *tsd.SortedWordList
}

type wordListBuildInFlight struct {
	dictPtr *tsd.Dictionary
	cancel  context.CancelFunc
	waiters int
	done    chan struct{}
	list    *tsd.SortedWordList
	err     error
}

// DictionaryService exposes dictionary selection, drag-and-drop ordering, and registration to Wails v3.
type DictionaryService struct {
	state      *State
	wlMu       sync.Mutex
	wlGen      uint64
	wlCache    *wordListCacheEntry
	wlBuilding *wordListBuildInFlight
}

// NewDictionaryService creates a DictionaryService backed by state.
func NewDictionaryService(state *State) *DictionaryService {
	return &DictionaryService{state: state}
}

// ListDictionaries returns all registered dictionaries in priority order.
func (ds *DictionaryService) ListDictionaries() []DictionaryInfo {
	ds.state.mu.RLock()
	defer ds.state.mu.RUnlock()
	return ds.state.listInfosLocked()
}

// SetEnabled toggles whether dictionary id is active for search queries.
func (ds *DictionaryService) SetEnabled(id string, enabled bool) ([]DictionaryInfo, error) {
	ds.state.mu.Lock()
	idx := ds.state.findIndexLocked(id)
	if idx < 0 {
		ds.state.mu.Unlock()
		return nil, fmt.Errorf("dictionary %q not found", id)
	}
	ds.state.dicts[idx].info.Enabled = enabled
	ds.state.syncConfigDictionariesLocked()
	saveErr := ds.state.saveConfigLocked()
	ds.state.mu.Unlock()

	infos := ds.state.emitDictsUpdated()
	if saveErr != nil {
		return infos, fmt.Errorf("save config: %w", saveErr)
	}
	return infos, nil
}

// ReorderDictionaries updates the priority order of all dictionaries (used by drag-and-drop and Up/Down controls).
func (ds *DictionaryService) ReorderDictionaries(orderedIDs []string) ([]DictionaryInfo, error) {
	ds.state.mu.Lock()
	if err := ds.state.reorderLocked(orderedIDs); err != nil {
		ds.state.mu.Unlock()
		return nil, err
	}
	ds.state.mu.Unlock()

	return ds.state.emitDictsUpdated(), nil
}

// addDictionaryPaths opens and appends one or more .tsd files by path.
func (ds *DictionaryService) addDictionaryPaths(paths []string) ([]DictionaryInfo, error) {
	ds.state.mu.Lock()
	var openErrs []error
	added := 0
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := ds.state.openAndAppendLocked(p, "", true); err != nil {
			openErrs = append(openErrs, fmt.Errorf("%s: %w", p, err))
		} else {
			added++
		}
	}
	if added == 0 && len(openErrs) > 0 {
		ds.state.mu.Unlock()
		return nil, errors.Join(openErrs...)
	}
	ds.state.syncConfigDictionariesLocked()
	saveErr := ds.state.saveConfigLocked()
	ds.state.mu.Unlock()

	infos := ds.state.emitDictsUpdated()
	if saveErr != nil {
		return infos, fmt.Errorf("save config: %w", saveErr)
	}
	if len(openErrs) > 0 {
		return infos, errors.Join(openErrs...)
	}
	return infos, nil
}

// BrowseAndAddDictionaries opens the native OS file picker for *.tsd files and registers chosen dictionaries.
func (ds *DictionaryService) BrowseAndAddDictionaries(ctx context.Context) ([]DictionaryInfo, error) {
	if err := ds.state.checkHostIOAllowed("adding host dictionaries"); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	paths, err := ds.state.Bridge().PickDictionaryFiles()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return ds.ListDictionaries(), nil
	}
	return ds.addDictionaryPaths(paths)
}

// BrowseAndAddDirectory opens the native OS directory picker and registers all *.tsd files in that directory.
func (ds *DictionaryService) BrowseAndAddDirectory(ctx context.Context) ([]DictionaryInfo, error) {
	if err := ds.state.checkHostIOAllowed("scanning host dictionary directories"); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := ds.state.Bridge().PickDictionaryDirectory()
	if err != nil {
		return nil, err
	}
	if dir == "" {
		return ds.ListDictionaries(), nil
	}
	return ds.scanDirectory(ctx, dir)
}

func (ds *DictionaryService) scanDirectory(ctx context.Context, dir string) ([]DictionaryInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	found, err := profile.DiscoverDictionaries(dir)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return ds.ListDictionaries(), nil
	}
	return ds.addDictionaryPaths(found)
}

// RemoveDictionary unregisters dictionary id from the profile and releases State's reference
// so the underlying file handle closes once any in-flight searches or lookups finish.
func (ds *DictionaryService) RemoveDictionary(id string) ([]DictionaryInfo, error) {
	if err := ds.state.checkHostIOAllowed("removing configured dictionaries"); err != nil {
		return nil, err
	}
	ds.state.mu.Lock()
	idx := ds.state.findIndexLocked(id)
	if idx < 0 {
		ds.state.mu.Unlock()
		return nil, fmt.Errorf("dictionary %q not found", id)
	}
	removed := ds.state.dicts[idx]
	ds.state.dicts = append(ds.state.dicts[:idx], ds.state.dicts[idx+1:]...)
	ds.state.syncConfigDictionariesLocked()
	saveErr := ds.state.saveConfigLocked()
	ds.state.mu.Unlock()

	_ = removed.release()
	ds.evictWordListForID(id)
	infos := ds.state.emitDictsUpdated()
	if saveErr != nil {
		return infos, fmt.Errorf("save config: %w", saveErr)
	}
	return infos, nil
}

// OpenDictionaryDirectory opens the host OS file manager in the directory containing dictionary id.
func (ds *DictionaryService) OpenDictionaryDirectory(id string) error {
	if err := ds.state.checkHostIOAllowed("opening host dictionary directories"); err != nil {
		return err
	}
	ds.state.mu.RLock()
	idx := ds.state.findIndexLocked(id)
	if idx < 0 {
		ds.state.mu.RUnlock()
		return fmt.Errorf("dictionary %q not found", id)
	}
	dictPath := ds.state.dicts[idx].info.Path
	ds.state.mu.RUnlock()

	dir := filepath.Dir(dictPath)
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve dictionary directory: %w", err)
	}
	info, err := os.Stat(absDir)
	if err != nil {
		return fmt.Errorf("stat dictionary directory %q: %w", absDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("dictionary parent %q is not a directory", absDir)
	}
	return ds.state.Bridge().OpenDirectory(absDir)
}

func (ds *DictionaryService) evictWordListForID(id string) {
	ds.wlMu.Lock()
	defer ds.wlMu.Unlock()
	if ds.wlCache != nil && ds.wlCache.dictID == id {
		ds.wlCache = nil
		ds.wlGen++
	}
}

// CloseDictionaryWordList releases any cached in-memory sorted word list when the Word List dialog closes.
func (ds *DictionaryService) CloseDictionaryWordList() {
	ds.wlMu.Lock()
	ds.wlCache = nil
	ds.wlGen++
	if ds.wlBuilding != nil && ds.wlBuilding.cancel != nil {
		ds.wlBuilding.cancel()
	}
	ds.wlMu.Unlock()
}

func (ds *DictionaryService) getOrBuildSortedWordList(ctx context.Context, id string, dict *tsd.Dictionary, holdDict func() func()) (*tsd.SortedWordList, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ds.wlMu.Lock()
	if ds.wlCache != nil && ds.wlCache.dictPtr == dict {
		list := ds.wlCache.list
		ds.wlMu.Unlock()
		return list, nil
	}

	if inflight := ds.wlBuilding; inflight != nil && inflight.dictPtr == dict {
		inflight.waiters++
		ds.wlMu.Unlock()

		select {
		case <-inflight.done:
			return inflight.list, inflight.err
		case <-ctx.Done():
			ds.wlMu.Lock()
			inflight.waiters--
			if inflight.waiters <= 0 && inflight.cancel != nil {
				inflight.cancel()
			}
			ds.wlMu.Unlock()
			return nil, ctx.Err()
		}
	}

	if ds.wlBuilding != nil && ds.wlBuilding.cancel != nil {
		ds.wlBuilding.cancel()
	}

	releaseWorkerDict := holdDict()
	if releaseWorkerDict == nil {
		ds.wlMu.Unlock()
		return nil, fmt.Errorf("dictionary %q is no longer open", id)
	}

	buildCtx, cancelBuild := context.WithCancel(context.Background())
	gen := ds.wlGen
	inflight := &wordListBuildInFlight{
		dictPtr: dict,
		cancel:  cancelBuild,
		waiters: 1,
		done:    make(chan struct{}),
	}
	ds.wlBuilding = inflight
	ds.wlMu.Unlock()

	go func() {
		defer releaseWorkerDict()
		list, err := dict.BuildSortedWordList(buildCtx)
		cancelBuild()

		ds.wlMu.Lock()
		inflight.list = list
		inflight.err = err
		if ds.wlBuilding == inflight {
			ds.wlBuilding = nil
		}
		if err == nil && ds.wlGen == gen {
			ds.wlCache = &wordListCacheEntry{
				dictPtr: dict,
				dictID:  id,
				list:    list,
			}
		}
		close(inflight.done)
		ds.wlMu.Unlock()
	}()

	select {
	case <-inflight.done:
		return inflight.list, inflight.err
	case <-ctx.Done():
		ds.wlMu.Lock()
		inflight.waiters--
		if inflight.waiters <= 0 && inflight.cancel != nil {
			inflight.cancel()
		}
		ds.wlMu.Unlock()
		return nil, ctx.Err()
	}
}

// GetDictionaryWords returns a paginated slice of alphabetically sorted words from dictionary req.ID.
func (ds *DictionaryService) GetDictionaryWords(ctx context.Context, req WordListRequest) (*WordListPage, error) {
	dict, info, release, err := ds.state.AcquireDictionaryByID(req.ID)
	if err != nil {
		return nil, err
	}
	defer release()

	holdDict := func() func() {
		d2, _, rel2, err2 := ds.state.AcquireDictionaryByID(req.ID)
		if err2 != nil {
			return nil
		}
		if d2 != dict {
			rel2()
			return nil
		}
		return rel2
	}

	list, err := ds.getOrBuildSortedWordList(ctx, info.ID, dict, holdDict)
	if err != nil {
		return nil, err
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultWordListPageLimit
	} else if limit > maxWordListPageLimit {
		limit = maxWordListPageLimit
	}

	offset := req.Offset
	jumpIdx := -1
	if strings.TrimSpace(req.JumpPrefix) != "" {
		jumpIdx = list.PrefixIndex(req.JumpPrefix)
		if offset < 0 {
			offset = (jumpIdx / limit) * limit
		}
	}
	if offset < 0 {
		offset = 0
	}

	return &WordListPage{
		DictID:     info.ID,
		DictName:   info.Name,
		TotalWords: list.Len(),
		Offset:     offset,
		Words:      list.Slice(offset, limit),
		JumpIndex:  jumpIdx,
	}, nil
}
