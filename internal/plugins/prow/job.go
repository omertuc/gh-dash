package prow

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dlvhdr/gh-dash/v4/internal/plugins"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

// jobView lists a Prow job's tests by how they went. Only the failed tests
// are listed up front: the rest, of which there may be thousands, are listed
// a page at a time, as asked for.
type jobView struct {
	check   plugins.Check
	job     jobRef
	opts    Options
	fetcher *fetcher

	loading bool
	err     error
	results *jobResults

	expanded [numTestStates]bool
	// shown is how many of each state's tests are listed when expanded
	shown [numTestStates]int
	// focused is the key of the focused item, whose details are shown when
	// it's a test
	focused string

	// items are the focusable items as last rendered
	items []jobItem
}

type jobItemKind int

const (
	itemOpen jobItemKind = iota
	itemRetry
	itemGroup
	itemTest
	itemMore
)

type jobItem struct {
	kind  jobItemKind
	state testState
	test  *testResult
}

func (it jobItem) key() string {
	switch it.kind {
	case itemTest:
		return "test/" + it.test.key()
	case itemGroup, itemMore:
		return fmt.Sprintf("%d/%d", it.kind, it.state)
	default:
		return fmt.Sprint(it.kind)
	}
}

type resultsMsg struct {
	results *jobResults
	err     error
}

func newJobView(check plugins.Check, job jobRef, opts Options) *jobView {
	return &jobView{
		check:   check,
		job:     job,
		opts:    opts,
		fetcher: newFetcher(opts.ArtifactsURL),
	}
}

// Title names the tab after the job, e.g. "e2e-aws" for "ci/prow/e2e-aws".
func (v *jobView) Title() string {
	name := v.job.name
	if n, ok := jobNameFromContext(v.check); ok {
		name = n
	}
	return " " + name
}

func (v *jobView) Init() tea.Cmd {
	return v.fetch()
}

func (v *jobView) fetch() tea.Cmd {
	v.loading = true
	v.err = nil
	f, job := v.fetcher, v.job
	return func() tea.Msg {
		res, err := f.fetchResults(job)
		return resultsMsg{results: res, err: err}
	}
}

func (v *jobView) Update(msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(resultsMsg); ok {
		v.loading = false
		v.err = msg.err
		v.results = msg.results
		if v.results != nil {
			// The failed tests are what's usually looked for
			if n := len(v.results.tests[testFailed]); n > 0 {
				v.expanded[testFailed] = true
				v.shown[testFailed] = min(n, v.opts.PageSize)
			}
		}
	}
	return nil
}

func (v *jobView) Focus(i int) bool {
	key := ""
	if i >= 0 && i < len(v.items) {
		key = v.items[i].key()
	}
	if key == v.focused {
		return false
	}
	// Only a test shows more when it's focused
	changed := strings.HasPrefix(key, "test/") || strings.HasPrefix(v.focused, "test/")
	v.focused = key
	return changed
}

func (v *jobView) Activate(i int) tea.Cmd {
	if i < 0 || i >= len(v.items) {
		return nil
	}
	it := v.items[i]
	switch it.kind {
	case itemOpen:
		return plugins.OpenURL(v.job.url)
	case itemRetry:
		return v.fetch()
	case itemGroup:
		if v.expanded[it.state] {
			v.expanded[it.state] = false
		} else {
			v.expanded[it.state] = true
			v.shown[it.state] = max(v.shown[it.state],
				min(v.opts.PageSize, len(v.results.tests[it.state])))
		}
	case itemMore:
		v.shown[it.state] = min(v.shown[it.state]+v.opts.PageSize,
			len(v.results.tests[it.state]))
	}
	return nil
}

var stateNames = [numTestStates]string{"Failed", "Flaky", "Passed", "Skipped"}

func (v *jobView) glyph(env plugins.Env, state testState) string {
	c := env.Ctx.Styles.Common
	switch state {
	case testFailed:
		return c.FailureGlyph
	case testFlaky:
		return lipgloss.NewStyle().Foreground(env.Ctx.Theme.WarningText).Render("≈")
	case testSkipped:
		return lipgloss.NewStyle().Foreground(env.Ctx.Theme.FaintText).Render("⊘")
	default:
		return c.SuccessGlyph
	}
}

