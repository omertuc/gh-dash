package data

import (
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"charm.land/log/v2"
	_ "modernc.org/sqlite"
)

const (
	doneStoreFilename = "done.db"

	// doneStoreRetention is how long entries are kept. Older notifications
	// are unlikely to be returned by the API anymore.
	doneStoreRetention = 90 * 24 * time.Hour
)

// doneStorePollInterval is how often Watch checks the database for commits
// made by other processes. It is a variable so tests can shorten it.
var doneStorePollInterval = time.Second

// DoneStore persists notification IDs along with the notification's
// updated_at at the time they were marked done. When checking whether a
// notification is still "done" we compare that timestamp against the
// notification's current updated_at: if the notification has been updated
// since it was marked done, it resurfaces.
//
// It is backed by a SQLite database that other processes may write to, e.g.
//
//	sqlite3 ~/.local/state/gh-dash/done.db \
//	  "INSERT OR REPLACE INTO done (id, updated_at) VALUES ('123', unixepoch())"
//
// Watch picks up such changes while gh-dash is running.
type DoneStore struct {
	db *sql.DB

	mu      sync.RWMutex
	entries map[string]time.Time // cache of the done table

	// Guarded by mu. changed is nil until Watch is called.
	changed chan struct{}
	undone  map[string]struct{}

	watchOnce sync.Once
	closeOnce sync.Once
	stop      chan struct{} // closed by Close to end polling
}

// Schema migrations, applied in order. PRAGMA user_version records how many
// have run.
var doneStoreMigrations = []func(tx *sql.Tx, dbPath string) error{
	func(tx *sql.Tx, dbPath string) error {
		if _, err := tx.Exec(`CREATE TABLE done (
			id TEXT PRIMARY KEY NOT NULL,
			updated_at INTEGER NOT NULL -- unix seconds
		)`); err != nil {
			return err
		}
		return importLegacyDoneFile(tx, filepath.Join(filepath.Dir(dbPath), legacyDoneFilename))
	},
}

func newDoneStore(filename string) *DoneStore {
	path, err := getStateFilePath(filename)
	if err == nil {
		var store *DoneStore
		if store, err = openDoneStore(path); err == nil {
			return store
		}
	}
	log.Error("Failed to open done notifications store", "err", err)
	return &DoneStore{entries: make(map[string]time.Time)}
}

func openDoneStore(path string) (*DoneStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	dsn := (&url.URL{
		Scheme: "file",
		Path:   path,
		RawQuery: url.Values{
			"_pragma": {"busy_timeout(5000)", "journal_mode(WAL)"},
			"_txlock": {"immediate"},
		}.Encode(),
	}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// A single long-lived connection: PRAGMA data_version is per connection
	// and only changes for commits made through other connections.
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)

	store := &DoneStore{
		db:      db,
		entries: make(map[string]time.Time),
		stop:    make(chan struct{}),
	}
	if err := store.migrate(path); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating %s: %w", path, err)
	}
	if _, err := db.Exec(
		`DELETE FROM done WHERE updated_at < ?`,
		time.Now().Add(-doneStoreRetention).Unix(),
	); err != nil {
		log.Warn("Failed to prune done notifications", "err", err)
	}
	entries, err := store.readAll()
	if err != nil {
		db.Close()
		return nil, err
	}
	store.entries = entries
	log.Debug("Loaded done notifications", "path", path, "count", len(entries))
	return store, nil
}

