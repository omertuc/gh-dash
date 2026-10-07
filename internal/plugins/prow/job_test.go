package prow

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/plugins"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func testEnv(t *testing.T) plugins.Env {
	t.Helper()
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	require.NoError(t, err)
	thm := theme.ParseTheme(&cfg)
	return plugins.Env{Ctx: &context.ProgramContext{
		Config: &cfg,
		Theme:  thm,
		Styles: context.InitStyles(thm),
	}}
}

func makeTests(state testState, n int) []*testResult {
	tests := make([]*testResult, n)
	for i := range tests {
		tests[i] = &testResult{suite: "suite", name: fmt.Sprintf("%s test %d", stateNames[state], i),
			state: state, runs: 1}
	}
	return tests
}

// newLoadedJobView returns a view of a job that failed 3 tests and passed
// 120, with a page size of 50.
func newLoadedJobView() *jobView {
	check := plugins.Check{Name: "ci/prow/e2e-aws", URL: jobURL("pull-ci-e2e-aws"), State: "FAILURE"}
	job, _ := parseJobURL(check.URL)
	p := New()
	v := newJobView(check, job, p.opts, p.results())
	res := &jobResults{result: "FAILURE"}
	res.tests[testFailed] = makeTests(testFailed, 3)
	res.tests[testFailed][0].message = "expected 1, got 2"
	res.tests[testFailed][0].output = strings.Repeat("log line\n", 30) + "the end"
	res.tests[testPassed] = makeTests(testPassed, 120)
	v.Update(resultsMsg{results: res})
	return v
}

// itemLines returns the first line of each item as rendered.
func itemLines(view string, items []plugins.Item) []string {
	lines := strings.Split(ansi.Strip(view), "\n")
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = lines[it.Line]
	}
	return out
}

func findItem(t *testing.T, lines []string, substr string) int {
	t.Helper()
	for i, l := range lines {
		if strings.Contains(l, substr) {
			return i
		}
	}
	t.Fatalf("no item has %q in %q", substr, lines)
	return -1
}

func TestJobViewListsOnlyTheFailedTestsUpFront(t *testing.T) {
	env := testEnv(t)
	v := newLoadedJobView()

	view, items := v.Render(env, 80)
	lines := itemLines(view, items)

	require.Equal(t, " e2e-aws", v.Title())
	require.Contains(t, ansi.Strip(view), "123 tests · 3 failed · 120 passed")
	// Open, the failed header and its 3 tests, and the passed header
	require.Len(t, items, 6)
	require.Contains(t, lines[0], "Open job in browser")
	require.Contains(t, lines[1], "Failed (3)")
	require.Contains(t, lines[2], "Failed test 0")
	require.Contains(t, lines[5], "Passed (120) · Load 50")
	require.True(t, items[5].Clickable)
	require.Equal(t, "load", items[5].Hint)
	require.NotContains(t, ansi.Strip(view), "Passed test", "passed tests aren't listed until asked for")
}

func TestJobViewLoadsAPageAtATime(t *testing.T) {
	env := testEnv(t)
	v := newLoadedJobView()
	view, items := v.Render(env, 80)

	v.Activate(findItem(t, itemLines(view, items), "Passed (120)"))
	view, items = v.Render(env, 80)
	lines := itemLines(view, items)
	require.Equal(t, 50, strings.Count(ansi.Strip(view), "Passed test"))
	more := findItem(t, lines, "Load 50 more")
	require.Contains(t, lines[more], "70 not shown")

	v.Activate(more)
	view, items = v.Render(env, 80)
	lines = itemLines(view, items)
	require.Equal(t, 100, strings.Count(ansi.Strip(view), "Passed test"))
	more = findItem(t, lines, "Load 20 more")

	v.Activate(more)
	view, items = v.Render(env, 80)
	require.Equal(t, 120, strings.Count(ansi.Strip(view), "Passed test"))
	require.NotContains(t, ansi.Strip(view), "more ·")

	// Collapsing hides them, and expanding again keeps what was loaded
	header := findItem(t, itemLines(view, items), "Passed (120)")
	v.Activate(header)
	view, items = v.Render(env, 80)
	require.NotContains(t, ansi.Strip(view), "Passed test")
	v.Activate(header)
	view, _ = v.Render(env, 80)
	require.Equal(t, 120, strings.Count(ansi.Strip(view), "Passed test"))
}

func TestJobViewShowsFocusedTestsFailure(t *testing.T) {
	env := testEnv(t)
	v := newLoadedJobView()
	view, items := v.Render(env, 80)
	test := findItem(t, itemLines(view, items), "Failed test 0")

	require.True(t, v.Focus(test))
	view, _ = v.Render(env, 80)

	plain := ansi.Strip(view)
	require.Contains(t, plain, "expected 1, got 2")
	require.Contains(t, plain, "the end", "the end of the output is shown")
	require.Contains(t, plain, "lines above")

	// Focusing a button doesn't change what's shown
	require.True(t, v.Focus(0))
	require.False(t, v.Focus(0))
	view, _ = v.Render(env, 80)
	require.NotContains(t, ansi.Strip(view), "expected 1, got 2")
}

func TestJobViewOpensTheJob(t *testing.T) {
	env := testEnv(t)
	v := newLoadedJobView()
	v.Render(env, 80)

	msg := v.Activate(0)()

	require.Equal(t, plugins.OpenURLMsg{URL: jobURL("pull-ci-e2e-aws")}, msg)
}

func TestJobViewExplainsPrivateArtifacts(t *testing.T) {
	env := testEnv(t)
	v := newLoadedJobView()
	v.Update(resultsMsg{err: errNotPublic})

	view, items := v.Render(env, 80)

	require.Contains(t, strings.Join(strings.Fields(ansi.Strip(view)), " "), "aren't public")
	require.Len(t, items, 1, "only opening the job is left")
}

func TestJobViewRetriesAfterAnError(t *testing.T) {
	env := testEnv(t)
	v := newLoadedJobView()
	v.Update(resultsMsg{err: errors.New("connection reset")})

	view, items := v.Render(env, 80)
	retry := findItem(t, itemLines(view, items), "Try again")

	require.NotNil(t, v.Activate(retry))
	view, _ = v.Render(env, 80)
	require.Contains(t, ansi.Strip(view), "Loading the test results")
}
