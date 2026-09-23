package data

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// stubSubjectFetch replaces fetching PRs with fn for the test.
func stubSubjectFetch(t *testing.T, fn func(url string) (EnrichedPullRequestData, error)) {
	t.Helper()
	prev := fetchSubjectPR
	fetchSubjectPR = fn
	t.Cleanup(func() { fetchSubjectPR = prev })
}

func TestSubjectCache_ConcurrentFetchesShareOneRequest(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	stubSubjectFetch(t, func(url string) (EnrichedPullRequestData, error) {
		calls.Add(1)
		<-release
		return EnrichedPullRequestData{Title: "the PR"}, nil
	})

	c := NewSubjectCache()
	updatedAt := time.Now()
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			subject, err := c.FetchPR("https://github.com/o/r/pull/1", updatedAt, time.Hour)
			require.NoError(t, err)
			require.Equal(t, "the PR", subject.PR.Title)
		})
	}
	require.Eventually(t, func() bool { return c.IsFetching("https://github.com/o/r/pull/1") },
		time.Second, time.Millisecond)
	close(release)
	wg.Wait()

	require.EqualValues(t, 1, calls.Load(), "the PR should be fetched once")
	_, ok := c.Get("https://github.com/o/r/pull/1", updatedAt)
	require.True(t, ok, "the fetched PR should be cached")
}

func TestSubjectCache_RefetchesWhenNotificationUpdatedOrTooOld(t *testing.T) {
	var calls atomic.Int32
	stubSubjectFetch(t, func(url string) (EnrichedPullRequestData, error) {
		calls.Add(1)
		return EnrichedPullRequestData{}, nil
	})

	c := NewSubjectCache()
	url := "https://github.com/o/r/pull/1"
	t0 := time.Now().Add(-time.Hour)

	_, err := c.FetchPR(url, t0, time.Hour)
	require.NoError(t, err)
	_, err = c.FetchPR(url, t0, time.Hour)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load(), "a fresh subject should be reused")

	_, ok := c.Get(url, t0.Add(time.Minute))
	require.False(t, ok, "a subject fetched before the notification was updated is stale")
	_, err = c.FetchPR(url, t0.Add(time.Minute), time.Hour)
	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load(), "an updated notification should refetch its subject")

	_, err = c.FetchPR(url, t0.Add(time.Minute), 0)
	require.NoError(t, err)
	require.EqualValues(t, 3, calls.Load(), "a subject older than maxAge should be refetched")
}

func TestSubjectCache_FailuresAreNotCached(t *testing.T) {
	fail := true
	stubSubjectFetch(t, func(url string) (EnrichedPullRequestData, error) {
		if fail {
			return EnrichedPullRequestData{}, errors.New("offline")
		}
		return EnrichedPullRequestData{Title: "the PR"}, nil
	})

	c := NewSubjectCache()
	url := "https://github.com/o/r/pull/1"
	_, err := c.FetchPR(url, time.Now(), time.Hour)
	require.Error(t, err)
	_, ok := c.Get(url, time.Time{})
	require.False(t, ok)

	fail = false
	subject, err := c.FetchPR(url, time.Now(), time.Hour)
	require.NoError(t, err)
	require.Equal(t, "the PR", subject.PR.Title)
}

func TestSubjectCache_PrunesOldest(t *testing.T) {
	c := NewSubjectCache()
	start := time.Now().Add(-time.Hour)
	for i := range maxCachedSubjects + 10 {
		c.Put(string(rune('a'+i%26))+time.Duration(i).String(), CachedSubject{
			PR:        &EnrichedPullRequestData{},
			FetchedAt: start.Add(time.Duration(i) * time.Second),
		})
	}
	require.Len(t, c.entries, maxCachedSubjects)
	_, ok := c.Get("a0s", time.Time{})
	require.False(t, ok, "the subject fetched longest ago should be dropped")
}

func TestCacheFile_RoundTrip(t *testing.T) {
	SetCacheDirForTesting(t.TempDir())
	t.Cleanup(func() { SetCacheDirForTesting("") })

	_, err := ReadCacheFile("missing.json")
	require.Error(t, err)

	require.NoError(t, WriteCacheFile("rows.json", []byte(`[1]`)))
	require.NoError(t, WriteCacheFile("rows.json", []byte(`[1,2]`)))
	contents, err := ReadCacheFile("rows.json")
	require.NoError(t, err)
	require.Equal(t, `[1,2]`, string(contents))
}

func TestCacheFile_DisabledInTests(t *testing.T) {
	require.ErrorIs(t, WriteCacheFile("rows.json", nil), errCacheDisabled,
		"tests shouldn't write to the user's cache")
}
