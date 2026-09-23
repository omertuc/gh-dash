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

	// The hints are the same wherever the preview is scrolled to
	for _, scroll := range []func(){m.ScrollToTop, m.viewport.HalfPageDown, m.ScrollToBottom} {
		scroll()
		got := ansi.Strip(m.renderPager())
		if !strings.HasSuffix(got, "% · Ctrl+u/d page") || len([]rune(got)) != len([]rune("  0% · Ctrl+u/d page")) {
			t.Errorf("got %q", got)
		}
	}
}

func TestPagerNoHintsWhenContentFits(t *testing.T) {
	m := newTestSidebar(3)
	if got := ansi.Strip(m.renderPager()); got != "100%" {
		t.Errorf("got %q", got)
	}
}

func TestPagerNavigationHintsWhenNavKeysScroll(t *testing.T) {
	m := newTestSidebar(50)
	m.viewport.SetWidth(200)
	m.SetNavKeysScroll(true)

	m.ScrollToTop()
	if got := ansi.Strip(m.renderPager()); got !=
		"  0% · k/↑ j/↓ scroll · Ctrl+u/d page · g/Home G/End · Esc dismiss" {
		t.Errorf("at top: got %q", got)
	}

	m.ScrollToBottom()
	if got := ansi.Strip(m.renderPager()); got !=
		"100% · k/↑ j/↓ scroll · Ctrl+u/d page · g/Home G/End · Esc dismiss" {
		t.Errorf("at bottom: got %q", got)
	}

	short := newTestSidebar(3)
	short.SetNavKeysScroll(true)
	if got := ansi.Strip(short.renderPager()); got != "100% · Esc dismiss" {
		t.Errorf("content fits: got %q", got)
	}
}

func TestPagerDropsLeastImportantHintsWhenNarrow(t *testing.T) {
	m := newTestSidebar(50)
	m.SetNavKeysScroll(true)

	m.viewport.SetWidth(51)
	if got := ansi.Strip(m.renderPager()); got != "  0% · k/↑ j/↓ scroll · Ctrl+u/d page · Esc dismiss" {
		t.Errorf("width 51: got %q", got)
	}

	m.viewport.SetWidth(36)
	if got := ansi.Strip(m.renderPager()); got != "  0% · k/↑ j/↓ scroll · Esc dismiss" {
		t.Errorf("width 36: got %q", got)
	}
}

func TestStickyHeaderStaysWhileContentScrolls(t *testing.T) {
	m := newTestSidebar(0)
	m.SetContentWithHeader("HEADER 1\nHEADER 2", strings.TrimSuffix(strings.Repeat("line\n", 50), "\n"))

	// Two header lines plus the scrolled indicator line
	if got := m.viewport.Height(); got != m.contentHeight-3 {
		t.Fatalf("viewport height = %d, want %d (room left below the header)", got, m.contentHeight-3)
	}

	for _, scroll := range []func(){m.ScrollToTop, func() { m.ScrollDown(5) }, m.ScrollToBottom} {
		scroll()
		lines := strings.Split(ansi.Strip(m.renderContent()), "\n")
		if strings.TrimSpace(lines[0]) != "HEADER 1" || strings.TrimSpace(lines[1]) != "HEADER 2" {
			t.Fatalf("header not at the top after scrolling: %q", lines[:3])
		}
	}
}

func TestTallHeaderScrollsWithContent(t *testing.T) {
	m := newTestSidebar(0)
	header := strings.TrimSuffix(strings.Repeat("HEADER\n", 8), "\n")
	m.SetContentWithHeader(header, strings.TrimSuffix(strings.Repeat("line\n", 50), "\n"))

	if m.headerIsSticky {
		t.Fatal("a header taking most of the space should not be sticky")
	}
	if got := m.viewport.Height(); got != m.contentHeight {
		t.Fatalf("viewport height = %d, want the full %d", got, m.contentHeight)
	}
	m.ScrollToBottom()
	if strings.Contains(ansi.Strip(m.viewport.View()), "HEADER") {
		t.Fatal("a non-sticky header should scroll out of view")
	}
}

func TestScrolledIndicatorShowsWhenNotAtTop(t *testing.T) {
	m := newTestSidebar(0)
	// A trailing blank line in the header is replaced by the indicator line
	m.SetContentWithHeader("HEADER\n", strings.TrimSuffix(strings.Repeat("line\n", 50), "\n"))

	indicatorLine := func() string {
		return strings.TrimSpace(strings.Split(ansi.Strip(m.renderContent()), "\n")[1])
	}

	m.ScrollToTop()
	if got := indicatorLine(); got != "" {
		t.Errorf("at top: indicator line = %q, want blank", got)
	}

	m.ScrollDown(1)
	if got := indicatorLine(); got != "▲" {
		t.Errorf("scrolled: indicator line = %q, want ▲", got)
	}

	if got := m.viewport.Height(); got != m.contentHeight-2 {
		t.Errorf("viewport height = %d, want %d", got, m.contentHeight-2)
	}
}
