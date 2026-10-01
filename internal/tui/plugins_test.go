package tui

import (
	"encoding/json"
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
	require.NoError(t, plugins.Configure(map[string]config.PluginConfig{
		"fake-ui": {Enabled: true},
		"prow":    {Enabled: true},
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
