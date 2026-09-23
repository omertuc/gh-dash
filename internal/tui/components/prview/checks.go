package prview

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
	ghchecks "github.com/dlvhdr/x/gh-checks"
)

type checkSectionStatus int

const (
	statusSuccess checkSectionStatus = iota
	statusFailure
	statusWaiting
	statusNonRequested
)

func (m *Model) renderChecksOverview() string {
	w := m.getIndentedContentWidth()

	if m.pr.Data.Primary.State == "MERGED" {
		return m.viewMergedStatus()
	}

	if m.pr.Data.Primary.State == "CLOSED" {
		return m.viewClosedStatus()
	}

	review, rStatus := m.viewReviewStatus()
	checks, cStatus := m.viewChecksStatus()
	merge, mStatus := m.viewMergeStatus()

	borderColor := m.ctx.Theme.FaintBorder
	if rStatus == statusFailure || cStatus == statusFailure || mStatus == statusFailure {
		borderColor = m.ctx.Theme.ErrorText
	} else if rStatus == statusSuccess && cStatus == statusSuccess && mStatus == statusSuccess {
		borderColor = m.ctx.Theme.SuccessText
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(w)
	parts := make([]string, 0)
	if review != "" {
		parts = append(parts, review)
	}
	if checks != "" {
		parts = append(parts, checks)
	}
	if merge != "" {
		parts = append(parts, merge)
	}

	return box.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

func (m *Model) viewChecksStatus() (string, checkSectionStatus) {
	checks := ""

	if !m.pr.Data.IsEnriched {
		return m.viewCheckCategory(
			m.ctx.Styles.Common.WaitingGlyph,
			"Loading...",
			"",
			false,
		), statusWaiting
	}

	stats := m.getChecksStats()
	var icon, title string
	var status checkSectionStatus

	statStrs := make([]string, 0)
	if stats.failed > 0 {
		icon = m.ctx.Styles.Common.FailureGlyph
		title = "Some checks were not successful"
		status = statusFailure
	} else if stats.awaitingApproval > 0 {
		icon = m.ctx.Styles.Common.ActionRequiredGlyph
		title = "Workflows awaiting approval"
		status = statusWaiting
	} else if stats.inProgress > 0 {
		icon = m.ctx.Styles.Common.WaitingGlyph
		title = "Some checks haven’t completed yet"
		status = statusWaiting
	} else if stats.succeeded > 0 {
		icon = m.ctx.Styles.Common.SuccessGlyph
		title = "All checks have passed"
		status = statusSuccess
	} else {
		return "", statusWaiting
	}

	if stats.failed > 0 {
		statStrs = append(statStrs, fmt.Sprintf("%d failing", stats.failed))
	}
	if stats.awaitingApproval > 0 {
		statStrs = append(
			statStrs,
			fmt.Sprintf(
				"%d awaiting approval. Press %s to run.",
				stats.awaitingApproval,
				m.ctx.Styles.KeyHint.Render(keys.PRKeys.ApproveWorkflows.Keys()[0])),
		)
	}
	if stats.inProgress > 0 {
		statStrs = append(statStrs, fmt.Sprintf("%d in progress", stats.inProgress))
	}
	if stats.skipped > 0 {
		statStrs = append(statStrs, fmt.Sprintf("%d skipped", stats.skipped))
	}
	if stats.neutral > 0 {
		statStrs = append(statStrs, fmt.Sprintf("%d neutral", stats.neutral))
	}
	if stats.succeeded > 0 {
		statStrs = append(statStrs, fmt.Sprintf("%d successful", stats.succeeded))
	}
	if title != "" {
		checksBar := m.viewChecksBar()
		checksBottom := lipgloss.JoinVertical(
			lipgloss.Left,
			strings.Join(statStrs, ", "),
			checksBar,
		)
		checks = m.viewCheckCategory(icon, title, checksBottom, false)
	}
	return checks, status
}

func (m *Model) viewMergeStatus() (string, checkSectionStatus) {
	var icon, title, subtitle string
	var status checkSectionStatus
	numReviewOwners := m.numRequestedReviewOwners()
	if m.pr.Data.Primary.MergeStateStatus == "CLEAN" ||
		m.pr.Data.Primary.MergeStateStatus == "UNSTABLE" {
		icon = m.ctx.Styles.Common.SuccessGlyph
		title = "No conflicts with base branch"
		subtitle = "Changes can be cleanly merged"
		status = statusSuccess
	} else if m.pr.Data.Primary.IsDraft {
		icon = m.ctx.Styles.Common.DraftGlyph
		title = "This pull request is still a work in progress"
		subtitle = "Draft pull requests cannot be merged"
		status = statusWaiting
	} else if m.pr.Data.Primary.MergeStateStatus == "BLOCKED" {
		icon = m.ctx.Styles.Common.FailureGlyph
		title = "Merging is blocked"
		if numReviewOwners > 0 {
			subtitle = "Waiting on code owner review"
		}
		status = statusFailure
	} else if m.pr.Data.Primary.Mergeable == "CONFLICTING" {
		icon = m.ctx.Styles.Common.FailureGlyph
		title = "This branch has conflicts that must be resolved"
		status = statusFailure
		if m.pr.Data.Primary.MergeStateStatus == "CLEAN" {
			subtitle = "Changes can be cleanly merged"
		}
	}
	return m.viewCheckCategory(icon, title, subtitle, true), status
}

func (m *Model) viewMergedStatus() string {
	w := m.getIndentedContentWidth()
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.ctx.Styles.Colors.MergedPR).
		Width(w)
	return box.Render(m.viewCheckCategory(
		m.ctx.Styles.Common.MergedGlyph,
		"Pull request successfully merged and closed",
		"The branch has been merged",
		true,
	))
}

func (m *Model) viewClosedStatus() string {
	w := m.getIndentedContentWidth()
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.ctx.Theme.FaintBorder).
		Width(w)
	return box.Render(m.viewCheckCategory(
		"",
		"Closed with unmerged commits",
		"This pull request is closed",
		true,
	))
}

