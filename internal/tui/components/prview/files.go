package prview

import (
	"fmt"

	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

func (m *Model) renderChangesOverview() string {
	w := m.getIndentedContentWidth() - 2
	changes := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(m.ctx.Theme.FaintBorder).
		Width(w).
		Padding(1)

	commits := lipgloss.NewStyle().
		Width(w).
		Padding(1)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder(), true).
		BorderForeground(m.ctx.Theme.FaintBorder).
		Width(m.getIndentedContentWidth())

	time := lipgloss.NewStyle().Render(utils.TimeElapsed(m.pr.Data.Primary.UpdatedAt))
	return box.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			changes.Render(
				lipgloss.JoinHorizontal(lipgloss.Top,
					lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Render(" "),
					fmt.Sprintf("%d files changed", m.pr.Data.Enriched.Files.TotalCount),
					" ",
					m.pr.RenderLines(false)),
			),
			commits.Render(
				lipgloss.JoinHorizontal(
					lipgloss.Top,
					lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Render(" "),
					fmt.Sprintf("%d commits", m.pr.Data.Enriched.Commits.TotalCount),
					" ",
					lipgloss.NewStyle().
						Foreground(m.ctx.Theme.FaintText).
						Render(fmt.Sprintf("%s ago", time)),
				),
			),
		),
	)
}

func (m *Model) renderChangedFiles() string {
	if m.commitFiles != nil {
		return m.renderCommitFiles()
	}
	return m.renderFiles(m.pr.Data.Enriched.Files.Nodes)
}

// renderCommitFiles renders the files changed by the commit the files tab
// was narrowed to.
func (m *Model) renderCommitFiles() string {
	faint := m.ctx.Styles.Common.FaintTextStyle
	cf := m.commitFiles
	switch {
	case cf.loading:
		return lipgloss.JoinHorizontal(lipgloss.Top,
			m.ctx.Styles.Common.WaitingGlyph, " ",
			faint.Render(fmt.Sprintf("Loading files of %s...", cf.abbreviatedOid)))
	case cf.err != nil:
		return lipgloss.JoinHorizontal(lipgloss.Top,
			m.ctx.Styles.Common.FailureGlyph, " ",
			faint.Render(fmt.Sprintf("Failed loading files of %s: %v", cf.abbreviatedOid, cf.err)))
	}

	additions, deletions := 0, 0
	for _, f := range cf.files {
		additions += f.Additions
		deletions += f.Deletions
	}
	heading := m.ctx.Styles.Common.MainTextStyle.MarginBottom(1).Render(lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Underline(true).Render(
			fmt.Sprintf("%d files changed in %s", len(cf.files), cf.abbreviatedOid)),
		" ",
		m.renderDiffStats(additions, deletions),
	))
	return lipgloss.JoinVertical(lipgloss.Left, heading, m.renderFiles(cf.files))
}

func (m *Model) renderFiles(changed []data.ChangedFile) string {
	files := make([]string, 0, len(changed))
	for _, file := range changed {
		files = append(files, m.renderFile(file))
	}

	return lipgloss.JoinVertical(lipgloss.Left, files...)
}

func (m *Model) renderFile(file data.ChangedFile) string {
	icon := m.renderChangeTypeIcon(file.ChangeType)
	additions := lipgloss.NewStyle().
		Foreground(m.ctx.Theme.SuccessText).
		Width(5).
		Render(fmt.Sprintf("+%d", file.Additions))
	deletions := lipgloss.NewStyle().
		Foreground(m.ctx.Theme.ErrorText).
		Width(5).
		Render(fmt.Sprintf("-%d", file.Deletions))
	prefix := lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.JoinHorizontal(lipgloss.Top, additions, deletions),
		" ",
		icon,
		" ")

	path := file.Path
	remaining := m.getIndentedContentWidth() - lipgloss.Width(prefix)
	if len(path) > remaining {
		path = lipgloss.JoinVertical(lipgloss.Left, path[0:remaining], " "+path[remaining:])
	}

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		prefix,
		path,
	)
}

func (m *Model) renderChangeTypeIcon(changeType string) string {
	switch changeType {
	case "ADDED":
		return lipgloss.NewStyle().Foreground(m.ctx.Theme.SuccessText).Render("")
	case "DELETED":
		return lipgloss.NewStyle().Foreground(m.ctx.Theme.ErrorText).Render("")
	case "RENAMED":
		return lipgloss.NewStyle().Foreground(m.ctx.Theme.WarningText).Render("")
	case "COPIED":
		return lipgloss.NewStyle().Foreground(m.ctx.Theme.WarningText).Render("")
	case "MODIFIED":
		return lipgloss.NewStyle().Foreground(m.ctx.Theme.WarningText).Render("")
	case "CHANGED":
		return lipgloss.NewStyle().Foreground(m.ctx.Theme.WarningText).Render("")
	default:
		return ""
	}
}
