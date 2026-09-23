package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	log "charm.land/log/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issuessection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prssection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tasks"
)

const (
	// commentWatchInterval is how often a watched PR/Issue is checked for a
	// reply. Checks that find nothing new don't count against the rate limit.
	commentWatchInterval = 5 * time.Second
	// commentWatchTimeout is how long to wait for a reply
	commentWatchTimeout = 10 * time.Minute
	// commentWatchClockSkew is how far back comments are listed, so the
	// posted comment is found even if the local clock is ahead of GitHub's
	commentWatchClockSkew = 5 * time.Minute
)

// commentWatch waits for a reply, e.g. from a bot, to a comment just posted
type commentWatch struct {
	id       int // tells apart the messages of a replaced watch
	subject  data.RowData
	isPR     bool
	body     string
	postedAt time.Time
	deadline time.Time
	watcher  *data.CommentWatcher
}

type commentWatchTickMsg struct{ id int }

type commentWatchCheckedMsg struct {
	id       int
	comments []data.WatchedComment
	changed  bool
	err      error
}

// commentReplyMsg carries the PR/Issue refetched after a reply to it came in
type commentReplyMsg struct {
	url   string
	PR    *data.EnrichedPullRequestData
	Issue *data.IssueData
}

// watchForReply starts waiting for a reply if msg is a posted comment,
// replacing any earlier watch
func (m *Model) watchForReply(msg tea.Msg) tea.Cmd {
	w := commentWatch{postedAt: time.Now()}
	switch msg := msg.(type) {
	case tasks.UpdatePRMsg:
		if msg.PostedComment == nil || msg.CommentedOn == nil {
			return nil
		}
		w.subject, w.isPR, w.body = msg.CommentedOn, true, msg.PostedComment.Body
	case tasks.UpdateIssueMsg:
		if msg.PostedComment == nil || msg.CommentedOn == nil {
			return nil
		}
		w.subject, w.body = msg.CommentedOn, msg.PostedComment.Body
	default:
		return nil
	}

	watcher, err := data.NewCommentWatcher(
		w.subject.GetRepoNameWithOwner(),
		w.subject.GetNumber(),
		w.postedAt.Add(-commentWatchClockSkew),
	)
	if err != nil {
		log.Error("Failed to watch for a reply", "err", err)
		return nil
	}
	w.watcher = watcher
	w.deadline = w.postedAt.Add(commentWatchTimeout)
	if m.commentWatch != nil {
		w.id = m.commentWatch.id + 1
	}
	m.commentWatch = &w
	return m.checkCommentWatch()
}

func (m *Model) checkCommentWatch() tea.Cmd {
	w := m.commentWatch
	return func() tea.Msg {
		comments, changed, err := w.watcher.Check()
		return commentWatchCheckedMsg{id: w.id, comments: comments, changed: changed, err: err}
	}
}

// updateCommentWatch handles the watch's messages
func (m *Model) updateCommentWatch(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case commentWatchTickMsg:
		if m.commentWatch == nil || m.commentWatch.id != msg.id {
			return nil
		}
		return m.checkCommentWatch()

	case commentWatchCheckedMsg:
		w := m.commentWatch
		if w == nil || w.id != msg.id {
			return nil
		}
		if msg.err != nil {
			log.Error("Failed to check for a reply", "err", msg.err)
			m.commentWatch = nil
			return nil
		}
		if msg.changed && w.hasReply(msg.comments, m.ctx.User) {
			m.commentWatch = nil
			return w.fetchSubject()
		}
		if time.Now().After(w.deadline) {
			m.commentWatch = nil
			return nil
		}
		return tea.Tick(commentWatchInterval, func(time.Time) tea.Msg {
			return commentWatchTickMsg{id: msg.id}
		})
	}
	return nil
}