func (m *Model) viewReviewStatus() (string, checkSectionStatus) {
	pr := m.pr
	if pr.Data == nil {
		return "", statusWaiting
	}

	var icon, title, subtitle string
	var status checkSectionStatus
	numReviewOwners := m.numRequestedReviewOwners()

	numApproving, numChangesRequested, numPending, numCommented := 0, 0, 0, 0

	for _, node := range pr.Data.Enriched.Reviews.Nodes {
		switch node.State {
		case "APPROVED":
			numApproving++
		case "CHANGES_REQUESTED":
			numChangesRequested++
		case "PENDING":
			numPending++
		case "COMMENTED":
			numCommented++
		}
	}

	switch pr.Data.Primary.ReviewDecision {
	case "APPROVED":
		icon = m.ctx.Styles.Common.SuccessGlyph
		title = "Changes approved"
		subtitle = fmt.Sprintf("%d approving reviews", numApproving)
		status = statusSuccess
	case "CHANGES_REQUESTED":
		icon = m.ctx.Styles.Common.FailureGlyph
		title = "Changes requested"
		subtitle = fmt.Sprintf("%d requested changes", numChangesRequested)
		status = statusFailure
	case "REVIEW_REQUIRED":
		icon = pr.Ctx.Styles.Common.WaitingGlyph
		title = "Review Required"

		branchRules := m.pr.Data.Primary.Repository.BranchProtectionRules.Nodes
		if len(branchRules) > 0 && branchRules[0].RequiresCodeOwnerReviews && numApproving < 1 {
			subtitle = "Code owner review required"
			status = statusFailure
		} else if numApproving < numReviewOwners {
			subtitle = "Code owner review required"
			status = statusFailure
		} else if len(branchRules) > 0 && numApproving <
			branchRules[0].RequiredApprovingReviewCount {
			subtitle = fmt.Sprintf("Need %d more approval",
				branchRules[0].RequiredApprovingReviewCount-numApproving)
			status = statusWaiting
		} else if numCommented > 0 {
			subtitle = fmt.Sprintf("%d reviewers left comments", numCommented)
			status = statusWaiting
		}
	default:
		icon = pr.Ctx.Styles.Common.PersonGlyph
		title = "Reviews"
		subtitle = "None requested"
		status = statusNonRequested
	}

	return m.viewCheckCategory(icon, title, subtitle, false), status
}

