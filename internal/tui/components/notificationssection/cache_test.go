package notificationssection

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	zone "github.com/lrstanley/bubblezone/v2"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/notificationrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func newCacheTestContext(t *testing.T) *context.ProgramContext {
	t.Helper()
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../../../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	require.NoError(t, err)
	ctx := &context.ProgramContext{
		Config:            &cfg,
		MainContentWidth:  120,
		MainContentHeight: 40,
	}
	ctx.Theme = theme.ParseTheme(ctx.Config)
	ctx.Styles = context.InitStyles(ctx.Theme)
	// The table marks its rows for the mouse
	zone.NewGlobal()

	data.SetCacheDirForTesting(t.TempDir())
	t.Cleanup(func() { data.SetCacheDirForTesting("") })
	store := data.NewDoneStoreForTesting(t, filepath.Join(t.TempDir(), "done.db"))
	t.Cleanup(data.OverrideDoneStoreForTesting(store))
	return ctx
}

func testNotification(id, title string, updatedAt time.Time) notificationrow.Data {
	return notificationrow.Data{Notification: data.NotificationData{
		Id:        id,
		Unread:    true,
		UpdatedAt: updatedAt,
		Subject:   data.NotificationSubject{Title: title, Type: data.SubjectTypeIssue},
	}}
}

func notificationIds(m *Model) []string {
	var ids []string
	for _, n := range m.Notifications {
		ids = append(ids, n.GetId())
	}
	return ids
}

func TestCachedRowsShownUntilRefreshed(t *testing.T) {
	ctx := newCacheTestContext(t)
	t1 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	// A previous session fetched and cached its rows
	prev := NewModel(0, ctx, config.NotificationsSectionConfig{}, time.Now())
	prev.Notifications = []notificationrow.Data{
		testNotification("A", "Cached A", t1),
		testNotification("B", "Cached B", t1),
		testNotification("C", "Cached C", t1),
	}
	prev.Notifications[0].Actor = "someone"
	prev.PageInfo = &data.PageInfo{}
	prev.saveCachedRows()
	listCacheWrites.flush()

	// B was marked done since
	data.GetDoneStore().MarkDone("B", t1)

	m := NewModel(0, ctx, config.NotificationsSectionConfig{}, time.Now())
	m.UpdateProgramContext(ctx)
	m.loadCachedRows()
	require.Equal(
		t,
		[]string{"A", "C"},
		notificationIds(&m),
		"cached rows should be shown, without done ones",
	)

	// While refreshing, the cached rows stay visible, rather than a spinner
	m.SetIsLoading(true)
	require.True(t, m.GetIsLoading(), "the section should show it's loading, e.g. in its tab")
	view := m.GetMainContent()
	require.Contains(t, view, "Cached A")
	require.NotContains(t, view, "Loading")

	// Rows shown from the cache aren't cached again
	m.saveCachedRows()
	require.Empty(t, listCacheWrites.pending)

	// The fresh rows replace the cached ones, staying on the same notification
	// and keeping details fetched for it before
	m.NextRow()
	require.Equal(t, "C", m.GetCurrNotification().GetId())
	m.LastFetch.TaskId = "fetch"
	m.Update(SectionNotificationsFetchedMsg{
		TaskId: "fetch",
		Notifications: []notificationrow.Data{
			testNotification("X", "New X", t1.Add(time.Hour)),
			testNotification("A", "Fresh A", t1),
			testNotification("C", "Fresh C", t1),
		},
	})
	require.Equal(t, []string{"X", "A", "C"}, notificationIds(&m))
	require.False(t, m.GetIsLoading())
	require.Equal(
		t,
		"C",
		m.GetCurrNotification().GetId(),
		"the cursor should stay on the same notification",
	)
	require.Equal(
		t,
		"someone",
		m.Notifications[1].Actor,
		"details of an unchanged notification should be kept",
	)
	require.True(t, strings.Contains(m.GetMainContent(), "New X"))

	// The fresh rows are cached for next time
	require.Contains(t, listCacheWrites.pending, m.listCacheFilename())
	listCacheWrites.flush()
	next := NewModel(0, ctx, config.NotificationsSectionConfig{}, time.Now())
	next.loadCachedRows()
	require.Equal(t, []string{"X", "A", "C"}, notificationIds(&next))
}

