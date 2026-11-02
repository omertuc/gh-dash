package prow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/plugins"
)

// mergeTreeOutput is what "git merge-tree --write-tree -z --name-only" printed
// for a PR conflicting in two files' contents and deleting a file modified on
// the target branch.
const mergeTreeOutput = "18c9c990cfb9f663a1dd344193ebc98e96bf17fc\x00f.txt\x00g.txt\x00sp ace.txt\x00\x00" +
	"1\x00f.txt\x00Auto-merging\x00Auto-merging f.txt\n\x00" +
	"1\x00f.txt\x00CONFLICT (contents)\x00CONFLICT (content): Merge conflict in f.txt\n\x00" +
	"1\x00g.txt\x00CONFLICT (modify/delete)\x00CONFLICT (modify/delete): g.txt deleted in pr and modified in main.  Version main of g.txt left in tree.\n\x00" +
	"1\x00sp ace.txt\x00Auto-merging\x00Auto-merging sp ace.txt\n\x00" +
	"1\x00sp ace.txt\x00CONFLICT (contents)\x00CONFLICT (content): Merge conflict in sp ace.txt\n\x00"

func TestParseMergeTree(t *testing.T) {
	tree, conflicts := parseMergeTree(mergeTreeOutput)

	require.Equal(t, "18c9c990cfb9f663a1dd344193ebc98e96bf17fc", tree)
	require.Equal(t, []conflict{
		{path: "f.txt", kinds: []string{"contents"}},
		{path: "g.txt", kinds: []string{"modify/delete"},
			messages: []string{"g.txt deleted in pr and modified in main.  Version main of g.txt left in tree."}},
		{path: "sp ace.txt", kinds: []string{"contents"}},
	}, conflicts)
}

func TestParseMergeTreeClean(t *testing.T) {
	tree, conflicts := parseMergeTree("18c9c990cfb9f663a1dd344193ebc98e96bf17fc\x00")

	require.Equal(t, "18c9c990cfb9f663a1dd344193ebc98e96bf17fc", tree)
	require.Empty(t, conflicts)
}

func TestIsRepoURL(t *testing.T) {
	for _, u := range []string{
		"git@github.com:openshift/origin.git",
		"https://github.com/openshift/origin",
		"https://github.com/OpenShift/Origin.git/",
		"ssh://git@github.com/openshift/origin.git",
	} {
		require.True(t, isRepoURL(u, "github.com", "openshift/origin"), u)
	}
	for _, u := range []string{
		"git@github.com:me/origin.git",
		"https://github.com/openshift/origin-fork",
		"https://gitlab.com/openshift/origin",
	} {
		require.False(t, isRepoURL(u, "github.com", "openshift/origin"), u)
	}
}

func TestTabsShowConflictsWhenNeedingRebase(t *testing.T) {
	p := New()
	managed := func(labels ...string) plugins.PR {
		pr := plugins.PR{Primary: &data.PullRequestData{Url: "https://github.com/o/r/pull/1"}}
		for _, l := range append(labels, labelLGTM) {
			pr.Primary.Labels.Nodes = append(pr.Primary.Labels.Nodes, data.Label{Name: l})
		}
		return pr
	}

	require.Empty(t, p.Tabs(managed()))
	require.Equal(t, []plugins.Tab{{Key: conflictsTabKey, Title: conflictsTitle}},
		p.Tabs(managed(labelNeedsRebase)))

	conflicting := managed()
	conflicting.Primary.Mergeable = "CONFLICTING"
	require.Len(t, p.Tabs(conflicting), 1)

	// Prow doesn't manage it
	unmanaged := plugins.PR{Primary: &data.PullRequestData{Mergeable: "CONFLICTING"}}
	require.Empty(t, p.Tabs(unmanaged))
}

// gitRepos sets up a repo standing in for GitHub, where PR 7 conflicts with
// main, and a clone of it.
func gitRepos(t *testing.T) (remote, clone string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git isn't installed")
	}
	dir := t.TempDir()
	remote, clone = filepath.Join(dir, "remote"), filepath.Join(dir, "clone")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@example.com",
			"GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@example.com")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	write := func(name, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(remote, name), []byte(content), 0o644))
	}

	require.NoError(t, os.Mkdir(remote, 0o755))
	run(remote, "init", "-q", "-b", "main")
	write("f.txt", "a\nb\nc\n")
	write("g.txt", "x\n")
	write("h.txt", "1\n")
	run(remote, "add", ".")
	run(remote, "commit", "-qm", "init")
	run(remote, "checkout", "-qb", "pr")
	write("f.txt", "a\nPR\nc\n")
	write("h.txt", "2\n")
	run(remote, "rm", "-q", "g.txt")
	run(remote, "commit", "-qam", "pr")
	run(remote, "update-ref", "refs/pull/7/head", "pr")
	run(remote, "checkout", "-q", "main")
	write("f.txt", "a\nMAIN\nc\n")
	write("g.txt", "z\n")
	run(remote, "commit", "-qam", "main")
	run(dir, "clone", "-q", remote, clone)
	return remote, clone
}

