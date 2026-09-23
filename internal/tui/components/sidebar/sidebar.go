package sidebar

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
)

type Model struct {
	IsOpen     bool
	data       string
	viewport   viewport.Model
	ctx        *context.ProgramContext
	emptyState string

	navKeysScroll bool
}

func NewModel() Model {
	vp := viewport.New(
		viewport.WithWidth(0),
		viewport.WithHeight(0),
	)

	return Model{
		IsOpen:     false,
		data:       "",
		viewport:   vp,
		ctx:        nil,
		emptyState: "Nothing selected...",
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Keys.PageDown):
			m.viewport.HalfPageDown()

		case key.Matches(msg, keys.Keys.PageUp):
			m.viewport.HalfPageUp()
		}
	}

	return m, nil
}

func (m Model) View() string {
	if !m.IsOpen {
		return ""
	}

	if m.ctx.PreviewPosition == "bottom" {
		height := m.ctx.DynamicPreviewHeight
		width := m.ctx.DynamicPreviewWidth
		style := m.ctx.Styles.Sidebar.BottomRoot.
			Height(height).
			Width(width)

		if m.data == "" {
			return style.Align(lipgloss.Center).Render(
				lipgloss.PlaceVertical(height, lipgloss.Center, m.emptyState),
			)
		}

		return style.Render(lipgloss.JoinVertical(
			lipgloss.Top,
			m.viewport.View(),
			m.renderPager(),
		))
	}

	// Right mode
	height := m.ctx.MainContentHeight
	style := m.ctx.Styles.Sidebar.Root.
		Height(height).
		Width(m.ctx.DynamicPreviewWidth)

	if m.data == "" {
		return style.Align(lipgloss.Center).Render(
			lipgloss.PlaceVertical(height, lipgloss.Center, m.emptyState),
		)
	}

	return style.Render(lipgloss.JoinVertical(
		lipgloss.Top,
		m.viewport.View(),
		m.renderPager(),
	))
}

// renderPager renders the scroll percentage followed by hints for the keys
// that move the preview, grouped by action. The hints don't depend on the
// scroll position so they stay put while scrolling. Hints that don't fit the
// width are dropped, least important first.
func (m Model) renderPager() string {
	scrollable := m.viewport.TotalLineCount() > m.viewport.Height()

	type hint struct {
		text     string
		priority int // lower is kept first when space runs out
	}
	var hints []hint
	if scrollable && m.navKeysScroll {
		hints = append(hints, hint{pairHint(keys.Keys.Up, keys.Keys.Down, "scroll"), 1})
	}
	if scrollable {
		hints = append(hints, hint{pairHint(keys.Keys.PageUp, keys.Keys.PageDown, "page"), 2})
	}
	if scrollable && m.navKeysScroll {
		hints = append(hints, hint{pairHint(keys.Keys.FirstLine, keys.Keys.LastLine, ""), 3})
	}
	if m.navKeysScroll {
		hints = append(hints, hint{bindingKeys(keys.NotificationKeys.BackToNotification) + " dismiss", 0})
	}

	const separator = " · "
	// Fixed width so the hints don't shift as the percentage changes
	pager := fmt.Sprintf("%3d%%", int(m.viewport.ScrollPercent()*100))

	// Pick hints by priority until the width runs out, then show them in order
	byPriority := slices.Clone(hints)
	slices.SortStableFunc(byPriority, func(a, b hint) int { return a.priority - b.priority })
	width := lipgloss.Width(pager)
	shown := map[string]bool{}
	for _, h := range byPriority {
		w := lipgloss.Width(separator + h.text)
		if m.viewport.Width() > 0 && width+w > m.viewport.Width() {
			continue
		}
		width += w
		shown[h.text] = true
	}
	for _, h := range hints {
		if shown[h.text] {
			pager += separator + h.text
		}
	}

	return m.ctx.Styles.Sidebar.PagerStyle.Render(pager)
}