func (s *DoneStore) migrate(path string) error {
	// _txlock=immediate makes concurrently starting instances wait for each
	// other here instead of both running the migrations.
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var version int
	if err := tx.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > len(doneStoreMigrations) {
		return fmt.Errorf("schema version %d is newer than supported (%d)",
			version, len(doneStoreMigrations))
	}
	if version == len(doneStoreMigrations) {
		return nil
	}
	for _, m := range doneStoreMigrations[version:] {
		if err := m(tx, path); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`,
		len(doneStoreMigrations))); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *DoneStore) readAll() (map[string]time.Time, error) {
	rows, err := s.db.Query(`SELECT id, updated_at FROM done`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make(map[string]time.Time)
	for rows.Next() {
		var id string
		var ts int64
		if err := rows.Scan(&id, &ts); err != nil {
			return nil, err
		}
		entries[id] = time.Unix(ts, 0).UTC()
	}
	return entries, rows.Err()
}

// MarkDone records the notification's current updated_at as the "done at"
// timestamp. If the notification later receives new activity (a newer
// updated_at), IsDone will return false.
func (s *DoneStore) MarkDone(id string, updatedAt time.Time) {
	if s.db != nil {
		if _, err := s.db.Exec(
			`INSERT OR REPLACE INTO done (id, updated_at) VALUES (?, ?)`,
			id, updatedAt.Unix(),
		); err != nil {
			log.Error("Failed to save done notification", "id", id, "err", err)
		}
	}
	s.mu.Lock()
	s.entries[id] = updatedAt
	s.mu.Unlock()
}

// IsDone returns true only if the notification has not been updated since it
// was marked done: !updatedAt.After(doneAt).
func (s *DoneStore) IsDone(id string, updatedAt time.Time) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	doneAt, ok := s.entries[id]
	if !ok {
		return false
	}
	return !updatedAt.After(doneAt)
}

// Remove removes a notification from the done store.
func (s *DoneStore) Remove(id string) {
	if s.db != nil {
		if _, err := s.db.Exec(`DELETE FROM done WHERE id = ?`, id); err != nil {
			log.Error("Failed to remove done notification", "id", id, "err", err)
		}
	}
	s.mu.Lock()
	delete(s.entries, id)
	s.mu.Unlock()
}

// Close closes the underlying database.
func (s *DoneStore) Close() error {
	if s.db == nil {
		return nil
	}
	s.closeOnce.Do(func() { close(s.stop) })
	return s.db.Close()
}

// replaceEntries swaps in a fresh copy of the table and, if anything changed,
// notifies Watch subscribers.
func (s *DoneStore) replaceEntries(entries map[string]time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false
	for id, old := range s.entries {
		if t, ok := entries[id]; !ok || t.Before(old) {
			// No longer done (or done as of an earlier update), so the
			// notification may need to resurface.
			if s.undone == nil {
				s.undone = make(map[string]struct{})
			}
			s.undone[id] = struct{}{}
			changed = true
		}
	}
	if !changed {
		changed = !maps.EqualFunc(s.entries, entries, time.Time.Equal)
	}
	s.entries = entries

	if changed {
		select {
		case s.changed <- struct{}{}:
		default: // a notification is already pending
		}
	}
}

// Watch starts reloading the store whenever another process commits to the
// database. Use WaitForChange to be told about the changes.
func (s *DoneStore) Watch() error {
	if s.db == nil {
		return errors.New("done store is not open")
	}
	s.watchOnce.Do(func() {
		s.mu.Lock()
		s.changed = make(chan struct{}, 1)
		s.mu.Unlock()
		go s.poll()
	})
	return nil
}

func (s *DoneStore) poll() {
	ticker := time.NewTicker(doneStorePollInterval)
	defer ticker.Stop()
	var lastVersion int64 = -1
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
		}
		var version int64
		if err := s.db.QueryRow(`PRAGMA data_version`).Scan(&version); err != nil {
			log.Warn("Failed to poll done notifications", "err", err)
			continue
		}
		if version == lastVersion {
			continue
		}
		// The first poll always reloads, catching anything committed between
		// opening the store and starting to watch it.
		lastVersion = version
		entries, err := s.readAll()
		if err != nil {
			log.Warn("Failed to reload done notifications", "err", err)
			lastVersion = -1
			continue
		}
		s.replaceEntries(entries)
	}
}

// WaitForChange blocks until another process changes which notifications are
// done. It returns the IDs that are no longer done (or are done only as of an
// earlier update), which may need to resurface. It must only be called after
// a successful Watch.
func (s *DoneStore) WaitForChange() []string {
	s.mu.RLock()
	changed := s.changed
	s.mu.RUnlock()
	<-changed

	s.mu.Lock()
	defer s.mu.Unlock()
	undone := make([]string, 0, len(s.undone))
	for id := range s.undone {
		undone = append(undone, id)
	}
	s.undone = nil
	return undone
}

// Singleton

var (
	doneStore     *DoneStore
	doneStoreOnce sync.Once
)

// GetDoneStore returns the singleton done store.
func GetDoneStore() *DoneStore {
	doneStoreOnce.Do(func() {
		doneStore = newDoneStore(doneStoreFilename)
	})
	return doneStore
}
