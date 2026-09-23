package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
)

// mouseWheelLines is how many lines the preview scrolls per wheel step
const mouseWheelLines = 3

// isAwaitingInput reports whether something is waiting on the keyboard, e.g. a
// search being typed or a y/n confirmation. Clicks elsewhere are ignored then,
// so they can't pull the rug out from under it.
func (m *Model) isAwaitingInput() bool {
	currSection := m.getCurrSection()
	return (currSection != nil && (currSection.IsSearchFocused() ||
		currSection.IsPromptConfirmationFocused())) ||
		m.prView.IsTextInputBoxFocused() ||
		m.issueSidebar.IsTextInputBoxFocused() ||
		m.sidebar.IsSearching() ||
		m.footer.ShowConfirmQuit ||
		m.notificationView.HasPendingAction() ||
		m.confirmingDraftDiscard ||
		m.mode == ModeSection
}

// onMouseWheel scrolls the preview when the mouse is over it, and otherwise
// moves between the rows of the current section.
func (m *Model) onMouseWheel(msg tea.MouseWheelMsg) tea.Cmd {
	if m.ctx.Config == nil {
		return nil
	}
	var delta int
	switch msg.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return nil
	}

	// Scrolling the preview is harmless even while typing in its editor
	if m.ctx.PreviewFullscreen || m.sidebar.InBounds(msg) {
		if delta > 0 {
			m.sidebar.ScrollDown(mouseWheelLines)
		} else {
			m.sidebar.ScrollUp(mouseWheelLines)
		}
		return nil
	}

	if m.isAwaitingInput() {
		return nil
	}
	currSection := m.getCurrSection()
	if currSection == nil || currSection.NumRows() == 0 {
		return nil
	}
	row := min(max(currSection.CurrRow()+delta, 0), currSection.NumRows()-1)
	return m.selectRow(currSection, row)
}

// onMouseClick acts on a left click on whatever is under the mouse.
func (m *Model) onMouseClick(msg tea.MouseClickMsg) tea.Cmd {
	if m.ctx.Config == nil || m.isAwaitingInput() {
		return nil
	}

	if view, ok := m.footer.ViewAt(msg); ok {
		if view == m.ctx.View {
			return nil
		}
		return m.switchToView(view)
	}

	if m.footer.IsHelpAt(msg) {
		m.footer.ShowAll = !m.footer.ShowAll
		m.syncMainContentDimensions()
		return nil
	}

	if id := m.tabs.SectionAt(msg); id >= 0 && m.ctx.View != config.RepoView {
		if id == m.currSectionId || m.getSectionAt(id) == nil {
			return nil
		}
		m.setCurrSectionId(id)
		return tea.Batch(m.onViewedRowChanged(), m.resumeLoadingSpinner())
	}

	if m.prView.SelectTabAt(msg) {
		return m.syncSidebar()
	}

	// Clicking a comment, commit or check in the preview focuses it
	if line := m.sidebar.ContentLineAt(msg); line >= 0 {
		if m.sidebar.FocusLine(line) {
			m.syncFocusedCommit(false)
		}
		return nil
	}

	currSection := m.getCurrSection()
	if currSection == nil {
		return nil
	}
	row := currSection.RowAt(msg)
	if row < 0 {
		return nil
	}
	if row != currSection.CurrRow() {
		return m.selectRow(currSection, row)
	}

	// Clicking the selected row again opens it
	switch {
	case m.ctx.View == config.NotificationsView && !m.isNotificationSubjectShown():
		return m.loadNotificationContent()
	case !m.sidebar.IsOpen:
		m.sidebar.IsOpen = true
		m.syncMainContentDimensions()
	}
	return nil
}

// selectRow moves to the given row of the section, fetching the next page
// when arriving at the last row, the same as moving there with the keyboard.
func (m *Model) selectRow(s section.Section, row int) tea.Cmd {
	prevRow := s.CurrRow()
	newRow := s.SelectRow(row)
	if newRow == prevRow {
		return nil
	}
	var cmds []tea.Cmd
	if newRow == s.NumRows()-1 && m.ctx.View != config.RepoView {
		cmds = append(cmds, s.FetchNextPageSectionRows()...)
	}
	cmds = append(cmds, m.onViewedRowChanged())
	return tea.Batch(cmds...)
}
