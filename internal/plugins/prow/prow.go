// Package prow is a plugin for repos whose PRs are managed by Prow
// (https://docs.prow.k8s.io), e.g. Kubernetes' and OpenShift's. It offers
// Prow's commands while writing a comment, shows who approved a PR and what
// still needs approving, browses a Prow job's test results, and reruns or
// overrides a PR's checks.
package prow

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/dlvhdr/gh-dash/v4/internal/plugins"
)

// Name is what the plugin is enabled by in the config.
const Name = "prow"

func init() {
	plugins.Register(Name, func() plugins.Plugin { return New() })
}

// Options are the plugin's settings.
type Options struct {
	// ArtifactsURL is where jobs' artifacts are fetched from, a Google Cloud
	// Storage compatible API
	ArtifactsURL string
	// PageSize is how many tests are shown at a time
	PageSize int
	// ExtraCommands are offered along with Prow's own, e.g. ones only a
	// repo's Prow understands, like "/jira refresh"
	ExtraCommands []plugins.CommentCommand
}

// Plugin is the prow plugin.
type Plugin struct {
	opts Options
}

// New returns the plugin with its default options.
func New() *Plugin {
	return &Plugin{opts: Options{
		ArtifactsURL: "https://storage.googleapis.com",
		PageSize:     50,
	}}
}

func (p *Plugin) Name() string { return Name }

// Configure applies the options:
//
//	artifactsUrl: https://storage.googleapis.com
//	pageSize: 50
//	extraCommands:
//	  - command: /jira refresh
//	    description: Refresh the Jira issue's state
func (p *Plugin) Configure(options map[string]any) error {
	for k, v := range options {
		switch k {
		case "artifactsUrl":
			s, ok := v.(string)
			if !ok || s == "" {
				return fmt.Errorf("artifactsUrl must be a URL")
			}
			p.opts.ArtifactsURL = strings.TrimRight(s, "/")
		case "pageSize":
			n, ok := v.(int)
			if f, isFloat := v.(float64); isFloat {
				n, ok = int(f), true
			}
			if !ok || n <= 0 {
				return fmt.Errorf("pageSize must be a positive number")
			}
			p.opts.PageSize = n
		case "extraCommands":
			list, ok := v.([]any)
			if !ok {
				return fmt.Errorf("extraCommands must be a list")
			}
			for _, item := range list {
				m, ok := item.(map[string]any)
				if !ok {
					return fmt.Errorf("extraCommands must list commands and descriptions")
				}
				command, _ := m["command"].(string)
				description, _ := m["description"].(string)
				if !strings.HasPrefix(command, "/") {
					return fmt.Errorf("extra command %q must start with /", command)
				}
				p.opts.ExtraCommands = append(p.opts.ExtraCommands,
					plugins.CommentCommand{Text: command, Description: description})
			}
		default:
			return fmt.Errorf("unknown option %q", k)
		}
	}
	return nil
}

// managesPR reports whether Prow manages the PR: its checks link to Prow
// jobs, Prow commented on it or it has Prow's labels.
func managesPR(pr plugins.PR) bool {
	for _, c := range pr.Checks() {
		if _, ok := parseJobURL(c.URL); ok || c.Name == "tide" {
			return true
		}
	}
	if pr.IsEnriched && pr.Enriched != nil {
		for _, c := range pr.Enriched.Comments.Nodes {
			if strings.Contains(c.Body, approvalNotifierMarker) ||
				strings.Contains(c.Body, "git.k8s.io/community/contributors/guide/pull-requests.md") {
				return true
			}
		}
	}
	for _, l := range pr.Labels() {
		if l == labelLGTM || l == labelApproved || strings.HasPrefix(l, "do-not-merge/") {
			return true
		}
	}
	return false
}

// staticCommands are Prow's commands that don't depend on the PR.
var staticCommands = []plugins.CommentCommand{
	{Text: "/lgtm", Description: "Looks good to me: add the lgtm label"},
	{Text: "/lgtm cancel", Description: "Remove the lgtm label"},
	{Text: "/approve", Description: "Approve the files you own"},
	{Text: "/approve cancel", Description: "Withdraw your approval"},
	{Text: "/approve no-issue", Description: "Approve without a linked issue"},
	{Text: "/hold", Description: "Block merging: add do-not-merge/hold"},
	{Text: "/hold cancel", Description: "Allow merging again"},
	{Text: "/unhold", Description: "Allow merging again"},
	{Text: "/retest", Description: "Rerun the failed jobs"},
	{Text: "/retest-required", Description: "Rerun the failed required jobs"},
	{Text: "/test all", Description: "Run all jobs"},
	{Text: "/test ?", Description: "List the jobs that can be run"},
	{Text: "/ok-to-test", Description: "Let jobs run for an outside contributor"},
	{Text: "/assign", Description: "Assign yourself"},
	{Text: "/unassign", Description: "Unassign yourself"},
	{Text: "/close", Description: "Close the PR"},
	{Text: "/reopen", Description: "Reopen the PR"},
}

