package actionmenu

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func newMenu() *Model {
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../../../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	if err != nil {
		panic(err)
	}
	ctx := &context.ProgramContext{Config: &cfg}
	ctx.Theme = theme.ParseTheme(ctx.Config)
	ctx.Styles = context.InitStyles(ctx.Theme)
	return New(ctx, "ci/prow/e2e-aws", []Item{
		{Key: "t", Label: "rerun", Detail: "/test e2e-aws"},
		{Key: "o", Label: "override", Detail: "/override ci/prow/e2e-aws",
			Confirm: "Comment /override ci/prow/e2e-aws?"},
	})
}

func press(m *Model, key string) Result {
	switch key {
	case "enter":
		return m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	case "esc":
		return m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	}
	return m.Update(tea.KeyPressMsg{Text: key, Code: rune(key[0])})
}

func TestListsItemsWithTheirKeys(t *testing.T) {
	view := ansi.Strip(newMenu().View())
	require.Contains(t, view, "ci/prow/e2e-aws")
	require.Contains(t, view, "t  rerun")
	require.Contains(t, view, "o  override  /override ci/prow/e2e-aws")
	require.Contains(t, view, "esc close")
}

func TestPickedByKeyOrCursor(t *testing.T) {
	require.Equal(t, Result{Picked: 0, Closed: true}, press(newMenu(), "t"))

	m := newMenu()
	require.Equal(t, none, press(m, "j"))
	require.Equal(t, none, press(m, "j"), "the cursor wraps")
	require.Equal(t, Result{Picked: 0, Closed: true}, press(m, "enter"))
}

func TestConfirmsFirstWhenAsked(t *testing.T) {
	m := newMenu()
	require.Equal(t, none, press(m, "o"))
	require.Contains(t, ansi.Strip(m.View()), "Comment /override ci/prow/e2e-aws?")
	require.Equal(t, Result{Picked: 1, Closed: true}, press(m, "y"))

	m = newMenu()
	press(m, "o")
	require.Equal(t, dismissed, press(m, "n"))
}

func TestDismissed(t *testing.T) {
	for _, key := range []string{"esc", "q", "."} {
		require.Equal(t, dismissed, press(newMenu(), key), key)
	}
	require.Equal(t, none, press(newMenu(), "x"), "unknown keys are ignored")
}

func TestClicks(t *testing.T) {
	m := newMenu()
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	override := -1
	for i, l := range lines {
		if strings.Contains(l, "override") {
			override = i
		}
	}
	require.Equal(t, none, m.Click(5, override), "clicking an item that asks first confirms it")
	require.Contains(t, ansi.Strip(m.View()), "Comment /override")

	require.Equal(t, Result{Picked: 0, Closed: true}, newMenu().Click(5, 2))
	require.Equal(t, dismissed, newMenu().Click(-1, 2))
	require.Equal(t, dismissed, newMenu().Click(5, 100))
}
