package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone/v2"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/footer"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issueview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/notificationrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/notificationssection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/notificationview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/sidebar"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tabs"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

// newPrefetchTestModel returns a notifications view, with the preview open,
// listing n PR notifications. Their PRs are in a repo named after the test,
// so they don't collide with other tests' in the shared subject cache.
func newPrefetchTestModel(t *testing.T, n int) *Model {
	t.Helper()
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	require.NoError(t, err)

	ctx := &context.ProgramContext{
		Config:       &cfg,
		ScreenWidth:  120,
		ScreenHeight: 40,
		View:         config.NotificationsView,
		StartTask:    func(task context.Task) tea.Cmd { return nil },
	}
	ctx.Theme = theme.ParseTheme(ctx.Config)
	ctx.Styles = context.InitStyles(ctx.Theme)
	zone.NewGlobal()

	repo := "owner/" + strings.ReplaceAll(t.Name(), "/", "-")
	notifications := notificationssection.NewModel(
		0,
		ctx,
		config.NotificationsSectionConfig{},
		time.Now(),
	)
	for i := range n {
		notifications.Notifications = append(notifications.Notifications, notificationrow.Data{
			Notification: data.NotificationData{
				Id:        fmt.Sprintf("notif-%d", i),
				Unread:    true,
				UpdatedAt: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
				Subject: data.NotificationSubject{
					Title: fmt.Sprintf("PR %d", i),
					Url:   fmt.Sprintf("https://api.github.com/repos/%s/pulls/%d", repo, i+1),
					Type:  data.SubjectTypePullRequest,
				},
				Repository: data.NotificationRepository{FullName: repo},
			},
		})
	}
	notifications.Table.SetRows(notifications.BuildRows())
	notifications.UpdateTotalItemsCount(n)

	m := &Model{
		ctx:              ctx,
		keys:             keys.Keys,
		notifications:    []section.Section{&notifications},
		sidebar:          sidebar.NewModel(),
		footer:           footer.NewModel(ctx),
		tabs:             tabs.NewModel(ctx),
		prView:           prview.NewModel(ctx),
		issueSidebar:     issueview.NewModel(ctx),
		notificationView: notificationview.NewModel(ctx),
		tasks:            map[string]context.Task{},
	}
	m.sidebar.IsOpen = true
	m.sidebar.UpdateProgramContext(ctx)
	m.syncMainContentDimensions()
	m.updateTabs()
	m.syncProgramContext()
	return m
}

func (m *Model) testNotification(i int) *notificationrow.Data {
	return &m.notifications[0].(*notificationssection.Model).Notifications[i]
}

// cacheTestPR puts a PR for the i'th notification in the subject cache, as
// if it was fetched ahead of time.
func (m *Model) cacheTestPR(i int, title string) {
	row := m.testNotification(i)
	data.GetSubjectCache().Put(row.GetUrl(), data.CachedSubject{
		PR:        &data.EnrichedPullRequestData{Title: title, Url: row.GetUrl()},
		UpdatedAt: row.Notification.UpdatedAt,
	})
}

func TestOpeningNotificationNotFetchedYetShowsLoadingFullscreen(t *testing.T) {
	m := newPrefetchTestModel(t, 3)
	require.False(t, m.ctx.PreviewFullscreen)

	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.True(t, m.notificationView.IsLoading(), "the PR should be loading")
	require.True(t, m.ctx.PreviewFullscreen, "the preview should take over the screen right away")
	require.Equal(t, m.openNotificationContentWidth(), m.sidebar.GetSidebarContentWidth())
	require.Contains(t, ansi.Strip(m.View().Content), "Loading pull request")

	// Other updates, e.g. a refresh finishing, don't stop it loading
	m.syncSidebar()
	require.True(t, m.notificationView.IsLoading())
	require.True(t, m.ctx.PreviewFullscreen)

	// Once fetched, the PR is shown
	m.cacheTestPR(0, "The fetched PR")
	row := m.testNotification(0)
	m.Update(notificationSubjectFetchedMsg{NotificationId: row.GetId(), Url: row.GetUrl()})
	require.False(t, m.notificationView.IsLoading())
	require.NotNil(t, m.notificationView.GetSubjectPR())
	require.Equal(t, "The fetched PR", m.notificationView.GetSubjectPR().Enriched.Title)
	require.True(t, m.ctx.PreviewFullscreen)
	require.Contains(t, ansi.Strip(m.View().Content), "The fetched PR")
}

func TestOpeningPrefetchedNotificationShowsItRightAway(t *testing.T) {
	m := newPrefetchTestModel(t, 3)
	m.cacheTestPR(0, "The prefetched PR")

	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.False(t, m.notificationView.IsLoading(), "a prefetched PR shouldn't load")
	require.NotNil(t, m.notificationView.GetSubjectPR())
	require.Equal(t, "The prefetched PR", m.notificationView.GetSubjectPR().Enriched.Title)
	require.True(t, m.ctx.PreviewFullscreen)
	require.Equal(t, m.openNotificationContentWidth(), m.sidebar.GetSidebarContentWidth(),
		"it should be shown at the width it was prerendered at")
	require.False(t, m.testNotification(0).Notification.Unread, "opening it should mark it as read")
}