// hasReply reports whether someone else created or edited a comment after the
// posted comment, e.g. a bot updating its status comment. Times are compared
// to the posted comment's, so they're all by GitHub's clock.
func (w *commentWatch) hasReply(comments []data.WatchedComment, user string) bool {
	postedAt := w.postedAt
	for i := len(comments) - 1; i >= 0; i-- {
		c := comments[i]
		if c.User.Login == user && strings.TrimSpace(c.Body) == strings.TrimSpace(w.body) {
			postedAt = c.CreatedAt
			break
		}
	}
	for _, c := range comments {
		if c.User.Login != user && c.UpdatedAt.After(postedAt) {
			return true
		}
	}
	return false
}

func (w *commentWatch) fetchSubject() func() tea.Msg {
	url, isPR := w.subject.GetUrl(), w.isPR
	return func() tea.Msg {
		reply := commentReplyMsg{url: url}
		if isPR {
			pr, err := data.FetchPullRequest(url)
			if err != nil {
				log.Error("Failed to fetch PR after a reply", "url", url, "err", err)
				return nil
			}
			reply.PR = &pr
		} else {
			issue, err := data.FetchIssue(url)
			if err != nil {
				log.Error("Failed to fetch issue after a reply", "url", url, "err", err)
				return nil
			}
			reply.Issue = &issue
		}
		return reply
	}
}

// applyCommentReply shows the refetched PR/Issue wherever it's shown
func (m *Model) applyCommentReply(reply commentReplyMsg) {
	if reply.PR != nil {
		for _, s := range m.prs {
			if s, ok := s.(*prssection.Model); ok {
				for i := range s.Prs {
					if s.Prs[i].Primary != nil && s.Prs[i].Primary.Url == reply.url {
						s.Prs[i].Enriched = *reply.PR
						s.Prs[i].IsEnriched = true
					}
				}
				s.Table.SetRows(s.BuildRows())
			}
		}
		m.prView.SetEnrichedPR(*reply.PR)
		if pr := m.notificationView.GetSubjectPR(); pr != nil && pr.Primary.Url == reply.url {
			prData := reply.PR.ToPullRequestData()
			m.notificationView.SetSubjectPR(&prrow.Data{
				Primary:    &prData,
				Enriched:   *reply.PR,
				IsEnriched: true,
			}, m.notificationView.GetSubjectId())
		}
	}
	if reply.Issue != nil {
		for _, s := range m.issues {
			if s, ok := s.(*issuessection.Model); ok {
				for i := range s.Issues {
					if s.Issues[i].Url == reply.url {
						s.Issues[i] = *reply.Issue
					}
				}
				s.Table.SetRows(s.BuildRows())
			}
		}
		if issue := m.notificationView.GetSubjectIssue(); issue != nil && issue.Url == reply.url {
			m.notificationView.SetSubjectIssue(reply.Issue, m.notificationView.GetSubjectId())
		}
	}
}

// scrollToLatestComment scrolls the sidebar down to the latest comment, e.g.
// a reply or a comment being posted, if it's showing the PR/Issue at url
func (m *Model) scrollToLatestComment(url string) {
	if m.sidebarShows(url) {
		m.sidebar.ScrollToBottom()
		m.sidebar.FocusLast()
	}
}

// sidebarShows reports whether the sidebar shows the PR/Issue at url
func (m *Model) sidebarShows(url string) bool {
	if !m.sidebar.IsOpen {
		return false
	}
	shown := false
	if row := m.getCurrRowData(); row != nil && row.GetUrl() == url {
		shown = true
	}
	if pr := m.notificationView.GetSubjectPR(); pr != nil && pr.Primary.Url == url {
		shown = m.isNotificationSubjectShown()
	}
	if issue := m.notificationView.GetSubjectIssue(); issue != nil && issue.Url == url {
		shown = m.isNotificationSubjectShown()
	}
	return shown
}

// syncPostingStatus shows a spinner in the sidebar's pager while a comment
// is being posted on the PR/Issue it shows, since the grayed out comment
// may be scrolled out of view
func (m *Model) syncPostingStatus() {
	status := ""
	for _, url := range m.ctx.PendingComments {
		if m.sidebarShows(url) {
			status = m.taskSpinner.View() + " posting comment"
			break
		}
	}
	m.sidebar.SetStatus(status)
}
