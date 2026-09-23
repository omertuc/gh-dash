package sidebar

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func typeSearch(m *Model, query string) {
	m.StartSearch()
	for _, r := range query {
		m.UpdateSearch(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestSearchFocusesCommentWithMatch(t *testing.T) {
	m := newCommentsSidebar(3, 3, 3, 3, 3)

	typeSearch(&m, "comment 3 line 1")
	if got := focusedComment(t, m); got != "comment 3" {
		t.Fatalf("typing: focused %q, want comment 3", got)
	}
	if !strings.Contains(ansi.Strip(m.renderContent()), "/comment 3 line 1") {
		t.Error("the search input should show the query while typing")
	}

	m.UpdateSearch(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.IsSearching() || !m.HasSearch() {
		t.Fatal("enter should keep the query to move between matches")
	}
	if !strings.Contains(ansi.Strip(m.renderPager()), "1/1") {
		t.Errorf("pager should show the match count, got %q", ansi.Strip(m.renderPager()))
	}
}

func TestSearchNextAndPrevWrapAround(t *testing.T) {
	m := newCommentsSidebar(3, 3, 3, 3, 3)
	typeSearch(&m, "line 2")
	m.UpdateSearch(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := focusedComment(t, m); got != "comment 0" {
		t.Fatalf("first match: focused %q, want comment 0", got)
	}

	m.SearchNext()
	if got := focusedComment(t, m); got != "comment 1" {
		t.Fatalf("n: focused %q, want comment 1", got)
	}
	m.SearchPrev()
	m.SearchPrev()
	if got := focusedComment(t, m); got != "comment 4" {
		t.Fatalf("N past the first: focused %q, want comment 4", got)
	}
	if !m.viewport.AtBottom() {
		t.Error("wrapping to the last match should scroll to it")
	}
}

func TestSearchIsCaseInsensitiveUnlessQueryHasUppercase(t *testing.T) {
	m := newCommentsSidebar(3)
	typeSearch(&m, "COMMENT")
	if m.HasSearch() && len(m.searchMatches) != 0 {
		t.Errorf("uppercase query matched %d times, want 0", len(m.searchMatches))
	}
	m.ClearSearch()
	typeSearch(&m, "comment")
	if len(m.searchMatches) != 3 {
		t.Errorf("lowercase query matched %d times, want 3", len(m.searchMatches))
	}
}

func TestSearchEscRestoresScroll(t *testing.T) {
	m := newCommentsSidebar(3, 3, 3, 3, 3)
	typeSearch(&m, "comment 4")
	if m.viewport.YOffset() == 0 {
		t.Fatal("typing should scroll to the match")
	}
	m.UpdateSearch(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.HasSearch() || m.IsSearching() {
		t.Fatal("esc should drop the search")
	}
	if m.viewport.YOffset() != 0 {
		t.Errorf("esc should scroll back, y = %d", m.viewport.YOffset())
	}
}