func (m *Model) viewCheckCategory(icon, title, subtitle string, isLast bool) string {
	w := m.getIndentedContentWidth() - 2
	part := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, !isLast, false).
		BorderForeground(m.ctx.Theme.FaintBorder).
		Width(w).
		Padding(1)

	sTitle := lipgloss.NewStyle().Bold(true)
	sSub := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText)

	category := lipgloss.JoinHorizontal(lipgloss.Top, icon, " ", sTitle.Render(title))

	if subtitle != "" {
		category = lipgloss.JoinVertical(
			lipgloss.Left,
			category,
			sSub.MarginLeft(2).Render(subtitle),
		)
	}
	if category == "" {
		return ""
	}
	return part.Render(category)
}

func (m *Model) viewChecksBar() string {
	w := m.getIndentedContentWidth() - 6
	stats := m.getChecksStats()
	total := float64(
		stats.failed + stats.skipped + stats.neutral + stats.succeeded + stats.inProgress + stats.awaitingApproval,
	)
	numSections := 0
	if stats.failed > 0 {
		numSections++
	}
	if stats.awaitingApproval > 0 {
		numSections++
	}
	if stats.inProgress > 0 {
		numSections++
	}
	if stats.skipped > 0 || stats.neutral > 0 {
		numSections++
	}
	if stats.succeeded > 0 {
		numSections++
	}
	// subtract number of spacers
	w -= numSections - 1
	if w < 0 {
		w = 0
	}

	sections := make([]string, 0)
	if stats.failed > 0 {
		failWidth := int(math.Floor((float64(stats.failed) / total) * float64(w)))
		sections = append(sections, lipgloss.NewStyle().Width(failWidth).Foreground(
			m.ctx.Theme.ErrorText).Height(1).Render(strings.Repeat("▃", failWidth)))
	}
	if stats.awaitingApproval > 0 {
		awWidth := int(math.Floor((float64(stats.awaitingApproval) / total) * float64(w)))
		sections = append(sections, lipgloss.NewStyle().Width(awWidth).Foreground(
			m.ctx.Theme.WarningText).Height(1).Render(strings.Repeat("▃", awWidth)))
	}
	if stats.inProgress > 0 {
		ipWidth := int(math.Floor((float64(stats.inProgress) / total) * float64(w)))
		sections = append(sections, lipgloss.NewStyle().Width(ipWidth).Foreground(
			m.ctx.Theme.WarningText).Height(1).Render(strings.Repeat("▃", ipWidth)))
	}
	if stats.skipped > 0 || stats.neutral > 0 {
		skipWidth := int(math.Floor((float64(stats.skipped+stats.neutral) / total) * float64(w)))
		sections = append(sections, lipgloss.NewStyle().Width(skipWidth).Foreground(
			m.ctx.Theme.FaintText).Height(1).Render(strings.Repeat("▃", skipWidth)))
	}
	if stats.succeeded > 0 {
		succWidth := int(math.Floor((float64(stats.succeeded) / total) * float64(w)))
		sections = append(sections, lipgloss.NewStyle().Width(succWidth).Foreground(
			m.ctx.Theme.SuccessText).Height(1).Render(strings.Repeat("▃", succWidth)))
	}

	return strings.Join(sections, " ")
}

func renderCheckRunName(checkRun data.CheckRun) string {
	var parts []string
	creator := strings.TrimSpace(string(checkRun.CheckSuite.Creator.Login))
	if creator != "" {
		parts = append(parts, creator)
	}

	workflow := strings.TrimSpace(string(checkRun.CheckSuite.WorkflowRun.Workflow.Name))
	if workflow != "" {
		parts = append(parts, workflow)
	}

	name := strings.TrimSpace(string(checkRun.Name))
	if name != "" {
		parts = append(parts, name)
	}

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		strings.Join(parts, "/"),
	)
}

