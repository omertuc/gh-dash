package prview

import (
	"slices"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/plugins"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/fuzzyselect"
)

// pluginTab is a plugin's view shown in a tab of its own: one a plugin shows
// for the PR, or one a check was opened in.
type pluginTab struct {
	// id tells the view's messages apart from those of views opened before
	id int64
	// view is nil until a PR's tab is first selected
	view plugins.View
	// check is the check the view was opened from, for a check's tab
	check *plugins.Check
	// prTab is the plugin and which of its tabs it is, for a PR's tab
	prTab plugins.PluginTab
	// items are the view's focusable items as last rendered
	items []plugins.Item
}

// title is the tab's name: its view's once it's open.
func (t *pluginTab) title() string {
	if t.view != nil {
		return t.view.Title()
	}
	return t.prTab.Title
}

var lastPluginViewId atomic.Int64

// PluginViewMsg carries a message produced by a plugin view's command back
// to the view.
type PluginViewMsg struct {
	Id  int64
	Msg tea.Msg
}

// pluginPR is the PR as plugins see it.
func (m *Model) pluginPR() plugins.PR {
	if !m.hasData() {
		return plugins.PR{}
	}
	return plugins.PR{
		Primary:    m.pr.Data.Primary,
		Enriched:   &m.pr.Data.Enriched,
		IsEnriched: m.pr.Data.IsEnriched,
	}
}

// commentSource suggests mentions while writing a comment, along with the
// enabled plugins' commands on lines starting with "/".
func (m *Model) commentSource() fuzzyselect.Source {
	mentions := &fuzzyselect.UserMentionSource{WithAtSymbol: true}
	commands := plugins.CommentCommands(m.pluginPR())
	if len(commands) == 0 {
		return mentions
	}
	suggestions := make([]fuzzyselect.Suggestion, 0, len(commands))
	for _, c := range commands {
		suggestions = append(suggestions, fuzzyselect.Suggestion{Value: c.Text, Detail: c.Description})
	}
	return &fuzzyselect.CommandSource{Commands: suggestions, Fallback: mentions}
}

// pluginTabs lists the plugins' tabs in the order they're shown, after the
// built-in ones: the PR's, then the one a check was opened in.
func (m Model) pluginTabs() []*pluginTab {
	if m.checkTab == nil {
		return m.prTabs
	}
	return append(slices.Clone(m.prTabs), m.checkTab)
}

// selectedPluginTab returns the plugin's tab that's selected, if any.
func (m Model) selectedPluginTab() *pluginTab {
	tabs := m.pluginTabs()
	if i := m.carousel.Cursor() - firstPluginTab; i >= 0 && i < len(tabs) {
		return tabs[i]
	}
	return nil
}

// pluginTabIndex returns the index of a plugin's tab among all the tabs, or
// -1 when it isn't shown.
func (m Model) pluginTabIndex(tab *pluginTab) int {
	if i := slices.Index(m.pluginTabs(), tab); i >= 0 {
		return firstPluginTab + i
	}
	return -1
}

// syncPluginTabs shows the tabs the enabled plugins show for the PR, e.g. its
// conflicts, keeping those already shown along with what they show. The
// selected tab stays selected, or the files tab is when it's gone.
func (m *Model) syncPluginTabs() {
	selected := m.selectedPluginTab()
	var tabs []*pluginTab
	if m.hasData() {
		for _, t := range plugins.Tabs(m.pluginPR()) {
			i := slices.IndexFunc(m.prTabs, func(tab *pluginTab) bool {
				return tab.prTab.Provider == t.Provider && tab.prTab.Key == t.Key
			})
			if i >= 0 {
				tabs = append(tabs, m.prTabs[i])
			} else {
				tabs = append(tabs, &pluginTab{id: lastPluginViewId.Add(1), prTab: t})
			}
		}
	}
	m.prTabs = tabs
	m.syncTabs()
	if selected == nil {
		return
	}
	if i := m.pluginTabIndex(selected); i >= 0 {
		m.carousel.SetCursor(i)
	} else {
		m.carousel.SetCursor(filesTab)
	}
}

