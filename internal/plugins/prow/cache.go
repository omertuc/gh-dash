package prow

import (
	"slices"
	"sync"
)

// maxCachedJobs is how many jobs' results are kept, the least recently used
// being dropped first.
const maxCachedJobs = 32

// maxPrefetches is how many jobs' results are prefetched at a time, so a PR
// with many failed jobs doesn't hog the network.
const maxPrefetches = 2

// resultsCache keeps jobs' test results, so that a job's tab opens with them
// right away when they were prefetched or the job was opened before.
type resultsCache struct {
	fetcher *fetcher
	// slots limits how many prefetches run at a time
	slots chan struct{}

	mu      sync.Mutex
	entries map[string]*cacheEntry
	// order lists the entries' keys, the least recently used first
	order []string
	// prefetchingFor is the PR jobs were last prefetched for, and generation
	// counts how many PRs that's been, to tell which prefetches are still
	// wanted
	prefetchingFor string
	generation     int
}

// cacheEntry is a job's results, once they're fetched.
type cacheEntry struct {
	once    sync.Once
	done    chan struct{}
	results *jobResults
	err     error
	// gen is the generation of the prefetch that wants the entry, guarded by
	// the cache's mu
	gen int
}

func newResultsCache(f *fetcher) *resultsCache {
	return &resultsCache{
		fetcher: f,
		slots:   make(chan struct{}, maxPrefetches),
		entries: map[string]*cacheEntry{},
	}
}

func (j jobRef) key() string {
	return j.bucket + "/" + j.path
}

// fetch fetches the job's results unless they were already, waiting for a
// fetch already underway.
func (e *cacheEntry) fetch(f *fetcher, job jobRef) {
	e.once.Do(func() {
		e.results, e.err = f.fetchResults(job)
		close(e.done)
	})
}

func (e *cacheEntry) isDone() bool {
	select {
	case <-e.done:
		return true
	default:
		return false
	}
}

// complete reports whether the entry holds the results of a job that
// finished, which won't change anymore, without waiting for them.
func (e *cacheEntry) complete() bool {
	return e.isDone() && e.err == nil && e.results.result != ""
}

// peek returns the job's results when they're cached and the job finished.
func (c *resultsCache) peek(job jobRef) (*jobResults, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[job.key()]
	if !ok || !e.complete() {
		return nil, false
	}
	c.touch(job.key())
	return e.results, true
}

// get returns the job's results, waiting for them when they're being
// prefetched. Unless fresh, a finished job's cached results are reused, as
// they won't change anymore.
func (c *resultsCache) get(job jobRef, fresh bool) (*jobResults, error) {
	k := job.key()
	if !fresh {
		c.mu.Lock()
		e, ok := c.entries[k]
		if ok {
			c.touch(k)
		}
		c.mu.Unlock()
		if ok {
			wasDone := e.isDone()
			// Runs the prefetch right away if it's still waiting its turn
			e.fetch(c.fetcher, job)
			if e.complete() {
				return e.results, nil
			}
			if e.err != nil && !wasDone {
				// It just failed, so it would likely fail again
				return nil, e.err
			}
		}
	}
	c.mu.Lock()
	e := c.add(k)
	c.mu.Unlock()
	e.fetch(c.fetcher, job)
	return e.results, e.err
}

// prefetch fetches the jobs' results of the given PR in the background, a few
// at a time, unless they're cached already. Jobs still waiting their turn
// from an earlier PR are dropped, as that PR isn't shown anymore.
func (c *resultsCache) prefetch(pr string, jobs []jobRef) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if pr != c.prefetchingFor {
		c.prefetchingFor = pr
		c.generation++
	}
	for _, job := range jobs {
		k := job.key()
		if e, ok := c.entries[k]; ok {
			// Still wanted, if it's waiting its turn
			e.gen = c.generation
			continue
		}
		e := c.add(k)
		e.gen = c.generation
		go func() {
			c.slots <- struct{}{}
			defer func() { <-c.slots }()
			if c.stillWanted(k, e) {
				e.fetch(c.fetcher, job)
			}
		}()
	}
}

// stillWanted reports whether a prefetch is still wanted once its turn
// comes, and otherwise drops its entry so that it's prefetched again should
// its PR be shown again.
func (c *resultsCache) stillWanted(k string, e *cacheEntry) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e.gen == c.generation || e.isDone() {
		return true
	}
	if c.entries[k] == e {
		delete(c.entries, k)
		c.order = slices.DeleteFunc(c.order, func(o string) bool { return o == k })
	}
	return false
}

// add adds a new entry for k, replacing any there was, and drops the least
// recently used entries beyond maxCachedJobs. It's called with mu held.
func (c *resultsCache) add(k string) *cacheEntry {
	e := &cacheEntry{done: make(chan struct{})}
	c.entries[k] = e
	c.touch(k)
	for len(c.order) > maxCachedJobs {
		delete(c.entries, c.order[0])
		c.order = c.order[1:]
	}
	return e
}

// touch marks k as the most recently used. It's called with mu held.
func (c *resultsCache) touch(k string) {
	c.order = slices.DeleteFunc(c.order, func(o string) bool { return o == k })
	c.order = append(c.order, k)
}
