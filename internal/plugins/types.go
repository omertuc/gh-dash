package plugins

import (
	tea "charm.land/bubbletea/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

// PR is the pull request a plugin is asked about.
type PR struct {
	Primary *data.PullRequestData
	// Enriched holds the PR's comments, reviews and checks, once IsEnriched
	Enriched   *data.EnrichedPullRequestData
	IsEnriched bool
}

// Labels returns the names of the PR's labels.
func (pr PR) Labels() []string {
	var labels []string
	seen := map[string]bool{}
	add := func(nodes []data.Label) {
		for _, l := range nodes {
			if !seen[l.Name] {
				seen[l.Name] = true
				labels = append(labels, l.Name)
			}
		}
	}
	if pr.Primary != nil {
		add(pr.Primary.Labels.Nodes)
	}
	if pr.IsEnriched && pr.Enriched != nil {
		add(pr.Enriched.Labels.Nodes)
	}
	return labels
}

// HasLabel reports whether the PR is labeled name.
func (pr PR) HasLabel(name string) bool {
	for _, l := range pr.Labels() {
		if l == name {
			return true
		}
	}
	return false
}

// Checks returns the checks of the PR's last commit, both check runs and
// commit statuses, along with the required ones that weren't reported yet.
func (pr PR) Checks() []Check {
	if !pr.IsEnriched || pr.Enriched == nil || len(pr.Enriched.Commits.Nodes) == 0 {
		return nil
	}
	var checks []Check
	reported := map[string]bool{}
	commit := pr.Enriched.Commits.Nodes[0].Commit
	for _, node := range commit.StatusCheckRollup.Contexts.Nodes {
		switch node.Typename {
		case "CheckRun":
			run := node.CheckRun
			state := string(run.Status)
			if run.Status == "COMPLETED" && run.Conclusion != "" {
				state = string(run.Conclusion)
			}
			url := string(run.DetailsUrl)
			if url == "" {
				url = string(run.Url)
			}
			checks = append(checks, Check{Name: string(run.Name), URL: url, State: state})
			reported[string(run.Name)] = true
		case "StatusContext":
			status := node.StatusContext
			checks = append(checks, Check{
				Name:            string(status.Context),
				URL:             string(status.TargetUrl),
				State:           string(status.State),
				IsStatusContext: true,
				Description:     string(status.Description),
			})
			reported[string(status.Context)] = true
		}
	}
	if rules := pr.Enriched.Repository.BranchProtectionRules.Nodes; len(rules) > 0 {
		for _, name := range rules[0].RequiredStatusCheckContexts {
			if !reported[string(name)] {
				checks = append(checks, Check{Name: string(name), State: "EXPECTED", Required: true})
			}
		}
	}
	return checks
}

// Check is one of a PR's checks: a check run or a commit status.
type Check struct {
	// Name is a check run's name or a commit status's context, e.g.
	// "ci/prow/e2e-aws"
	Name string
	// URL is where the check's details are, e.g. its job's page
	URL string
	// State is how it went, as GitHub puts it, e.g. "SUCCESS", "FAILURE",
	// "PENDING" or "IN_PROGRESS"
	State string
	// IsStatusContext is whether it's a commit status rather than a check run
	IsStatusContext bool
	// Description is a commit status's description
	Description string
	// Required is set for checks that are required but weren't reported yet
	Required bool
}

// Status is how things stand, which decides the glyph and color something is
// shown with.
type Status int

const (
	StatusNeutral Status = iota
	StatusSuccess
	StatusPending
	StatusFailure
)

// Panel is a box of information shown in a PR's overview.
type Panel struct {
	Title string
	// Status colors the panel's border and picks its title's glyph
	Status   Status
	Subtitle string
	Rows     []PanelRow
}

// PanelRow is a line of a panel.
type PanelRow struct {
	Status Status
	// Glyph is shown before the text, instead of the status's glyph
	Glyph string
	Text  string
	// Faint rows are less important, e.g. details
	Faint bool
}

// View is shown in a tab of its own, e.g. a CI job's test results. It's a
// small bubbletea model whose content is rendered into the preview, where
// its items can be focused with the navigation keys and acted on with enter
// or a click.
//
// Messages produced by the view's commands are delivered back to its Update,
// so views may fetch things in the background.
type View interface {
	// Title is the tab's name, e.g. "Job e2e-aws"
	Title() string
	// Init starts the view, e.g. fetching what it shows
	Init() tea.Cmd
	// Update handles the messages produced by the view's commands
	Update(msg tea.Msg) tea.Cmd
	// Render renders the view at the given width, along with its focusable
	// items in the order they're shown
	Render(env Env, width int) (string, []Item)
	// Focus is called when the item at the given index, as last rendered, is
	// focused, or with -1 when none is. It reports whether the view changed,
	// e.g. to show the item's details.
	Focus(item int) bool
	// Activate acts on the item at the given index, e.g. when enter is
	// pressed on it or it's clicked
	Activate(item int) tea.Cmd
}

// Item is something focusable in a view's content.
type Item struct {
	// Line is the line of the rendered content the item starts on
	Line int
	// Hint describes what activating the item does, e.g. "load", shown
	// after the key activating it while it's focused
	Hint string
	// Clickable items are activated by clicking them, e.g. buttons, rather
	// than only focused
	Clickable bool
}

// Env gives views access to gh-dash's styles and theme.
type Env struct {
	Ctx *context.ProgramContext
}