// pairHint describes a pair of up/down bindings followed by a label, e.g.
// "k/↑ j/↓ scroll".
func pairHint(up, down key.Binding, label string) string {
	keysText := combinedBindingKeys(up, down)
	if label == "" {
		return keysText
	}
	return keysText + " " + label
}

// combinedBindingKeys lists the keys of an up/down pair, sharing a common
// modifier to save space, e.g. "Ctrl+u/d" rather than "Ctrl+u Ctrl+d".
func combinedBindingKeys(up, down key.Binding) string {
	upName, downName := bindingKeys(up), bindingKeys(down)
	i := strings.LastIndex(upName, "+")
	if len(up.Keys()) == 1 && len(down.Keys()) == 1 && i > 0 &&
		strings.HasPrefix(downName, upName[:i+1]) {
		return upName + "/" + downName[i+1:]
	}
	return upName + " " + downName
}

// bindingKeys lists all of a binding's keys, e.g. "k/↑".
func bindingKeys(b key.Binding) string {
	var names []string
	for _, k := range b.Keys() {
		names = append(names, keyName(k))
	}
	// Show plain characters first since they're what people usually type,
	// then symbols like arrows, then named keys
	slices.SortStableFunc(names, func(a, b string) int {
		return cmp.Compare(keyNameRank(a), keyNameRank(b))
	})
	return strings.Join(names, "/")
}

func keyNameRank(name string) int {
	switch {
	case len(name) == 1:
		return 0
	case len([]rune(name)) == 1:
		return 1
	}
	return 2
}

var specialKeyNames = map[string]string{
	"up":     "↑",
	"down":   "↓",
	"left":   "←",
	"right":  "→",
	"pgup":   "PgUp",
	"pgdown": "PgDn",
}

// keyName formats a key for display, e.g. "ctrl+d" as "Ctrl+d" and "up" as "↑".
func keyName(k string) string {
	if name, ok := specialKeyNames[k]; ok {
		return name
	}
	parts := strings.Split(k, "+")
	for i, part := range parts {
		if len([]rune(part)) > 1 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, "+")
}

// SetNavKeysScroll sets whether the navigation keys (j/k, g/G) scroll the
// sidebar, so the pager can show them as hints.
func (m *Model) SetNavKeysScroll(navKeysScroll bool) {
	m.navKeysScroll = navKeysScroll
}

func (m *Model) SetContent(data string) {
	m.data = data
	m.viewport.SetContent(data)
}

func (m *Model) GetSidebarContentWidth() int {
	if m.ctx == nil || m.ctx.Config == nil {
		return 0
	}
	if m.ctx.PreviewPosition == "bottom" {
		return max(0, m.ctx.DynamicPreviewWidth)
	}
	return max(0, m.ctx.DynamicPreviewWidth-m.ctx.Styles.Sidebar.BorderWidth)
}

func (m *Model) ScrollToTop() {
	m.viewport.GotoTop()
}

func (m *Model) ScrollToBottom() {
	m.viewport.GotoBottom()
}

func (m *Model) ScrollDown(lines int) {
	m.viewport.ScrollDown(lines)
}

func (m *Model) ScrollUp(lines int) {
	m.viewport.ScrollUp(lines)
}

func (m *Model) YOffset() int {
	return m.viewport.YOffset()
}

func (m *Model) ScrollToPercent(percent float64) {
	totalLines := m.viewport.TotalLineCount()
	targetLine := int(float64(totalLines) * percent)
	m.viewport.SetYOffset(targetLine)
}

func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) {
	if ctx == nil {
		return
	}
	m.ctx = ctx
	if m.ctx.PreviewPosition == "bottom" {
		m.viewport.SetHeight(m.ctx.DynamicPreviewHeight - m.ctx.Styles.Sidebar.PagerHeight)
	} else {
		m.viewport.SetHeight(m.ctx.MainContentHeight - m.ctx.Styles.Sidebar.PagerHeight)
	}
	m.viewport.SetWidth(m.GetSidebarContentWidth())
}
