package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"

	// Makes the built-in plugins available to be enabled in the config
	_ "github.com/dlvhdr/gh-dash/v4/internal/plugins/all"
)

// focusedPluginItem returns the index of the item of a plugin's view that's
// focused in an open notification's PR, as the view listed it, if any.
func (m *Model) focusedPluginItem() (int, bool) {
	if !m.isNotificationSubjectShown() || m.notificationView.GetSubjectPR() == nil ||
		!m.prView.IsPluginTab() {
		return 0, false
	}
	i := m.sidebar.FocusedAnchor()
	if i < 0 || i >= len(m.sidebarComments) || m.sidebarComments[i].Plugin == nil {
		return 0, false
	}
	return *m.sidebarComments[i].Plugin, true
}

func (m *Model) hasFocusedPluginItem() bool {
	_, ok := m.focusedPluginItem()
	return ok
}

// syncFocusedPluginItem keeps an item of a plugin's view focused, e.g. the
// first in view on entering its tab, and tells the view which one, the way
// syncFocusedCheck expands the focused check. The view is laid out anew when
// that changes what it shows.
func (m *Model) syncFocusedPluginItem(fromBelow bool) {
	if !m.isNotificationSubjectShown() || m.notificationView.GetSubjectPR() == nil ||
		!m.prView.IsPluginTab() {
		return
	}
	item, ok := m.focusedPluginItem()
	if !ok && m.sidebar.FocusVisible() {
		item, ok = m.focusedPluginItem()
	}
	if !ok {
		item = -1
	}
	if !m.prView.SetFocusedPluginItem(item) {
		return
	}
	anchor := m.sidebar.FocusedAnchor()
	m.renderSidebarPRContent()
	m.sidebar.FocusAnchor(anchor, fromBelow)
}

// activatePluginItem acts on the item of a plugin's view at the given index,
// e.g. loading more tests, keeping it focused.
func (m *Model) activatePluginItem(item int) tea.Cmd {
	cmd := m.prView.ActivatePluginItem(item)
	m.refreshPluginTab()
	return cmd
}

// refreshPluginTab lays out the plugin's view anew, e.g. after it changed,
// keeping the same item focused.
func (m *Model) refreshPluginTab() {
	if !m.prView.IsPluginTab() || !m.isNotificationSubjectShown() {
		m.syncSidebar()
		return
	}
	anchor := m.sidebar.FocusedAnchor()
	m.renderSidebarPRContent()
	if anchor >= 0 {
		m.sidebar.FocusAnchor(anchor, false)
	}
	// The item now there may be another one, e.g. the first of the tests
	// that were just loaded
	m.syncFocusedPluginItem(false)
}

// onPluginViewMsg delivers a message produced by a plugin view's command
// back to the view.
func (m *Model) onPluginViewMsg(msg prview.PluginViewMsg) tea.Cmd {
	cmd, ok := m.prView.UpdatePluginView(msg)
	if ok {
		m.refreshPluginTab()
	}
	return cmd
}

// openFocusedCheckInPlugin opens the focused check in a tab of its own when
// a plugin opens it, e.g. to list a CI job's tests. It reports whether one
// did.
func (m *Model) openFocusedCheckInPlugin() (tea.Cmd, bool) {
	check, ok := m.focusedCheck()
	if !ok {
		return nil, false
	}
	cmd, ok := m.prView.OpenCheckInPlugin(check)
	if !ok {
		return nil, false
	}
	m.sidebar.ScrollToTop()
	m.syncSidebar()
	return cmd, true
}

// pluginFocusHint returns the hint for acting on the focused item of a
// plugin's view, e.g. "enter load".
func (m *Model) pluginFocusHint() string {
	i, ok := m.focusedPluginItem()
	if !ok {
		return ""
	}
	item, ok := m.prView.PluginItem(i)
	if !ok || item.Hint == "" {
		return ""
	}
	return keys.HintKeys(keys.NotificationKeys.ActivateItem) + " " + item.Hint
}

// checkFocusHint returns the hint for opening the focused check, e.g.
// "enter view tests" when a plugin opens it.
func (m *Model) checkFocusHint() string {
	hint := keys.HintKeys(keys.NotificationKeys.OpenCheck) + " open"
	if check, ok := m.focusedCheck(); ok {
		if label := m.prView.CheckAction(check); label != "" {
			hint = keys.HintKeys(keys.NotificationKeys.OpenCheck) + " " + label
		}
	}
	return hint
}

// clickPluginItem activates a clicked item of a plugin's view when it's
// something like a button.
func (m *Model) clickPluginItem() tea.Cmd {
	i, ok := m.focusedPluginItem()
	if !ok {
		return nil
	}
	if item, ok := m.prView.PluginItem(i); ok && item.Clickable {
		return m.activatePluginItem(i)
	}
	return nil
}
