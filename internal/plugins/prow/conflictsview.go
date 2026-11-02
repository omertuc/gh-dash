package prow

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dlvhdr/gh-dash/v4/internal/plugins"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
)

// conflictsView lists the files a PR conflicts in with its target branch,
// whose conflicts are shown in the diff pager: all of them with the diff
// key, or the focused file's when it's activated.
type conflictsView struct {
	req   conflictsRequest
	cache *conflictsCache

	loading bool
	err     error
	res     *mergeResult

	// focused is the path of the focused file, whose details are shown
	focused string
	// items are the focusable items as last rendered: the index of a
	// conflict, or -1 for checking again
	items []int
}

type conflictsMsg struct {
	res *mergeResult
	err error
}

func newConflictsView(req conflictsRequest, cache *conflictsCache) *conflictsView {
	return &conflictsView{req: req, cache: cache}
}

// Title counts the conflicting files once they're known, e.g. "Conflicts
// (3)".
func (v *conflictsView) Title() string {
	if v.res != nil && len(v.res.conflicts) > 0 {
		return fmt.Sprintf("%s (%d)", conflictsTitle, len(v.res.conflicts))
	}
	return conflictsTitle
}

// Init shows the PR's conflicts right away when they were found recently,
// and otherwise finds them.
func (v *conflictsView) Init() tea.Cmd {
	if v.req.err != nil {
		v.err = v.req.err
		return nil
	}
	if res, ok := v.cache.peek(v.req); ok {
		v.Update(conflictsMsg{res: res})
		return nil
	}
	return v.find(false)
}

// find finds the PR's conflicts, reusing recently found ones unless fresh.
func (v *conflictsView) find(fresh bool) tea.Cmd {
	v.loading = true
	v.err = nil
	c, req := v.cache, v.req
	return func() tea.Msg {
		res, err := c.get(req, fresh)
		return conflictsMsg{res: res, err: err}
	}
}

func (v *conflictsView) Update(msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(conflictsMsg); ok {
		v.loading = false
		v.res, v.err = msg.res, msg.err
	}
	return nil
}

func (v *conflictsView) Focus(i int) bool {
	path := ""
	if i >= 0 && i < len(v.items) && v.items[i] >= 0 {
		path = v.res.conflicts[v.items[i]].path
	}
	if path == v.focused {
		return false
	}
	v.focused = path
	return true
}

func (v *conflictsView) Activate(i int) tea.Cmd {
	if i < 0 || i >= len(v.items) {
		return nil
	}
	if v.items[i] < 0 {
		return v.find(true)
	}
	if diff := v.res.conflicts[v.items[i]].diff; diff != "" {
		return plugins.ShowDiff(diff)
	}
	return nil
}

// Diff shows all the conflicts once they're found.
func (v *conflictsView) Diff() (string, bool) {
	if v.loading || v.res == nil {
		return "", false
	}
	diff := v.res.diff()
	return diff, diff != ""
}

