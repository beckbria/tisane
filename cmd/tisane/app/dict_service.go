package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"tisane/pkg/profile"
)

// DictionaryService exposes dictionary selection, drag-and-drop ordering, and registration to Wails v3.
type DictionaryService struct {
	state *State
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