type CheckCategory int

const (
	CheckWaiting CheckCategory = iota
	CheckFailure
	CheckSuccess
)

func (m *Model) renderCheckRunConclusion(checkRun data.CheckRun) (CheckCategory, string) {
	if ghchecks.IsStatusWaiting(string(checkRun.Status)) {
		return CheckWaiting, m.ctx.Styles.Common.WaitingGlyph
	}

	if ghchecks.IsConclusionAFailure(string(checkRun.Conclusion)) {
		return CheckFailure, m.ctx.Styles.Common.FailureGlyph
	}

	return CheckSuccess, m.ctx.Styles.Common.SuccessGlyph
}

func (m *Model) renderStatusContextConclusion(
	statusContext data.StatusContext,
) (CheckCategory, string) {
	conclusionStr := string(statusContext.State)
	if ghchecks.IsStatusWaiting(conclusionStr) {
		return CheckWaiting, m.ctx.Styles.Common.WaitingGlyph
	}

	if ghchecks.IsConclusionAFailure(conclusionStr) {
		return CheckFailure, m.ctx.Styles.Common.FailureGlyph
	}

	return CheckSuccess, m.ctx.Styles.Common.SuccessGlyph
}

func renderStatusContextName(statusContext data.StatusContext) string {
	var parts []string
	creator := strings.TrimSpace(string(statusContext.Creator.Login))
	if creator != "" {
		parts = append(parts, creator)
	}

	context := strings.TrimSpace(string(statusContext.Context))
	if context != "" && context != "/" {
		parts = append(parts, context)
	}
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		strings.Join(parts, "/"),
	)
}

// checkGroup is where a check is listed, in the order they're listed.
type checkGroup int

const (
	groupAwaitingApproval checkGroup = iota
	groupPending
	groupFailure
	groupWaiting
	groupRest
)

// checkItem is a check as listed in the checks tab.
type checkItem struct {
	// key identifies the check across refreshes, which may reorder them
	key   string
	group checkGroup
	glyph string
	name  string
	// when is when the check last changed, e.g. completed, if known
	when time.Time
	// details are shown below the check while it's expanded
	details []string
	// url is where the check is opened, if anywhere
	url string
}

