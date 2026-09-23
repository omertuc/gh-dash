package common

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// RenderPending grays out rendered, e.g. a comment that's still being posted.
// Its own colors are dropped, since they'd override the gray.
func RenderPending(rendered string, faint color.Color) string {
	style := lipgloss.NewStyle().Foreground(faint)
	lines := strings.Split(ansi.Strip(rendered), "\n")
	for i, line := range lines {
		lines[i] = style.Render(line)
	}
	return strings.Join(lines, "\n")
}

// CommentAnchor locates a comment within rendered preview content.
type CommentAnchor struct {
	// Line is the line of the rendered content the comment starts on
	Line   int
	Author string
	// Body is the comment's markdown
	Body string
	// Commit is set when the anchor is a commit rather than a comment: the
	// index of the commit in the PR's commits
	Commit *int
	// Check is set when the anchor is a check rather than a comment: the
	// index of the check in the PR's checks as they're listed
	Check *int
}

// IsComment reports whether the anchor is a comment, rather than e.g. a
// commit or a check.
func (a CommentAnchor) IsComment() bool {
	return a.Commit == nil && a.Check == nil
}

// QuoteReply quotes a comment's markdown the way GitHub's "Quote reply"
// does, ready for a reply to be typed below it.
func QuoteReply(body string) string {
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if body == "" {
		return ""
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if line == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + line
		}
	}
	return strings.Join(lines, "\n") + "\n\n"
}

// AppendParagraph adds text to the end of existing text as a new paragraph.
func AppendParagraph(existing, text string) string {
	existing = strings.TrimRight(existing, "\n")
	if existing == "" {
		return text
	}
	return existing + "\n\n" + text
}
