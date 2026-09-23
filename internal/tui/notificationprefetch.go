package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/log/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issueview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/notificationrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/notificationssection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
)

const (
	// prefetchAhead and prefetchBehind are how many notifications below and
	// above the current one have their PR/Issue fetched and rendered ahead of
	// being opened, so opening them is instant.
	prefetchAhead  = 5
	prefetchBehind = 1

	// prefetchRetryDelay is how long to hold off fetching a notification's
	// PR/Issue ahead of time again after it failed.
	prefetchRetryDelay = 30 * time.Second

	// maxPrerendered bounds how many prerendered subjects are remembered.
	maxPrerendered = 1000
)

// prerenderKey identifies a notification subject rendered ahead of time, as
// fetched at a given time, for a given width and color scheme.
type prerenderKey struct {
	url       string
	fetchedAt time.Time
	width     int
	dark      bool
}

// initPrefetchState makes the maps tracking what's fetched and rendered ahead
// of time.
func (m *Model) initPrefetchState() {
	if m.prefetching == nil {
		m.prefetching = make(map[string]bool)
		m.prefetchFailed = make(map[string]time.Time)
		m.prerendered = make(map[prerenderKey]bool)
	}
}

// prefetchNotificationSubjects fetches and renders the PRs/Issues of the
// notifications around the current one, which are likely to be opened next.
// Those already fetched or being fetched are skipped, so it's cheap to call
// on every update.
func (m *Model) prefetchNotificationSubjects() tea.Cmd {
	if m.ctx.Config == nil || m.ctx.View != config.NotificationsView {
		return nil
	}
	section, ok := m.getCurrSection().(*notificationssection.Model)
	if !ok || section == nil {
		return nil
	}

	m.initPrefetchState()
	var cmds []tea.Cmd
	for _, row := range section.NotificationsNearCursor(prefetchBehind, prefetchAhead) {
		cmds = append(cmds, m.prefetchNotificationSubject(row))
	}
	return tea.Batch(cmds...)
}

func (m *Model) prefetchNotificationSubject(row notificationrow.Data) tea.Cmd {
	subjectType := row.GetSubjectType()
	if subjectType != data.SubjectTypePullRequest && subjectType != data.SubjectTypeIssue {
		return nil
	}

	url := row.GetUrl()
	subject, ok := data.GetSubjectCache().Get(url, row.Notification.UpdatedAt)
	if ok && time.Since(subject.FetchedAt) < data.SubjectMaxAge {
		return m.prerenderNotificationSubject(url, subject)
	}
	if m.prefetching[url] || time.Since(m.prefetchFailed[url]) < prefetchRetryDelay {
		return nil
	}
	m.prefetching[url] = true
	return fetchNotificationSubject(row)
}

// fetchNotificationSubject fetches a notification's PR/Issue into the
// subject cache, or waits for it if it's already being fetched.
func fetchNotificationSubject(row notificationrow.Data) tea.Cmd {
	msg := notificationSubjectFetchedMsg{
		NotificationId:   row.GetId(),
		Url:              row.GetUrl(),
		LatestCommentUrl: row.GetLatestCommentUrl(),
	}
	updatedAt := row.Notification.UpdatedAt
	isPR := row.GetSubjectType() == data.SubjectTypePullRequest
	return func() tea.Msg {
		cache := data.GetSubjectCache()
		if isPR {
			_, msg.Err = cache.FetchPR(msg.Url, updatedAt, data.SubjectMaxAge)
		} else {
			_, msg.Err = cache.FetchIssue(msg.Url, updatedAt, data.SubjectMaxAge)
		}
		return msg
	}
}

// onNotificationSubjectFetched shows a fetched PR/Issue if its notification
// is waiting for it, and renders it ahead of time otherwise.
func (m *Model) onNotificationSubjectFetched(msg notificationSubjectFetchedMsg) tea.Cmd {
	m.initPrefetchState()
	delete(m.prefetching, msg.Url)
	waiting := m.notificationView.GetLoadingId() == msg.NotificationId

	if msg.Err != nil {
		m.prefetchFailed[msg.Url] = time.Now()
		if waiting {
			m.stopNotificationLoading(msg.NotificationId)
			log.Error("failed fetching notification subject", "url", msg.Url, "err", msg.Err)
			m.ctx.Error = msg.Err
		}
		return nil
	}
	delete(m.prefetchFailed, msg.Url)

	subject, ok := data.GetSubjectCache().Get(msg.Url, time.Time{})
	if !ok {
		return nil
	}
	if waiting {
		m.stopNotificationLoading(msg.NotificationId)
		m.showNotificationSubject(msg.NotificationId, subject, msg.LatestCommentUrl)
		return nil
	}
	return m.prerenderNotificationSubject(msg.Url, subject)
}

