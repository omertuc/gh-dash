package prow

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/plugins"
)

func finishedJobFiles() map[string]string {
	return map[string]string{
		"job/1/started.json":                 `{"timestamp": 1000}`,
		"job/1/finished.json":                `{"timestamp": 4600, "result": "FAILURE"}`,
		"job/1/artifacts/junit_operator.xml": operatorJunit,
	}
}

func TestPrefetchedResultsAreFetchedOnce(t *testing.T) {
	srv, listings := fakeGCS(t, finishedJobFiles(), true)
	c := newResultsCache(newFetcher(srv.URL))
	job := jobRef{bucket: "bucket", path: "job/1"}

	c.prefetch("pr", []jobRef{job})
	require.Eventually(t, func() bool { _, ok := c.peek(job); return ok }, 5*time.Second, 10*time.Millisecond)
	res, err := c.get(job, false)

	require.NoError(t, err)
	require.Equal(t, "FAILURE", res.result)
	require.EqualValues(t, 1, listings.Load(), "the prefetched results are reused")

	_, err = c.get(job, true)
	require.NoError(t, err)
	require.EqualValues(t, 2, listings.Load(), "fresh results are fetched anew")
}

func TestRunningJobsResultsAreFetchedAnew(t *testing.T) {
	files := finishedJobFiles()
	delete(files, "job/1/finished.json")
	srv, listings := fakeGCS(t, files, true)
	c := newResultsCache(newFetcher(srv.URL))
	job := jobRef{bucket: "bucket", path: "job/1"}

	_, err := c.get(job, false)
	require.NoError(t, err)
	_, ok := c.peek(job)
	require.False(t, ok, "a running job's results may still change")
	_, err = c.get(job, false)
	require.NoError(t, err)

	require.EqualValues(t, 2, listings.Load())
}

func TestPrefetchDropsJobsOfAnotherPR(t *testing.T) {
	srv, listings := fakeGCS(t, finishedJobFiles(), true)
	c := newResultsCache(newFetcher(srv.URL))
	job := jobRef{bucket: "bucket", path: "job/1"}
	// Every prefetch slot is taken, so the job waits its turn
	for range maxPrefetches {
		c.slots <- struct{}{}
	}

	c.prefetch("pr-a", []jobRef{job})
	c.prefetch("pr-b", nil)
	for range maxPrefetches {
		<-c.slots
	}

	require.Eventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return len(c.entries) == 0
	}, 5*time.Second, 10*time.Millisecond)
	require.EqualValues(t, 0, listings.Load())
}

func TestPrefetchKeepsJobsWantedAgain(t *testing.T) {
	srv, listings := fakeGCS(t, finishedJobFiles(), true)
	c := newResultsCache(newFetcher(srv.URL))
	job := jobRef{bucket: "bucket", path: "job/1"}
	for range maxPrefetches {
		c.slots <- struct{}{}
	}

	c.prefetch("pr-a", []jobRef{job})
	c.prefetch("pr-b", nil)
	c.prefetch("pr-a", []jobRef{job})
	for range maxPrefetches {
		<-c.slots
	}

	require.Eventually(t, func() bool { _, ok := c.peek(job); return ok }, 5*time.Second, 10*time.Millisecond)
	require.EqualValues(t, 1, listings.Load())
}

func TestCacheDropsLeastRecentlyUsed(t *testing.T) {
	c := newResultsCache(newFetcher("http://unused"))
	first := jobRef{bucket: "bucket", path: "job/0"}
	c.add(first.key())
	for i := range maxCachedJobs {
		if i == maxCachedJobs/2 {
			c.touch(first.key())
		}
		c.add(jobRef{bucket: "bucket", path: "job/" + string(rune('a'+i))}.key())
	}

	require.Len(t, c.entries, maxCachedJobs)
	require.Contains(t, c.entries, first.key(), "it was used recently")
}

func TestPluginPrefetchesFailedJobs(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	p := New()
	require.NoError(t, p.Configure(map[string]any{"artifactsUrl": srv.URL}))
	pr := newTestPR(t, prowStatuses(), nil)
	pr.Primary = &data.PullRequestData{Url: "https://github.com/org/repo/pull/1"}

	p.Prefetch(pr)

	failed, _ := parseJobURL(jobURL("pull-ci-e2e-aws"))
	passed, _ := parseJobURL(jobURL("pull-ci-unit"))
	c := p.results()
	c.mu.Lock()
	defer c.mu.Unlock()
	require.Contains(t, c.entries, failed.key())
	require.NotContains(t, c.entries, passed.key())
}

func TestJobViewShowsCachedResultsRightAway(t *testing.T) {
	p := New()
	check := plugins.Check{Name: "ci/prow/e2e-aws", URL: jobURL("pull-ci-e2e-aws"), State: "FAILURE"}
	job, _ := parseJobURL(check.URL)
	e := p.results().add(job.key())
	e.results = &jobResults{result: "FAILURE"}
	e.results.tests[testFailed] = makeTests(testFailed, 1)
	close(e.done)
	e.once.Do(func() {})

	v := p.OpenCheck(plugins.PR{}, check)

	require.Nil(t, v.Init(), "nothing is left to fetch")
	require.False(t, v.(*jobView).loading)
	require.NotNil(t, v.(*jobView).results)
}
