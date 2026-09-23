package tui

import (
	"testing"
	"time"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

func watchedComment(login, body string, createdAt, updatedAt time.Time) data.WatchedComment {
	c := data.WatchedComment{Body: body, CreatedAt: createdAt, UpdatedAt: updatedAt}
	c.User.Login = login
	return c
}

func TestHasReply(t *testing.T) {
	// GitHub's clock is behind the local one
	posted := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	w := commentWatch{body: "/retest\n", postedAt: posted.Add(time.Minute)}
	mine := watchedComment("me", "/retest", posted, posted)

	tests := []struct {
		name     string
		comments []data.WatchedComment
		want     string
	}{
		{
			name:     "only the posted comment",
			comments: []data.WatchedComment{mine},
		},
		{
			name: "comment from before the posted one",
			comments: []data.WatchedComment{
				watchedComment("bot", "old", posted.Add(-time.Second), posted.Add(-time.Second)),
				mine,
			},
		},
		{
			name: "reply after the posted comment, by GitHub's clock",
			comments: []data.WatchedComment{
				mine,
				watchedComment("bot", "on it", posted.Add(time.Second), posted.Add(time.Second)),
			},
			want: "on it",
		},
		{
			name: "earlier comment edited after the posted one",
			comments: []data.WatchedComment{
				watchedComment("bot", "status", posted.Add(-time.Hour), posted.Add(time.Second)),
				mine,
			},
			want: "status",
		},
		{
			name: "own later comment isn't a reply",
			comments: []data.WatchedComment{
				mine,
				watchedComment("me", "another", posted.Add(time.Second), posted.Add(time.Second)),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := w.hasReply(tt.comments, "me"); got != (tt.want != "") {
				t.Errorf("hasReply() = %v, want reply %q", got, tt.want)
			}
		})
	}
}