// CommentCommands offers Prow's commands, including running each of the PR's
// jobs with /test and overriding each of its statuses with /override,
// failing ones first.
func (p *Plugin) CommentCommands(pr plugins.PR) []plugins.CommentCommand {
	if !managesPR(pr) {
		return nil
	}
	commands := slices.Clone(staticCommands)

	checks := pr.Checks()
	sort.SliceStable(checks, func(i, j int) bool {
		return stateRank(checks[i].State) < stateRank(checks[j].State)
	})

	for _, job := range testableJobs(pr, checks) {
		commands = append(commands, plugins.CommentCommand{
			Text:        "/test " + job.name,
			Description: job.description,
		})
	}
	for _, c := range checks {
		if c.Name == "tide" || !c.IsStatusContext && !c.Required {
			// Only commit statuses can be overridden
			continue
		}
		commands = append(commands, plugins.CommentCommand{
			Text:        "/override " + c.Name,
			Description: "Mark as passed · " + humanize(c.State),
		})
	}
	return append(commands, p.opts.ExtraCommands...)
}

// CheckCommands offers rerunning the check's job with /test, and overriding
// the check with /override unless it passed.
func (p *Plugin) CheckCommands(pr plugins.PR, check plugins.Check) []plugins.CheckCommand {
	if !managesPR(pr) || check.Name == "tide" {
		return nil
	}
	var commands []plugins.CheckCommand
	if name, ok := jobNameFromContext(check); ok {
		commands = append(commands, plugins.CheckCommand{
			Key: "t", Label: "rerun", Comment: "/test " + name,
		})
	}
	if (check.IsStatusContext || check.Required) && strings.ToUpper(check.State) != "SUCCESS" {
		// Only commit statuses can be overridden
		commands = append(commands, plugins.CheckCommand{
			Key: "o", Label: "override", Comment: "/override " + check.Name, Confirm: true,
		})
	}
	return commands
}

type testableJob struct {
	name        string
	description string
}

// testCommandRegex finds the jobs Prow offers running in its comments, e.g.
// "`/test e2e-aws`".
var testCommandRegex = regexp.MustCompile("`/test ([^`\\s?]+)`")

// testableJobs lists the jobs that /test runs: those of the PR's statuses
// that are Prow jobs, and those Prow listed in its comments.
func testableJobs(pr plugins.PR, checks []plugins.Check) []testableJob {
	var jobs []testableJob
	seen := map[string]bool{"all": true}
	add := func(name, description string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		jobs = append(jobs, testableJob{name: name, description: description})
	}
	for _, c := range checks {
		if name, ok := jobNameFromContext(c); ok {
			desc := "Run the job · " + humanize(c.State)
			if c.Required {
				desc += " · required"
			}
			add(name, desc)
		}
	}
	if pr.IsEnriched && pr.Enriched != nil {
		for _, c := range pr.Enriched.Comments.Nodes {
			for _, m := range testCommandRegex.FindAllStringSubmatch(c.Body, -1) {
				add(m[1], "Run the job")
			}
		}
	}
	return jobs
}

// jobNameFromContext returns the name /test runs a status's job by, e.g.
// "e2e-aws" for "ci/prow/e2e-aws".
func jobNameFromContext(c plugins.Check) (string, bool) {
	if name, ok := strings.CutPrefix(c.Name, "ci/prow/"); ok {
		return name, true
	}
	if _, ok := parseJobURL(c.URL); ok {
		// Upstream, e.g. Kubernetes', statuses are named after their job
		return c.Name, true
	}
	return "", false
}

// stateRank orders checks by how much they need attention: failed first,
// then pending, then the rest.
func stateRank(state string) int {
	switch strings.ToUpper(state) {
	case "FAILURE", "ERROR", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE":
		return 0
	case "PENDING", "EXPECTED", "IN_PROGRESS", "QUEUED", "WAITING", "REQUESTED":
		return 1
	default:
		return 2
	}
}

// humanize turns an API state into words, e.g. "IN_PROGRESS" into
// "in progress".
func humanize(state string) string {
	if state == "" {
		return "unknown"
	}
	if state == "EXPECTED" {
		return "not reported yet"
	}
	return strings.ToLower(strings.ReplaceAll(state, "_", " "))
}

// CheckAction opens Prow jobs whose artifacts are in Google Cloud Storage in
// a tab listing their tests.
func (p *Plugin) CheckAction(check plugins.Check) (string, bool) {
	if _, ok := parseJobURL(check.URL); ok {
		return "view tests", true
	}
	return "", false
}

// OpenCheck returns a view of the check's job's test results.
func (p *Plugin) OpenCheck(pr plugins.PR, check plugins.Check) plugins.View {
	job, _ := parseJobURL(check.URL)
	return newJobView(check, job, p.opts)
}
