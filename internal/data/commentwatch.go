package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	gh "github.com/cli/go-gh/v2/pkg/api"
)

// WatchedComment is an issue or PR comment, as listed by the REST API
type WatchedComment struct {
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CommentWatcher checks an issue or PR for new comments, e.g. a bot's reply.
// It makes conditional requests: as long as nothing changed GitHub answers
// 304 Not Modified, which doesn't count against the rate limit, so it's
// cheap to check often.
type CommentWatcher struct {
	client *gh.RESTClient
	path   string
	etag   string
}

// NewCommentWatcher watches the comments of issue or PR number in repo
// (owner/name) that were created or edited since since.
func NewCommentWatcher(repo string, number int, since time.Time) (*CommentWatcher, error) {
	w := &CommentWatcher{
		path: fmt.Sprintf(
			"repos/%s/issues/%d/comments?per_page=100&since=%s",
			repo,
			number,
			url.QueryEscape(since.UTC().Format(time.RFC3339)),
		),
	}
	client, err := gh.NewRESTClient(gh.ClientOptions{
		Transport: etagTransport{w: w, next: http.DefaultTransport},
	})
	if err != nil {
		return nil, err
	}
	w.client = client
	return w, nil
}

// Check returns the watched comments, or changed is false when they haven't
// changed since the last check. Not safe for concurrent use.
func (w *CommentWatcher) Check() (comments []WatchedComment, changed bool, err error) {
	resp, err := w.client.Request(http.MethodGet, w.path, nil)
	if err != nil {
		var httpErr *gh.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotModified {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&comments); err != nil {
		return nil, false, err
	}
	w.etag = resp.Header.Get("ETag")
	return comments, true, nil
}

// etagTransport makes the watcher's requests conditional on its last ETag
type etagTransport struct {
	w    *CommentWatcher
	next http.RoundTripper
}

func (t etagTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.w.etag != "" {
		req = req.Clone(req.Context())
		req.Header.Set("If-None-Match", t.w.etag)
	}
	return t.next.RoundTrip(req)
}
