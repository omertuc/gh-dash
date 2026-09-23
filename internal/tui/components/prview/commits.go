package prview

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
	checks "github.com/dlvhdr/x/gh-checks"
)

// renderCommits renders the commits tab along with where each commit starts,
// so they can be focused. The expanded commit shows its full message.
func (m *Model) renderCommits() (string, []common.CommentAnchor) {
	main := m.ctx.Styles.Common.MainTextStyle
	faint := m.ctx.Styles.Common.FaintTextStyle
	fainter := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintBorder)

	if !m.pr.Data.IsEnriched {
		return lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.ctx.Styles.Common.WaitingGlyph,
			" ",
			faint.Render("Loading..."),
		), nil
	}

	commits := m.pr.Data.Enriched.AllCommits.Nodes
	heading := m.ctx.Styles.Common.MainTextStyle.MarginBottom(1).Underline(true).Render(
		fmt.Sprintf("%s  %d commits", constants.CommitIcon, len(commits)))

	parts := []string{heading}
	line := lipgloss.Height(heading)
	anchors := make([]common.CommentAnchor, 0, len(commits))
	for i, commit := range commits {
		commit := commit.Commit
		name := commit.Author.User.Login
		if name == "" {
			name = commit.Author.Name
		}
		headline, body := fullCommitMessage(commit.MessageHeadline, commit.MessageBody)
		left := fmt.Sprintf(
			"%s %s",
			faint.Render(constants.VerticalCommitIcon),
			main.Render(headline),
		)
		right := faint.Render(commit.AbbreviatedOid)
		wright := lipgloss.Width(right)
		fullLeft := left
		left = ansi.Truncate(left, max(0, m.getIndentedContentWidth()-wright-1), constants.Ellipsis)
		pad := fainter.Render(" " + strings.Repeat(constants.HorizontalLineIcon,
			max(1, m.getIndentedContentWidth()-lipgloss.Width(left)-wright)-1) + " ")

		title := lipgloss.JoinHorizontal(lipgloss.Top, left, pad, right)

		statsStr := ""
		if commit.StatusCheckRollup.Contexts.TotalCount > 0 {
			stats := m.getStatusCheckRollupStats(commit.StatusCheckRollup)
			statsStr = lipgloss.JoinHorizontal(lipgloss.Top,
				" ",
				faint.Render(constants.SmallDotIcon),
				" ",
				m.commitStateSign(commit.StatusCheckRollup.State),
				" ",
				faint.Render(fmt.Sprintf("%d/%d", stats.succeeded,
					commit.StatusCheckRollup.Contexts.TotalCount)),
			)
		}

		desc := lipgloss.JoinHorizontal(lipgloss.Top,
			fainter.Render("│ "),
			faint.Render(fmt.Sprintf("@%s", name)),
			faint.Render(" committed "),
			faint.Render(utils.TimeElapsed(commit.CommittedDate)),
			faint.Render(" ago"),
			statsStr,
			" ",
			faint.Render(constants.SmallDotIcon),
			" ",
			m.renderDiffStats(commit.Additions, commit.Deletions),
		)
		rendered := lipgloss.JoinVertical(lipgloss.Left, title, desc)

		if commit.Oid != "" && commit.Oid == m.expandedCommit {
			// The title was cut short, so show the whole of it with the body
			message := body
			if left != fullLeft {
				message = strings.TrimSpace(headline + "\n\n" + body)
			}
			if message != "" {
				rendered = lipgloss.JoinVertical(lipgloss.Left, rendered,
					m.renderCommitMessage(message))
			}
		}

		if i > 0 {
			parts = append(parts, fainter.Render("│"))
			line++
		}
		idx := i
		anchors = append(anchors, common.CommentAnchor{
			Line:   line,
			Author: name,
			Body:   strings.TrimSpace(headline + "\n\n" + body),
			Commit: &idx,
		})
		parts = append(parts, rendered)
		line += lipgloss.Height(rendered)
	}

	return lipgloss.JoinVertical(lipgloss.Left, parts...), anchors
}

// fullCommitMessage undoes GitHub cutting long headlines short, where the
// rest of the headline starts the body, e.g. "Fix the…" and "…bug".
func fullCommitMessage(headline, body string) (string, string) {
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	rest, ok := strings.CutPrefix(body, "…")
	if !strings.HasSuffix(headline, "…") || !ok {
		return headline, body
	}
	restOfHeadline, body, _ := strings.Cut(rest, "\n")
	return strings.TrimSuffix(headline, "…") + restOfHeadline, strings.TrimSpace(body)
}

// renderCommitMessage renders a commit's message below its title, wrapped and
// continuing the line down the side of the commit.
func (m *Model) renderCommitMessage(message string) string {
	fainter := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintBorder)
	prefix := fainter.Render("│ ")
	wrapped := m.ctx.Styles.Common.MainTextStyle.
		Width(max(1, m.getIndentedContentWidth()-lipgloss.Width(prefix))).
		Render(message)
	lines := append([]string{""}, strings.Split(wrapped, "\n")...)
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// renderDiffStats renders lines added and removed, e.g. "+12 -3".
func (m *Model) renderDiffStats(additions, deletions int) string {
	return lipgloss.NewStyle().Foreground(m.ctx.Theme.SuccessText).Render(fmt.Sprintf("+%d", additions)) +
		" " +
		lipgloss.NewStyle().Foreground(m.ctx.Theme.ErrorText).Render(fmt.Sprintf("-%d", deletions))
}

func (m *Model) commitStateSign(state checks.CommitState) string {
	switch state {
	case checks.CommitStateError, checks.CommitStateFailure:
		return m.ctx.Styles.Common.FailureGlyph
	case checks.CommitStatePending, checks.CommitStateExpected, checks.CommitStateUnknown:
		return m.ctx.Styles.Common.WaitingGlyph
	case checks.CommitStateSuccess:
		return m.ctx.Styles.Common.SuccessGlyph
	}

	return ""
}
