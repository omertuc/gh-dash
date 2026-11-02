package prow

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dlvhdr/gh-dash/v4/internal/plugins"
)

const (
	labelNeedsRebase = "needs-rebase"
	conflictsTabKey  = "conflicts"
	conflictsTitle   = " Conflicts"

	// conflictsTTL is how long a PR's conflicts are reused before they're
	// looked for again, as its target branch moves on
	conflictsTTL = 10 * time.Minute
)

// needsRebase reports whether the PR can't merge into its target branch as
// it is: Prow labeled it needs-rebase or GitHub found it conflicting.
func needsRebase(pr plugins.PR) bool {
	if pr.HasLabel(labelNeedsRebase) {
		return true
	}
	if pr.IsEnriched && pr.Enriched != nil && pr.Enriched.Mergeable != "" {
		return pr.Enriched.Mergeable == "CONFLICTING"
	}
	return pr.Primary != nil && pr.Primary.Mergeable == "CONFLICTING"
}

// Tabs shows the files a PR conflicts in when it needs a rebase.
func (p *Plugin) Tabs(pr plugins.PR) []plugins.Tab {
	if pr.Primary == nil || !managesPR(pr) || !needsRebase(pr) {
		return nil
	}
	return []plugins.Tab{{Key: conflictsTabKey, Title: conflictsTitle}}
}

// OpenTab returns a view of the files the PR conflicts in.
func (p *Plugin) OpenTab(env plugins.Env, pr plugins.PR, key string) plugins.View {
	return newConflictsView(newConflictsRequest(env, pr), p.conflictsCache())
}

// conflictsCache returns the cache of PRs' conflicts.
func (p *Plugin) conflictsCache() *conflictsCache {
	p.conflictsOnce.Do(func() {
		p.conflicts = newConflictsCache()
	})
	return p.conflicts
}

// conflictsRequest is what's needed to find a PR's conflicts.
type conflictsRequest struct {
	prURL string
	// repo is the PR's repo, e.g. "openshift/origin", and repoDir its local
	// clone, where the PR is merged
	repo    string
	repoDir string
	// remote is the local clone's remote of the PR's repo, or its URL when it
	// has none
	remote string
	number int
	// base is the PR's target branch
	base string
	// headOid is the PR's last commit, as last seen, if known
	headOid string
	// err is why the PR can't be merged locally, e.g. there's no local clone
	err error
}

func newConflictsRequest(env plugins.Env, pr plugins.PR) conflictsRequest {
	p := pr.Primary
	req := conflictsRequest{
		prURL:  p.Url,
		repo:   p.GetRepoNameWithOwner(),
		number: p.Number,
		base:   p.BaseRefName,
	}
	if pr.IsEnriched && pr.Enriched != nil {
		if commits := pr.Enriched.AllCommits.Nodes; len(commits) > 0 {
			req.headOid = commits[len(commits)-1].Commit.Oid
		}
		if pr.Enriched.BaseRefName != "" {
			req.base = pr.Enriched.BaseRefName
		}
	}
	if req.base == "" {
		req.err = errors.New("the PR's target branch isn't known")
		return req
	}
	dir, err := env.Ctx.RepoLocalPath(req.repo)
	if err != nil {
		req.err = err
		return req
	}
	req.repoDir = dir
	if _, err := git(dir, "rev-parse", "--git-dir"); err != nil {
		req.err = fmt.Errorf("there's no clone of %s at %s: clone it there, "+
			"or point repoPaths in your config.yml at one", req.repo, shortenHome(dir))
		return req
	}
	host := "github.com"
	if u, err := url.Parse(p.Url); err == nil && u.Host != "" {
		host = u.Host
	}
	req.remote = remoteFor(dir, host, req.repo)
	return req
}

// remoteFor returns the clone's remote of repo, so it's fetched the way the
// user set it up, e.g. over ssh, or failing that repo's URL.
func remoteFor(dir, host, repo string) string {
	out, _ := git(dir, "remote", "-v")
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[2] == "(fetch)" && isRepoURL(fields[1], host, repo) {
			return fields[0]
		}
	}
	return "https://" + host + "/" + repo + ".git"
}

