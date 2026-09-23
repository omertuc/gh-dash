package prview

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

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

func TestTabsHintShownWhenItFits(t *testing.T) {
	m := newTestModelWithWidth(t, &data.PullRequestData{Title: "hint"}, nil, nil, 0)

	m.SetWidth(120)
	header := ansi.Strip(m.ViewHeader())
	if !strings.Contains(header, "]→ [←") {
		t.Fatalf("expected tabs hint in wide header:\n%s", header)
	}
	for _, line := range strings.Split(header, "\n") {
		if strings.Contains(line, "]→ [←") && ansi.StringWidth(line) > 120 {
			t.Fatalf("tab bar is wider than the preview: %d", ansi.StringWidth(line))
		}
	}

	m.SetWidth(m.carousel.ItemsWidth() + 3)
	if strings.Contains(ansi.Strip(m.ViewHeader()), "]→ [←") {
		t.Fatal("hint should be dropped when it doesn't fit next to all tabs")
	}
}
