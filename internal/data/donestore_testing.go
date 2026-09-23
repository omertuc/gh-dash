package data

import (
	"sync"
	"testing"
)

// NewDoneStoreForTesting creates a DoneStore backed by a database at the
// given path, closed when the test ends.
func NewDoneStoreForTesting(t testing.TB, path string) *DoneStore {
	t.Helper()
	store, err := openDoneStore(path)
	if err != nil {
		t.Fatalf("opening done store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// OverrideDoneStoreForTesting replaces the singleton DoneStore with the given
// store. It returns a function that restores the original store.
func OverrideDoneStoreForTesting(store *DoneStore) func() {
	// Mark the singleton as initialized without opening the real store.
	doneStoreOnce.Do(func() {})
	old := doneStore
	doneStore = store
	return func() {
		doneStore = old
		if old == nil {
			doneStoreOnce = sync.Once{}
		}
	}
}
