package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/plugins"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/actionmenu"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/notificationssection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tasks"
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
// "enter view tests" when a plugin opens it, and for acting on it when
// plugins offer commands on it.
func (m *Model) checkFocusHint() string {
	hint := keys.HintKeys(keys.NotificationKeys.OpenCheck) + " open"
	if check, ok := m.focusedCheck(); ok {
		if label := m.prView.CheckAction(check); label != "" {
			hint = keys.HintKeys(keys.NotificationKeys.OpenCheck) + " " + label
		}
	}
	if m.hasFocusedCheckCommands() {
		hint += " · " + keys.HintKeys(keys.NotificationKeys.CheckCommands) + " actions"
	}
	return hint
}

func (m *Model) hasFocusedCheckCommands() bool {
	check, ok := m.focusedCheck()
	if !ok {
		return false
	}
	_, commands := m.prView.CheckCommands(check)
	return len(commands) > 0
}

// showFocusedCheckCommands floats a menu by the check focused in an open
// notification's PR, listing the commands plugins offer on it, e.g.
// rerunning its job. They're kept as they are now, so the check stays the
// one acted on even if a refresh reorders the checks meanwhile.
func (m *Model) showFocusedCheckCommands() {
	check, ok := m.focusedCheck()
	if !ok {
		return
	}
	name, commands := m.prView.CheckCommands(check)
	if len(commands) == 0 {
		return
	}
	items := make([]actionmenu.Item, 0, len(commands))
	for _, c := range commands {
		item := actionmenu.Item{Key: c.Key, Label: c.Label, Detail: c.Comment}
		if c.Confirm {
			item.Confirm = fmt.Sprintf("Comment %s?", c.Comment)
		}
		items = append(items, item)
	}
	m.checkMenu = actionmenu.New(m.ctx, name, items)
	m.checkCommands = commands
}

// onCheckMenuResult posts the command picked in the check's menu, if any,
// once the menu is done.
func (m *Model) onCheckMenuResult(res actionmenu.Result) tea.Cmd {
	if !res.Closed {
		return nil
	}
	commands := m.checkCommands
	m.checkMenu, m.checkCommands = nil, nil
	if res.Picked < 0 || res.Picked >= len(commands) {
		return nil
	}
	return m.postCheckCommand(commands[res.Picked])
}

// clickCheckMenu handles a click while the check's menu is open: on one of
// its commands it picks it, and anywhere else it dismisses the menu.
func (m *Model) clickCheckMenu(msg tea.MouseClickMsg) tea.Cmd {
	mouse := msg.Mouse()
	return m.onCheckMenuResult(m.checkMenu.Click(mouse.X-m.checkMenuPos.X, mouse.Y-m.checkMenuPos.Y))
}

// viewCheckMenu lays out the check's menu, when open, just below the
// focused check's first line, or above it when there's no room below.
// Without the check's position, e.g. before the preview was first drawn, it
// floats at the top of the preview.
func (m *Model) viewCheckMenu() *lipgloss.Layer {
	if m.checkMenu == nil {
		return nil
	}
	view := m.checkMenu.View()
	w, h := lipgloss.Width(view), lipgloss.Height(view)
	pos := m.ctx.PreviewCursorPosition()
	x, y := pos.X+2, pos.Y+2
	if cx, cy, cw, ok := m.sidebar.FocusedScreenPos(); ok {
		x, y = cx+4, cy+1
		if x+w > cx+cw {
			x = cx + cw - w
		}
		if bottom := m.ctx.ScreenHeight - common.FooterHeight; y+h > bottom {
			y = cy - h
		}
	}
	x = max(0, min(x, m.ctx.ScreenWidth-w))
	y = max(0, min(y, m.ctx.ScreenHeight-h))
	m.checkMenuPos = tea.Position{X: x, Y: y}
	return lipgloss.NewLayer(view).X(x).Y(y).Z(1)
}

// postCheckCommand comments a check's command on the open notification's PR,
// e.g. "/test e2e-aws".
func (m *Model) postCheckCommand(c plugins.CheckCommand) tea.Cmd {
	pr := m.notificationView.GetSubjectPR()
	if pr == nil {
		return nil
	}
	sid := tasks.SectionIdentifier{Id: m.currSectionId, Type: notificationssection.SectionType}
	return tasks.CommentOnPR(m.ctx, sid, pr, c.Comment)
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
