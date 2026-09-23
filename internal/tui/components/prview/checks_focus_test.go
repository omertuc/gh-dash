package prview

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	graphql "github.com/cli/shurcooL-graphql"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

func newTestModelWithFocusableChecks(t *testing.T) Model {
	t.Helper()
	failed := makeCheckRun("build", "COMPLETED", "FAILURE")
	failed.Title = "3 errors"
	failed.StartedAt = time.Now().Add(-5 * time.Minute)
	failed.CompletedAt = failed.StartedAt.Add(2*time.Minute + 13*time.Second)
	failed.DetailsUrl = "https://github.com/owner/repo/actions/runs/1/job/2"
	failed.Url = "https://github.com/owner/repo/runs/2"
	passed := makeCheckRun("lint", "COMPLETED", "SUCCESS")
	passed.Url = "https://github.com/owner/repo/runs/3"

	m := newTestModelForChecks(t, checksTestOptions{
		checkSuites: data.CheckSuites{Nodes: []data.CheckSuiteNode{
			makeCheckSuite("Deploy", "COMPLETED", "ACTION_REQUIRED"),
		}},
		checkRuns:            []data.CheckRun{passed, failed},
		rollupState:          "FAILURE",
		requiredStatusChecks: []string{"e2e"},
	})
	m.pr.Data.Primary.State = "OPEN"
	return m
}

func TestRenderChecksAnchorsEachCheck(t *testing.T) {
	m := newTestModelWithFocusableChecks(t)

	view, anchors := m.renderChecks()

	lines := strings.Split(ansi.Strip(view), "\n")
	// Listed as awaiting approval, pending, failed and then the rest
	want := []string{"Deploy", "e2e", "build", "lint"}
	require.Len(t, anchors, len(want))
	for i, name := range want {
		require.NotNil(t, anchors[i].Check)
		require.Equal(t, i, *anchors[i].Check)
		require.False(t, anchors[i].IsComment())
		require.Contains(t, lines[anchors[i].Line], name, "anchor %d should be on its check", i)
	}
}

func TestChecksTabAnchorsBelowOverview(t *testing.T) {
	m := newTestModelWithFocusableChecks(t)
	m.setTab(checksTab)

	body, anchors := m.ViewBodyWithAnchors()

	lines := strings.Split(ansi.Strip(body), "\n")
	require.Len(t, anchors, 4)
	require.Contains(t, lines[anchors[2].Line], "build")
}

func TestRenderChecksExpandsOnlyFocusedCheck(t *testing.T) {
	m := newTestModelWithFocusableChecks(t)

	view, anchors := m.renderChecks()
	require.NotContains(t, ansi.Strip(view), "3 errors")
	// When each check last changed is shown without focusing it
	lines := strings.Split(ansi.Strip(view), "\n")
	require.True(t, strings.HasSuffix(strings.TrimRight(lines[anchors[2].Line], " "), "3m ago"),
		"got %q", lines[anchors[2].Line])

	require.True(t, m.SetFocusedCheck(2))
	view, anchors = m.renderChecks()
	plain := ansi.Strip(view)
	require.Contains(t, plain, "Failure · took 2m13s")
	require.Contains(t, plain, "3 errors")
	require.Contains(t, plain, "actions/runs/1/job/2")
	require.Equal(t, 2, m.ExpandedCheckIndex())

	// The checks after the expanded one move down
	lines = strings.Split(plain, "\n")
	require.Contains(t, lines[anchors[3].Line], "lint")

	require.False(t, m.SetFocusedCheck(2), "focusing the same check changes nothing")
	require.True(t, m.SetFocusedCheck(-1))
	require.Equal(t, -1, m.ExpandedCheckIndex())
}

func TestLongCheckNameIsCutShortAndShownWhenExpanded(t *testing.T) {
	m := newTestModelWithFocusableChecks(t)
	long := strings.Repeat("very-long-check-name-", 5) + "end"
	nodes := m.pr.Data.Enriched.Commits.Nodes[0].Commit.StatusCheckRollup.Contexts.Nodes
	nodes[1].CheckRun.Name = graphql.String(long)

	view, anchors := m.renderChecks()
	lines := strings.Split(ansi.Strip(view), "\n")
	require.NotContains(t, lines[anchors[2].Line], "end")
	require.Contains(t, lines[anchors[2].Line], "ago", "the time should still fit")

	m.SetFocusedCheck(2)
	view, _ = m.renderChecks()
	var joined strings.Builder
	for _, l := range strings.Split(ansi.Strip(view), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(l), "│ "); ok {
			joined.WriteString(rest)
		}
	}
	require.Contains(t, joined.String(), long, "the expanded check should show its whole name")
}

func TestCheckUrlPrefersDetails(t *testing.T) {
	m := newTestModelWithFocusableChecks(t)

	require.Equal(t, "https://github.com/owner/repo/actions/runs/1/job/2", m.CheckUrl(2))
	require.Equal(t, "https://github.com/owner/repo/runs/3", m.CheckUrl(3))
	require.Empty(t, m.CheckUrl(1), "an unreported check has nowhere to open")
	require.Empty(t, m.CheckUrl(9))
}