// checkItems lists the checks of the PR's last commit in the order they're
// shown: awaiting approval, pending, failed, in progress and then the rest.
func (m *Model) checkItems() []checkItem {
	commits := m.pr.Data.Enriched.Commits.Nodes
	if len(commits) == 0 {
		return nil
	}
	lastCommit := commits[0]
	var items []checkItem

	// Collect check suites that don't appear in statusCheckRollup
	for _, suite := range lastCommit.Commit.CheckSuites.Nodes {
		workflowName := strings.TrimSpace(string(suite.WorkflowRun.Workflow.Name))
		if workflowName == "" {
			workflowName = strings.TrimSpace(string(suite.App.Name))
		}
		if workflowName == "" {
			workflowName = "Workflow"
		}

		if suite.Conclusion == "ACTION_REQUIRED" {
			// Workflow requires approval before it can run
			items = append(items, checkItem{
				key:   "suite/" + workflowName,
				group: groupAwaitingApproval,
				glyph: m.ctx.Styles.Common.ActionRequiredGlyph,
				name:  workflowName,
				details: []string{fmt.Sprintf("Awaiting approval to run. Press %s to approve all.",
					m.ctx.Styles.KeyHint.Render(keys.PRKeys.ApproveWorkflows.Keys()[0]))},
				url:  string(suite.Url),
				when: suite.CreatedAt,
			})
		} else if suite.Status == "QUEUED" || suite.Status == "PENDING" || suite.Status == "WAITING" {
			// Workflow is queued/pending (will run automatically)
			items = append(items, checkItem{
				key:     "suite/" + workflowName,
				group:   groupPending,
				glyph:   m.ctx.Styles.Common.WaitingGlyph,
				name:    workflowName,
				details: []string{humanizeState(string(suite.Status))},
				url:     string(suite.Url),
				when:    suite.CreatedAt,
			})
		}
	}

	// Build a set of reported check names to compare against required checks
	reportedChecks := make(map[string]bool)

	for _, node := range lastCommit.Commit.StatusCheckRollup.Contexts.Nodes {
		var item checkItem
		var category CheckCategory
		var checkName string
		switch node.Typename {
		case "CheckRun":
			checkRun := node.CheckRun
			category, item.glyph = m.renderCheckRunConclusion(checkRun)
			checkName = string(checkRun.Name)
			item.name = renderCheckRunName(checkRun)
			item.key = "run/" + item.name
			item.details = checkRunDetails(checkRun)
			item.when = checkRun.CompletedAt
			if item.when.IsZero() {
				item.when = checkRun.StartedAt
			}
			item.url = string(checkRun.DetailsUrl)
			if item.url == "" {
				item.url = string(checkRun.Url)
			}
		case "StatusContext":
			statusContext := node.StatusContext
			category, item.glyph = m.renderStatusContextConclusion(statusContext)
			checkName = string(statusContext.Context)
			item.name = renderStatusContextName(statusContext)
			item.key = "status/" + item.name
			item.details = statusContextDetails(statusContext)
			item.when = statusContext.CreatedAt
			item.url = string(statusContext.TargetUrl)
		}

		reportedChecks[checkName] = true

		switch category {
		case CheckWaiting:
			item.group = groupWaiting
		case CheckFailure:
			item.group = groupFailure
		default:
			item.group = groupRest
		}
		items = append(items, item)
	}

	// Check for required status checks that haven't been reported yet
	branchRules := m.pr.Data.Primary.Repository.BranchProtectionRules.Nodes
	if len(branchRules) > 0 {
		for _, requiredContext := range branchRules[0].RequiredStatusCheckContexts {
			contextName := string(requiredContext)
			if !reportedChecks[contextName] {
				// Required check hasn't been reported yet
				items = append(items, checkItem{
					key:     "required/" + contextName,
					group:   groupPending,
					glyph:   m.ctx.Styles.Common.WaitingGlyph,
					name:    contextName,
					details: []string{"Required, but hasn't been reported yet"},
				})
			}
		}
	}

	slices.SortStableFunc(items, func(a, b checkItem) int {
		return int(a.group) - int(b.group)
	})
	return items
}

// checkRunDetails describes a check run's state, how long it took and the
// title of its output, e.g. "Failure · took 2m13s". When it happened is shown
// on the check itself.
func checkRunDetails(checkRun data.CheckRun) []string {
	state := humanizeState(string(checkRun.Status))
	if checkRun.Status == "COMPLETED" && checkRun.Conclusion != "" {
		state = humanizeState(string(checkRun.Conclusion))
	}
	parts := []string{state}
	if !checkRun.CompletedAt.IsZero() && !checkRun.StartedAt.IsZero() {
		parts = append(parts,
			"took "+checkRun.CompletedAt.Sub(checkRun.StartedAt).Round(time.Second).String())
	}

	details := []string{strings.Join(parts, " · ")}
	if title := strings.TrimSpace(string(checkRun.Title)); title != "" {
		details = append(details, title)
	}
	return details
}

// statusContextDetails describes a commit status's state and its
// description.
func statusContextDetails(statusContext data.StatusContext) []string {
	details := []string{humanizeState(string(statusContext.State))}
	if description := strings.TrimSpace(string(statusContext.Description)); description != "" {
		details = append(details, description)
	}
	return details
}

// humanizeState turns an API state into words, e.g. "IN_PROGRESS" into
// "In progress".
func humanizeState(state string) string {
	if state == "" {
		return "Unknown"
	}
	s := strings.ToLower(strings.ReplaceAll(state, "_", " "))
	return strings.ToUpper(s[:1]) + s[1:]
}