// isRepoURL reports whether a remote's URL is repo's on host, e.g.
// "git@github.com:openshift/origin.git" for "openshift/origin".
func isRepoURL(u, host, repo string) bool {
	u = strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(u, "/"), ".git"))
	repo = strings.ToLower(repo)
	return strings.Contains(u, strings.ToLower(host)) &&
		(strings.HasSuffix(u, "/"+repo) || strings.HasSuffix(u, ":"+repo))
}

// mergeResult is how the PR merges into its target branch.
type mergeResult struct {
	baseOid string
	headOid string
	// conflicts are the files the PR conflicts in, in the order git lists
	// them
	conflicts []conflict
}

// conflict is a file the PR conflicts in.
type conflict struct {
	path string
	// kinds are the kinds of the file's conflicts, e.g. "content" or
	// "modify/delete"
	kinds []string
	// messages explain them, e.g. "g.txt deleted in ... and modified in ..."
	messages []string
	// diff is how merging the PR changes the file on the target branch,
	// conflict markers and all, or "" when it doesn't, e.g. when the file
	// was deleted by the PR but kept as the target branch has it
	diff string
}

// diff returns the diffs of all the files the PR conflicts in.
func (r *mergeResult) diff() string {
	var b strings.Builder
	for _, c := range r.conflicts {
		b.WriteString(c.diff)
	}
	return b.String()
}

// findConflicts fetches the PR and its target branch into the local clone
// and merges them there, without touching its work tree, to find the files
// they conflict in. The refs it fetches into are removed once it's done.
//
// The conflict markers are labeled by the refs, e.g. "gh-dash/main" and
// "gh-dash/pull/123".
func findConflicts(req conflictsRequest) (*mergeResult, error) {
	baseRef := "refs/gh-dash/" + req.base
	headRef := "refs/gh-dash/pull/" + strconv.Itoa(req.number)
	defer func() {
		_, _ = git(req.repoDir, "update-ref", "-d", baseRef)
		_, _ = git(req.repoDir, "update-ref", "-d", headRef)
	}()
	if _, err := git(req.repoDir, "fetch", "--quiet", "--no-tags", "--no-write-fetch-head",
		"--no-auto-gc", "--no-recurse-submodules", req.remote,
		"+refs/heads/"+req.base+":"+baseRef,
		"+refs/pull/"+strconv.Itoa(req.number)+"/head:"+headRef); err != nil {
		return nil, fmt.Errorf("couldn't fetch the PR: %w", err)
	}
	out, err := git(req.repoDir, "rev-parse", baseRef, headRef)
	if err != nil {
		return nil, err
	}
	oids := strings.Fields(out)
	if len(oids) != 2 {
		return nil, fmt.Errorf("couldn't tell what was fetched: %q", out)
	}
	res := &mergeResult{baseOid: oids[0], headOid: oids[1]}

	out, err = git(req.repoDir, "merge-tree", "--write-tree", "-z", "--name-only",
		strings.TrimPrefix(baseRef, "refs/"), strings.TrimPrefix(headRef, "refs/"))
	var exitErr *exec.ExitError
	if err != nil && !(errors.As(err, &exitErr) && exitErr.ExitCode() == 1) {
		// 1 is for conflicts
		return nil, fmt.Errorf("couldn't merge the PR: %w", err)
	}
	tree, conflicts := parseMergeTree(out)
	for i, c := range conflicts {
		diff, err := git(req.repoDir, "diff", "--color=always", "--no-ext-diff",
			res.baseOid, tree, "--", c.path)
		if err != nil {
			return nil, err
		}
		conflicts[i].diff = diff
	}
	res.conflicts = conflicts
	return res, nil
}

