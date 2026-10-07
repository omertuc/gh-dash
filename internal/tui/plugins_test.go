package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/plugins"
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

// prWithProwJobJSON is a PR whose last commit has a Prow job's status and a
// check the fake plugin opens.
const prWithProwJobJSON = `{"Commits": {"TotalCount": 1, "Nodes": [{"Commit": {
	"StatusCheckRollup": {"State": "FAILURE", "Contexts": {
		"TotalCount": 2,
		"Nodes": [
			{"Typename": "StatusContext", "StatusContext": {"Context": "ci/prow/e2e-aws",
				"State": "FAILURE",
				"TargetUrl": "https://prow.ci.openshift.org/view/gs/bucket/pr-logs/pull/1/pull-ci-e2e-aws/9"}},
			{"Typename": "CheckRun", "CheckRun": {"Name": "fake", "Status": "COMPLETED",
				"Conclusion": "SUCCESS", "DetailsUrl": "https://fake.example.com/1"}}
		]}}}}]}}`

// fakeView lists a button that loads a row when pressed, and a row.
type fakeView struct {
	loaded  bool
	focused []int
}

type fakeLoadedMsg struct{}

func (v *fakeView) Title() string { return "Fake" }
func (v *fakeView) Init() tea.Cmd { return nil }
func (v *fakeView) Focus(i int) bool {
	v.focused = append(v.focused, i)
	return false
}

func (v *fakeView) Update(msg tea.Msg) tea.Cmd {
	if _, ok := msg.(fakeLoadedMsg); ok {
		v.loaded = true
	}
	return nil
}

func (v *fakeView) Activate(i int) tea.Cmd {
	if i == 0 {
		return func() tea.Msg { return fakeLoadedMsg{} }
	}
	return nil
}

func (v *fakeView) Render(env plugins.Env, width int) (string, []plugins.Item) {
	lines := []string{"Fake view", "", "[Load]", "Row"}
	items := []plugins.Item{{Line: 2, Hint: "press", Clickable: true}, {Line: 3}}
	if v.loaded {
		lines = append(lines, "Loaded row")
		items = append(items, plugins.Item{Line: 4})
	}
	return strings.Join(lines, "\n"), items
}

type fakePlugin struct{ view *fakeView }

func (p *fakePlugin) Name() string                   { return "fake-ui" }
func (p *fakePlugin) Configure(map[string]any) error { return nil }
func (p *fakePlugin) CheckAction(c plugins.Check) (string, bool) {
	return "view fake", strings.HasPrefix(c.URL, "https://fake.example.com")
}

func (p *fakePlugin) OpenCheck(pr plugins.PR, c plugins.Check) plugins.View {
	p.view = &fakeView{}
	return p.view
}

var fake = &fakePlugin{}

func init() {
	plugins.Register("fake-ui", func() plugins.Plugin { return fake })
}

func newPluginTestModel(t *testing.T) Model {
	t.Helper()
	// Prow jobs' artifacts aren't found, rather than fetched from the internet
	artifacts := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(artifacts.Close)
	require.NoError(t, plugins.Configure(map[string]config.PluginConfig{
		"fake-ui": {Enabled: true},
		"prow":    {Enabled: true, Options: map[string]any{"artifactsUrl": artifacts.URL}},
	}))
	t.Cleanup(func() { _ = plugins.Configure(nil) })

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
	require.NoError(t, json.Unmarshal([]byte(prWithProwJobJSON), &enriched))
	pr := &prrow.Data{Primary: &prData, Enriched: enriched, IsEnriched: true}
	m.notificationView.SetSubjectPR(pr, "test-notification-id")
	m.prView.SetRow(pr)
	m.prView.SetWidth(60)
	for !m.prView.IsChecksTab() {
		m.prView.NextTab()
	}
	m.setSidebarPRContent()
	return m
}

// run delivers the messages cmd produces to the model, as bubbletea would.
func run(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			run(m, c)
		}
	default:
		_, next := m.Update(msg)
		run(m, next)
	}
}

