package sidebar

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
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
	m.SetContentWithHeader("HEADER 1\nHEADER 2", strings.TrimSuffix(strings.Repeat("line\n", 50), "\n"), "", nil)

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
	m.SetContentWithHeader(header, strings.TrimSuffix(strings.Repeat("line\n", 50), "\n"), "", nil)

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
	m.SetContentWithHeader("HEADER\n", strings.TrimSuffix(strings.Repeat("line\n", 50), "\n"), "", nil)

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

// newCommentsSidebar returns a sidebar whose content is a 2 line preamble
// followed by comments of the given heights.
func newCommentsSidebar(commentHeights ...int) Model {
	m := newTestSidebar(0)
	lines := []string{"preamble", "preamble"}
	anchors := []int{}
	for i, h := range commentHeights {
		anchors = append(anchors, len(lines))
		for j := range h {
			lines = append(lines, fmt.Sprintf("comment %d line %d", i, j))
		}
	}
	m.SetContentWithHeader("", strings.Join(lines, "\n"), "", anchors)
	return m
}

func focusedComment(t *testing.T, m Model) string {
	t.Helper()
	for _, line := range strings.Split(ansi.Strip(m.renderContent()), "\n") {
		// The bar replaces the first column, here the "c" of "comment"
		if strings.HasPrefix(line, "▎") {
			return "comment " + strings.Fields(line)[1]
		}
	}
	return ""
}

func TestFocusMovesBetweenComments(t *testing.T) {
	// The viewport is 10 lines tall
	m := newCommentsSidebar(3, 3, 3, 3)

	m.FocusNext()
	if got := focusedComment(t, m); got != "comment 0" {
		t.Fatalf("first j: focused %q, want comment 0", got)
	}
	m.FocusNext()
	m.FocusNext()
	if got := focusedComment(t, m); got != "comment 2" {
		t.Fatalf("third j: focused %q, want comment 2", got)
	}
	m.FocusNext()
	if got := focusedComment(t, m); got != "comment 3" {
		t.Fatalf("fourth j: focused %q, want comment 3", got)
	}
	if !m.viewport.AtBottom() {
		t.Fatal("focusing the last comment should scroll it into view")
	}

	m.FocusPrev()
	if got := focusedComment(t, m); got != "comment 2" {
		t.Fatalf("k: focused %q, want comment 2", got)
	}
}

func TestFocusScrollsThroughTallComments(t *testing.T) {
	// comment 1 is taller than the 10 line viewport
	m := newCommentsSidebar(3, 25, 3)

	m.FocusNext() // comment 0
	m.FocusNext() // comment 1, shown from its top
	if got := focusedComment(t, m); got != "comment 1" {
		t.Fatalf("focused %q, want comment 1", got)
	}
	start := m.viewport.YOffset()

	// j keeps reading comment 1 until its end is visible
	for range 10 {
		if m.viewport.YOffset()+m.viewport.Height() >= 5+25 {
			break
		}
		m.FocusNext()
		if got := focusedComment(t, m); got != "comment 1" {
			t.Fatalf("while reading a tall comment: focused %q, want comment 1", got)
		}
	}
	if m.viewport.YOffset() <= start {
		t.Fatal("j should scroll through a tall comment")
	}

	m.FocusNext()
	if got := focusedComment(t, m); got != "comment 2" {
		t.Fatalf("after reading comment 1: focused %q, want comment 2", got)
	}
}

func TestFocusFallsBackWithoutAnchors(t *testing.T) {
	m := newTestSidebar(50)
	if m.FocusNext() || m.FocusPrev() {
		t.Fatal("focus should report false when there are no comments")
	}
}

func TestFocusResetsWhenCommentsChange(t *testing.T) {
	m := newCommentsSidebar(3, 3)
	m.FocusNext()
	if m.focus < 0 {
		t.Fatal("expected a focused comment")
	}
	m.SetContentWithHeader("", "other\ncontent\nhere", "", []int{1})
	if m.focus != -1 {
		t.Fatal("focus should reset when the comments change")
	}
}

