package prow

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/plugins"
)

// notApprovedComment is how Prow's approval notifier comments on a PR that
// still needs approving.
const notApprovedComment = `[APPROVALNOTIFIER] This PR is **NOT APPROVED**

This pull-request has been approved by: *<a href="https://github.com/openshift/origin/pull/31679#" title="Author self-approved">jacobsee</a>*
**Once this PR has been reviewed and has the lgtm label**, please assign [kyrtapz](https://github.com/kyrtapz) for approval. For more information see [the Code Review Process](https://git.k8s.io/community/contributors/guide/owners.md#the-code-review-process).

The full list of commands accepted by this bot can be found [here](https://go.k8s.io/bot-commands?repo=openshift%2Forigin).

<details open>
Needs approval from an approver in each of these files:

- **[test/extended/networking/OWNERS](https://github.com/openshift/origin/blob/main/test/extended/networking/OWNERS)**
- ~~[pkg/OWNERS](https://github.com/openshift/origin/blob/main/pkg/OWNERS)~~ [alice,bob]

Approvers can indicate their approval by writing ` + "`/approve`" + ` in a comment
Approvers can cancel approval by writing ` + "`/approve cancel`" + ` in a comment
</details>
<!-- META={"approvers":["kyrtapz"]} -->`

const approvedComment = `[APPROVALNOTIFIER] This PR is **APPROVED**

This pull-request has been approved by: *<a href="#" title="Author self-approved">jacobsee</a>*, *<a href="#issuecomment-1" title="Approved">kyrtapz</a>*

<details >
Needs approval from an approver in each of these files:

- ~~[test/extended/networking/OWNERS](https://github.com/openshift/origin/blob/main/test/extended/networking/OWNERS)~~ [kyrtapz]

</details>
<!-- META={"approvers":[]} -->`

const failedTestsComment = "@jacobsee: The following tests **failed**, say `/retest` to rerun all failed tests:\n\n" +
	"Test name | Commit | Details | Required | Rerun command\n--- | --- | --- | --- | ---\n" +
	"ci/prow/e2e-gcp | abc | [link](https://prow.ci.openshift.org/view/gs/b/pr-logs/pull/1/job/2) | true | `/test e2e-gcp`\n"

type testComment struct {
	author string
	body   string
	at     time.Time
}

type testStatus struct {
	context string
	state   string
	url     string
}

// newTestPR builds a PR with the given statuses on its last commit, comments
// and labels.
func newTestPR(t *testing.T, statuses []testStatus, comments []testComment, labels ...string) plugins.PR {
	t.Helper()
	var nodes []map[string]any
	for _, s := range statuses {
		nodes = append(nodes, map[string]any{
			"Typename": "StatusContext",
			"StatusContext": map[string]any{
				"Context": s.context, "State": s.state, "TargetUrl": s.url,
			},
		})
	}
	var commentNodes []map[string]any
	for _, c := range comments {
		commentNodes = append(commentNodes, map[string]any{
			"Author": map[string]any{"Login": c.author}, "Body": c.body,
			"CreatedAt": c.at, "UpdatedAt": c.at,
		})
	}
	var labelNodes []map[string]any
	for _, l := range labels {
		labelNodes = append(labelNodes, map[string]any{"Name": l})
	}
	raw, err := json.Marshal(map[string]any{
		"Labels":   map[string]any{"Nodes": labelNodes},
		"Comments": map[string]any{"Nodes": commentNodes},
		"Commits": map[string]any{"Nodes": []any{map[string]any{"Commit": map[string]any{
			"StatusCheckRollup": map[string]any{"Contexts": map[string]any{"Nodes": nodes}},
		}}}},
	})
	require.NoError(t, err)
	var enriched data.EnrichedPullRequestData
	require.NoError(t, json.Unmarshal(raw, &enriched))
	return plugins.PR{Primary: &data.PullRequestData{}, Enriched: &enriched, IsEnriched: true}
}

func jobURL(job string) string {
	return "https://prow.ci.openshift.org/view/gs/test-platform-results-public/pr-logs/pull/1/" + job + "/123"
}

