// Package actionmenu is a small menu floating over the screen, listing what
// can be done to something, e.g. a PR's check. An action is picked with its
// key, or by moving to it and pressing enter, or by clicking it, and actions
// that are hard to take back ask for a y first. Esc, q or a click elsewhere
// dismisses it.
package actionmenu

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

// Item is an action listed in the menu.
type Item struct {
	// Key picks the action, e.g. "t"
	Key string
	// Label is what the action does, e.g. "rerun"
	Label string
	// Detail is shown faintly after the label, e.g. "/test e2e-aws"
	Detail string
	// Confirm, if set, is asked before the action is picked, e.g.
	// "Comment /override ci/prow/e2e-aws?"
	Confirm string
}

// Model is an open menu.
type Model struct {
	ctx   *context.ProgramContext
	title string
	items []Item
	// cursor is the item enter picks
	cursor int
	// confirming is the index of the item whose confirmation is asked, or -1
	confirming int
}

// New returns a menu titled title, e.g. the check's name, listing items.
func New(ctx *context.ProgramContext, title string, items []Item) *Model {
	return &Model{ctx: ctx, title: title, items: items, confirming: -1}
}

// Result is what a key or click did to the menu.
type Result struct {
	// Picked is the index of the picked item, or -1 when none was
	Picked int
	// Closed is whether the menu is done, picked or dismissed
	Closed bool
}

var (
	none      = Result{Picked: -1}
	dismissed = Result{Picked: -1, Closed: true}
)

// Update handles a key pressed while the menu is open.
func (m *Model) Update(msg tea.KeyMsg) Result {
	key := msg.String()
	if m.confirming >= 0 {
		switch key {
		case "y", "Y", "enter":
			return Result{Picked: m.confirming, Closed: true}
		default:
			return dismissed
		}
	}
	switch key {
	case "esc", "q", ".", "ctrl+c":
		return dismissed
	case "down", "j", "ctrl+n", "tab":
		m.cursor = (m.cursor + 1) % len(m.items)
		return none
	case "up", "k", "ctrl+p", "shift+tab":
		m.cursor = (m.cursor - 1 + len(m.items)) % len(m.items)
		return none
	case "enter":
		return m.pick(m.cursor)
	}
	for i, it := range m.items {
		if it.Key == key {
			return m.pick(i)
		}
	}
	return none
}

// pick picks the item at index i, or asks to confirm it first.
func (m *Model) pick(i int) Result {
	m.cursor = i
	if m.items[i].Confirm != "" {
		m.confirming = i
		return none
	}
	return Result{Picked: i, Closed: true}
}

// Click handles a click at the given cell of the menu as last rendered, e.g.
// (3, 2) for the third column of its third line. Clicks outside it dismiss
// it.
func (m *Model) Click(x, y int) Result {
	view := m.View()
	if x < 0 || y < 0 || x >= lipgloss.Width(view) || y >= lipgloss.Height(view) {
		return dismissed
	}
	if m.confirming >= 0 {
		return none
	}
	// The items start below the border and the title
	if i := y - 2; i >= 0 && i < len(m.items) {
		return m.pick(i)
	}
	return none
}

// View renders the menu.
func (m *Model) View() string {
	theme := m.ctx.Theme
	faint := lipgloss.NewStyle().Foreground(theme.FaintText)
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.PrimaryText).Render(m.title)

	var lines []string
	if m.confirming >= 0 {
		lines = []string{
			title,
			lipgloss.NewStyle().Foreground(theme.WarningText).Render(m.items[m.confirming].Confirm),
			faint.Render("y yes · n no"),
		}
		return m.box(lines)
	}

	labelWidth, detailWidth := 0, 0
	for _, it := range m.items {
		labelWidth = max(labelWidth, lipgloss.Width(it.Label))
		detailWidth = max(detailWidth, lipgloss.Width(it.Detail))
	}
	rows := make([]string, 0, len(m.items))
	for i, it := range m.items {
		// Every part is given the background, as each one's styling ends it
		base := lipgloss.NewStyle()
		if i == m.cursor {
			base = base.Background(theme.SelectedBackground)
		}
		keyStyle := base.Bold(true).Foreground(theme.SuccessText)
		pointer := base.Render("  ")
		if i == m.cursor {
			pointer = keyStyle.Render(constants.SelectionIcon + " ")
		}
		row := pointer + keyStyle.Render(it.Key) + base.Render("  ") +
			base.Width(labelWidth).Render(it.Label)
		if detailWidth > 0 {
			row += base.Render("  ") + base.Foreground(theme.FaintText).Width(detailWidth).Render(it.Detail)
		}
		rows = append(rows, row)
	}
	width := lipgloss.Width(title)
	if len(rows) > 0 {
		width = max(width, lipgloss.Width(rows[0]))
	}
	lines = append([]string{title}, rows...)
	lines = append(lines, faint.Render("enter pick · esc close"))
	return m.box(lines)
}

func (m *Model) box(lines []string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.ctx.Theme.PrimaryBorder).
		Padding(0, 1).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