// renderChecks renders the list of checks along with where each check starts,
// relative to the start of what's rendered, so they can be focused. The
// expanded check shows its details.
func (m *Model) renderChecks() (string, []common.CommentAnchor) {
	title := m.ctx.Styles.Common.MainTextStyle.MarginBottom(1).
		Underline(true).
		Render(" All Checks")

	commits := m.pr.Data.Enriched.Commits.Nodes
	if len(commits) == 0 {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			title,
			"Loading...",
		), nil
	}

	items := m.checkItems()
	if len(items) == 0 {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			title,
			lipgloss.NewStyle().
				Italic(true).
				PaddingLeft(2).
				Width(m.getIndentedContentWidth()).
				Render("No checks to display..."),
		), nil
	}

	counts := map[checkGroup]int{}
	for _, it := range items {
		counts[it.group]++
	}

	indented := lipgloss.NewStyle().PaddingLeft(2).Width(m.getIndentedContentWidth())
	parts := []string{title}
	line := lipgloss.Height(title)
	add := func(s string) {
		s = indented.Render(s)
		parts = append(parts, s)
		line += lipgloss.Height(s)
	}
	sectionHeader := func(s string) string {
		return lipgloss.NewStyle().
			Bold(true).
			Foreground(m.ctx.Theme.WarningText).
			Render(s)
	}

	anchors := make([]common.CommentAnchor, 0, len(items))
	for i, it := range items {
		// Awaiting approval and pending checks are listed first, under headers
		if i == 0 || it.group != items[i-1].group {
			if i > 0 && items[i-1].group <= groupPending {
				add("") // spacing
			}
			switch it.group {
			case groupAwaitingApproval:
				add(sectionHeader(fmt.Sprintf("Awaiting Approval (%d)", counts[it.group])))
			case groupPending:
				add(sectionHeader(fmt.Sprintf("Pending (%d)", counts[it.group])))
			}
		}

		idx := i
		anchors = append(anchors, common.CommentAnchor{Line: line, Body: it.name, Check: &idx})
		check, truncated := m.renderCheckTitle(it)
		if it.key == m.expandedCheck {
			if truncated {
				// Show the whole of the name cut short on the check's line
				it.details = append([]string{it.name}, it.details...)
			}
			check = lipgloss.JoinVertical(lipgloss.Left, check, m.renderCheckDetails(it))
		}
		add(check)
	}

	return lipgloss.JoinVertical(lipgloss.Left, parts...), anchors
}

// renderCheckTitle renders a check's line: its state and name, with when it
// last changed at the right edge, e.g. "5m ago". A name too long for the line
// is cut short, which it reports.
func (m *Model) renderCheckTitle(it checkItem) (string, bool) {
	width := m.getIndentedContentWidth() - 2
	left := lipgloss.JoinHorizontal(lipgloss.Top, it.glyph, " ", it.name)
	right := ""
	if !it.when.IsZero() {
		right = m.ctx.Styles.Common.FaintTextStyle.Render(utils.TimeElapsed(it.when) + " ago")
	}
	space := width - lipgloss.Width(right)
	if right != "" {
		space-- // keep a space before it
	}
	truncated := lipgloss.Width(left) > space
	if truncated {
		left = ansi.Truncate(left, max(0, space), constants.Ellipsis)
	}
	if right == "" {
		return left, truncated
	}
	pad := strings.Repeat(" ", max(1, width-lipgloss.Width(left)-lipgloss.Width(right)))
	return left + pad + right, truncated
}

// renderCheckDetails renders an expanded check's details below it, along a
// line down its side, ending with where it's opened.
func (m *Model) renderCheckDetails(it checkItem) string {
	fainter := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintBorder)
	prefix := fainter.Render("│ ")
	width := max(1, m.getIndentedContentWidth()-2-lipgloss.Width(prefix))

	lines := make([]string, 0, len(it.details)+1)
	for _, d := range it.details {
		wrapped := m.ctx.Styles.Common.MainTextStyle.Width(width).Render(d)
		lines = append(lines, strings.Split(wrapped, "\n")...)
	}
	if it.url != "" {
		lines = append(lines, m.ctx.Styles.Common.FaintTextStyle.Render(
			ansi.Truncate(it.url, width, constants.Ellipsis)))
	}
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// SetFocusedCheck expands the check at the given index, as listed, to show
// its details, collapsing any other. It reports whether that changed
// anything.
func (m *Model) SetFocusedCheck(i int) bool {
	key := ""
	if m.hasData() {
		if items := m.checkItems(); i >= 0 && i < len(items) {
			key = items[i].key
		}
	}
	if key == m.expandedCheck {
		return false
	}
	m.expandedCheck = key
	return true
}

