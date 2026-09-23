package data

import (
	"slices"
	"sync"
	"time"
)

const (
	// SubjectMaxAge is how long a fetched notification subject is reused by
	// background fetches before it's fetched again, e.g. to pick up new CI
	// results, which don't change the notification's updated_at.
	SubjectMaxAge = 5 * time.Minute

	// maxCachedSubjects bounds the subject cache. When it fills up, the
	// subjects fetched longest ago are dropped.
	maxCachedSubjects = 300
)

// The fetch functions used by the subject cache. Variables so tests can
// replace them.
var (
	fetchSubjectPR    = FetchPullRequest
	fetchSubjectIssue = FetchIssue
)

// CachedSubject is a notification's PR or Issue, as fetched for it.
// Exactly one of PR and Issue is set.
type CachedSubject struct {
	PR    *EnrichedPullRequestData
	Issue *IssueData
	// UpdatedAt is the notification's updated_at the subject was fetched for
	UpdatedAt time.Time
	FetchedAt time.Time
}

type subjectEntry struct {
	subject CachedSubject
	err     error
	done    chan struct{} // closed once the fetch finishes
}

// SubjectCache keeps the PRs and Issues of notifications, so opening a
// notification doesn't have to wait for its subject to be fetched. Concurrent
// fetches of the same subject share a single request.
//
// Subjects are keyed by URL and remember the notification's updated_at they
// were fetched for: once the notification has been updated since, e.g. with
// a new comment, the subject is fetched again.
type SubjectCache struct {
	mu      sync.Mutex
	entries map[string]*subjectEntry
}

func NewSubjectCache() *SubjectCache {
	return &SubjectCache{entries: make(map[string]*subjectEntry)}
}

var subjectCache = NewSubjectCache()

// GetSubjectCache returns the cache shared by everything fetching
// notification subjects.
func GetSubjectCache() *SubjectCache {
	return subjectCache
}

// Get returns the subject at url if it has been fetched for a notification
// updated at updatedAt or later, regardless of how long ago.
func (c *SubjectCache) Get(url string, updatedAt time.Time) (CachedSubject, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[url]
	if !ok || !e.isDone() || e.err != nil || e.subject.UpdatedAt.Before(updatedAt) {
		return CachedSubject{}, false
	}
	return e.subject, true
}

// Put stores a subject fetched elsewhere, e.g. when refreshing an open
// notification. A fetch in flight for it is left to finish.
func (c *SubjectCache) Put(url string, subject CachedSubject) {
	if subject.FetchedAt.IsZero() {
		subject.FetchedAt = time.Now()
	}
	done := make(chan struct{})
	close(done)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[url] = &subjectEntry{subject: subject, done: done}
	c.pruneLocked()
}

// IsFetching reports whether the subject at url is being fetched.
func (c *SubjectCache) IsFetching(url string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[url]
	return ok && !e.isDone()
}

// FetchPR returns the PR at url, fetching it unless it was fetched for this
// notification update less than maxAge ago. If it's already being fetched,
// that fetch is waited for.
func (c *SubjectCache) FetchPR(
	url string,
	updatedAt time.Time,
	maxAge time.Duration,
) (CachedSubject, error) {
	return c.fetch(url, updatedAt, maxAge, func() (CachedSubject, error) {
		pr, err := fetchSubjectPR(url)
		return CachedSubject{PR: &pr}, err
	})
}

// FetchIssue is like FetchPR, for an Issue.
func (c *SubjectCache) FetchIssue(
	url string,
	updatedAt time.Time,
	maxAge time.Duration,
) (CachedSubject, error) {
	return c.fetch(url, updatedAt, maxAge, func() (CachedSubject, error) {
		issue, err := fetchSubjectIssue(url)
		return CachedSubject{Issue: &issue}, err
	})
}

func (c *SubjectCache) fetch(
	url string,
	updatedAt time.Time,
	maxAge time.Duration,
	fetch func() (CachedSubject, error),
) (CachedSubject, error) {
	c.mu.Lock()
	e, ok := c.entries[url]
	switch {
	case ok && !e.isDone():
		// Join the fetch in flight. It may be for an older update of the
		// notification, but it has only just been started.
	case ok && e.err == nil && !e.subject.UpdatedAt.Before(updatedAt) &&
		time.Since(e.subject.FetchedAt) < maxAge:
		c.mu.Unlock()
		return e.subject, nil
	default:
		e = &subjectEntry{done: make(chan struct{})}
		c.entries[url] = e
		c.pruneLocked()
		c.mu.Unlock()

		subject, err := fetch()
		subject.UpdatedAt = updatedAt
		subject.FetchedAt = time.Now()

		c.mu.Lock()
		e.subject, e.err = subject, err
		close(e.done)
		// Don't keep failures, so the next fetch tries again
		if err != nil && c.entries[url] == e {
			delete(c.entries, url)
		}
		c.mu.Unlock()
		return subject, err
	}
	c.mu.Unlock()

	<-e.done
	return e.subject, e.err
}

// pruneLocked drops the subjects fetched longest ago once there are too
// many. c.mu must be held.
func (c *SubjectCache) pruneLocked() {
	if len(c.entries) <= maxCachedSubjects {
		return
	}
	type fetched struct {
		url string
		at  time.Time
	}
	var done []fetched
	for url, e := range c.entries {
		if e.isDone() {
			done = append(done, fetched{url, e.subject.FetchedAt})
		}
	}
	slices.SortFunc(done, func(a, b fetched) int { return a.at.Compare(b.at) })
	for _, f := range done[:max(0, min(len(done), len(c.entries)-maxCachedSubjects))] {
		delete(c.entries, f.url)
	}
}

func (e *subjectEntry) isDone() bool {
	select {
	case <-e.done:
		return true
	default:
		return false
	}
}