func TestFocusHintOnCommentTitle(t *testing.T) {
	m := newTestSidebar(0)
	box := lipgloss.NewStyle().Width(30).Border(lipgloss.RoundedBorder()).Render("alice 3h ago")
	comment := lipgloss.NewStyle().PaddingLeft(2).Render(box + "\nthe comment body")
	m.SetContentWithHeader("", "title\n\n"+comment, "", []int{2})
	m.SetFocusHint("r reply")

	title := func() string {
		return strings.Split(ansi.Strip(m.renderContent()), "\n")[3]
	}
	if strings.Contains(title(), "r reply") {
		t.Fatal("hint shown before a comment is focused")
	}

	m.FocusNext()
	got := title()
	if !strings.Contains(got, "alice 3h ago") || !strings.HasSuffix(strings.TrimRight(got, " "), "r reply │") {
		t.Fatalf("expected the hint inside the title's box, got %q", got)
	}
	if ansi.StringWidth(got) != ansi.StringWidth(ansi.Strip(m.viewport.View()[:strings.Index(m.viewport.View(), "\n")])) {
		t.Fatalf("hint changed the line's width: %q", got)
	}
}

func TestFocusAnchorKeepsFocusAfterRelayout(t *testing.T) {
	m := newCommentsSidebar(3, 3, 3)
	m.FocusNext()
	m.FocusNext()
	anchor := m.FocusedAnchor()

	// The focused comment grows, moving the ones after it
	m.SetContentWithHeader("", strings.Repeat("line\n", 5)+strings.Repeat("line\n", 12),
		"", []int{2, 5, 17})
	m.FocusAnchor(anchor, false)

	if got := m.FocusedAnchor(); got != anchor {
		t.Fatalf("focused anchor = %d, want %d", got, anchor)
	}
	if m.viewport.YOffset() != 5 {
		t.Fatalf("a comment taller than the view should be shown from its top, offset = %d",
			m.viewport.YOffset())
	}
}

func TestFocusHintBelowFullTitle(t *testing.T) {
	m := newTestSidebar(0)
	full := strings.Repeat("─", m.viewport.Width())
	m.SetContentWithHeader("", "heading\n"+full+"\n│ @alice committed", "", []int{1})
	m.SetFocusHint("enter files")

	m.FocusNext()
	lines := strings.Split(ansi.Strip(m.renderContent()), "\n")
	if strings.Contains(lines[1], "enter files") {
		t.Fatalf("hint covered the full title: %q", lines[1])
	}
	if !strings.Contains(lines[2], "enter files") {
		t.Fatalf("expected the hint on the line below the title, got %q", lines[2])
	}
}

func TestFooterStaysDockedBelowScrollingContent(t *testing.T) {
	m := newTestSidebar(0)
	m.SetContentWithHeader("", strings.TrimSuffix(strings.Repeat("line\n", 50), "\n"),
		"EDITOR 1\nEDITOR 2", nil)

	if got := m.viewport.Height(); got != m.contentHeight-2 {
		t.Fatalf("viewport height = %d, want %d (room left above the footer)", got, m.contentHeight-2)
	}
	for _, scroll := range []func(){m.ScrollToTop, func() { m.ScrollDown(5) }, m.ScrollToBottom} {
		scroll()
		lines := strings.Split(ansi.Strip(m.renderContent()), "\n")
		n := len(lines)
		// The footer sits right above the pager line
		if strings.TrimSpace(lines[n-3]) != "EDITOR 1" || strings.TrimSpace(lines[n-2]) != "EDITOR 2" {
			t.Fatalf("footer not docked at the bottom: %q", lines[n-4:])
		}
	}
}

func TestActionBarDropsHintsThatDontFit(t *testing.T) {
	m := newTestSidebar(50)
	m.SetActionHints([]ActionHint{{"D", "done"}, {"u", "unsubscribe"}, {"c", "comment"}})
	m.SetContentWithHeader("header", strings.Repeat("line\n", 50), "", nil)
	height := lipgloss.Height(m.renderContent())

	if got := ansi.Strip(m.renderActionBar()); got != "  D done · u unsubscribe · c comment" {
		t.Errorf("got %q", got)
	}

	m.viewport.SetWidth(30)
	if got := ansi.Strip(m.renderActionBar()); got != "  D done · ? more" {
		t.Errorf("narrow: got %q", got)
	}

	// The bar takes a line from the content rather than growing the preview
	m.SetActionHints(nil)
	m.SetContentWithHeader("header", strings.Repeat("line\n", 50), "", nil)
	if got := lipgloss.Height(m.renderContent()); got != height {
		t.Errorf("height changed from %d to %d", height, got)
	}
}