// ExpandedCheckIndex returns the index of the check shown with its details,
// as listed, or -1 when none is.
func (m Model) ExpandedCheckIndex() int {
	if m.expandedCheck == "" || !m.hasData() {
		return -1
	}
	for i, it := range m.checkItems() {
		if it.key == m.expandedCheck {
			return i
		}
	}
	return -1
}

// CheckUrl returns where the check at the given index, as listed, is opened,
// or "" when it can't be.
func (m Model) CheckUrl(i int) string {
	if !m.hasData() {
		return ""
	}
	if items := m.checkItems(); i >= 0 && i < len(items) {
		return items[i].url
	}
	return ""
}

// IsChecksTab reports whether the checks tab is selected.
func (m Model) IsChecksTab() bool {
	return m.carousel.Cursor() == checksTab
}

type checksStats struct {
	succeeded        int
	neutral          int
	failed           int
	skipped          int
	inProgress       int
	awaitingApproval int
}

func (m *Model) getStatusCheckRollupStats(rollup data.StatusCheckRollupStats) checksStats {
	var res checksStats
	allChecks := make([]data.ContextCountByState, 0)
	allChecks = append(allChecks, rollup.Contexts.CheckRunCountsByState...)
	allChecks = append(allChecks, rollup.Contexts.StatusContextCountsByState...)

	for _, count := range allChecks {
		state := string(count.State)
		if ghchecks.IsStatusWaiting(state) {
			res.inProgress += int(count.Count)
		} else if ghchecks.IsConclusionAFailure(state) {
			res.failed += int(count.Count)
		} else if ghchecks.IsConclusionASkip(state) {
			res.skipped += int(count.Count)
		} else if ghchecks.IsConclusionNeutral(state) {
			res.neutral += int(count.Count)
		} else if ghchecks.IsConclusionASuccess(state) {
			res.succeeded += int(count.Count)
		}
	}

	return res
}

func (m *Model) getChecksStats() checksStats {
	var res checksStats
	commits := m.pr.Data.Enriched.Commits.Nodes
	if len(commits) == 0 {
		return res
	}

	lastCommit := commits[0]
	allChecks := make([]data.ContextCountByState, 0)
	allChecks = append(
		allChecks,
		lastCommit.Commit.StatusCheckRollup.Contexts.CheckRunCountsByState...)
	allChecks = append(
		allChecks,
		lastCommit.Commit.StatusCheckRollup.Contexts.StatusContextCountsByState...)

	for _, count := range allChecks {
		state := string(count.State)
		if ghchecks.IsStatusWaiting(state) {
			res.inProgress += int(count.Count)
		} else if ghchecks.IsConclusionAFailure(state) {
			res.failed += int(count.Count)
		} else if ghchecks.IsConclusionASkip(state) {
			res.skipped += int(count.Count)
		} else if ghchecks.IsConclusionNeutral(state) {
			res.neutral += int(count.Count)
		} else if ghchecks.IsConclusionASuccess(state) {
			res.succeeded += int(count.Count)
		}
	}

	// Count check suites that don't appear in statusCheckRollup
	for _, suite := range lastCommit.Commit.CheckSuites.Nodes {
		if suite.Conclusion == "ACTION_REQUIRED" {
			res.awaitingApproval++
		} else if suite.Status == "QUEUED" || suite.Status == "PENDING" || suite.Status == "WAITING" {
			res.inProgress++
		}
	}

	return res
}

func (m *Model) numRequestedReviewOwners() int {
	numOwners := 0

	for _, node := range m.pr.Data.Enriched.ReviewRequests.Nodes {
		if node.AsCodeOwner {
			numOwners++
		}
	}

	return numOwners
}
