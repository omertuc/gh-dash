package issueview

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/markdown"
)

func TestMarkdownPrerendererRendersWhatThePreviewShows(t *testing.T) {
	const width = 100
	ctx := newTestContext(t)
	m := NewModel(ctx)
	m.SetWidth(width)
	unique := t.Name() + time.Now().String()
	issue := &data.IssueData{
		Title: "An issue",
		Body:  "The **description** <!-- hidden -->\n\n" + unique,
	}
	issue.Comments.Nodes = []data.IssueComment{
		{Body: "A `comment` " + unique, UpdatedAt: time.Now()},
	}
	m.SetRow(issue)

	MarkdownPrerenderer(ctx, issue, width)()
	prerendered := markdown.RenderCacheSizeForTesting()

	m.ViewBodyWithAnchors()
	require.Equal(t, prerendered, markdown.RenderCacheSizeForTesting(),
		"showing the issue should only render what was rendered ahead of time")
}
