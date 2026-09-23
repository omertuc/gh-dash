package prview

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

func newTestModelWithCommits(t *testing.T) Model {
	t.Helper()
	m := newTestModelForAction(t)
	m.SetWidth(80)
	m.pr.Data.Primary.Url = "https://github.com/owner/repo/pull/1"
	m.pr.Data.Primary.Repository.NameWithOwner = "owner/repo"

	commits := &m.pr.Data.Enriched.AllCommits
	commits.Nodes = slices.Grow(commits.Nodes, 2)[:2]
	first := &commits.Nodes[0].Commit
	first.Oid = "aaaaaaa1111111111111111111111111111111111"
	first.AbbreviatedOid = "aaaaaaa"
	first.MessageHeadline = "First commit"
	first.MessageBody = "Explains the first commit"
	first.Additions = 12
	first.Deletions = 3
	second := &commits.Nodes[1].Commit
	second.Oid = "bbbbbbb2222222222222222222222222222222222"
	second.AbbreviatedOid = "bbbbbbb"
	second.MessageHeadline = "Second commit"
	second.MessageBody = "Explains the second commit"
	return m
}

func TestRenderCommitsAnchorsEachCommit(t *testing.T) {
	m := newTestModelWithCommits(t)
	m.SetFocusedCommit(0)

	view, anchors := m.renderCommits()

	require.Len(t, anchors, 2)
	lines := strings.Split(ansi.Strip(view), "\n")
	for i, headline := range []string{"First commit", "Second commit"} {
		require.NotNil(t, anchors[i].Commit)
		require.Equal(t, i, *anchors[i].Commit)
		require.Contains(t, lines[anchors[i].Line], headline,
			"anchor %d should be on its commit's title", i)
	}
}

func TestRenderCommitsShowsDiffStats(t *testing.T) {
	m := newTestModelWithCommits(t)

	view, _ := m.renderCommits()

	require.Contains(t, ansi.Strip(view), "+12 -3")
}

func TestRenderCommitsExpandsOnlyFocusedCommit(t *testing.T) {
	m := newTestModelWithCommits(t)

	view, _ := m.renderCommits()
	require.NotContains(t, ansi.Strip(view), "Explains the")

	require.True(t, m.SetFocusedCommit(1))
	view, _ = m.renderCommits()
	require.NotContains(t, ansi.Strip(view), "Explains the first commit")
	require.Contains(t, ansi.Strip(view), "Explains the second commit")

	require.False(t, m.SetFocusedCommit(1), "focusing the same commit changes nothing")
}

func TestFullCommitMessageJoinsCutHeadline(t *testing.T) {
	headline, body := fullCommitMessage("Fix the lo…", "…ng bug\n\nDetails")
	require.Equal(t, "Fix the long bug", headline)
	require.Equal(t, "Details", body)

	headline, body = fullCommitMessage("Short", "Body")
	require.Equal(t, "Short", headline)
	require.Equal(t, "Body", body)
}

func TestViewCommitFilesNarrowsFilesTab(t *testing.T) {
	m := newTestModelWithCommits(t)
	m.setTab(commitsTab)

	cmd := m.ViewCommitFiles(1)

	require.NotNil(t, cmd)
	require.Equal(t, filesTab, m.carousel.Cursor())
	require.Contains(t, m.carousel.SelectedItem(), "(bbbbbbb)")
	require.Contains(t, ansi.Strip(m.ViewBody()), "Loading files of bbbbbbb")

	m.SetCommitFiles(CommitFilesMsg{
		Oid:   "bbbbbbb2222222222222222222222222222222222",
		Files: []data.ChangedFile{{Path: "main.go", Additions: 4, Deletions: 1, ChangeType: "MODIFIED"}},
	})
	body := ansi.Strip(m.ViewBody())
	require.Contains(t, body, "1 files changed in bbbbbbb")
	require.Contains(t, body, "main.go")
}

func TestCommitFilesOfAnotherCommitAreDropped(t *testing.T) {
	m := newTestModelWithCommits(t)
	m.ViewCommitFiles(1)

	m.SetCommitFiles(CommitFilesMsg{
		Oid: "aaaaaaa1111111111111111111111111111111111",
		Err: errors.New("stale"),
	})

	require.Contains(t, ansi.Strip(m.ViewBody()), "Loading files of bbbbbbb")
}

func TestLeavingFilesTabShowsAllFiles(t *testing.T) {
	m := newTestModelWithCommits(t)
	m.ViewCommitFiles(0)

	m.PrevTab()
	m.NextTab()

	require.Equal(t, filesTab, m.carousel.Cursor())
	require.NotContains(t, m.carousel.SelectedItem(), "(")
	require.NotContains(t, ansi.Strip(m.ViewBody()), "aaaaaaa")
}

func TestViewCommitFilesOfOnlyCommitShowsAllFiles(t *testing.T) {
	m := newTestModelWithCommits(t)
	m.pr.Data.Enriched.AllCommits.Nodes = m.pr.Data.Enriched.AllCommits.Nodes[:1]
	m.setTab(commitsTab)

	cmd := m.ViewCommitFiles(0)

	require.Nil(t, cmd, "the PR's files are already known")
	require.Equal(t, filesTab, m.carousel.Cursor())
	require.NotContains(t, m.carousel.SelectedItem(), "(")
	require.Nil(t, m.commitFiles)
}
