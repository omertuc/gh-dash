package sidebar

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// focusItem is a range of content lines that can be focused, e.g. a comment.
type focusItem struct {
	start, end int // content lines [start, end)
	// preamble is the content before the first anchor, e.g. an issue's
	// description. It can be focused but isn't highlighted.
	preamble bool
}

// focusItems splits the viewport content into focusable items at the anchors.
func (m Model) focusItems() []focusItem {
	if len(m.anchors) == 0 {
		return nil
	}

	offset := 0
	if m.header != "" && !m.headerIsSticky {
		// The header scrolls along with the content above the body
		offset = lipgloss.Height(m.header)
	}
	total := m.viewport.TotalLineCount()

	var items []focusItem
	if first := m.anchors[0] + offset; first > 0 {
		items = append(items, focusItem{start: 0, end: first, preamble: true})
	}
	for i, anchor := range m.anchors {
		end := total
		if i+1 < len(m.anchors) {
			end = m.anchors[i+1] + offset
		}
		items = append(items, focusItem{start: anchor + offset, end: end})
	}
	return items
}

// FocusNext moves the focus down: it scrolls through the focused item if it
// continues below the view, and otherwise focuses the next item. It reports
// false when there's nothing to focus, so the caller can scroll instead.
func (m *Model) FocusNext() bool {
	items := m.focusItems()
	if len(items) == 0 {
		return false
	}
	y, h := m.viewport.YOffset(), m.viewport.Height()

	if !m.isFocusVisible(items) {
		// Start from the item at the top of the view, and if it's already
		// fully visible, just focus it
		m.focus = itemAt(items, y)
		if it := items[m.focus]; !it.preamble && it.start >= y && it.end <= y+h {
			return true
		}
	}

	it := items[m.focus]
	if it.end > y+h {
		// Keep reading the focused item
		m.viewport.SetYOffset(y + min(max(h/2, 1), it.end-(y+h)))
		return true
	}
	if m.focus+1 < len(items) {
		m.focus++
		m.revealFocused(items, false)
	}
	return true
}

// FocusPrev moves the focus up, the mirror image of FocusNext.
func (m *Model) FocusPrev() bool {
	items := m.focusItems()
	if len(items) == 0 {
		return false
	}
	y, h := m.viewport.YOffset(), m.viewport.Height()

	if !m.isFocusVisible(items) {
		// Start from the item at the bottom of the view, and if it's already
		// fully visible, just focus it
		m.focus = itemAt(items, y+h-1)
		if it := items[m.focus]; !it.preamble && it.start >= y && it.end <= y+h {
			return true
		}
	}

	it := items[m.focus]
	if it.start < y {
		// Keep reading the focused item upwards
		m.viewport.SetYOffset(y - min(max(h/2, 1), y-it.start))
		return true
	}
	if m.focus > 0 {
		m.focus--
		m.revealFocused(items, true)
	}
	return true
}

// FocusedAnchor returns the index of the anchor whose item is focused, or -1
// when nothing (or only the content before the first anchor) is focused.
func (m Model) FocusedAnchor() int {
	items := m.focusItems()
	if m.focus < 0 || m.focus >= len(items) || items[m.focus].preamble {
		return -1
	}
	if items[0].preamble {
		return m.focus - 1
	}
	return m.focus
}

// SetFocusHint sets the key hint shown on the focused item's title line, e.g.
// "r reply".
func (m *Model) SetFocusHint(hint string) {
	m.focusHint = hint
}

// FocusLast focuses the last item, e.g. after jumping to the bottom.
func (m *Model) FocusLast() {
	if items := m.focusItems(); len(items) > 0 {
		m.focus = len(items) - 1
	}
}

// FocusAnchor focuses the item at the given anchor and scrolls to show it,
// e.g. to keep the focus on an item after the content was laid out anew. An
// item taller than the view is shown from its bottom when fromBelow.
func (m *Model) FocusAnchor(anchor int, fromBelow bool) {
	items := m.focusItems()
	if len(items) > 0 && items[0].preamble {
		anchor++
	}
	if anchor < 0 || anchor >= len(items) {
		return
	}
	m.focus = anchor
	m.revealFocused(items, fromBelow)
}