// openPluginTab opens a PR's tab once it's first selected, returning the
// command starting its view.
func (m *Model) openPluginTab(tab *pluginTab) tea.Cmd {
	if tab == nil || tab.view != nil {
		return nil
	}
	tab.view = tab.prTab.Provider.OpenTab(plugins.Env{Ctx: m.ctx}, m.pluginPR(), tab.prTab.Key)
	m.syncTabs()
	return m.wrapPluginCmd(tab, tab.view.Init())
}

// renderPluginPanels renders the panels the enabled plugins show in the
// overview, e.g. who approved the PR.
func (m *Model) renderPluginPanels() string {
	panels := plugins.Panels(m.pluginPR())
	rendered := make([]string, 0, len(panels))
	for _, p := range panels {
		rendered = append(rendered, m.renderPluginPanel(p))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rendered...)
}

func (m *Model) statusGlyph(s plugins.Status) string {
	switch s {
	case plugins.StatusSuccess:
		return m.ctx.Styles.Common.SuccessGlyph
	case plugins.StatusFailure:
		return m.ctx.Styles.Common.FailureGlyph
	case plugins.StatusPending:
		return m.ctx.Styles.Common.WaitingGlyph
	default:
		return m.ctx.Styles.Common.FaintTextStyle.Render("•")
	}
}

// renderPluginPanel renders a panel in a box like the checks overview's,
// colored by how things stand.
func (m *Model) renderPluginPanel(p plugins.Panel) string {
	w := m.getIndentedContentWidth()
	borderColor := m.ctx.Theme.FaintBorder
	switch p.Status {
	case plugins.StatusSuccess:
		borderColor = m.ctx.Theme.SuccessText
	case plugins.StatusFailure:
		borderColor = m.ctx.Theme.ErrorText
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(w).
		Padding(0, 1)

	// The rows wrap within the box, below their glyph
	rowWidth := max(1, w-box.GetHorizontalFrameSize()-2)
	lines := []string{m.statusGlyph(p.Status) + " " + lipgloss.NewStyle().Bold(true).Render(p.Title)}
	if p.Subtitle != "" {
		lines = append(lines, m.ctx.Styles.Common.FaintTextStyle.MarginLeft(2).Render(p.Subtitle))
	}
	if len(p.Rows) > 0 {
		lines = append(lines, "")
	}
	for _, r := range p.Rows {
		glyph := r.Glyph
		if glyph == "" {
			glyph = m.statusGlyph(r.Status)
		}
		style := lipgloss.NewStyle().Width(rowWidth)
		if r.Faint {
			style = style.Foreground(m.ctx.Theme.FaintText)
		}
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, glyph, " ", style.Render(r.Text)))
	}
	return box.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// CheckAction returns what opening the check at the given index, as listed,
// does when a plugin opens it, e.g. "view tests", or "" when it's opened in
// the browser.
func (m Model) CheckAction(i int) string {
	if !m.hasData() {
		return ""
	}
	items := m.checkItems()
	if i < 0 || i >= len(items) {
		return ""
	}
	if _, label, ok := plugins.CheckOpenerFor(items[i].check); ok {
		return label
	}
	return ""
}

// OpenCheckInPlugin opens the check at the given index, as listed, in a tab
// of its own when a plugin opens it, and switches to it. It reports whether
// a plugin opened it, along with the command starting the plugin's view.
func (m *Model) OpenCheckInPlugin(i int) (tea.Cmd, bool) {
	if !m.hasData() {
		return nil, false
	}
	items := m.checkItems()
	if i < 0 || i >= len(items) {
		return nil, false
	}
	opener, _, ok := plugins.CheckOpenerFor(items[i].check)
	if !ok {
		return nil, false
	}
	check := items[i].check
	view := opener.OpenCheck(m.pluginPR(), check)
	m.checkTab = &pluginTab{id: lastPluginViewId.Add(1), view: view, check: &check}
	m.syncTabs()
	m.setTab(m.pluginTabIndex(m.checkTab))
	return m.wrapPluginCmd(m.checkTab, view.Init()), true
}

// wrapPluginCmd wraps the messages of the command of a plugin tab's view so
// they find their way back to it.
func (m *Model) wrapPluginCmd(tab *pluginTab, cmd tea.Cmd) tea.Cmd {
	id := tab.id
	return plugins.WrapCmd(cmd, func(msg tea.Msg) tea.Msg {
		return PluginViewMsg{Id: id, Msg: msg}
	})
}