func (v *jobView) Render(env plugins.Env, width int) (string, []plugins.Item) {
	ctx := env.Ctx
	faint := ctx.Styles.Common.FaintTextStyle
	bold := lipgloss.NewStyle().Bold(true)

	var lines []string
	var items []plugins.Item
	v.items = v.items[:0]
	add := func(s string) {
		lines = append(lines, strings.Split(s, "\n")...)
	}
	addItem := func(it jobItem, s, hint string, clickable bool) {
		items = append(items, plugins.Item{Line: len(lines), Hint: hint, Clickable: clickable})
		v.items = append(v.items, it)
		add(s)
	}
	fit := func(s string) string { return ansi.Truncate(s, width, constants.Ellipsis) }

	// The job
	add(bold.Underline(true).Render(fit(v.Title())))
	add("")
	add(fit(v.renderJobStatus(env)))
	add(faint.Render(fit(v.job.name + " #" + v.job.buildID)))
	add("")
	addItem(jobItem{kind: itemOpen},
		lipgloss.NewStyle().Foreground(ctx.Theme.PrimaryText).Render("↗ Open job in browser"),
		"open", true)
	add("")

	switch {
	case v.loading:
		add(ctx.Styles.Common.WaitingGlyph + " " + faint.Render("Loading the test results…"))
	case errors.Is(v.err, errNotPublic):
		add(lipgloss.NewStyle().Width(width).Render(
			ctx.Styles.Common.FailureGlyph + " " + fmt.Sprintf(
				"The job's artifacts in gs://%s aren't public, so its tests can't be listed here. "+
					"Open the job in the browser instead.", v.job.bucket)))
	case v.err != nil:
		add(lipgloss.NewStyle().Width(width).Render(
			ctx.Styles.Common.FailureGlyph + " Couldn't load the test results: " + v.err.Error()))
		add("")
		addItem(jobItem{kind: itemRetry},
			lipgloss.NewStyle().Foreground(ctx.Theme.PrimaryText).Render("↻ Try again"),
			"retry", true)
	case v.results != nil:
		v.renderTests(env, width, add, addItem)
	}

	return strings.Join(lines, "\n"), items
}

// renderJobStatus renders how the job went, e.g. "✗ Failure · took 1h2m ·
// 5m ago".
func (v *jobView) renderJobStatus(env plugins.Env) string {
	c := env.Ctx.Styles.Common
	faint := c.FaintTextStyle
	state := v.check.State
	if v.results != nil && v.results.result != "" {
		state = v.results.result
	}
	glyph := c.SuccessGlyph
	switch stateRank(state) {
	case 0:
		glyph = c.FailureGlyph
	case 1:
		glyph = c.WaitingGlyph
	}
	if state == "ABORTED" {
		glyph = c.FailureGlyph
	}
	parts := []string{glyph + " " + strings.ToUpper(humanize(state)[:1]) + humanize(state)[1:]}
	if r := v.results; r != nil {
		switch {
		case !r.finished.IsZero() && !r.started.IsZero():
			parts = append(parts, faint.Render("took "+r.finished.Sub(r.started).Round(time.Second).String()),
				faint.Render(utils.TimeElapsed(r.finished)+" ago"))
		case !r.started.IsZero():
			parts = append(parts, faint.Render("started "+utils.TimeElapsed(r.started)+" ago"))
		}
	}
	return strings.Join(parts, faint.Render(" · "))
}

