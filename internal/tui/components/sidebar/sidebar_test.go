package sidebar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func newTestSidebar(numLines int) Model {
	th := *theme.DefaultTheme
	ctx := &context.ProgramContext{
		Config:            &config.Config{},
		Theme:             th,
		Styles:            context.InitStyles(th),
		MainContentHeight: 11,
	}
	m := NewModel()
	m.IsOpen = true
	m.UpdateProgramContext(ctx)
	m.viewport.SetWidth(40)
	m.SetContent(strings.TrimSuffix(strings.Repeat("line\n", numLines), "\n"))
	return m
}

func TestPagerScrollHints(t *testing.T) {
	m := newTestSidebar(50)

	m.ScrollToTop()
	if got := ansi.Strip(m.renderPager()); got != "0% · Ctrl+d ↓" {
		t.Errorf("at top: got %q", got)
	}

	m.viewport.HalfPageDown()
	if got := ansi.Strip(m.renderPager()); !strings.Contains(got, "Ctrl+u ↑ · Ctrl+d ↓") {
		t.Errorf("in the middle: got %q", got)
	}

	m.ScrollToBottom()
	if got := ansi.Strip(m.renderPager()); got != "100% · Ctrl+u ↑" {
		t.Errorf("at bottom: got %q", got)
	}
}

func TestPagerNoHintsWhenContentFits(t *testing.T) {
	m := newTestSidebar(3)
	if got := ansi.Strip(m.renderPager()); got != "100%" {
		t.Errorf("got %q", got)
	}
}
