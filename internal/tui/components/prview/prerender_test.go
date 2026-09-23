package prview

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/markdown"
)

func TestMarkdownPrerendererRendersWhatThePreviewShows(t *testing.T) {
	const width = 100
	m := newTestModelWithWidth(t, &data.PullRequestData{Title: "A PR"}, nil, nil, width)
	unique := t.Name() + time.Now().String()
	pr := &m.pr.Data.Enriched
	pr.Body = "The **description** <!-- hidden -->\n\n| a | b |\n|---|---|\n| 1 | 2 |\n" + unique
	pr.Comments.Nodes = []data.Comment{{Body: "A `comment` " + unique, UpdatedAt: time.Now()}}
	pr.Reviews.Nodes = []data.Review{{Body: "A _review_ " + unique, State: "APPROVED"}}
	pr.ReviewThreads.Nodes = slices.Grow(pr.ReviewThreads.Nodes, 1)[:1]
	pr.ReviewThreads.Nodes[0].Path = "main.go"
	pr.ReviewThreads.Nodes[0].Comments.Nodes = []data.ReviewComment{
		{Body: "A thread comment " + unique},
	}

	MarkdownPrerenderer(m.ctx, pr, width)()
	prerendered := markdown.RenderCacheSizeForTesting()

	m.GoToFirstTab()
	m.ViewBodyWithAnchors()
	m.GoToActivityTab()
	m.ViewBodyWithAnchors()
	require.Equal(t, prerendered, markdown.RenderCacheSizeForTesting(),
		"showing the PR should only render what was rendered ahead of time")
}