func TestNotificationView_PluginOpensCheckInItsOwnTab(t *testing.T) {
	m := newPluginTestModel(t)

	// The failed Prow job is focused first, and the prow plugin opens it
	check, ok := m.focusedCheck()
	require.True(t, ok)
	require.Equal(t, 0, check)
	m.updateSidebarHints()
	require.Contains(t, ansi.Strip(m.sidebar.View()),
		keys.HintKeys(keys.NotificationKeys.OpenCheck)+" view tests")

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd, "opening the job starts fetching its tests")
	require.True(t, m.prView.IsPluginTab())
	require.Contains(t, m.prView.SelectedTab(), "e2e-aws")
	require.Contains(t, ansi.Strip(m.sidebar.View()), "Loading the test results")

	// Tabs can be switched to and from it
	m.Update(tea.KeyPressMsg{Text: "h"})
	require.Contains(t, m.prView.SelectedTab(), "Files Changed")
	m.Update(tea.KeyPressMsg{Text: "l"})
	require.True(t, m.prView.IsPluginTab())
}

func TestNotificationView_PluginViewItemsAreFocusedAndActivated(t *testing.T) {
	m := newPluginTestModel(t)
	m.Update(tea.KeyPressMsg{Text: "j"})
	check, _ := m.focusedCheck()
	require.Equal(t, 1, check, "the fake check is second")

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	run(&m, cmd)
	require.True(t, m.prView.IsPluginTab())
	require.Equal(t, "Fake", m.prView.SelectedTab())

	// Entering the tab focuses its first item, with its hint
	i, ok := m.focusedPluginItem()
	require.True(t, ok)
	require.Equal(t, 0, i)
	m.updateSidebarHints()
	view := ansi.Strip(m.sidebar.View())
	require.Contains(t, view, keys.HintKeys(keys.NotificationKeys.ActivateItem)+" press")
	require.NotContains(t, view, "Loaded row")

	m.Update(tea.KeyPressMsg{Text: "j"})
	i, _ = m.focusedPluginItem()
	require.Equal(t, 1, i)
	require.Equal(t, 1, fake.view.focused[len(fake.view.focused)-1], "the view is told what's focused")
	m.Update(tea.KeyPressMsg{Text: "k"})

	// Enter activates the button, whose message finds its way to the view
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)
	run(&m, cmd)
	require.True(t, fake.view.loaded)
	require.Contains(t, ansi.Strip(m.sidebar.View()), "Loaded row")
	i, ok = m.focusedPluginItem()
	require.True(t, ok, "the button stays focused")
	require.Equal(t, 0, i)
	require.NotNil(t, m.notificationView.GetSubjectPR(), "enter should not close the notification")
}

func TestNotificationView_StaleViewMessagesAreDropped(t *testing.T) {
	m := newPluginTestModel(t)
	m.Update(tea.KeyPressMsg{Text: "j"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	first := fake.view

	// Opening another check's view replaces it
	m.Update(tea.KeyPressMsg{Text: "h"})
	for !m.prView.IsChecksTab() {
		m.Update(tea.KeyPressMsg{Text: "h"})
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotSame(t, first, fake.view)

	m.Update(prview.PluginViewMsg{Id: -1, Msg: fakeLoadedMsg{}})
	require.False(t, fake.view.loaded)
	require.False(t, first.loaded)
}

func pendingCommentBodies(ctx *context.ProgramContext) []string {
	var bodies []string
	for c := range ctx.PendingComments {
		bodies = append(bodies, c.Body)
	}
	return bodies
}

func TestNotificationView_CheckCommandsArePickedAndPosted(t *testing.T) {
	m := newPluginTestModel(t)
	m.ctx.User = "me"
	actions := keys.HintKeys(keys.NotificationKeys.CheckCommands) + " actions"
	menu := func() string {
		require.NotNil(t, m.checkMenu, "the menu should be open")
		return ansi.Strip(m.checkMenu.View())
	}

	// The failed Prow job is focused, with its commands a key away in a menu
	m.updateSidebarHints()
	require.Contains(t, ansi.Strip(m.sidebar.View()), actions)
	m.Update(tea.KeyPressMsg{Text: "."})
	require.Contains(t, menu(), "ci/prow/e2e-aws")
	require.Contains(t, menu(), "t  rerun     /test e2e-aws")
	require.Contains(t, menu(), "o  override  /override ci/prow/e2e-aws")
	require.NotContains(t, menu(), "/retest", "rerunning all failed jobs isn't about this check")

	// The menu floats over the screen
	layer := m.viewCheckMenu()
	require.NotNil(t, layer)
	require.Equal(t, m.checkMenu.View(), layer.GetContent())

	// Esc dismisses it, and nothing else
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.Nil(t, m.checkMenu)
	require.NotNil(t, m.notificationView.GetSubjectPR(), "esc should not close the notification")
	require.Empty(t, pendingCommentBodies(m.ctx))

	// Rerunning the job comments right away
	m.Update(tea.KeyPressMsg{Text: "."})
	_, cmd := m.Update(tea.KeyPressMsg{Text: "t"})
	require.NotNil(t, cmd)
	require.Nil(t, m.checkMenu)
	require.Equal(t, []string{"/test e2e-aws"}, pendingCommentBodies(m.ctx))

	// Overriding it asks first
	m.ctx.PendingComments = nil
	m.Update(tea.KeyPressMsg{Text: "."})
	m.Update(tea.KeyPressMsg{Text: "o"})
	require.Contains(t, menu(), "Comment /override ci/prow/e2e-aws?")
	m.Update(tea.KeyPressMsg{Text: "n"})
	require.Nil(t, m.checkMenu)
	require.Empty(t, pendingCommentBodies(m.ctx))

	m.Update(tea.KeyPressMsg{Text: "."})
	m.Update(tea.KeyPressMsg{Text: "o"})
	m.Update(tea.KeyPressMsg{Text: "y"})
	require.Equal(t, []string{"/override ci/prow/e2e-aws"}, pendingCommentBodies(m.ctx))

	// Clicking a command picks it, and clicking elsewhere dismisses the menu
	m.ctx.PendingComments = nil
	m.Update(tea.KeyPressMsg{Text: "."})
	m.viewCheckMenu()
	pos := m.checkMenuPos
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: pos.X + 5, Y: pos.Y + 2})
	require.Nil(t, m.checkMenu)
	require.Equal(t, []string{"/test e2e-aws"}, pendingCommentBodies(m.ctx))

	m.ctx.PendingComments = nil
	m.Update(tea.KeyPressMsg{Text: "."})
	m.viewCheckMenu()
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: m.checkMenuPos.X - 1, Y: m.checkMenuPos.Y})
	require.Nil(t, m.checkMenu)
	require.Empty(t, pendingCommentBodies(m.ctx))

	// The fake check has no commands, so . does nothing there
	m.Update(tea.KeyPressMsg{Text: "j"})
	m.updateSidebarHints()
	require.NotContains(t, ansi.Strip(m.sidebar.View()), actions)
	m.Update(tea.KeyPressMsg{Text: "."})
	require.Nil(t, m.checkMenu)
}