// parseMergeTree parses what "git merge-tree --write-tree -z --name-only"
// prints: the merged tree, the conflicted files, and messages about them.
func parseMergeTree(out string) (string, []conflict) {
	fields := strings.Split(out, "\x00")
	if len(fields) == 0 {
		return "", nil
	}
	tree := fields[0]
	var conflicts []conflict
	byPath := map[string]int{}
	i := 1
	for ; i < len(fields) && fields[i] != ""; i++ {
		byPath[fields[i]] = len(conflicts)
		conflicts = append(conflicts, conflict{path: fields[i]})
	}
	// Each message is the number of paths it's about, the paths, its kind,
	// e.g. "CONFLICT (contents)", and the message itself
	for i++; i < len(fields); {
		n, err := strconv.Atoi(fields[i])
		if err != nil || i+n+2 >= len(fields) {
			break
		}
		paths := fields[i+1 : i+1+n]
		kind, msg := fields[i+1+n], strings.TrimSpace(fields[i+2+n])
		i += n + 3
		kind, ok := strings.CutPrefix(kind, "CONFLICT (")
		if !ok {
			// e.g. "Auto-merging"
			continue
		}
		kind = strings.TrimSuffix(kind, ")")
		if _, rest, ok := strings.Cut(msg, ": "); ok {
			// e.g. "CONFLICT (modify/delete): g.txt deleted in ..."
			msg = rest
		}
		if strings.HasPrefix(msg, "Merge conflict in ") {
			// Its kind says as much
			msg = ""
		}
		for _, p := range paths {
			j, ok := byPath[p]
			if !ok {
				continue
			}
			c := &conflicts[j]
			if !containsString(c.kinds, kind) {
				c.kinds = append(c.kinds, kind)
			}
			if msg != "" && !containsString(c.messages, msg) {
				c.messages = append(c.messages, msg)
			}
		}
	}
	return tree, conflicts
}

func containsString(list []string, s string) bool {
	for _, l := range list {
		if l == s {
			return true
		}
	}
	return false
}

// git runs git in dir, returning what it printed. Errors carry what it
// printed to stderr.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	// Asking for credentials would garble the screen
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return stdout.String(), &gitError{msg: msg, err: err}
		}
		return stdout.String(), err
	}
	return stdout.String(), nil
}

type gitError struct {
	msg string
	err error
}

func (e *gitError) Error() string { return e.msg }
func (e *gitError) Unwrap() error { return e.err }

// shortenHome shows paths in the home directory starting with ~.
func shortenHome(path string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rest, ok := strings.CutPrefix(path, home); ok && (rest == "" || rest[0] == '/') {
			return "~" + rest
		}
	}
	return path
}

// conflictsCache keeps PRs' conflicts for a while, so their tab opens with
// them right away when it's opened again, and merges one PR at a time in
// each local clone, so their fetches don't trip over each other.
type conflictsCache struct {
	mu      sync.Mutex
	entries map[string]*conflictsEntry
	repos   map[string]*sync.Mutex
}

type conflictsEntry struct {
	// headOid is the PR's last commit when its conflicts were asked for
	headOid string
	at      time.Time
	done    chan struct{}
	res     *mergeResult
	err     error
}

func newConflictsCache() *conflictsCache {
	return &conflictsCache{
		entries: map[string]*conflictsEntry{},
		repos:   map[string]*sync.Mutex{},
	}
}

// entry returns the PR's entry, a new one when fresh, when the PR changed
// since or when it's too old or failed, along with whether it's new and its
// conflicts need finding.
func (c *conflictsCache) entry(req conflictsRequest, fresh bool) (*conflictsEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[req.prURL]; ok && !fresh && e.headOid == req.headOid &&
		time.Since(e.at) < conflictsTTL {
		select {
		case <-e.done:
			if e.err == nil {
				return e, false
			}
		default:
			return e, false
		}
	}
	e := &conflictsEntry{headOid: req.headOid, at: time.Now(), done: make(chan struct{})}
	c.entries[req.prURL] = e
	return e, true
}

// get returns the PR's conflicts, finding them unless they were found
// recently or are being found.
func (c *conflictsCache) get(req conflictsRequest, fresh bool) (*mergeResult, error) {
	e, isNew := c.entry(req, fresh)
	if isNew {
		lock := c.repoLock(req.repoDir)
		lock.Lock()
		e.res, e.err = findConflicts(req)
		lock.Unlock()
		close(e.done)
	}
	<-e.done
	return e.res, e.err
}

// peek returns the PR's conflicts when they were found recently, without
// waiting for them.
func (c *conflictsCache) peek(req conflictsRequest) (*mergeResult, bool) {
	c.mu.Lock()
	e, ok := c.entries[req.prURL]
	c.mu.Unlock()
	if !ok || e.headOid != req.headOid || time.Since(e.at) >= conflictsTTL {
		return nil, false
	}
	select {
	case <-e.done:
		return e.res, e.err == nil
	default:
		return nil, false
	}
}

func (c *conflictsCache) repoLock(dir string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	lock, ok := c.repos[dir]
	if !ok {
		lock = &sync.Mutex{}
		c.repos[dir] = lock
	}
	return lock
}