// FocusVisible focuses the first item in view, or else the first item,
// skipping the content before the first anchor. It reports false when there's
// nothing to focus.
func (m *Model) FocusVisible() bool {
	items := m.focusItems()
	y, h := m.viewport.YOffset(), m.viewport.Height()
	first := -1
	for i, it := range items {
		if it.preamble {
			continue
		}
		if it.start < y+h && it.end > y {
			m.focus = i
			return true
		}
		if first < 0 {
			first = i
		}
	}
	if first < 0 {
		return false
	}
	m.focus = first
	m.revealFocused(items, false)
	return true
}

// SetFocusLabel sets what the focusable items are called in the navigation
// hint, e.g. "comment".
func (m *Model) SetFocusLabel(label string) {
	m.focusLabel = label
}

// ResetFocus clears the focus, e.g. when showing different content.
func (m *Model) ResetFocus() {
	m.focus = -1
}

func (m Model) isFocusVisible(items []focusItem) bool {
	if m.focus < 0 || m.focus >= len(items) {
		return false
	}
	y, h := m.viewport.YOffset(), m.viewport.Height()
	it := items[m.focus]
	return it.start < y+h && it.end > y
}

// revealFocused scrolls as little as possible to show the focused item. An
// item taller than the view is shown from its top, or from its bottom when
// arriving at it from below.
func (m *Model) revealFocused(items []focusItem, fromBelow bool) {
	y, h := m.viewport.YOffset(), m.viewport.Height()
	it := items[m.focus]
	switch {
	case it.end-it.start > h && fromBelow:
		y = it.end - h
	case it.end-it.start > h:
		y = it.start
	case it.start < y:
		y = it.start
	case it.end > y+h:
		y = it.end - h
	}
	m.viewport.SetYOffset(y)
}

// itemAt returns the index of the item containing line.
func itemAt(items []focusItem, line int) int {
	for i, it := range items {
		if line < it.end {
			return i
		}
	}
	return len(items) - 1
}

// highlightFocused marks the focused item's visible lines with a bar in the
// left margin.
func (m Model) highlightFocused(view string) string {
	items := m.focusItems()
	if m.focus < 0 || m.focus >= len(items) || items[m.focus].preamble {
		return view
	}
	it := items[m.focus]
	bar := lipgloss.NewStyle().Foreground(m.ctx.Theme.PrimaryText).Render("▎")

	lines := strings.Split(view, "\n")
	y := m.viewport.YOffset()
	for i := range lines {
		if line := y + i; line >= it.start && line < it.end {
			lines[i] = bar + ansi.TruncateLeft(lines[i], 1, "")
		}
	}

	if m.focusHint != "" {
		title := it.start
		// Comments start with a box around the author, whose title is on
		// the line below the box's top border
		boxed := false
		if i := title - y; i >= 0 && i < len(lines) && strings.Contains(ansi.Strip(lines[i]), "╭") {
			title++
			boxed = true
		}
		if i := title - y; i >= 0 && i < len(lines) {
			hinted := m.withTitleHint(lines[i], boxed)
			// A title filling the line, e.g. a commit's, leaves no room, so
			// try the line below it, e.g. the commit's author
			if hinted == lines[i] && i+1 < len(lines) && title+1 < it.end {
				lines[i+1] = m.withTitleHint(lines[i+1], boxed)
			} else {
				lines[i] = hinted
			}
		}
	}
	return strings.Join(lines, "\n")
}

// withTitleHint places the focus hint at the right end of a comment's title
// line: just inside the closing border of its box when boxed, otherwise
// at the right edge of the content. The hint is left out if it would cover
// text.
func (m Model) withTitleHint(line string, boxed bool) string {
	hint := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Render(m.focusHint)
	hintWidth := lipgloss.Width(hint)

	plain := ansi.Strip(line)
	end := m.viewport.Width() - m.ctx.Styles.Sidebar.ContentPadding
	if i := strings.LastIndex(plain, "│"); boxed && i >= 0 {
		// Just inside the box's right border, leaving a space before it
		end = ansi.StringWidth(plain[:i]) - 1
	}
	start := end - hintWidth
	if start < 1 {
		return line
	}
	// Pad short lines so there are cells to place the hint in
	if w := ansi.StringWidth(line); w < end {
		line += strings.Repeat(" ", end-w)
	}
	if strings.TrimSpace(ansi.Strip(ansi.Cut(line, start-1, end))) != "" {
		return line
	}
	return ansi.Cut(line, 0, start) + hint + ansi.Cut(line, end, ansi.StringWidth(line))
}