func TestLeavingNotificationWhileLoading(t *testing.T) {
	m := newPrefetchTestModel(t, 3)

	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.True(t, m.ctx.PreviewFullscreen)

	// j scrolls the loading preview rather than moving to the next
	// notification behind it
	m.Update(tea.KeyPressMsg{Text: "j"})
	require.Equal(t, 0, m.getCurrSection().CurrRow())
	require.True(t, m.notificationView.IsLoading())

	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.False(t, m.notificationView.IsLoading(), "esc should stop waiting for the PR")
	require.False(t, m.ctx.PreviewFullscreen, "esc should go back to the list")

	// It isn't opened when it arrives after all
	m.cacheTestPR(0, "The PR")
	row := m.testNotification(0)
	m.Update(notificationSubjectFetchedMsg{NotificationId: row.GetId(), Url: row.GetUrl()})
	require.Nil(t, m.notificationView.GetSubjectPR())
	require.False(t, m.ctx.PreviewFullscreen)
}

func TestFailedFetchWhileLoadingShowsError(t *testing.T) {
	m := newPrefetchTestModel(t, 1)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	row := m.testNotification(0)
	m.Update(notificationSubjectFetchedMsg{
		NotificationId: row.GetId(), Url: row.GetUrl(), Err: errors.New("offline"),
	})
	require.False(t, m.notificationView.IsLoading())
	require.False(t, m.ctx.PreviewFullscreen)
	require.EqualError(t, m.ctx.Error, "offline")
}

func TestPrefetchesNotificationsAroundCursor(t *testing.T) {
	m := newPrefetchTestModel(t, 10)
	url := func(i int) string { return m.testNotification(i).GetUrl() }

	require.NotNil(t, m.prefetchNotificationSubjects())
	for i := range 6 {
		require.True(t, m.prefetching[url(i)], "notification %d should be prefetched", i)
	}
	require.False(t, m.prefetching[url(6)], "notifications further down can wait")
	require.Nil(
		t,
		m.prefetchNotificationSubjects(),
		"notifications being fetched aren't fetched again",
	)

	// Moving down even once starts on the next one
	m.Update(tea.KeyPressMsg{Text: "j"})
	require.True(t, m.prefetching[url(6)])
	require.False(t, m.prefetching[url(7)])
}

func TestPrefetchedSubjectIsPrerenderedOnce(t *testing.T) {
	m := newPrefetchTestModel(t, 1)
	m.cacheTestPR(0, "The PR")
	row := m.testNotification(0)

	cmd := m.onNotificationSubjectFetched(notificationSubjectFetchedMsg{
		NotificationId: row.GetId(), Url: row.GetUrl(),
	})
	require.NotNil(t, cmd, "a prefetched PR should be rendered ahead of time")
	require.Nil(t, cmd())
	require.Nil(t, m.prefetchNotificationSubjects(), "it shouldn't be fetched or rendered again")
}

func TestFailedPrefetchIsRetriedLater(t *testing.T) {
	m := newPrefetchTestModel(t, 1)
	row := m.testNotification(0)

	require.NotNil(t, m.prefetchNotificationSubjects())
	m.onNotificationSubjectFetched(notificationSubjectFetchedMsg{
		NotificationId: row.GetId(), Url: row.GetUrl(), Err: errors.New("offline"),
	})
	require.Nil(t, m.ctx.Error, "failing to fetch ahead of time isn't worth an error")
	require.Nil(t, m.prefetchNotificationSubjects(), "it shouldn't be retried right away")

	m.prefetchFailed[row.GetUrl()] = time.Now().Add(-prefetchRetryDelay)
	require.NotNil(t, m.prefetchNotificationSubjects(), "it should be retried later")
}

func TestRefreshWhileNotificationIsOpenKeepsIt(t *testing.T) {
	m := newPrefetchTestModel(t, 3)
	m.cacheTestPR(0, "The first PR")
	m.cacheTestPR(1, "The second PR")
	notifications := m.notifications[0].(*notificationssection.Model)
	notifications.LastFetch.TaskId = "fetch"
	m.tasks["fetch"] = context.Task{Id: "fetch"}

	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, "notif-0", m.notificationView.GetSubjectId())

	// A refresh that started before it was opened, so before it was read,
	// lands without it, and with a new one on top
	fresh := []notificationrow.Data{*m.testNotification(1), *m.testNotification(2)}
	fresh[0].Notification.Id, fresh[1].Notification.Id = "notif-new", "notif-1"
	fresh[0].Notification.UpdatedAt = fresh[0].Notification.UpdatedAt.Add(time.Hour)
	m.Update(constants.TaskFinishedMsg{
		SectionId:   0,
		SectionType: notificationssection.SectionType,
		TaskId:      "fetch",
		Msg: notificationssection.SectionNotificationsFetchedMsg{
			TaskId:        "fetch",
			Notifications: fresh,
		},
	})

	row, ok := m.getCurrRowData().(*notificationrow.Data)
	require.True(t, ok)
	require.Equal(t, "notif-0", row.GetId(), "the open notification should stay selected")
	require.Equal(t, "notif-0", m.notificationView.GetSubjectId())
	require.Equal(t, "The first PR", m.notificationView.GetSubjectPR().Enriched.Title)
	require.True(t, m.ctx.PreviewFullscreen)
	require.Contains(t, ansi.Strip(m.View().Content), "The first PR")
}

func TestKeysNeverActOnAnotherNotificationsSubject(t *testing.T) {
	m := newPrefetchTestModel(t, 3)
	m.cacheTestPR(0, "The first PR")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, "notif-0", m.notificationView.GetSubjectId())

	// Should the cursor ever end up on another notification behind the open
	// one, its PR's actions, e.g. its diff, are no longer available
	m.getCurrSection().(*notificationssection.Model).Table.SelectItem(1)
	m.Update(tea.KeyPressMsg{Text: keys.PRKeys.Diff.Keys()[0]})
	require.Nil(t, m.notificationView.GetSubjectPR())
}
