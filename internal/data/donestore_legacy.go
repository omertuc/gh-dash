package data

import (
	"database/sql"
	"encoding/json"
	"os"
	"time"

	"charm.land/log/v2"
)

// legacyDoneFilename is the JSON file done notifications were stored in
// before the SQLite store. It is imported once and then left untouched, so
// downgrading still finds it.
const legacyDoneFilename = "done.json"

// readLegacyDoneFile parses the legacy JSON done store. A missing file yields
// an empty map. It supports both of its on-disk formats:
//   - {"id": "2024-01-15T10:30:00Z", ...}  (map of ID → RFC 3339 timestamp)
//   - ["id1", "id2", ...]                (plain array of IDs)
//
// Plain IDs carry no timestamp. The legacy store resurfaced them on load, so
// they are dropped here too, as are entries past the retention period.
func readLegacyDoneFile(path string) (map[string]time.Time, error) {
	entries := make(map[string]time.Time)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return entries, nil
		}
		return nil, err
	}

	var tsMap map[string]string
	if err := json.Unmarshal(data, &tsMap); err != nil {
		var idList []string
		if err := json.Unmarshal(data, &idList); err != nil {
			return nil, err
		}
		return entries, nil
	}

	cutoff := time.Now().Add(-doneStoreRetention)
	for id, raw := range tsMap {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			log.Warn("Skipping legacy done entry with invalid timestamp",
				"id", id, "raw", raw, "err", err)
			continue
		}
		if t.Before(cutoff) {
			continue
		}
		entries[id] = t
	}
	return entries, nil
}

// importLegacyDoneFile copies the legacy JSON done store into the done table.
// A corrupt legacy file is logged and skipped rather than blocking startup.
func importLegacyDoneFile(tx *sql.Tx, path string) error {
	entries, err := readLegacyDoneFile(path)
	if err != nil {
		log.Warn("Skipping unreadable legacy done notifications file", "path", path, "err", err)
		return nil
	}
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO done (id, updated_at) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for id, t := range entries {
		if _, err := stmt.Exec(id, t.Unix()); err != nil {
			return err
		}
	}
	if len(entries) > 0 {
		log.Info("Imported legacy done notifications", "path", path, "count", len(entries))
	}
	return nil
}
