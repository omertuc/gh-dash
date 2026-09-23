package prview

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

const (
	forcePushIcon = "" // nf-oct-repo_push
	referenceIcon = "" // nf-oct-cross_reference
	assigneeIcon  = "" // nf-oct-person
	renameIcon    = "" // nf-oct-pencil
	milestoneIcon = "" // nf-oct-milestone
	branchIcon    = "" // nf-oct-git_branch
	lockIcon      = "" // nf-oct-lock
	unlockIcon    = "" // nf-oct-unlock
	dismissedIcon = "" // nf-oct-x
	readyIcon     = "" // nf-oct-eye
)

// styled is an already styled argument to eventText, which is kept as is
type styled string

// renderTimelineItems renders a run of consecutive timeline events, one per
// line, like GitHub shows them between comments. Consecutive commits are
// grouped under one "added N commits" line.
func (m *Model) renderTimelineItems(items []data.TimelineItem) string {
	var lines []string
	for i := 0; i < len(items); i++ {
		if items[i].Typename != "PullRequestCommit" {
			if line := m.renderTimelineEvent(items[i]); line != "" {
				lines = append(lines, line)
			}
			continue
		}
		j := i + 1
		for j < len(items) && items[j].Typename == "PullRequestCommit" {
			j++
		}
		lines = append(lines, m.renderTimelineCommits(items[i:j])...)
		i = j - 1
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func (m *Model) renderTimelineCommits(commits []data.TimelineItem) []string {
	faint := m.ctx.Styles.Common.FaintTextStyle
	main := m.ctx.Styles.Common.MainTextStyle

	first := commits[0].PullRequestCommit.Commit
	author := first.Author.User.Login
	if author == "" {
		author = first.Author.Name
	}
	noun := "commit"
	if len(commits) > 1 {
		noun = "commits"
	}
	lines := []string{m.renderEventLine(
		faint.Render(constants.CommitIcon),
		author,
		m.eventText("added %d "+noun, len(commits)),
		first.CommittedDate,
	)}
	for _, c := range commits {
		commit := c.PullRequestCommit.Commit
		sha := faint.Render(" " + commit.AbbreviatedOid)
		headline := ansi.Truncate(
			"  "+faint.Render(constants.VerticalCommitIcon)+" "+main.Render(commit.MessageHeadline),
			max(0, m.getIndentedContentWidth()-lipgloss.Width(sha)),
			constants.Ellipsis,
		)
		lines = append(lines, headline+sha)
	}
	return lines
}

// renderTimelineEvent renders an event as a single line, or "" for event
// types it doesn't know
func (m *Model) renderTimelineEvent(item data.TimelineItem) string {
	faint := m.ctx.Styles.Common.FaintTextStyle
	labelName := func(label data.Label) styled {
		return styled(lipgloss.NewStyle().Foreground(lipgloss.Color("#" + label.Color)).Render(label.Name))
	}
	pr := m.pr.Data.Enriched

	var icon, text string
	iconColor := m.ctx.Theme.FaintText
	switch item.Typename {
	case "HeadRefForcePushedEvent":
		e := item.HeadRefForcePushedEvent
		icon = forcePushIcon
		text = m.eventText("force-pushed %s from %s to %s",
			pr.HeadRefName, e.BeforeCommit.AbbreviatedOid, e.AfterCommit.AbbreviatedOid)
	case "BaseRefForcePushedEvent":
		e := item.BaseRefForcePushedEvent
		icon = forcePushIcon
		text = m.eventText("force-pushed %s from %s to %s",
			pr.BaseRefName, e.BeforeCommit.AbbreviatedOid, e.AfterCommit.AbbreviatedOid)
	case "BaseRefChangedEvent":
		e := item.BaseRefChangedEvent
		icon = branchIcon
		text = m.eventText("changed the base branch from %s to %s",
			e.PreviousRefName, e.CurrentRefName)
	case "CrossReferencedEvent":
		e := item.CrossReferencedEvent
		source := e.Source.Issue
		if e.Source.Typename == "PullRequest" {
			source = e.Source.PullRequest
		}
		ref := fmt.Sprintf("#%d", source.Number)
		if source.Repository.NameWithOwner != pr.Repository.NameWithOwner {
			ref = source.Repository.NameWithOwner + ref
		}
		format := "mentioned this in %s %s %s"
		if e.WillCloseTarget {
			format = "linked %s %s %s that will close this"
		}
		icon = referenceIcon
		text = m.eventText(format, m.subjectStateIcon(e.Source.Typename, source.State), ref, source.Title)
	case "ReferencedEvent":
		e := item.ReferencedEvent
		icon = referenceIcon
		text = m.eventText("referenced this in commit %s %s",
			e.Commit.AbbreviatedOid, e.Commit.MessageHeadline)
	case "LabeledEvent":
		label := item.LabeledEvent.Label
		icon = constants.LabelsIcon
		text = m.eventText("added the %s label", labelName(label))
	case "UnlabeledEvent":
		label := item.UnlabeledEvent.Label
		icon = constants.LabelsIcon
		text = m.eventText("removed the %s label", labelName(label))
	case "AssignedEvent":
		icon = assigneeIcon
		text = m.assigneeText("assigned", item.AssignedEvent.Actor, item.AssignedEvent.Assignee)
	case "UnassignedEvent":
		icon = assigneeIcon
		text = m.assigneeText("unassigned", item.UnassignedEvent.Actor, item.UnassignedEvent.Assignee)
	case "ReviewRequestedEvent":
		icon = constants.CodeReviewIcon
		text = m.eventText("requested a review from %s",
			reviewerName(item.ReviewRequestedEvent.RequestedReviewer))
	case "ReviewRequestRemovedEvent":
		icon = constants.CodeReviewIcon
		text = m.eventText("removed the review request for %s",
			reviewerName(item.ReviewRequestRemovedEvent.RequestedReviewer))
	case "ReviewDismissedEvent":
		icon = dismissedIcon
		text = m.eventText("dismissed %s's review", item.ReviewDismissedEvent.Review.Author.Login)
	case "RenamedTitleEvent":
		e := item.RenamedTitleEvent
		icon = renameIcon
		text = m.eventText("changed the title %s %s",
			styled(faint.Strikethrough(true).Render(e.PreviousTitle)), e.CurrentTitle)
	case "MilestonedEvent":
		icon = milestoneIcon
		text = m.eventText("added this to the %s milestone", item.MilestonedEvent.MilestoneTitle)
	case "DemilestonedEvent":
		icon = milestoneIcon
		text = m.eventText("removed this from the %s milestone", item.DemilestonedEvent.MilestoneTitle)
	case "MergedEvent":
		e := item.MergedEvent
		icon, iconColor = constants.MergedIcon, m.ctx.Theme.SuccessText
		text = m.eventText("merged commit %s into %s", e.Commit.AbbreviatedOid, e.MergeRefName)
	case "ClosedEvent":
		icon, iconColor = constants.ClosedIcon, m.ctx.Theme.ErrorText
		text = m.eventText("closed this")
	case "ReopenedEvent":
		icon, iconColor = constants.OpenIcon, m.ctx.Theme.SuccessText
		text = m.eventText("reopened this")
	case "ReadyForReviewEvent":
		icon = readyIcon
		text = m.eventText("marked this as ready for review")
	case "ConvertToDraftEvent":
		icon = constants.DraftIcon
		text = m.eventText("marked this as a draft")
	case "HeadRefDeletedEvent":
		icon = branchIcon
		text = m.eventText("deleted the %s branch", pr.HeadRefName)
	case "HeadRefRestoredEvent":
		icon = branchIcon
		text = m.eventText("restored the %s branch", pr.HeadRefName)
	case "AutoMergeEnabledEvent":
		icon = constants.MergedIcon
		text = m.eventText("enabled auto-merge")
	case "AutoMergeDisabledEvent":
		icon = constants.MergedIcon
		text = m.eventText("disabled auto-merge")
	case "AddedToMergeQueueEvent":
		icon = constants.MergeQueueIcon
		text = m.eventText("added this to the merge queue")
	case "RemovedFromMergeQueueEvent":
		icon = constants.MergeQueueIcon
		text = m.eventText("removed this from the merge queue")
	case "LockedEvent":
		icon = lockIcon
		text = m.eventText("locked the conversation")
	case "UnlockedEvent":
		icon = unlockIcon
		text = m.eventText("unlocked the conversation")
	default:
		return ""
	}
	e := item.Event()
	return m.renderEventLine(lipgloss.NewStyle().Foreground(iconColor).Render(icon), e.Actor.Login, text, e.CreatedAt)
}

// renderEventLine renders "icon actor text · time", truncated to the width
// of the tab
func (m *Model) renderEventLine(icon, actor, text string, at time.Time) string {
	faint := m.ctx.Styles.Common.FaintTextStyle
	line := icon + " "
	if actor != "" {
		line += m.ctx.Styles.Common.MainTextStyle.Bold(true).Render(actor) + " "
	}
	line += text
	if !at.IsZero() {
		line += faint.Render(" · " + utils.TimeElapsed(at))
	}
	return ansi.Truncate(line, m.getIndentedContentWidth(), constants.Ellipsis)
}

// eventText formats an event's description, fading its words and
// highlighting its arguments, e.g. branch names and commits
func (m *Model) eventText(format string, args ...any) string {
	faint := m.ctx.Styles.Common.FaintTextStyle
	main := m.ctx.Styles.Common.MainTextStyle
	parts := strings.Split(format, "%")
	var b strings.Builder
	b.WriteString(faint.Render(parts[0]))
	for i, part := range parts[1:] {
		// Each part starts with its verb, e.g. "s" or "d"
		if i < len(args) {
			switch arg := args[i].(type) {
			case styled:
				b.WriteString(string(arg))
			default:
				b.WriteString(main.Render(fmt.Sprintf("%"+part[:1], arg)))
			}
		}
		b.WriteString(faint.Render(part[1:]))
	}
	return b.String()
}

func (m *Model) assigneeText(verb string, actor data.TimelineActor, assignee data.TimelineAssignee) string {
	if assignee.Actor.Login == actor.Login {
		return m.eventText(verb + " themselves")
	}
	return m.eventText(verb+" %s", assignee.Actor.Login)
}

// subjectStateIcon shows whether the issue or PR that references this one is
// open, closed or merged
func (m *Model) subjectStateIcon(typename, state string) styled {
	icon, color := constants.OpenIcon, m.ctx.Theme.SuccessText
	switch {
	case state == "MERGED":
		icon, color = constants.MergedIcon, m.ctx.Theme.SecondaryText
	case state == "CLOSED" && typename == "Issue":
		icon, color = constants.SuccessIcon, m.ctx.Theme.SecondaryText
	case state == "CLOSED":
		icon, color = constants.ClosedIcon, m.ctx.Theme.ErrorText
	}
	return styled(lipgloss.NewStyle().Foreground(color).Render(icon))
}

func reviewerName(r data.TimelineReviewer) string {
	if r.Typename == "Team" {
		return r.Team.Name
	}
	return r.Actor.Login
}
