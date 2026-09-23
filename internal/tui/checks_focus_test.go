package tui

import (
	"encoding/json"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/footer"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issueview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/notificationrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/notificationssection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/notificationview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/sidebar"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

// prWithChecksJSON is a PR's last commit with a failed and a passed check.
const prWithChecksJSON = `{"Commits": {"TotalCount": 1, "Nodes": [{"Commit": {
	"StatusCheckRollup": {"State": "FAILURE", "Contexts": {
		"TotalCount": 2,
		"CheckRunCountsByState": [{"State": "FAILURE", "Count": 1}, {"State": "SUCCESS", "Count": 1}],
		"Nodes": [
			{"Typename": "CheckRun", "CheckRun": {"Name": "lint", "Status": "COMPLETED",
				"Conclusion": "SUCCESS", "Url": "https://github.com/o/r/runs/1"}},
			{"Typename": "CheckRun", "CheckRun": {"Name": "build", "Status": "COMPLETED",
				"Conclusion": "FAILURE", "Title": "3 errors",
				"DetailsUrl": "https://github.com/o/r/actions/runs/1/job/2"}}
		]}}}}]}}`

func TestNotificationView_FocusedCheckExpandsAndEnterOpensIt(t *testing.T) {
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	require.NoError(t, err)

	ctx := &context.ProgramContext{
		Config:              &cfg,
		View:                config.NotificationsView,
		MainContentHeight:   40,
		DynamicPreviewWidth: 64,
		ScreenWidth:         140,
		ScreenHeight:        50,
		StartTask:           func(context.Task) tea.Cmd { return nil },
	}
	ctx.Theme = theme.ParseTheme(ctx.Config)
	ctx.Styles = context.InitStyles(ctx.Theme)

	sidebarModel := sidebar.NewModel()
	sidebarModel.IsOpen = true
	sidebarModel.UpdateProgramContext(ctx)

	m := Model{
		ctx:              ctx,
		keys:             keys.Keys,
		prView:           prview.NewModel(ctx),
		sidebar:          sidebarModel,
		issueSidebar:     issueview.NewModel(ctx),
		notificationView: notificationview.NewModel(ctx),
		footer:           footer.NewModel(ctx),
	}

	notifications := notificationssection.NewModel(0, ctx, config.NotificationsSectionConfig{}, time.Now())
	notifications.Notifications = []notificationrow.Data{
		{Notification: data.NotificationData{Id: "test-notification-id"}},
	}
	notifications.Table.SetRows(notifications.BuildRows())
	m.notifications = []section.Section{&notifications}

	prData := data.PullRequestData{Title: "A PR", State: "OPEN", Url: "https://github.com/o/r/pull/1"}
	var enriched data.EnrichedPullRequestData
	require.NoError(t, json.Unmarshal([]byte(prWithChecksJSON), &enriched))
	pr := &prrow.Data{Primary: &prData, Enriched: enriched, IsEnriched: true}
	m.notificationView.SetSubjectPR(pr, "test-notification-id")
	m.prView.SetRow(pr)
	m.prView.SetWidth(60)
	for !m.prView.IsChecksTab() {
		m.prView.NextTab()
	}
	m.setSidebarPRContent()

	m.updateSidebarHints()
	focused := func() int {
		t.Helper()
		check, ok := m.focusedCheck()
		require.True(t, ok, "a check should always be focused")
		return check
	}

	// Failed checks are listed first, and entering the tab focuses the first
	require.Equal(t, 0, focused())
	view := ansi.Strip(m.sidebar.View())
	require.Contains(t, view, "3 errors", "the focused check should show its details")
	require.Contains(t, view, "actions/runs/1/job/2")
	require.NotContains(t, view, "runs/1\n", "the unfocused check should be collapsed")
	require.False(t, m.hasFocusedComment(), "a check isn't a comment to reply to")
	require.Contains(t, view, keys.HintKeys(keys.NotificationKeys.OpenCheck)+" open",
		"the focused check should hint at its key")

	m.Update(tea.KeyPressMsg{Text: "j"})
	require.Equal(t, 1, focused())
	view = ansi.Strip(m.sidebar.View())
	require.NotContains(t, view, "3 errors", "the unfocused check should collapse")
	require.Contains(t, view, "github.com/o/r/runs/1")

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd, "enter should open the check in the browser")
	require.NotNil(t, m.notificationView.GetSubjectPR(), "enter should not close the notification")
	require.True(t, m.prView.IsChecksTab())

	// Coming back to the tab, the check that was focused is focused again
	m.Update(tea.KeyPressMsg{Text: "h"})
	m.Update(tea.KeyPressMsg{Text: "l"})
	require.True(t, m.prView.IsChecksTab())
	require.Equal(t, 1, focused())
}