// prerenderNotificationSubject renders the markdown of a notification's
// PR/Issue in the background, at the width it's shown at once opened, so
// showing it is instant.
func (m *Model) prerenderNotificationSubject(url string, subject data.CachedSubject) tea.Cmd {
	width := m.openNotificationContentWidth()
	if width <= 0 {
		return nil
	}
	key := prerenderKey{
		url:       url,
		fetchedAt: subject.FetchedAt,
		width:     width,
		dark:      m.ctx.HasDarkBackground,
	}
	if m.prerendered[key] {
		return nil
	}
	if len(m.prerendered) >= maxPrerendered {
		clear(m.prerendered)
	}
	m.prerendered[key] = true

	var prerender func()
	switch {
	case subject.PR != nil:
		prerender = prview.MarkdownPrerenderer(m.ctx, subject.PR, width)
	case subject.Issue != nil:
		prerender = issueview.MarkdownPrerenderer(m.ctx, subject.Issue, width)
	default:
		return nil
	}
	return func() tea.Msg {
		prerender()
		return nil
	}
}

// openNotificationContentWidth is the width of the preview's content once a
// notification is open, taking up the whole screen.
func (m *Model) openNotificationContentWidth() int {
	return m.ctx.ScreenWidth - m.ctx.Styles.Sidebar.BorderWidth
}

// showNotificationSubject opens a notification's fetched PR or Issue.
func (m *Model) showNotificationSubject(
	notificationId string,
	subject data.CachedSubject,
	latestCommentUrl string,
) {
	switch {
	case subject.PR != nil:
		m.showNotificationPR(notificationId, *subject.PR, latestCommentUrl)
	case subject.Issue != nil:
		m.showNotificationIssue(notificationId, *subject.Issue, latestCommentUrl)
	}
}

func (m *Model) showNotificationPR(
	notificationId string,
	pr data.EnrichedPullRequestData,
	latestCommentUrl string,
) {
	// Convert enriched PR to prrow.Data for display
	prData := pr.ToPullRequestData()
	m.notificationView.SetSubjectPR(&prrow.Data{
		Primary:    &prData,
		Enriched:   pr,
		IsEnriched: true,
	}, notificationId)
	keys.SetNotificationSubject(keys.NotificationSubjectPR)
	// Take over the screen before rendering, so it's rendered only once, at
	// the width it was rendered at ahead of time
	m.syncFullscreenLayout()
	width := m.sidebar.GetSidebarContentWidth()
	m.prView.SetSectionId(0)
	m.prView.SetRow(m.notificationView.GetSubjectPR())
	m.prView.SetWidth(width)
	m.prView.SetEnrichedPR(pr)
	m.restoreDraft(notificationId, true)
	// Switch to Activity tab and scroll to bottom if there's a latest comment
	// (indicates there's new activity to show)
	if latestCommentUrl != "" {
		m.prView.GoToActivityTab()
		m.setSidebarPRContent()
		m.sidebar.ScrollToBottom()
	} else {
		// For notifications without comments (new PRs, state changes, etc.)
		// show the Overview tab without scrolling
		m.prView.GoToFirstTab()
		m.setSidebarPRContent()
	}
	m.markNotificationAsRead(notificationId)
}

func (m *Model) showNotificationIssue(
	notificationId string,
	issue data.IssueData,
	latestCommentUrl string,
) {
	m.notificationView.SetSubjectIssue(&issue, notificationId)
	keys.SetNotificationSubject(keys.NotificationSubjectIssue)
	m.syncFullscreenLayout()
	width := m.sidebar.GetSidebarContentWidth()
	m.issueSidebar.SetSectionId(0)
	m.issueSidebar.SetRow(m.notificationView.GetSubjectIssue())
	m.issueSidebar.SetWidth(width)
	m.restoreDraft(notificationId, false)
	m.setSidebarIssueContent()
	// Scroll to bottom if there's a latest comment (indicates new activity)
	if latestCommentUrl != "" {
		m.sidebar.ScrollToBottom()
	}
	m.markNotificationAsRead(notificationId)
}

// syncFullscreenLayout lays out the screen for whether a notification is open,
// without re-rendering the preview.
func (m *Model) syncFullscreenLayout() {
	if m.ctx.Config == nil || m.ctx.PreviewFullscreen == m.isNotificationOpen() {
		return
	}
	m.syncMainContentDimensions()
	m.syncProgramContext()
}