func TestFindConflicts(t *testing.T) {
	remote, clone := gitRepos(t)

	res, err := findConflicts(conflictsRequest{repoDir: clone, remote: remote, base: "main", number: 7})

	require.NoError(t, err)
	require.Len(t, res.conflicts, 2)
	require.Equal(t, "f.txt", res.conflicts[0].path)
	require.Equal(t, []string{"contents"}, res.conflicts[0].kinds)
	diff := ansi.Strip(res.conflicts[0].diff)
	require.Contains(t, diff, "+<<<<<<< gh-dash/main")
	require.Contains(t, diff, "+PR")
	require.Contains(t, diff, "+>>>>>>> gh-dash/pull/7")
	require.Equal(t, "g.txt", res.conflicts[1].path)
	require.Equal(t, []string{"modify/delete"}, res.conflicts[1].kinds)
	require.Empty(t, res.conflicts[1].diff, "the file's kept as main has it")
	require.Equal(t, res.conflicts[0].diff, res.diff())
	require.NotContains(t, res.diff(), "h.txt", "only the conflicting files are shown")

	// The refs fetched into are gone, as is any trace in FETCH_HEAD
	refs, err := git(clone, "for-each-ref", "refs/gh-dash")
	require.NoError(t, err)
	require.Empty(t, refs)
	_, err = os.Stat(filepath.Join(clone, ".git", "FETCH_HEAD"))
	require.True(t, os.IsNotExist(err))
}

func TestFindConflictsMergesCleanly(t *testing.T) {
	remote, clone := gitRepos(t)

	res, err := findConflicts(conflictsRequest{repoDir: clone, remote: remote, base: "pr", number: 7})

	require.NoError(t, err)
	require.Empty(t, res.conflicts)
}

func TestFindConflictsReportsGitsErrors(t *testing.T) {
	remote, clone := gitRepos(t)

	_, err := findConflicts(conflictsRequest{repoDir: clone, remote: remote, base: "main", number: 8})

	require.ErrorContains(t, err, "couldn't fetch the PR")
	require.ErrorContains(t, err, "refs/pull/8/head")
}

func TestRemoteForPrefersTheClonesRemote(t *testing.T) {
	_, clone := gitRepos(t)
	_, err := git(clone, "remote", "add", "upstream", "git@github.com:o/r.git")
	require.NoError(t, err)

	require.Equal(t, "upstream", remoteFor(clone, "github.com", "o/r"))
	require.Equal(t, "https://github.com/o/other.git", remoteFor(clone, "github.com", "o/other"))
}

func TestConflictsView(t *testing.T) {
	remote, clone := gitRepos(t)
	req := conflictsRequest{prURL: "https://github.com/o/r/pull/7", repoDir: clone, remote: remote,
		base: "main", number: 7}
	v := newConflictsView(req, newConflictsCache())
	env := testEnv(t)

	cmd := v.Init()
	require.NotNil(t, cmd)
	body, _ := v.Render(env, 80)
	require.Contains(t, ansi.Strip(body), "Merging the PR into main")
	_, ok := v.Diff()
	require.False(t, ok, "there's nothing to show yet")

	v.Update(cmd())

	require.Equal(t, conflictsTitle+" (2)", v.Title())
	body, items := v.Render(env, 80)
	lines := strings.Split(ansi.Strip(body), "\n")
	require.Contains(t, ansi.Strip(body), "Needs a rebase: 2 files conflict with main")
	require.Len(t, items, 3)
	require.Contains(t, lines[items[0].Line], "f.txt")
	require.Contains(t, lines[items[0].Line], "contents")
	require.Equal(t, "show its conflicts", items[0].Hint)
	require.Contains(t, lines[items[1].Line], "g.txt")
	require.Empty(t, items[1].Hint, "there's no diff to show")
	require.Contains(t, lines[items[2].Line], "Check again")

	diff, ok := v.Diff()
	require.True(t, ok)
	require.Contains(t, ansi.Strip(diff), "<<<<<<< gh-dash/main")
	require.Equal(t, plugins.ShowDiffMsg{Diff: v.res.conflicts[0].diff}, v.Activate(0)())
	require.Nil(t, v.Activate(1))

	// Focusing a file explains its conflicts
	require.True(t, v.Focus(1))
	body, _ = v.Render(env, 80)
	require.Contains(t, ansi.Strip(body), "g.txt deleted in gh-dash/pull/7 and modified in gh-dash/main")
	require.Contains(t, ansi.Strip(body), "Kept as main has it")

	// Opened again, it shows them right away
	again := newConflictsView(req, v.cache)
	require.Nil(t, again.Init())
	require.Equal(t, conflictsTitle+" (2)", again.Title())
}

func TestConflictsViewWithoutAClone(t *testing.T) {
	v := newConflictsView(conflictsRequest{err: os.ErrNotExist}, newConflictsCache())

	require.Nil(t, v.Init())
	body, items := v.Render(testEnv(t), 80)
	require.Contains(t, ansi.Strip(body), "Couldn't find the conflicts")
	require.Empty(t, items, "checking again won't help")
}
