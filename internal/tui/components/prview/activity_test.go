package prview

import (
	"fmt"
	"testing"
	"time"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

const benchCommentBody = "Thanks for the PR! A few notes:\n\n" +
	"- The `renderRow` change looks good, but consider **caching** the result\n" +
	"- We should add a test for the empty state\n\n" +
	"```go\nfunc (m *Model) SyncViewPortContent() {\n\tfor i := range m.Rows {\n\t\t_ = i\n\t}\n}\n```\n\n" +
	"See [the docs](https://example.com/docs) for more details. > quoted text here"

// BenchmarkActivityView measures rendering the activity tab of a PR with many
// comments, which happens whenever the preview content changes.
func BenchmarkActivityView(b *testing.B) {
	m := newTestModelWithWidth(b, &data.PullRequestData{Title: "bench"}, nil, nil, 80)
	for i := range 30 {
		m.pr.Data.Enriched.Comments.Nodes = append(m.pr.Data.Enriched.Comments.Nodes, data.Comment{
			Body:      fmt.Sprintf("%s (%d)", benchCommentBody, i),
			UpdatedAt: time.Now(),
		})
	}
	m.GoToActivityTab()
	for b.Loop() {
		_ = m.View()
	}
}
