package notificationssection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"charm.land/log/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/notificationrow"
)

const (
	// listCacheVersion is bumped whenever the cached rows' format changes, so
	// that older caches are ignored rather than misread.
	listCacheVersion = 1

	// maxCachedRows is the most of a section's rows that are cached. At most
	// a page of them is, see numCachedRows.
	maxCachedRows = 100

	// listCacheWriteDelay is how long writes are held back, so a burst of
	// updates, e.g. comment counts arriving, costs a single write.
	listCacheWriteDelay = 500 * time.Millisecond
)

// listCache is what's kept on disk of a section's rows, so they can be shown
// right away on startup while they're being refreshed.
type listCache struct {
	Version       int                    `json:"version"`
	SavedAt       time.Time              `json:"savedAt"`
	Notifications []notificationrow.Data `json:"notifications"`
}

// listCacheFilename returns the name of the file caching the rows fetched
// for the given search. Sections with the same search share it.
func listCacheFilename(search string, includeRead bool) string {
	sum := sha256.Sum256([]byte(search + "\x00" + strconv.FormatBool(includeRead)))
	return "notifications-" + hex.EncodeToString(sum[:8]) + ".json"
}

// numCachedRows returns how many rows are cached and shown from the cache: as
// many as the first fetch brings, so the cursor can't be on a cached row that
// isn't among the fetched ones just because it's further down.
func (m *Model) numCachedRows() int {
	limit := m.Ctx.Config.Defaults.NotificationsLimit
	if limit <= 0 {
		return maxCachedRows
	}
	return min(limit, maxCachedRows)
}

func (m *Model) listCacheFilename() string {
	return listCacheFilename(m.GetSearchValue(), m.Ctx.Config.IncludeReadNotifications)
}

// loadCachedRows shows the rows cached for the section's search, if any,
// until they're refreshed. Rows marked done since are left out.
func (m *Model) loadCachedRows() {
	contents, err := data.ReadCacheFile(m.listCacheFilename())
	if err != nil {
		return
	}
	var cache listCache
	if err := json.Unmarshal(contents, &cache); err != nil || cache.Version != listCacheVersion {
		log.Debug("Ignoring notifications cache", "section", m.Id, "err", err)
		return
	}

	doneStore := data.GetDoneStore()
	notifications := make([]notificationrow.Data, 0, len(cache.Notifications))
	for _, n := range cache.Notifications[:min(len(cache.Notifications), m.numCachedRows())] {
		if !doneStore.IsDone(n.GetId(), n.Notification.UpdatedAt) {
			n.HasDraft = false
			notifications = append(notifications, n)
		}
	}
	if len(notifications) == 0 {
		return
	}

	m.Notifications = notifications
	m.TotalCount = len(notifications)
	m.showingCached = true
	m.Table.SetRows(m.BuildRows())
	m.UpdateTotalItemsCount(m.TotalCount)
	// The pager shows when the rows were fetched, so it's clear they're old
	m.UpdateLastUpdated(cache.SavedAt)
	log.Debug("Loaded cached notifications", "section", m.Id, "count", len(notifications))
}

// saveCachedRows schedules caching the section's rows. Only rows that have
// been fetched are cached, not ones shown from the cache.
func (m *Model) saveCachedRows() {
	if m.PageInfo == nil || m.showingCached {
		return
	}
	rows := m.Notifications[:min(len(m.Notifications), m.numCachedRows())]
	listCacheWrites.schedule(m.listCacheFilename(), listCache{
		Version:       listCacheVersion,
		SavedAt:       m.LastUpdated(),
		Notifications: append([]notificationrow.Data(nil), rows...),
	})
}

// listCacheWriter writes cached rows in the background, coalescing writes
// to the same file.
type listCacheWriter struct {
	mu      sync.Mutex
	pending map[string]listCache
	timer   *time.Timer

	// flushMu serializes flushes, so an older snapshot is never written
	// after a newer one
	flushMu sync.Mutex
}

var listCacheWrites = &listCacheWriter{}

func (w *listCacheWriter) schedule(filename string, cache listCache) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending == nil {
		w.pending = make(map[string]listCache)
	}
	w.pending[filename] = cache
	if w.timer == nil {
		w.timer = time.AfterFunc(listCacheWriteDelay, w.flush)
	}
}

// flush writes all pending caches.
func (w *listCacheWriter) flush() {
	w.flushMu.Lock()
	defer w.flushMu.Unlock()

	w.mu.Lock()
	pending := w.pending
	w.pending = nil
	w.timer = nil
	w.mu.Unlock()

	for filename, cache := range pending {
		contents, err := json.Marshal(cache)
		if err == nil {
			err = data.WriteCacheFile(filename, contents)
		}
		if err != nil {
			log.Debug("Failed to cache notifications", "file", filename, "err", err)
		}
	}
}
