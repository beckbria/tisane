package app

import (
	"fmt"

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
	_ = ds.state.saveConfigLocked()
	infos := ds.state.listInfosLocked()
	bridge := ds.state.bridge
	ds.state.mu.Unlock()

	bridge.EmitEvent("dicts:updated", infos)
	return infos, nil
}

// ReorderDictionaries updates the priority order of all dictionaries (used by drag-and-drop and Up/Down controls).
func (ds *DictionaryService) ReorderDictionaries(orderedIDs []string) ([]DictionaryInfo, error) {
	ds.state.mu.Lock()
	if err := ds.state.reorderLocked(orderedIDs); err != nil {
		ds.state.mu.Unlock()
		return nil, err
	}
	infos := ds.state.listInfosLocked()
	bridge := ds.state.bridge
	ds.state.mu.Unlock()

	bridge.EmitEvent("dicts:updated", infos)
	return infos, nil
}

// AddDictionaryPaths opens and appends one or more .tsd files by path.
func (ds *DictionaryService) AddDictionaryPaths(paths []string) ([]DictionaryInfo, error) {
	ds.state.mu.Lock()
	var lastErr error
	added := 0
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := ds.state.openAndAppendLocked(p, "", true); err != nil {
			lastErr = err
		} else {
			added++
		}
	}
	if added == 0 && lastErr != nil {
		ds.state.mu.Unlock()
		return nil, lastErr
	}
	ds.state.syncConfigDictionariesLocked()
	_ = ds.state.saveConfigLocked()
	infos := ds.state.listInfosLocked()
	bridge := ds.state.bridge
	ds.state.mu.Unlock()

	bridge.EmitEvent("dicts:updated", infos)
	return infos, nil
}

// BrowseAndAddDictionaries opens the native OS file picker for *.tsd files and registers chosen dictionaries.
func (ds *DictionaryService) BrowseAndAddDictionaries() ([]DictionaryInfo, error) {
	paths, err := ds.state.Bridge().PickDictionaryFiles()
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return ds.ListDictionaries(), nil
	}
	return ds.AddDictionaryPaths(paths)
}

// BrowseAndAddDirectory opens the native OS directory picker and registers all *.tsd files in that directory.
func (ds *DictionaryService) BrowseAndAddDirectory() ([]DictionaryInfo, error) {
	dir, err := ds.state.Bridge().PickDictionaryDirectory()
	if err != nil {
		return nil, err
	}
	if dir == "" {
		return ds.ListDictionaries(), nil
	}
	return ds.ScanDirectory(dir)
}

// ScanDirectory discovers all *.tsd files inside dir and registers any new ones.
func (ds *DictionaryService) ScanDirectory(dir string) ([]DictionaryInfo, error) {
	found, err := profile.DiscoverDictionaries(dir)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return ds.ListDictionaries(), nil
	}
	return ds.AddDictionaryPaths(found)
}

// RemoveDictionary closes and unregisters dictionary id from the profile.
func (ds *DictionaryService) RemoveDictionary(id string) ([]DictionaryInfo, error) {
	ds.state.mu.Lock()
	idx := ds.state.findIndexLocked(id)
	if idx < 0 {
		ds.state.mu.Unlock()
		return nil, fmt.Errorf("dictionary %q not found", id)
	}
	removed := ds.state.dicts[idx]
	ds.state.dicts = append(ds.state.dicts[:idx], ds.state.dicts[idx+1:]...)
	ds.state.syncConfigDictionariesLocked()
	_ = ds.state.saveConfigLocked()
	infos := ds.state.listInfosLocked()
	bridge := ds.state.bridge
	ds.state.mu.Unlock()

	_ = removed.release()
	bridge.EmitEvent("dicts:updated", infos)
	return infos, nil
}
