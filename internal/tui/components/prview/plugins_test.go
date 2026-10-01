package prview

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/plugins"
	_ "github.com/dlvhdr/gh-dash/v4/internal/plugins/prow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/fuzzyselect"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
)

// prowPRJSON is a PR with a failed Prow job that's still waiting on approval.
const prowPRJSON = `{
	"Labels": {"Nodes": [{"Name": "lgtm"}]},
	"Comments": {"Nodes": [{"Author": {"Login": "openshift-ci[bot]"},
		"Body": "[APPROVALNOTIFIER] This PR is **NOT APPROVED**\n\nThis pull-request has been approved by: *alice*\n\n- **[pkg/OWNERS](https://github.com/o/r/blob/main/pkg/OWNERS)**\n"}]},
	"Commits": {"TotalCount": 1, "Nodes": [{"Commit": {"StatusCheckRollup": {"Contexts": {"Nodes": [
		{"Typename": "StatusContext", "StatusContext": {"Context": "ci/prow/e2e-aws", "State": "FAILURE",
			"TargetUrl": "https://prow.ci.openshift.org/view/gs/bucket/pr-logs/pull/1/pull-ci-e2e-aws/9"}}
	]}}}}]}}`

func newProwTestModel(t *testing.T, enabled bool) Model {
	t.Helper()
	require.NoError(t, plugins.Configure(map[string]config.PluginConfig{"prow": {Enabled: enabled}}))
	t.Cleanup(func() { _ = plugins.Configure(nil) })

	m := newTestModelWithFocusableChecks(t)
	var enriched data.EnrichedPullRequestData
	require.NoError(t, json.Unmarshal([]byte(prowPRJSON), &enriched))
	primary := &data.PullRequestData{State: "OPEN", Url: "https://github.com/o/r/pull/1"}
	m.SetRow(&prrow.Data{Primary: primary, Enriched: enriched, IsEnriched: true})
	m.SetWidth(80)
	return m
}

func TestOverviewShowsPluginPanels(t *testing.T) {
	m := newProwTestModel(t, true)

	overview := ansi.Strip(m.viewOverviewTab())

	require.Contains(t, overview, "Prow")
	require.Contains(t, overview, "LGTM'd, waiting on approval")
	require.Contains(t, overview, "Approved by @alice")
	require.Contains(t, overview, "Needs /approve for pkg/OWNERS")
}

func TestPluginsAreOffUnlessEnabled(t *testing.T) {
	m := newProwTestModel(t, false)

	require.NotContains(t, ansi.Strip(m.viewOverviewTab()), "Needs /approve")
	require.IsType(t, &fuzzyselect.UserMentionSource{}, m.commentSource())
	require.Empty(t, m.CheckAction(0))
	_, opened := m.OpenCheckInPlugin(0)
	require.False(t, opened)
}

func TestCommentSourceOffersPluginCommands(t *testing.T) {
	m := newProwTestModel(t, true)

	src, ok := m.commentSource().(*fuzzyselect.CommandSource)

	require.True(t, ok)
	var values []string
	for _, s := range src.Commands {
		values = append(values, s.Value)
	}
	require.Contains(t, values, "/lgtm")
	require.Contains(t, values, "/test e2e-aws")
	require.Contains(t, values, "/override ci/prow/e2e-aws")
	require.IsType(t, &fuzzyselect.UserMentionSource{}, src.Fallback)
}

func TestOpenCheckInPluginAddsATab(t *testing.T) {
	m := newProwTestModel(t, true)
	m.setTab(checksTab)
	require.Equal(t, "view tests", m.CheckAction(0))

	cmd, opened := m.OpenCheckInPlugin(0)

	require.True(t, opened)
	require.NotNil(t, cmd)
	require.True(t, m.IsPluginTab())
	require.Len(t, m.carousel.Items(), len(tabs)+1)
	body, anchors := m.ViewBodyWithAnchors()
	require.Contains(t, ansi.Strip(body), "Open job in browser")
	require.NotEmpty(t, anchors)
	require.NotNil(t, anchors[0].Plugin)
	require.False(t, anchors[0].IsComment())
	require.Contains(t, strings.Split(ansi.Strip(body), "\n")[anchors[0].Line], "Open job in browser")

	// Another PR drops the tab
	m.SetRow(&prrow.Data{Primary: &data.PullRequestData{Url: "https://github.com/o/r/pull/2"}})
	require.False(t, m.IsPluginTab())
	require.Len(t, m.carousel.Items(), len(tabs))
}
