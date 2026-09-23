package sidebar

import (
	"fmt"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// searchMatch is where the search query was found in the content.
type searchMatch struct {
	line       int // content line
	start, end int // cells within the line
}

// StartSearch shows an input in place of the pager for finding text in the
// content, e.g. in a PR's comments. The content scrolls to the first match
// as the query is typed.
func (m *Model) StartSearch() tea.Cmd {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.Placeholder = "search"
	ti.SetStyles(textinput.Styles{
		Focused: textinput.StyleState{
			Prompt:      lipgloss.NewStyle().Foreground(m.ctx.Theme.SecondaryText),
			Text:        lipgloss.NewStyle().Foreground(m.ctx.Theme.PrimaryText),
			Placeholder: lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText),
		},
		// Blinking needs its messages routed back here, and a steady cursor
		// does just as well
		Cursor: textinput.CursorStyle{Color: m.ctx.Theme.FaintText, Shape: tea.CursorBar},
	})
	ti.SetWidth(max(1, m.viewport.Width()-m.ctx.Styles.Sidebar.ContentPadding-2))

	m.searchInput = ti
	m.searching = true
	// Searching again starts from what's in view
	m.searchOrigin = m.viewport.YOffset()
	return m.searchInput.Focus()
}

// IsSearching reports whether the search query is being typed.
func (m Model) IsSearching() bool {
	return m.searching
}

// HasSearch reports whether there's a search query to move between the
// matches of.
func (m Model) HasSearch() bool {
	return m.searchQuery != ""
}

// UpdateSearch handles a key while the search query is being typed: enter
// keeps the query to move between its matches, and esc drops it.
func (m *Model) UpdateSearch(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		m.searching = false
		if len(m.searchMatches) == 0 {
			m.ClearSearch()
		}
		return nil
	case "esc":
		m.ClearSearch()
		m.viewport.SetYOffset(m.searchOrigin)
		return nil
	}

	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	if query := m.searchInput.Value(); query != m.searchQuery {
		m.searchQuery = query
		m.findMatches()
		m.revealMatch(m.firstMatchFrom(m.searchOrigin))
	}
	return cmd
}

// ClearSearch drops the search query and its highlights.
func (m *Model) ClearSearch() {
	m.searching = false
	m.searchQuery = ""
	m.searchMatches = nil
	m.searchCurrent = -1
}

// SearchNext moves to the next match of the search query, wrapping around to
// the first. It reports false when there's no query.
func (m *Model) SearchNext() bool {
	if !m.HasSearch() {
		return false
	}
	if len(m.searchMatches) > 0 {
		m.revealMatch((m.searchCurrent + 1) % len(m.searchMatches))
	}
	return true
}

// SearchPrev moves to the previous match of the search query, wrapping around
// to the last. It reports false when there's no query.
func (m *Model) SearchPrev() bool {
	if !m.HasSearch() {
		return false
	}
	if n := len(m.searchMatches); n > 0 {
		m.revealMatch((m.searchCurrent - 1 + n) % n)
	}
	return true
}

// findMatches finds the search query in the content. It's case-insensitive
// unless the query has uppercase letters.
func (m *Model) findMatches() {
	m.searchMatches = nil
	m.searchCurrent = -1
	query := m.searchQuery
	if query == "" {
		return
	}
	fold := !strings.ContainsFunc(query, unicode.IsUpper)
	if fold {
		query = strings.ToLower(query)
	}
	for i, line := range strings.Split(m.viewportContent, "\n") {
		plain := ansi.Strip(line)
		if fold {
			plain = strings.ToLower(plain)
		}
		for from := 0; ; {
			idx := strings.Index(plain[from:], query)
			if idx < 0 {
				break
			}
			idx += from
			start := ansi.StringWidth(plain[:idx])
			m.searchMatches = append(m.searchMatches, searchMatch{
				line:  i,
				start: start,
				end:   start + ansi.StringWidth(query),
			})
			from = idx + len(query)
		}
	}
}

// refreshSearch finds the search query again after the content changed,
// keeping to the match on the same line where there still is one.
func (m *Model) refreshSearch() {
	if !m.HasSearch() {
		return
	}
	line := -1
	if m.searchCurrent >= 0 && m.searchCurrent < len(m.searchMatches) {
		line = m.searchMatches[m.searchCurrent].line
	}
	m.findMatches()
	if line >= 0 {
		m.searchCurrent = m.firstMatchFrom(line)
	}
}

// firstMatchFrom returns the first match at or below line, wrapping around
// to the first match, or -1 when there are none.
func (m Model) firstMatchFrom(line int) int {
	if len(m.searchMatches) == 0 {
		return -1
	}
	for i, match := range m.searchMatches {
		if match.line >= line {
			return i
		}
	}
	return 0
}

// revealMatch makes the given match the current one, scrolls to show it
// and focuses the item it's in, e.g. a comment, so it can be acted on.
func (m *Model) revealMatch(i int) {
	m.searchCurrent = i
	if i < 0 || i >= len(m.searchMatches) {
		return
	}
	line := m.searchMatches[i].line
	y, h := m.viewport.YOffset(), m.viewport.Height()
	if line < y || line >= y+h {
		// Show the match a little below the top, so what leads to it is in view
		m.viewport.SetYOffset(line - h/3)
	}
	if items := m.focusItems(); len(items) > 0 {
		m.focus = itemAt(items, line)
	}
}

// highlightMatches marks the search query's matches in the visible lines,
// the current match more strongly than the others.
func (m Model) highlightMatches(view string) string {
	if len(m.searchMatches) == 0 {
		return view
	}
	matchStyle := lipgloss.NewStyle().
		Foreground(m.ctx.Theme.PrimaryText).
		Background(m.ctx.Theme.FaintBorder)
	currentStyle := lipgloss.NewStyle().
		Foreground(m.ctx.Theme.InvertedText).
		Background(m.ctx.Theme.WarningText)

	lines := strings.Split(view, "\n")
	y := m.viewport.YOffset()
	ranges := map[int][]lipgloss.Range{}
	for i, match := range m.searchMatches {
		if match.line < y || match.line >= y+len(lines) {
			continue
		}
		style := matchStyle
		if i == m.searchCurrent {
			style = currentStyle
		}
		ranges[match.line-y] = append(ranges[match.line-y], lipgloss.NewRange(match.start, match.end, style))
	}
	for i, r := range ranges {
		lines[i] = lipgloss.StyleRanges(lines[i], r...)
	}
	return strings.Join(lines, "\n")
}

// renderSearchInput renders the input the search query is typed in, in place
// of the pager.
func (m Model) renderSearchInput() string {
	return lipgloss.NewStyle().Height(m.ctx.Styles.Sidebar.PagerStyle.GetHeight()).
		MaxWidth(m.viewport.Width()).
		Render(m.ctx.Styles.Sidebar.PagerStyle.UnsetHeight().Render(m.searchInput.View()))
}

// searchStatus describes the current match among the matches, e.g. "/foo
// 2/5".
func (m Model) searchStatus() string {
	if len(m.searchMatches) == 0 {
		return fmt.Sprintf("/%s no matches", m.searchQuery)
	}
	return fmt.Sprintf("/%s %d/%d", m.searchQuery, m.searchCurrent+1, len(m.searchMatches))
}
