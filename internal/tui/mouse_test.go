package tui

import (
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/branchsidebar"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/footer"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issueview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/notificationview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prssection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/sidebar"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tabs"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

// newMouseTestModel returns a PRs view with two sections, the first of which
// has a few PRs.
func newMouseTestModel(t *testing.T) *Model {
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
		View:         config.PRsView,
		StartTask:    func(task context.Task) tea.Cmd { return nil },
	}
	ctx.Theme = theme.ParseTheme(ctx.Config)
	ctx.Styles = context.InitStyles(ctx.Theme)

	var sections []section.Section
	for i, title := range []string{"Mine", "Review"} {
		s := prssection.NewModel(i, ctx, config.PrsSectionConfig{Title: title},
			time.Now(), time.Now())
		sections = append(sections, &s)
	}
	first := sections[0].(*prssection.Model)
	for i := range 5 {
		first.Prs = append(first.Prs, prrow.Data{Primary: &data.PullRequestData{
			Number: i + 1, Title: fmt.Sprintf("PR %d", i+1), State: "OPEN",
		}})
	}
	first.Table.SetRows(first.BuildRows())

	m := &Model{
		ctx:              ctx,
		keys:             keys.Keys,
		prs:              sections,
		sidebar:          sidebar.NewModel(),
		footer:           footer.NewModel(ctx),
		tabs:             tabs.NewModel(ctx),
		prView:           prview.NewModel(ctx),
		issueSidebar:     issueview.NewModel(ctx),
		branchSidebar:    branchsidebar.NewModel(ctx),
		notificationView: notificationview.NewModel(ctx),
		tasks:            map[string]context.Task{},
	}
	m.sidebar.UpdateProgramContext(ctx)
	m.syncMainContentDimensions()
	m.updateTabs()
	m.syncProgramContext()
	return m
}

// enableZones turns on mouse zones for the test, rendering views with markers.
func enableZones(t *testing.T) {
	zone.SetEnabled(true)
	t.Cleanup(func() { zone.SetEnabled(false) })
}

// render renders the model and waits for its zones to be registered, which
// happens asynchronously.
func render(t *testing.T, m *Model, zoneId string) *zone.ZoneInfo {
	t.Helper()
	m.View()
	var z *zone.ZoneInfo
	require.Eventually(t, func() bool {
		z = zone.Get(zoneId)
		return !z.IsZero()
	}, time.Second, time.Millisecond, "zone %q wasn't rendered", zoneId)
	return z
}

func click(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func TestMouse_ClickingARowSelectsIt(t *testing.T) {
	enableZones(t)
	m := newMouseTestModel(t)
	rows := render(t, m, "table-rows")
	itemHeight := 3 // the test config's table isn't compact, and has separators

	m.Update(click(rows.StartX+5, rows.StartY+3*itemHeight))
	require.Equal(t, 3, m.getCurrSection().CurrRow())

	// Below the last row there's nothing to select
	m.View()
	m.Update(click(rows.StartX+5, rows.StartY+10*itemHeight))
	require.Equal(t, 3, m.getCurrSection().CurrRow())
}

func TestMouse_ClickingTheSelectedRowOpensThePreview(t *testing.T) {
	enableZones(t)
	m := newMouseTestModel(t)
	rows := render(t, m, "table-rows")
	require.False(t, m.sidebar.IsOpen)

	m.Update(click(rows.StartX+5, rows.StartY))
	require.True(t, m.sidebar.IsOpen)
}

func TestMouse_WheelMovesBetweenRows(t *testing.T) {
	enableZones(t)
	m := newMouseTestModel(t)
	rows := render(t, m, "table-rows")
	wheel := func(button tea.MouseButton) {
		m.Update(tea.MouseWheelMsg{X: rows.StartX + 5, Y: rows.StartY, Button: button})
	}

	wheel(tea.MouseWheelDown)
	wheel(tea.MouseWheelDown)
	require.Equal(t, 2, m.getCurrSection().CurrRow())
	wheel(tea.MouseWheelUp)
	require.Equal(t, 1, m.getCurrSection().CurrRow())
	wheel(tea.MouseWheelUp)
	wheel(tea.MouseWheelUp)
	require.Equal(t, 0, m.getCurrSection().CurrRow())
}

func TestMouse_ClickingASectionTabSwitchesToIt(t *testing.T) {
	enableZones(t)
	m := newMouseTestModel(t)
	tab := render(t, m, "section-tab-1")

	m.Update(click(tab.StartX, tab.StartY))
	require.Equal(t, 1, m.currSectionId)
	require.Equal(t, 1, m.tabs.CurrSectionId())
}

func TestMouse_ClickingAViewSwitchesToIt(t *testing.T) {
	enableZones(t)
	m := newMouseTestModel(t)
	button := render(t, m, "view-issues")

	m.Update(click(button.StartX, button.StartY))
	require.Equal(t, config.IssuesView, m.ctx.View)
}

func TestMouse_ClicksAreIgnoredWhileSearching(t *testing.T) {
	enableZones(t)
	m := newMouseTestModel(t)
	rows := render(t, m, "table-rows")
	m.getCurrSection().SetIsSearching(true)

	m.Update(click(rows.StartX+5, rows.StartY+4))
	require.Equal(t, 0, m.getCurrSection().CurrRow())
}
