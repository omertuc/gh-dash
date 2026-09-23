package sidebar

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
)

type Model struct {
	IsOpen     bool
	data       string
	viewport   viewport.Model
	ctx        *context.ProgramContext
	emptyState string

	// header is shown above the scrolling content and doesn't scroll with it
	header string
	body   string
	// footer is shown below the scrolling content, e.g. an editor
	footer string
	// viewportContent is what was last put in the viewport
	viewportContent string
	// headerIsSticky is whether the viewport content was laid out with the
	// header fixed above it (true) or scrolling along with it (false)
	headerIsSticky bool
	// contentHeight is the height available for the header and content
	contentHeight int

	navKeysScroll bool

	// anchors are the body lines where focusable items (comments) start
	anchors []int
	// focus is the index of the focused item, or -1 for none
	focus int
	// focusHint is shown on the focused item's title line
	focusHint string
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
		focus:      -1,
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

		return style.Render(m.renderContent())
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

	return style.Render(m.renderContent())
}

func (m Model) renderContent() string {
	parts := []string{m.highlightFocused(m.viewport.View())}
	if m.footer != "" {
		parts = append(parts, m.footer)
	}
	parts = append(parts, m.renderPager())
	if m.headerIsSticky {
		parts = append([]string{trimTrailingBlankLines(m.header), m.renderScrolledIndicator()}, parts...)
	}
	return lipgloss.JoinVertical(lipgloss.Top, parts...)
}

// renderScrolledIndicator renders the line between a sticky header and the
// content, which shows an arrow when there's more content scrolled out of
// view above. The line is always there so the layout doesn't shift.
func (m Model) renderScrolledIndicator() string {
	indicator := ""
	if !m.viewport.AtTop() {
		indicator = "▲"
	}
	return lipgloss.NewStyle().
		Width(m.viewport.Width()).
		Align(lipgloss.Center).
		Foreground(m.ctx.Theme.FaintText).
		Render(indicator)
}

// stickyHeaderHeight is the height taken above the content by a sticky
// header: the header itself, without trailing blank lines since the
// indicator line separates it from the content, plus the indicator line.
func (m Model) stickyHeaderHeight() int {
	return lipgloss.Height(trimTrailingBlankLines(m.header)) + 1
}

func trimTrailingBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	for len(lines) > 1 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

func (m Model) contentHeightForViewport() int {
	if m.headerIsSticky {
		return m.availableHeight() - m.stickyHeaderHeight()
	}
	return m.availableHeight()
}

// availableHeight is the height left for the header and content after the
// footer.
func (m Model) availableHeight() int {
	if m.footer == "" {
		return m.contentHeight
	}
	return m.contentHeight - lipgloss.Height(m.footer)
}

// stickyHeader reports whether the header is shown fixed above the content.
// A header taking most of the space would leave too little room to scroll, so
// then it scrolls along with the content instead.
func (m Model) stickyHeader() bool {
	return m.header != "" && m.stickyHeaderHeight() <= m.availableHeight()/2
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
		label := "scroll"
		if len(m.anchors) > 0 {
			label = "comment"
		}
		hints = append(hints, hint{pairHint(keys.Keys.Up, keys.Keys.Down, label), 1})
	}
	if scrollable {
		hints = append(hints, hint{pairHint(keys.Keys.PageUp, keys.Keys.PageDown, "page"), 2})
	}
	if scrollable && m.navKeysScroll {
		hints = append(hints, hint{pairHint(keys.Keys.FirstLine, keys.Keys.LastLine, ""), 3})
	}
	if m.navKeysScroll {
		hints = append(hints, hint{keys.HintKeys(keys.NotificationKeys.BackToNotification) + " dismiss", 0})
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
	keysText := keys.HintKeyPair(up, down)
	if label == "" {
		return keysText
	}
	return keysText + " " + label
}

// SetNavKeysScroll sets whether the navigation keys (j/k, g/G) scroll the
// sidebar, so the pager can show them as hints.
func (m *Model) SetNavKeysScroll(navKeysScroll bool) {
	m.navKeysScroll = navKeysScroll
}

func (m *Model) SetContent(data string) {
	m.SetContentWithHeader("", data, "", nil)
}

// SetContentWithHeader sets content to scroll between a header and a footer
// that stay in place, e.g. a PR's title and a comment being written. anchors
// are the lines of data where focusable items (comments) start, which the
// navigation keys move between.
func (m *Model) SetContentWithHeader(header, data, footer string, anchors []int) {
	if !slices.Equal(anchors, m.anchors) {
		m.focus = -1
	}
	m.anchors = anchors
	m.header = header
	m.footer = footer
	m.body = data
	m.data = header + data
	m.layout()
}

// layout sizes the viewport and fills it, with the header either fixed above
// it or, when there's not enough room for that, scrolling along with it.
func (m *Model) layout() {
	m.headerIsSticky = m.stickyHeader()
	height := m.availableHeight()
	content := m.body
	if m.headerIsSticky {
		height -= m.stickyHeaderHeight()
	} else if m.header != "" {
		content = lipgloss.JoinVertical(lipgloss.Left, m.header, m.body)
	}
	m.viewport.SetHeight(max(0, height))
	// Setting content measures every line, and this runs on every keystroke
	// while writing a comment, usually with the same content
	if content != m.viewportContent {
		m.viewportContent = content
		m.viewport.SetContent(content)
	}
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
		m.contentHeight = m.ctx.DynamicPreviewHeight - m.ctx.Styles.Sidebar.PagerHeight
	} else {
		m.contentHeight = m.ctx.MainContentHeight - m.ctx.Styles.Sidebar.PagerHeight
	}
	if m.header != "" && m.stickyHeader() != m.headerIsSticky {
		m.layout()
	} else {
		m.viewport.SetHeight(max(0, m.contentHeightForViewport()))
	}
	m.viewport.SetWidth(m.GetSidebarContentWidth())
}
