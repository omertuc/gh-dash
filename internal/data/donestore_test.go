package data

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

func newTestDoneStore(t *testing.T) (*DoneStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), doneStoreFilename)
	return NewDoneStoreForTesting(t, path), path
}

// openOutsideWriter opens a separate connection to the database, as another
// process would.
func openOutsideWriter(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("opening outside connection: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func writeLegacyFile(t *testing.T, dir string, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, legacyDoneFilename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDoneStore(t *testing.T) {
	baseTime := time.Now().Add(-1 * time.Hour).UTC().Truncate(time.Second)

	t.Run("IsDone compares against updatedAt when marked done", func(t *testing.T) {
		store, _ := newTestDoneStore(t)
		store.MarkDone("id1", baseTime)

		if !store.IsDone("id1", baseTime) {
			t.Error("Should be done when updatedAt equals doneAt")
		}
		if !store.IsDone("id1", baseTime.Add(-time.Hour)) {
			t.Error("Should be done when updatedAt is older than doneAt")
		}
		if store.IsDone("id1", baseTime.Add(time.Hour)) {
			t.Error("Should NOT be done when notification has new activity")
		}
		if store.IsDone("unknown", baseTime) {
			t.Error("Should NOT be done for an ID not in the store")
		}
	})

	t.Run("Remove", func(t *testing.T) {
		store, _ := newTestDoneStore(t)
		store.MarkDone("id1", baseTime)
		store.Remove("id1")
		if store.IsDone("id1", baseTime) {
			t.Error("Should NOT be done after Remove")
		}
	})

	t.Run("re-marking overwrites the timestamp", func(t *testing.T) {
		store, _ := newTestDoneStore(t)
		store.MarkDone("id1", baseTime)
		later := baseTime.Add(30 * time.Minute)
		store.MarkDone("id1", later)
		if !store.IsDone("id1", later) {
			t.Error("Should be done as of the second mark")
		}
	})

	t.Run("persists across reopening", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), doneStoreFilename)
		store1 := NewDoneStoreForTesting(t, path)
		store1.MarkDone("id1", baseTime)
		store1.MarkDone("id2", baseTime.Add(30*time.Minute))
		store1.MarkDone("id3", baseTime)
		store1.Remove("id3")
		store1.Close()

		store2 := NewDoneStoreForTesting(t, path)
		if !store2.IsDone("id1", baseTime) {
			t.Error("Reopened store should have id1 as done")
		}
		if !store2.IsDone("id2", baseTime.Add(30*time.Minute)) {
			t.Error("Reopened store should have id2 as done")
		}
		if store2.IsDone("id3", baseTime) {
			t.Error("Reopened store should not have removed id3")
		}
	})

	t.Run("prunes entries past retention on open", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), doneStoreFilename)
		now := time.Now().UTC().Truncate(time.Second)
		store1 := NewDoneStoreForTesting(t, path)
		store1.MarkDone("fresh", now.Add(-24*time.Hour))
		store1.MarkDone("border", now.Add(-89*24*time.Hour))
		store1.MarkDone("expired", now.Add(-91*24*time.Hour))
		store1.Close()

		store2 := NewDoneStoreForTesting(t, path)
		for id, want := range map[string]bool{"fresh": true, "border": true, "expired": false} {
			if _, got := store2.entries[id]; got != want {
				t.Errorf("entry %q kept = %v, want %v", id, got, want)
			}
		}
	})

	t.Run("concurrent writes are all kept", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), doneStoreFilename)
		store := NewDoneStoreForTesting(t, path)
		ids := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
		var wg sync.WaitGroup
		for _, id := range ids {
			wg.Go(func() { store.MarkDone(id, baseTime) })
		}
		wg.Wait()
		store.Close()

		reopened := NewDoneStoreForTesting(t, path)
		for _, id := range ids {
			if !reopened.IsDone(id, baseTime) {
				t.Errorf("%s should be done", id)
			}
		}
	})

	t.Run("refuses a database from a newer version", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), doneStoreFilename)
		NewDoneStoreForTesting(t, path).Close()
		db := openOutsideWriter(t, path)
		if _, err := db.Exec(`PRAGMA user_version = 99`); err != nil {
			t.Fatal(err)
		}
		if _, err := openDoneStore(path); err == nil {
			t.Error("opening a newer schema should fail")
		}
	})
}