func TestCachedRowsAreKeptPerSearch(t *testing.T) {
	ctx := newCacheTestContext(t)

	m := NewModel(0, ctx, config.NotificationsSectionConfig{Filters: "reason:mention"}, time.Now())
	m.Notifications = []notificationrow.Data{testNotification("A", "A", time.Now())}
	m.PageInfo = &data.PageInfo{}
	m.saveCachedRows()
	listCacheWrites.flush()

	other := NewModel(
		1,
		ctx,
		config.NotificationsSectionConfig{Filters: "reason:author"},
		time.Now(),
	)
	other.loadCachedRows()
	require.Empty(t, other.Notifications, "another search's rows shouldn't be shown")

	same := NewModel(
		2,
		ctx,
		config.NotificationsSectionConfig{Filters: "reason:mention"},
		time.Now(),
	)
	same.loadCachedRows()
	require.Equal(t, []string{"A"}, notificationIds(&same))
}

func TestNotificationsNearCursor(t *testing.T) {
	ctx := newCacheTestContext(t)
	m := NewModel(0, ctx, config.NotificationsSectionConfig{}, time.Now())
	for _, id := range []string{"0", "1", "2", "3", "4", "5", "6", "7", "8"} {
		m.Notifications = append(m.Notifications, testNotification(id, id, time.Now()))
	}
	m.Table.SetRows(m.BuildRows())
	m.UpdateTotalItemsCount(len(m.Notifications))

	ids := func(rows []notificationrow.Data) []string {
		var ids []string
		for _, r := range rows {
			ids = append(ids, r.GetId())
		}
		return ids
	}
	require.Equal(t, []string{"0", "1", "2", "3", "4", "5"}, ids(m.NotificationsNearCursor(1, 5)))
	m.NextRow()
	m.NextRow()
	require.Equal(
		t,
		[]string{"2", "3", "4", "5", "6", "7", "1"},
		ids(m.NotificationsNearCursor(1, 5)),
		"the current row, then the ones below, then the one above",
	)
	m.LastItem()
	require.True(t, slices.Equal([]string{"8", "7"}, ids(m.NotificationsNearCursor(1, 5))))
}

// cachedModel returns a section showing the given rows from the cache, being
// refreshed by the fetch with task ID "fetch".
func cachedModel(t *testing.T, ctx *context.ProgramContext, rows []notificationrow.Data) *Model {
	t.Helper()
	prev := NewModel(0, ctx, config.NotificationsSectionConfig{}, time.Now())
	prev.Notifications = rows
	prev.PageInfo = &data.PageInfo{}
	prev.saveCachedRows()
	listCacheWrites.flush()

	m := NewModel(0, ctx, config.NotificationsSectionConfig{}, time.Now())
	m.UpdateProgramContext(ctx)
	m.loadCachedRows()
	require.NotNil(t, m.SetIsLoading(true), "the refresh spinner should be started")
	m.LastFetch.TaskId = "fetch"
	return &m
}

func TestRefreshingCachedRowsIsShown(t *testing.T) {
	ctx := newCacheTestContext(t)
	t1 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	m := cachedModel(t, ctx, []notificationrow.Data{testNotification("A", "A", t1)})

	require.Contains(t, m.GetPagerContent(), "Refreshing")

	// A failed refresh leaves the cached rows, marked as such
	m.Update(SectionNotificationsFetchFailedMsg{TaskId: "fetch"})
	require.False(t, m.GetIsLoading())
	require.NotContains(t, m.GetPagerContent(), "Refreshing")
	require.Contains(t, m.GetPagerContent(), "cached")

	// Once refreshed, they're no longer marked as cached
	m.SetIsLoading(true)
	m.Update(SectionNotificationsFetchedMsg{
		TaskId:        "fetch",
		Notifications: []notificationrow.Data{testNotification("A", "A", t1)},
	})
	require.NotContains(t, m.GetPagerContent(), "Refreshing")
	require.NotContains(t, m.GetPagerContent(), "cached")
}