func (v *jobView) renderTests(
	env plugins.Env,
	width int,
	add func(string),
	addItem func(jobItem, string, string, bool),
) {
	ctx := env.Ctx
	faint := ctx.Styles.Common.FaintTextStyle
	r := v.results
	total := 0
	for _, tests := range r.tests {
		total += len(tests)
	}
	if total == 0 {
		msg := "The job didn't report any tests"
		if r.result == "" {
			msg = "The job is still running and hasn't reported any tests yet"
		}
		add(faint.Render(msg))
		return
	}

	// e.g. "4,210 tests · 2 failed · 1 flaky · 3,421 passed · 786 skipped"
	summary := []string{fmt.Sprintf("%s tests", thousands(total))}
	for s := range numTestStates {
		if n := len(r.tests[s]); n > 0 {
			summary = append(summary, fmt.Sprintf("%s %s", thousands(n), strings.ToLower(stateNames[s])))
		}
	}
	add(lipgloss.NewStyle().Width(width).Render(strings.Join(summary, faint.Render(" · "))))
	if r.result == "" {
		add(faint.Render("The job is still running, so more tests may come"))
	}
	if r.skippedFiles > 0 {
		add(faint.Render(fmt.Sprintf("%d junit files were too big to read", r.skippedFiles)))
	}

	for s := range numTestStates {
		tests := r.tests[s]
		if len(tests) == 0 {
			continue
		}
		add("")
		arrow := "▸"
		if v.expanded[s] {
			arrow = "▾"
		}
		header := fmt.Sprintf("%s %s %s", arrow, v.glyph(env, s),
			lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%s (%s)", stateNames[s], thousands(len(tests)))))
		hint := "collapse"
		if !v.expanded[s] {
			n := min(v.opts.PageSize, len(tests))
			load := "Load " + thousands(n)
			if n == len(tests) && n > 1 {
				load = "Load all " + thousands(n)
			}
			header += faint.Render(" · ") + lipgloss.NewStyle().Foreground(ctx.Theme.PrimaryText).Render(load)
			hint = "load"
		}
		addItem(jobItem{kind: itemGroup, state: s}, header, hint, true)
		if !v.expanded[s] {
			continue
		}

		shown := min(v.shown[s], len(tests))
		for _, t := range tests[:shown] {
			it := jobItem{kind: itemTest, state: s, test: t}
			line := v.renderTestTitle(env, width, t)
			if it.key() == v.focused {
				if details := v.renderTestDetails(env, width, t); details != "" {
					line += "\n" + details
				}
			}
			addItem(it, line, "", false)
		}
		if left := len(tests) - shown; left > 0 {
			n := min(v.opts.PageSize, left)
			more := lipgloss.NewStyle().Foreground(ctx.Theme.PrimaryText).
				Render(fmt.Sprintf("  Load %s more", thousands(n))) +
				faint.Render(fmt.Sprintf(" · %s not shown", thousands(left)))
			addItem(jobItem{kind: itemMore, state: s}, more, "load", true)
		}
	}
}

// renderTestTitle renders a test's line: its state and name, with how long
// it took at the right edge.
func (v *jobView) renderTestTitle(env plugins.Env, width int, t *testResult) string {
	left := "  " + v.glyph(env, t.state) + " " + t.name
	right := ""
	if t.duration > 0 {
		right = env.Ctx.Styles.Common.FaintTextStyle.Render(formatDuration(t.duration))
	}
	space := width - lipgloss.Width(right)
	if right != "" {
		space--
	}
	if lipgloss.Width(left) > space {
		left = ansi.Truncate(left, max(0, space), constants.Ellipsis)
	}
	if right == "" {
		return left
	}
	return left + strings.Repeat(" ", max(1, width-lipgloss.Width(left)-lipgloss.Width(right))) + right
}

const (
	// maxMessageLines is how much of a failure's message is shown
	maxMessageLines = 6
	// maxOutputLines is how much of the end of a failure's output is shown
	maxOutputLines = 12
)

// renderTestDetails renders a focused test's details below it, along a line
// down its side: its full name, suite, runs and why it failed.
func (v *jobView) renderTestDetails(env plugins.Env, width int, t *testResult) string {
	ctx := env.Ctx
	faint := ctx.Styles.Common.FaintTextStyle
	prefix := "    " + lipgloss.NewStyle().Foreground(ctx.Theme.FaintBorder).Render("│ ")
	w := max(1, width-lipgloss.Width(prefix))
	wrap := lipgloss.NewStyle().Width(w)

	var lines []string
	addWrapped := func(s string, style lipgloss.Style) {
		lines = append(lines, strings.Split(style.Render(wrap.Render(s)), "\n")...)
	}
	if lipgloss.Width(t.name)+4 > width {
		addWrapped(t.name, lipgloss.NewStyle())
	}
	if t.suite != "" {
		addWrapped(t.suite, faint)
	}
	switch {
	case t.state == testFlaky:
		addWrapped(fmt.Sprintf("Failed %d of %d runs", t.failures, t.runs), faint)
	case t.runs > 1:
		addWrapped(fmt.Sprintf("Ran %d times", t.runs), faint)
	}
	if t.message != "" {
		msg, cut := headLines(clean(t.message), maxMessageLines, w)
		addWrapped(msg, lipgloss.NewStyle().Foreground(ctx.Theme.ErrorText))
		if cut > 0 {
			lines = append(lines, faint.Render(fmt.Sprintf("… %d more lines", cut)))
		}
	}
	if t.output != "" && t.output != t.message {
		out, cut := tailLines(clean(t.output), maxOutputLines, w)
		if cut > 0 {
			lines = append(lines, faint.Render(fmt.Sprintf("… %d lines above", cut)))
		}
		addWrapped(out, lipgloss.NewStyle())
	}
	if len(lines) == 0 {
		return ""
	}
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// clean makes test output safe to show: without escape sequences, carriage
// returns or tabs.
func clean(s string) string {
	s = ansi.Strip(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.ReplaceAll(s, "\t", "    ")
}

// headLines keeps the first n lines of s, with each line cut to a few times
// the width it's wrapped at. It returns how many lines were left out.
func headLines(s string, n, width int) (string, int) {
	lines := strings.Split(s, "\n")
	cut := max(0, len(lines)-n)
	lines = lines[:min(n, len(lines))]
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, 3*width, constants.Ellipsis)
	}
	return strings.Join(lines, "\n"), cut
}

// tailLines keeps the last n lines of s, the way headLines keeps the first.
func tailLines(s string, n, width int) (string, int) {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	cut := max(0, len(lines)-n)
	lines = lines[cut:]
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, 3*width, constants.Ellipsis)
	}
	return strings.Join(lines, "\n"), cut
}

// formatDuration formats how long a test took, e.g. "1.2s" or "3m4s".
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		return d.Round(time.Second).String()
	}
}

// thousands formats n with thousands separators, e.g. "3,421".
func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
