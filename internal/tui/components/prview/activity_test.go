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
// comments, which happens whenever the comments change.
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
		_, _ = m.renderActivityWithAnchors()
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

func TestActivityAnchorsPointAtCommentStarts(t *testing.T) {
	m := newTestModelWithWidth(t, &data.PullRequestData{Title: "anchors"}, nil, nil, 80)
	for i := range 3 {
		m.pr.Data.Enriched.Comments.Nodes = append(m.pr.Data.Enriched.Comments.Nodes, data.Comment{
			Body:      fmt.Sprintf("%s (%d)", benchCommentBody, i),
			UpdatedAt: time.Now().Add(time.Duration(i) * time.Minute),
		})
	}
	m.GoToActivityTab()

	body, anchors := m.ViewBodyWithAnchors()
	if len(anchors) != 3 {
		t.Fatalf("got %d anchors, want 3", len(anchors))
	}
	lines := strings.Split(ansi.Strip(body), "\n")
	for i, anchor := range anchors {
		// Each comment starts with the top border of its author box
		if !strings.HasPrefix(strings.TrimSpace(lines[anchor.Line]), "╭") {
			t.Errorf("anchor %d at line %d doesn't start a comment: %q", i, anchor.Line, lines[anchor.Line])
		}
	}

	m.GoToFirstTab()
	if _, anchors := m.ViewBodyWithAnchors(); anchors != nil {
		t.Errorf("overview tab should have no anchors, got %v", anchors)
	}
}

func TestActivityCacheRefreshesWhenCommentsChange(t *testing.T) {
	m := newTestModelWithWidth(t, &data.PullRequestData{Title: "cache"}, nil, nil, 80)
	m.pr.Data.Enriched.Comments.Nodes = []data.Comment{{Body: "original comment", UpdatedAt: time.Now()}}
	m.GoToActivityTab()

	first, _ := m.ViewBodyWithAnchors()
	again, _ := m.ViewBodyWithAnchors()
	if first != again {
		t.Fatal("rendering unchanged comments twice should give the same result")
	}

	// A fetch replaces the comments list
	m.pr.Data.Enriched.Comments.Nodes = []data.Comment{{Body: "a new comment", UpdatedAt: time.Now()}}
	updated, _ := m.ViewBodyWithAnchors()
	if !strings.Contains(ansi.Strip(updated), "a new comment") {
		t.Fatal("the activity tab should show the new comments")
	}
}

func TestActivityShowsTimelineEventsBetweenComments(t *testing.T) {
	m := newTestModelWithWidth(t, &data.PullRequestData{Title: "timeline"}, nil, nil, 100)
	start := time.Now().Add(-time.Hour)
	at := func(minutes int) time.Time { return start.Add(time.Duration(minutes) * time.Minute) }

	enriched := &m.pr.Data.Enriched
	enriched.HeadRefName = "my-branch"
	enriched.Comments.Nodes = []data.Comment{
		{Body: "first comment", CreatedAt: at(1), UpdatedAt: at(50)},
		{Body: "second comment", CreatedAt: at(10), UpdatedAt: at(10)},
	}
	commit := func(minutes int, headline string) data.TimelineItem {
		item := data.TimelineItem{Typename: "PullRequestCommit"}
		item.PullRequestCommit.Commit.CommittedDate = at(minutes)
		item.PullRequestCommit.Commit.MessageHeadline = headline
		item.PullRequestCommit.Commit.Author.User.Login = "dev"
		return item
	}
	forcePush := data.TimelineItem{Typename: "HeadRefForcePushedEvent"}
	forcePush.HeadRefForcePushedEvent.CreatedAt = at(5)
	forcePush.HeadRefForcePushedEvent.Actor.Login = "dev"
	forcePush.HeadRefForcePushedEvent.BeforeCommit.AbbreviatedOid = "aaaaaaa"
	forcePush.HeadRefForcePushedEvent.AfterCommit.AbbreviatedOid = "bbbbbbb"
	crossRef := data.TimelineItem{Typename: "CrossReferencedEvent"}
	crossRef.CrossReferencedEvent.CreatedAt = at(20)
	crossRef.CrossReferencedEvent.Actor.Login = "other"
	crossRef.CrossReferencedEvent.Source.Typename = "Issue"
	crossRef.CrossReferencedEvent.Source.Issue.Number = 42
	crossRef.CrossReferencedEvent.Source.Issue.Title = "Some bug"
	enriched.TimelineItems.Nodes = []data.TimelineItem{
		commit(2, "fix the thing"), commit(3, "fix it again"), forcePush, crossRef,
	}
	m.GoToActivityTab()

	body, anchors := m.ViewBodyWithAnchors()
	plain := ansi.Strip(body)
	want := []string{
		"first comment",
		"dev added 2 commits",
		"fix the thing",
		"fix it again",
		"dev force-pushed my-branch from aaaaaaa to bbbbbbb",
		"second comment",
		"other mentioned this in",
		"#42 Some bug",
	}
	pos := 0
	for _, w := range want {
		i := strings.Index(plain[pos:], w)
		if i < 0 {
			t.Fatalf("expected %q after position %d in:\n%s", w, pos, plain)
		}
		pos += i + len(w)
	}

	if len(anchors) != 2 {
		t.Fatalf("got %d anchors, want 2, events shouldn't be focusable", len(anchors))
	}
	lines := strings.Split(plain, "\n")
	for i, anchor := range anchors {
		if !strings.HasPrefix(strings.TrimSpace(lines[anchor.Line]), "╭") {
			t.Errorf("anchor %d at line %d doesn't start a comment: %q", i, anchor.Line, lines[anchor.Line])
		}
	}
}