func TestActionsDuringRefreshArentUndone(t *testing.T) {
	ctx := newCacheTestContext(t)
	t1 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	m := cachedModel(t, ctx, []notificationrow.Data{
		testNotification("A", "A", t1),
		testNotification("B", "B", t1),
		testNotification("C", "C", t1),
		testNotification("D", "D", t1),
	})

	// While refreshing, B is marked done, C and D are marked read
	m.Update(UpdateNotificationMsg{Id: "B", IsRemoved: true})
	require.True(t, m.GetIsLoading(), "the section should still be refreshing")
	m.Update(UpdateNotificationReadStateMsg{Id: "C", Unread: false})
	m.Update(UpdateNotificationReadStateMsg{Id: "D", Unread: false})

	// The refresh was fetched before those took effect, and D was updated since
	m.Update(SectionNotificationsFetchedMsg{
		TaskId: "fetch",
		Notifications: []notificationrow.Data{
			testNotification("A", "A", t1),
			testNotification("B", "B", t1),
			testNotification("C", "C", t1),
			testNotification("D", "D", t1.Add(time.Hour)),
		},
	})
	require.Equal(t, []string{"A", "C", "D"}, notificationIds(m), "B should stay done")
	require.False(t, m.Notifications[1].Notification.Unread, "C should stay read")
	require.True(t, m.Notifications[2].Notification.Unread, "D was updated since it was read")
}

func TestCursorStaysInPlaceWhenRefreshed(t *testing.T) {
	ctx := newCacheTestContext(t)
	t1 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	var cached []notificationrow.Data
	for i := range 15 {
		cached = append(cached, testNotification(fmt.Sprint(i), fmt.Sprintf("Title %02d", i), t1))
	}
	m := cachedModel(t, ctx, cached)

	lineOf := func(title string) int {
		for i, line := range strings.Split(m.GetMainContent(), "\n") {
			if strings.Contains(line, title) {
				return i
			}
		}
		return -1
	}

	// Scroll down past the first page, then back up a little, so the
	// cursor is in the middle of the view
	for range 12 {
		m.NextRow()
	}
	for range 3 {
		m.PrevRow()
	}
	require.Equal(t, "9", m.GetCurrNotification().GetId())
	line := lineOf("Title 09")
	require.Greater(t, line, lineOf("Title 08"), "the row above should be shown")

	// New ones came in, and one above the cursor was marked done elsewhere
	var fresh []notificationrow.Data
	for i := range 6 {
		fresh = append(fresh, testNotification(fmt.Sprint("new", i), fmt.Sprint("New ", i), t1.Add(time.Hour)))
	}
	for _, n := range cached {
		if n.GetId() != "3" {
			fresh = append(fresh, n)
		}
	}
	m.Update(SectionNotificationsFetchedMsg{TaskId: "fetch", Notifications: fresh})
	require.Equal(t, "9", m.GetCurrNotification().GetId())
	require.Equal(t, line, lineOf("Title 09"), "the selected row shouldn't move on screen")

	// When the current one is gone, the cursor stays put on the next one
	m.Update(UpdateNotificationMsg{Id: "9", IsRemoved: true})
	require.Equal(t, "10", m.GetCurrNotification().GetId())
	require.Equal(t, line, lineOf("Title 10"))
}

func TestRefreshKeepsTheOpenNotification(t *testing.T) {
	ctx := newCacheTestContext(t)
	t1 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	m := cachedModel(t, ctx, []notificationrow.Data{
		testNotification("A", "A", t1.Add(2*time.Hour)),
		testNotification("B", "B", t1.Add(time.Hour)),
		testNotification("C", "C", t1),
	})

	// A is opened, which marks it read, and C is read too
	m.Update(UpdateNotificationReadStateMsg{Id: "A", Unread: false})
	m.Update(UpdateNotificationReadStateMsg{Id: "C", Unread: false})

	// The refresh left them out, as it started before they were read. A new
	// one came in too.
	m.Update(SectionNotificationsFetchedMsg{
		TaskId: "fetch",
		Notifications: []notificationrow.Data{
			testNotification("X", "X", t1.Add(3*time.Hour)),
			testNotification("B", "B", t1.Add(time.Hour)),
		},
	})
	require.Equal(t, []string{"X", "A", "B", "C"}, notificationIds(m))
	require.Equal(t, "A", m.GetCurrNotification().GetId(), "the open notification should stay selected")
}