func (v *conflictsView) Render(env plugins.Env, width int) (string, []plugins.Item) {
	ctx := env.Ctx
	c := ctx.Styles.Common
	faint := c.FaintTextStyle
	bold := lipgloss.NewStyle().Bold(true)
	link := lipgloss.NewStyle().Foreground(ctx.Theme.PrimaryText)
	wrap := lipgloss.NewStyle().Width(width)

	var lines []string
	var items []plugins.Item
	v.items = v.items[:0]
	add := func(s string) {
		lines = append(lines, strings.Split(s, "\n")...)
	}
	addItem := func(i int, s, hint string, clickable bool) {
		items = append(items, plugins.Item{Line: len(lines), Hint: hint, Clickable: clickable})
		v.items = append(v.items, i)
		add(s)
	}
	fit := func(s string) string { return ansi.Truncate(s, width, constants.Ellipsis) }
	checkAgain := func() {
		add("")
		addItem(-1, link.Render("↻ Check again"), "check again", true)
	}

	add(bold.Underline(true).Render(fit(v.Title())))
	add("")

	switch {
	case v.loading:
		add(wrap.Render(c.WaitingGlyph + " " + faint.Render(fmt.Sprintf(
			"Merging the PR into %s in %s…", v.req.base, shortenHome(v.req.repoDir)))))
	case v.err != nil:
		add(wrap.Render(c.FailureGlyph + " Couldn't find the conflicts: " + v.err.Error()))
		if v.req.err == nil {
			checkAgain()
		}
	case v.res != nil && len(v.res.conflicts) == 0:
		add(wrap.Render(c.SuccessGlyph + " " + fmt.Sprintf("Merges cleanly into %s now", v.req.base)))
		add(wrap.Render(faint.Render("GitHub or Prow may not have caught up yet")))
		add(faint.Render(fit(v.mergedRefs())))
		checkAgain()
	case v.res != nil:
		files := "files conflict"
		if len(v.res.conflicts) == 1 {
			files = "file conflicts"
		}
		add(wrap.Render(c.FailureGlyph + " " + fmt.Sprintf("Needs a rebase: %d %s with %s",
			len(v.res.conflicts), files, v.req.base)))
		add(faint.Render(fit(v.mergedRefs())))
		add("")
		for i, conflict := range v.res.conflicts {
			line := v.renderConflict(env, width, conflict)
			hint := ""
			if conflict.diff != "" {
				hint = "show its conflicts"
			}
			if conflict.path == v.focused {
				if details := v.renderDetails(env, width, conflict); details != "" {
					line += "\n" + details
				}
			}
			addItem(i, line, hint, false)
		}
		checkAgain()
		if v.res.diff() != "" {
			add("")
			add(wrap.Render(faint.Render(fmt.Sprintf(
				"%s shows all the conflicts in your diff pager, %s just the focused file's",
				keys.HintKeys(keys.PRKeys.Diff), keys.HintKeys(keys.NotificationKeys.ActivateItem)))))
		}
	}
	return strings.Join(lines, "\n"), items
}

// mergedRefs tells what was merged, e.g. "abc1234 into main at def5678".
func (v *conflictsView) mergedRefs() string {
	return fmt.Sprintf("%s into %s at %s", short(v.res.headOid), v.req.base, short(v.res.baseOid))
}

func short(oid string) string {
	return oid[:min(len(oid), 7)]
}

// renderConflict renders a file's line: its path, with the kinds of its
// conflicts at the right edge.
func (v *conflictsView) renderConflict(env plugins.Env, width int, c conflict) string {
	left := env.Ctx.Styles.Common.FailureGlyph + " " + c.path
	right := env.Ctx.Styles.Common.FaintTextStyle.Render(strings.Join(c.kinds, ", "))
	space := width - lipgloss.Width(right) - 1
	if lipgloss.Width(left) > space {
		left = ansi.Truncate(left, max(0, space), constants.Ellipsis)
	}
	return left + strings.Repeat(" ", max(1, width-lipgloss.Width(left)-lipgloss.Width(right))) + right
}

// renderDetails renders a focused file's details below it, along a line down
// its side: its full path, when it doesn't fit, and how it conflicts.
func (v *conflictsView) renderDetails(env plugins.Env, width int, c conflict) string {
	ctx := env.Ctx
	prefix := "  " + lipgloss.NewStyle().Foreground(ctx.Theme.FaintBorder).Render("│ ")
	wrap := lipgloss.NewStyle().Width(max(1, width-lipgloss.Width(prefix)))

	var lines []string
	if lipgloss.Width(c.path)+2 > width-lipgloss.Width(strings.Join(c.kinds, ", "))-1 {
		lines = append(lines, strings.Split(wrap.Render(c.path), "\n")...)
	}
	for _, msg := range c.messages {
		lines = append(lines, strings.Split(ctx.Styles.Common.FaintTextStyle.Render(wrap.Render(msg)), "\n")...)
	}
	if c.diff == "" {
		lines = append(lines, ctx.Styles.Common.FaintTextStyle.Render(
			wrap.Render(fmt.Sprintf("Kept as %s has it, so there's no diff to show", v.req.base))))
	}
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}