func TestDoneStoreLegacyImport(t *testing.T) {
	recent := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)

	t.Run("imports the timestamped format", func(t *testing.T) {
		dir := t.TempDir()
		legacyPath := writeLegacyFile(t, dir, map[string]string{
			"id1":     recent.Format(time.RFC3339),
			"id2":     recent.Add(-time.Minute).Format(time.RFC3339),
			"expired": recent.Add(-91 * 24 * time.Hour).Format(time.RFC3339),
			"bad":     "not a time",
		})
		before, _ := os.ReadFile(legacyPath)

		store := NewDoneStoreForTesting(t, filepath.Join(dir, doneStoreFilename))
		if !store.IsDone("id1", recent) || !store.IsDone("id2", recent.Add(-time.Minute)) {
			t.Error("legacy entries should be imported")
		}
		if store.IsDone("id1", recent.Add(time.Second)) {
			t.Error("imported entries should keep their timestamps")
		}
		for _, id := range []string{"expired", "bad"} {
			if _, ok := store.entries[id]; ok {
				t.Errorf("%q should not be imported", id)
			}
		}

		after, err := os.ReadFile(legacyPath)
		if err != nil || string(after) != string(before) {
			t.Error("legacy file should be left untouched")
		}
	})

	t.Run("imports only once", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, doneStoreFilename)
		writeLegacyFile(t, dir, map[string]string{"id1": recent.Format(time.RFC3339)})

		store := NewDoneStoreForTesting(t, path)
		store.Remove("id1")
		store.Close()

		reopened := NewDoneStoreForTesting(t, path)
		if reopened.IsDone("id1", recent) {
			t.Error("legacy entries should not be re-imported after being removed")
		}
	})

	t.Run("plain ID list resurfaces, as it did before", func(t *testing.T) {
		dir := t.TempDir()
		writeLegacyFile(t, dir, []string{"id1", "id2"})
		store := NewDoneStoreForTesting(t, filepath.Join(dir, doneStoreFilename))
		if len(store.entries) != 0 {
			t.Errorf("got %d entries, want 0", len(store.entries))
		}
	})

	t.Run("corrupt legacy file doesn't block startup", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, legacyDoneFilename), []byte("{invalid"), 0o644); err != nil {
			t.Fatal(err)
		}
		store := NewDoneStoreForTesting(t, filepath.Join(dir, doneStoreFilename))
		store.MarkDone("id1", recent)
		if !store.IsDone("id1", recent) {
			t.Error("store should work despite a corrupt legacy file")
		}
	})

	t.Run("no legacy file", func(t *testing.T) {
		store, _ := newTestDoneStore(t)
		if len(store.entries) != 0 {
			t.Errorf("got %d entries, want 0", len(store.entries))
		}
	})
}

func TestDoneStoreWatch(t *testing.T) {
	orig := doneStorePollInterval
	doneStorePollInterval = 10 * time.Millisecond
	t.Cleanup(func() { doneStorePollInterval = orig })

	recent := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)

	waitForChange := func(t *testing.T, store *DoneStore) []string {
		t.Helper()
		got := make(chan []string, 1)
		go func() { got <- store.WaitForChange() }()
		select {
		case undone := <-got:
			slices.Sort(undone)
			return undone
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for change")
			return nil
		}
	}

	expectNoChange := func(t *testing.T, store *DoneStore) {
		t.Helper()
		time.Sleep(10 * doneStorePollInterval)
		select {
		case <-store.changed:
			t.Error("unexpected change notification")
		default:
		}
	}

	setup := func(t *testing.T) (*DoneStore, *sql.DB) {
		store, path := newTestDoneStore(t)
		store.MarkDone("keep", recent)
		store.MarkDone("undo", recent)
		store.MarkDone("rewind", recent)
		if err := store.Watch(); err != nil {
			t.Fatal(err)
		}
		expectNoChange(t, store)
		return store, openOutsideWriter(t, path)
	}

	t.Run("outside insert hides the notification", func(t *testing.T) {
		store, db := setup(t)
		if _, err := db.Exec(`INSERT INTO done (id, updated_at) VALUES ('new', ?)`,
			recent.Unix()); err != nil {
			t.Fatal(err)
		}
		if undone := waitForChange(t, store); len(undone) != 0 {
			t.Errorf("undone = %v, want none", undone)
		}
		if !store.IsDone("new", recent) {
			t.Error("outside insert should be picked up")
		}
	})

	t.Run("outside delete and rewind report undone IDs", func(t *testing.T) {
		store, db := setup(t)
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		tx.Exec(`DELETE FROM done WHERE id = 'undo'`)
		tx.Exec(`UPDATE done SET updated_at = ? WHERE id = 'rewind'`,
			recent.Add(-time.Hour).Unix())
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if got, want := waitForChange(t, store), []string{"rewind", "undo"}; !slices.Equal(got, want) {
			t.Errorf("undone = %v, want %v", got, want)
		}
		if store.IsDone("undo", recent) || store.IsDone("rewind", recent) {
			t.Error("outside changes should be picked up")
		}
		if !store.IsDone("keep", recent) {
			t.Error("untouched entry should remain done")
		}
	})

	t.Run("own writes don't notify", func(t *testing.T) {
		store, _ := setup(t)
		store.MarkDone("mine", recent)
		store.Remove("keep")
		expectNoChange(t, store)
	})

	t.Run("Watch is idempotent", func(t *testing.T) {
		store, _ := setup(t)
		changed := store.changed
		if err := store.Watch(); err != nil {
			t.Fatal(err)
		}
		if store.changed != changed {
			t.Error("second Watch should not replace the channel")
		}
	})
}