func prowStatuses() []testStatus {
	return []testStatus{
		{context: "tide", state: "PENDING", url: "https://prow.ci.openshift.org/pr"},
		{context: "ci/prow/unit", state: "SUCCESS", url: jobURL("pull-ci-unit")},
		{context: "ci/prow/e2e-aws", state: "FAILURE", url: jobURL("pull-ci-e2e-aws")},
	}
}

func TestParseApprovalNotApproved(t *testing.T) {
	a := parseApproval(notApprovedComment)

	require.False(t, a.approved)
	require.Equal(t, []approver{{login: "jacobsee", how: "Author self-approved"}}, a.approvers)
	require.Equal(t, []string{"test/extended/networking/OWNERS"}, a.pending)
	require.Equal(t, []ownersApproval{{path: "pkg/OWNERS", by: []string{"alice", "bob"}}}, a.satisfied)
	require.Equal(t, []string{"kyrtapz"}, a.suggested)
}

func TestParseApprovalApproved(t *testing.T) {
	a := parseApproval(approvedComment)

	require.True(t, a.approved)
	require.Equal(t, []approver{
		{login: "jacobsee", how: "Author self-approved"},
		{login: "kyrtapz", how: "Approved"},
	}, a.approvers)
	require.Empty(t, a.pending)
	require.Empty(t, a.suggested)
}

func TestLatestApprovalIsTheLastUpdated(t *testing.T) {
	now := time.Now()
	pr := newTestPR(t, nil, []testComment{
		{author: "openshift-ci[bot]", body: approvedComment, at: now},
		{author: "openshift-ci[bot]", body: notApprovedComment, at: now.Add(-time.Hour)},
	})

	a, ok := latestApproval(pr)

	require.True(t, ok)
	require.True(t, a.approved)
}

func TestLgtmGiversDropsCancelled(t *testing.T) {
	now := time.Now()
	pr := newTestPR(t, nil, []testComment{
		{author: "carol", body: "looks good\n/lgtm", at: now.Add(-3 * time.Hour)},
		{author: "dave", body: "/lgtm", at: now.Add(-2 * time.Hour)},
		{author: "carol", body: "/lgtm cancel\nwait, one more thing", at: now.Add(-time.Hour)},
		{author: "erin", body: "not /lgtm yet", at: now},
	})

	require.Equal(t, []string{"dave"}, lgtmGivers(pr))
}

func panelTexts(p plugins.Panel) string {
	var lines []string
	for _, r := range p.Rows {
		lines = append(lines, r.Text)
	}
	return strings.Join(lines, "\n")
}

func TestPanelNotApproved(t *testing.T) {
	pr := newTestPR(t, prowStatuses(), []testComment{
		{author: "openshift-ci[bot]", body: notApprovedComment, at: time.Now()},
	})

	panels := New().Panels(pr)

	require.Len(t, panels, 1)
	require.Equal(t, plugins.StatusPending, panels[0].Status)
	texts := panelTexts(panels[0])
	require.Contains(t, texts, "Needs /lgtm")
	require.Contains(t, texts, "Approved by @jacobsee (author)")
	require.Contains(t, texts, "Needs /approve for test/extended/networking/OWNERS")
	require.Contains(t, texts, "Suggested approvers: @kyrtapz")
	require.Contains(t, texts, "pkg/OWNERS approved by @alice, @bob")
}

func TestPanelReadyToMerge(t *testing.T) {
	pr := newTestPR(t, prowStatuses(), []testComment{
		{author: "openshift-ci[bot]", body: approvedComment, at: time.Now()},
		{author: "dave", body: "/lgtm", at: time.Now()},
	}, "lgtm", "approved")

	panels := New().Panels(pr)

	require.Len(t, panels, 1)
	require.Equal(t, plugins.StatusSuccess, panels[0].Status)
	texts := panelTexts(panels[0])
	require.Contains(t, texts, "LGTM by @dave")
	require.Contains(t, texts, "Approved by @jacobsee (author), @kyrtapz")
	require.NotContains(t, texts, "Needs")
}

