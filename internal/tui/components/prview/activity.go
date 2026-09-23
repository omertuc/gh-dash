package prview

import (
	"fmt"
	"sort"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/markdown"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

type RenderedActivity struct {
	CreatedAt      time.Time
	RenderedString string
	Author         string
	Body           string
	// Event is set for timeline events, e.g. commits and force pushes, which
	// are rendered along with the events next to them
	Event *data.TimelineItem
}

// activityTime is when a comment or review was posted. Comments added locally
// after posting them only have an update time.
func activityTime(createdAt, updatedAt time.Time) time.Time {
	if createdAt.IsZero() {
		return updatedAt
	}
	return createdAt
}

func (m *Model) renderActivity() string {
	activity, _ := m.renderActivityWithAnchors()
	return activity
}

// renderActivityWithAnchors renders the activity tab along with where each
// comment or review starts. Timeline events, e.g. pushed commits, show
// between them in the order they happened.
func (m *Model) renderActivityWithAnchors() (string, []common.CommentAnchor) {
	width := m.getIndentedContentWidth()
	markdownRenderer := markdown.GetMarkdownRenderer(width, m.ctx)
	bodyStyle := lipgloss.NewStyle()

	var activities []RenderedActivity
	var comments []comment

	if !m.pr.Data.IsEnriched {
		return bodyStyle.Render("Loading..."), nil
	}

	for _, review := range m.pr.Data.Enriched.ReviewThreads.Nodes {
		path := review.Path
		line := review.Line
		for _, c := range review.Comments.Nodes {
			comments = append(comments, comment{
				Author:    c.Author.Login,
				Body:      c.Body,
				CreatedAt: activityTime(c.CreatedAt, c.UpdatedAt),
				UpdatedAt: c.UpdatedAt,
				Path:      &path,
				Line:      &line,
			})
		}
	}

	for _, c := range m.pr.Data.Enriched.Comments.Nodes {
		comments = append(comments, comment{
			Author:    c.Author.Login,
			Body:      c.Body,
			CreatedAt: activityTime(c.CreatedAt, c.UpdatedAt),
			UpdatedAt: c.UpdatedAt,
		})
	}

	for _, comment := range comments {
		renderedComment, err := m.renderComment(comment, markdownRenderer)
		if err != nil {
			continue
		}
		activities = append(activities, RenderedActivity{
			CreatedAt:      comment.CreatedAt,
			RenderedString: renderedComment,
			Author:         comment.Author,
			Body:           comment.Body,
		})
	}

	for _, review := range m.pr.Data.Enriched.Reviews.Nodes {
		renderedReview, err := m.renderReview(review, markdownRenderer)
		if err != nil {
			continue
		}
		activities = append(activities, RenderedActivity{
			CreatedAt:      activityTime(review.CreatedAt, review.UpdatedAt),
			RenderedString: renderedReview,
			Author:         review.Author.Login,
			Body:           review.Body,
		})
	}
	numComments := len(activities)

	for i := range m.pr.Data.Enriched.TimelineItems.Nodes {
		item := &m.pr.Data.Enriched.TimelineItems.Nodes[i]
		activities = append(activities, RenderedActivity{CreatedAt: item.Event().CreatedAt, Event: item})
	}

	sort.SliceStable(activities, func(i, j int) bool {
		return activities[i].CreatedAt.Before(activities[j].CreatedAt)
	})

	body := ""
	var anchors []common.CommentAnchor
	if len(activities) == 0 {
		body = renderEmptyState()
	} else {
		title := m.ctx.Styles.Common.MainTextStyle.MarginBottom(1).Underline(true).Render(
			fmt.Sprintf("%s  %d comments", constants.CommentsIcon, numComments))
		line := lipgloss.Height(title)
		var renderedActivities []string
		for i := 0; i < len(activities); i++ {
			activity := activities[i]
			if activity.Event != nil {
				var events []data.TimelineItem
				for ; i < len(activities) && activities[i].Event != nil; i++ {
					events = append(events, *activities[i].Event)
				}
				i--
				if rendered := m.renderTimelineItems(events); rendered != "" {
					renderedActivities = append(renderedActivities, rendered)
					line += lipgloss.Height(rendered)
				}
				continue
			}
			renderedActivities = append(renderedActivities, activity.RenderedString)
			anchors = append(anchors, common.CommentAnchor{
				Line:   line,
				Author: activity.Author,
				Body:   activity.Body,
			})
			line += lipgloss.Height(activity.RenderedString)
		}
		body = lipgloss.JoinVertical(lipgloss.Left, renderedActivities...)
		body = lipgloss.JoinVertical(lipgloss.Left, title, body)
	}

	return bodyStyle.Render(body), anchors
}

func renderEmptyState() string {
	return lipgloss.NewStyle().Italic(true).Render("No comments...")
}

type comment struct {
	Author    string
	CreatedAt time.Time
	UpdatedAt time.Time
	Body      string
	Path      *string
	Line      *int
}

func (m *Model) renderComment(
	comment comment,
	markdownRenderer markdown.Renderer,
) (string, error) {
	width := m.getIndentedContentWidth()
	authorAndTime := lipgloss.NewStyle().
		Width(width).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(m.ctx.Theme.FaintBorder).Render(
		lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.ctx.Styles.Common.MainTextStyle.Render(comment.Author),
			" ",
			lipgloss.NewStyle().
				Foreground(m.ctx.Theme.FaintText).
				Render(utils.TimeElapsed(comment.UpdatedAt)),
		))

	var header string
	if comment.Path != nil && comment.Line != nil {
		filePath := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Width(width).Render(
			fmt.Sprintf(
				"%s#l%d",
				*comment.Path,
				*comment.Line,
			),
		)
		header = lipgloss.JoinVertical(lipgloss.Left, authorAndTime, filePath, "")
	} else {
		header = authorAndTime
	}

	body := lineCleanupRegex.ReplaceAllString(comment.Body, "")
	body, err := markdownRenderer.Render(body)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		body,
	), err
}

func (m *Model) renderReview(
	review data.Review,
	markdownRenderer markdown.Renderer,
) (string, error) {
	header := m.renderReviewHeader(review)
	body, err := markdownRenderer.Render(review.Body)
	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		body,
	), err
}

func (m *Model) renderReviewHeader(review data.Review) string {
	return lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderReviewDecision(review.State),
		" ",
		m.ctx.Styles.Common.MainTextStyle.Render(review.Author.Login),
		" ",
		lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Render(
			"reviewed "+utils.TimeElapsed(review.UpdatedAt)),
	)
}

func (m *Model) renderReviewDecision(decision string) string {
	switch decision {
	case "PENDING":
		return m.ctx.Styles.Common.WaitingGlyph
	case "COMMENTED":
		return lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Render("󰈈")
	case "APPROVED":
		return m.ctx.Styles.Common.SuccessGlyph
	case "CHANGES_REQUESTED":
		return m.ctx.Styles.Common.FailureGlyph
	}

	return ""
}
