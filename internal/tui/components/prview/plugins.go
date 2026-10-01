package prview

import (
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/plugins"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/fuzzyselect"
)

// pluginTab is a plugin's view shown in a tab of its own.
type pluginTab struct {
	// id tells the view's messages apart from those of views opened before
	id   int64
	view plugins.View
	// items are the view's focusable items as last rendered
	items []plugins.Item
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
	view := opener.OpenCheck(m.pluginPR(), items[i].check)
	m.pluginTab = &pluginTab{id: lastPluginViewId.Add(1), view: view}
	m.syncTabs()
	m.setTab(pluginViewTab)
	return m.wrapPluginCmd(view.Init()), true
}

// wrapPluginCmd wraps the messages of the plugin view's command so they find
// their way back to it.
func (m *Model) wrapPluginCmd(cmd tea.Cmd) tea.Cmd {
	id := m.pluginTab.id
	return plugins.WrapCmd(cmd, func(msg tea.Msg) tea.Msg {
		return PluginViewMsg{Id: id, Msg: msg}
	})
}

// UpdatePluginView delivers a message to the plugin view it's meant for. It
// reports whether the view is still open to take it.
func (m *Model) UpdatePluginView(msg PluginViewMsg) (tea.Cmd, bool) {
	if m.pluginTab == nil || m.pluginTab.id != msg.Id {
		return nil, false
	}
	return m.wrapPluginCmd(m.pluginTab.view.Update(msg.Msg)), true
}

// viewPluginTab renders the plugin's view along with where its items start.
func (m Model) viewPluginTab() (string, []common.CommentAnchor) {
	body, items := m.pluginTab.view.Render(plugins.Env{Ctx: m.ctx}, m.getIndentedContentWidth())
	m.pluginTab.items = items
	anchors := make([]common.CommentAnchor, 0, len(items))
	for i, it := range items {
		idx := i
		anchors = append(anchors, common.CommentAnchor{Line: it.Line, Plugin: &idx})
	}
	return lipgloss.NewStyle().Padding(0, m.ctx.Styles.Sidebar.ContentPadding).Render(body), anchors
}

// IsPluginTab reports whether a plugin's view is the selected tab.
func (m Model) IsPluginTab() bool {
	return m.pluginTab != nil && m.carousel.Cursor() == pluginViewTab
}

// PluginItem returns the plugin view's item at the given index, as last
// rendered.
func (m Model) PluginItem(i int) (plugins.Item, bool) {
	if m.pluginTab == nil || i < 0 || i >= len(m.pluginTab.items) {
		return plugins.Item{}, false
	}
	return m.pluginTab.items[i], true
}

// SetFocusedPluginItem tells the plugin's view which of its items is
// focused, or -1 for none. It reports whether the view changed.
func (m *Model) SetFocusedPluginItem(i int) bool {
	if m.pluginTab == nil {
		return false
	}
	return m.pluginTab.view.Focus(i)
}

// ActivatePluginItem acts on the plugin view's item at the given index,
// e.g. a button loading more tests.
func (m *Model) ActivatePluginItem(i int) tea.Cmd {
	if m.pluginTab == nil {
		return nil
	}
	return m.wrapPluginCmd(m.pluginTab.view.Activate(i))
}