func TestPanelOnHold(t *testing.T) {
	pr := newTestPR(t, prowStatuses(), nil, "lgtm", "approved", "do-not-merge/hold")

	panels := New().Panels(pr)

	require.Len(t, panels, 1)
	require.Equal(t, plugins.StatusFailure, panels[0].Status)
	require.Contains(t, panelTexts(panels[0]), "On hold")
}

func TestNoPanelOrCommandsWithoutProw(t *testing.T) {
	pr := newTestPR(t, []testStatus{
		{context: "build", state: "SUCCESS", url: "https://github.com/o/r/actions/runs/1"},
	}, []testComment{{author: "someone", body: "/lgtm", at: time.Now()}})

	p := New()
	require.Empty(t, p.Panels(pr))
	require.Empty(t, p.CommentCommands(pr))
}

func commandTexts(commands []plugins.CommentCommand) []string {
	var texts []string
	for _, c := range commands {
		texts = append(texts, c.Text)
	}
	return texts
}

func TestCommentCommands(t *testing.T) {
	pr := newTestPR(t, prowStatuses(), []testComment{
		{author: "openshift-ci[bot]", body: failedTestsComment, at: time.Now()},
	})

	texts := commandTexts(New().CommentCommands(pr))

	for _, want := range []string{"/lgtm", "/approve", "/hold", "/hold cancel", "/retest",
		"/test e2e-aws", "/test unit", "/test e2e-gcp",
		"/override ci/prow/e2e-aws", "/override ci/prow/unit"} {
		require.Contains(t, texts, want)
	}
	require.NotContains(t, texts, "/override tide")
	// Failing jobs come first, as they're the ones to rerun or override
	require.Less(t, slices.Index(texts, "/test e2e-aws"), slices.Index(texts, "/test unit"))
	require.Less(t, slices.Index(texts, "/override ci/prow/e2e-aws"),
		slices.Index(texts, "/override ci/prow/unit"))
}

func TestConfigure(t *testing.T) {
	p := New()
	require.NoError(t, p.Configure(map[string]any{
		"pageSize":     20,
		"artifactsUrl": "https://example.com/",
		"extraCommands": []any{
			map[string]any{"command": "/jira refresh", "description": "Refresh Jira"},
		},
	}))
	require.Equal(t, 20, p.opts.PageSize)
	require.Equal(t, "https://example.com", p.opts.ArtifactsURL)

	pr := newTestPR(t, prowStatuses(), nil)
	require.Contains(t, commandTexts(p.CommentCommands(pr)), "/jira refresh")

	require.Error(t, New().Configure(map[string]any{"pageSize": 0}))
	require.Error(t, New().Configure(map[string]any{"nope": true}))
	require.Error(t, New().Configure(map[string]any{
		"extraCommands": []any{map[string]any{"command": "jira"}},
	}))
}

func TestCheckAction(t *testing.T) {
	p := New()

	label, ok := p.CheckAction(plugins.Check{Name: "ci/prow/unit", URL: jobURL("pull-ci-unit")})
	require.True(t, ok)
	require.Equal(t, "view tests", label)

	_, ok = p.CheckAction(plugins.Check{Name: "build", URL: "https://github.com/o/r/actions/runs/1"})
	require.False(t, ok)
}

func TestParseJobURL(t *testing.T) {
	job, ok := parseJobURL(
		"https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/openshift_origin/1/pull-ci-e2e/123")
	require.True(t, ok)
	require.Equal(t, "test-platform-results", job.bucket)
	require.Equal(t, "pr-logs/pull/openshift_origin/1/pull-ci-e2e/123", job.path)
	require.Equal(t, "pull-ci-e2e", job.name)
	require.Equal(t, "123", job.buildID)

	_, ok = parseJobURL("https://prow.k8s.io/view/gcs/kubernetes-ci-logs/logs/ci-job/9")
	require.True(t, ok)

	for _, u := range []string{
		"", "https://github.com/o/r/actions/runs/1", "https://prow.ci.openshift.org/pr?query=x",
		"https://prow.ci.openshift.org/view/s3/bucket/logs/job/1",
	} {
		_, ok := parseJobURL(u)
		require.False(t, ok, u)
	}
}

func TestThousands(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567"} {
		require.Equal(t, want, thousands(n), fmt.Sprint(n))
	}
}