func TestNotificationView_CheckCommandsGoByTheKeyPressed(t *testing.T) {
	m := newPluginTestModel(t)
	m.ctx.User = "me"

	// On a Hebrew layout, the keys of . and t type ץ and א
	m.Update(tea.KeyPressMsg{Code: 'ץ', BaseCode: '.', Text: "ץ"})
	require.NotNil(t, m.checkMenu)
	m.Update(tea.KeyPressMsg{Code: 'א', BaseCode: 't', Text: "א"})
	require.Nil(t, m.checkMenu)
	require.Equal(t, []string{"/test e2e-aws"}, pendingCommentBodies(m.ctx))
}

func TestNotificationView_CheckCommandsInACheckTab(t *testing.T) {
	m := newPluginTestModel(t)
	m.ctx.User = "me"
	actions := keys.HintKeys(keys.NotificationKeys.CheckCommands) + " actions"

	// The failed Prow job's tab offers the job's commands too
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	run(&m, cmd)
	require.True(t, m.prView.IsPluginTab())
	m.updateSidebarHints()
	require.Contains(t, ansi.Strip(m.sidebar.View()), actions)
	m.Update(tea.KeyPressMsg{Text: "."})
	require.NotNil(t, m.checkMenu)
	menu := ansi.Strip(m.checkMenu.View())
	require.Contains(t, menu, "ci/prow/e2e-aws")
	require.Contains(t, menu, "t  rerun     /test e2e-aws")
	m.Update(tea.KeyPressMsg{Text: "t"})
	require.Nil(t, m.checkMenu)
	require.Equal(t, []string{"/test e2e-aws"}, pendingCommentBodies(m.ctx))
	require.True(t, m.prView.IsPluginTab(), "the job's tab stays open")

	// The fake check's tab has no commands, so . does nothing there
	m = newPluginTestModel(t)
	m.Update(tea.KeyPressMsg{Text: "j"})
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	run(&m, cmd)
	require.Equal(t, "Fake", m.prView.SelectedTab())
	m.updateSidebarHints()
	require.NotContains(t, ansi.Strip(m.sidebar.View()), actions)
	m.Update(tea.KeyPressMsg{Text: "."})
	require.Nil(t, m.checkMenu)
}
