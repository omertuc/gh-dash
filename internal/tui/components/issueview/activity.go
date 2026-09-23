package issueview

import (
	"sort"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/markdown"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

type RenderedActivity struct {
	UpdatedAt      time.Time
	RenderedString string
	Author         string
	Body           string
}

func (m *Model) renderActivity() string {
	activity, _ := m.renderActivityWithAnchors()
	return activity
}

// renderActivityWithAnchors renders the comments along with where each one
// starts.
func (m *Model) renderActivityWithAnchors() (string, []common.CommentAnchor) {
	width := m.getIndentedContentWidth() - 2
	markdownRenderer := markdown.GetMarkdownRenderer(width, m.ctx)

	var activity []RenderedActivity
	for _, comment := range m.issue.Data.Comments.Nodes {
		renderedComment, err := m.renderComment(comment, markdownRenderer)
		if err != nil {
			continue
		}
		activity = append(activity, RenderedActivity{
			UpdatedAt:      comment.UpdatedAt,
			RenderedString: renderedComment,
			Author:         comment.Author.Login,
			Body:           comment.Body,
		})
	}

	sort.Slice(activity, func(i, j int) bool {
		return activity[i].UpdatedAt.Before(activity[j].UpdatedAt)
	})

	body := ""
	bodyStyle := lipgloss.NewStyle().PaddingLeft(2)
	title := m.renderActivitiesTitle()
	var anchors []common.CommentAnchor
	if len(activity) == 0 {
		body = renderEmptyState()
	} else {
		line := lipgloss.Height(title)
		var renderedActivities []string
		for _, activity := range activity {
			renderedActivities = append(renderedActivities, activity.RenderedString)
			anchors = append(anchors, common.CommentAnchor{
				Line:   line,
				Author: activity.Author,
				Body:   activity.Body,
			})
			line += lipgloss.Height(activity.RenderedString)
		}
		body = lipgloss.JoinVertical(lipgloss.Left, renderedActivities...)
	}

	return lipgloss.JoinVertical(lipgloss.Left, title, bodyStyle.Render(body)), anchors
}

func (m Model) renderActivitiesTitle() string {
	return m.ctx.Styles.Common.MainTextStyle.
		MarginBottom(1).
		Underline(true).
		Render(" Comments")
}

func renderEmptyState() string {
	return lipgloss.NewStyle().Italic(true).Render("No comments...")
}

func (m *Model) renderComment(
	comment data.IssueComment,
	markdownRenderer markdown.Renderer,
) (string, error) {
	width := m.getIndentedContentWidth() - 2
	header := lipgloss.NewStyle().
		Width(width).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(m.ctx.Theme.FaintBorder).Render(
		lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.ctx.Styles.Common.MainTextStyle.Render(comment.Author.Login),
			" ",
			lipgloss.NewStyle().
				Foreground(m.ctx.Theme.FaintText).
				Render(utils.TimeElapsed(comment.UpdatedAt)),
		))

	body := lineCleanupRegex.ReplaceAllString(comment.Body, "")
	body, err := markdownRenderer.Render(body)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		body,
	), err
}