// UpdatePluginView delivers a message to the plugin view it's meant for. It
// reports whether the view is still open to take it.
func (m *Model) UpdatePluginView(msg PluginViewMsg) (tea.Cmd, bool) {
	i := slices.IndexFunc(m.pluginTabs(), func(tab *pluginTab) bool {
		return tab.id == msg.Id && tab.view != nil
	})
	if i < 0 {
		return nil, false
	}
	tab := m.pluginTabs()[i]
	cmd := m.wrapPluginCmd(tab, tab.view.Update(msg.Msg))
	// e.g. its title counts what it found
	m.syncTabs()
	return cmd, true
}

// viewPluginTab renders the plugin's view along with where its items start.
func (m Model) viewPluginTab(tab *pluginTab) (string, []common.CommentAnchor) {
	body, items := tab.view.Render(plugins.Env{Ctx: m.ctx}, m.getIndentedContentWidth())
	tab.items = items
	anchors := make([]common.CommentAnchor, 0, len(items))
	for i, it := range items {
		idx := i
		anchors = append(anchors, common.CommentAnchor{Line: it.Line, Plugin: &idx})
	}
	return lipgloss.NewStyle().Padding(0, m.ctx.Styles.Sidebar.ContentPadding).Render(body), anchors
}

// IsPluginTab reports whether a plugin's view is the selected tab.
func (m Model) IsPluginTab() bool {
	tab := m.selectedPluginTab()
	return tab != nil && tab.view != nil
}

// PluginItem returns the selected plugin view's item at the given index, as
// last rendered.
func (m Model) PluginItem(i int) (plugins.Item, bool) {
	tab := m.selectedPluginTab()
	if tab == nil || i < 0 || i >= len(tab.items) {
		return plugins.Item{}, false
	}
	return tab.items[i], true
}

// SetFocusedPluginItem tells the selected plugin's view which of its items
// is focused, or -1 for none. It reports whether the view changed.
func (m *Model) SetFocusedPluginItem(i int) bool {
	tab := m.selectedPluginTab()
	if tab == nil || tab.view == nil {
		return false
	}
	return tab.view.Focus(i)
}

// ActivatePluginItem acts on the selected plugin view's item at the given
// index, e.g. a button loading more tests.
func (m *Model) ActivatePluginItem(i int) tea.Cmd {
	tab := m.selectedPluginTab()
	if tab == nil || tab.view == nil {
		return nil
	}
	return m.wrapPluginCmd(tab, tab.view.Activate(i))
}

// PluginTabDiff returns the diff of the selected plugin's view, when it has
// one to show instead of the PR's, e.g. the PR's conflicts.
func (m Model) PluginTabDiff() (string, bool) {
	tab := m.selectedPluginTab()
	if tab == nil {
		return "", false
	}
	if d, ok := tab.view.(plugins.Differ); ok {
		return d.Diff()
	}
	return "", false
}

// CheckCommands returns the name of the check at the given index, as listed,
// along with the commands the enabled plugins offer on it, e.g. rerunning
// its job.
func (m *Model) CheckCommands(i int) (string, []plugins.CheckCommand) {
	if !m.hasData() {
		return "", nil
	}
	items := m.checkItems()
	if i < 0 || i >= len(items) {
		return "", nil
	}
	check := items[i].check
	return check.Name, plugins.CheckCommands(m.pluginPR(), check)
}

// PluginTabCheckCommands returns the name of the check whose tab is
// selected, along with the commands the enabled plugins offer on it as it's
// listed now, e.g. rerunning its job.
func (m *Model) PluginTabCheckCommands() (string, []plugins.CheckCommand) {
	tab := m.selectedPluginTab()
	if tab == nil || tab.check == nil || !m.hasData() {
		return "", nil
	}
	check := *tab.check
	for _, it := range m.checkItems() {
		if it.check.Name == check.Name {
			// Its state may have changed since, e.g. once it was rerun
			check = it.check
			break
		}
	}
	return check.Name, plugins.CheckCommands(m.pluginPR(), check)
}
