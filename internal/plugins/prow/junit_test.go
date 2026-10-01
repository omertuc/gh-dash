package prow

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const e2eJunit = `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="openshift-tests" tests="5">
    <testcase name="passes" time="1.5"/>
    <testcase name="flakes" time="2">
      <failure message="timed out">some output</failure>
      <system-out>lots of logs</system-out>
    </testcase>
    <testcase name="flakes" time="3"/>
    <testcase name="fails" time="4">
      <failure message="expected 1, got 2">first try</failure>
    </testcase>
    <testcase name="fails" time="5">
      <error message="panic">second try</error>
    </testcase>
    <testsuite name="nested">
      <testcase name="is skipped"><skipped message="not on this platform"/></testcase>
    </testsuite>
    <testcase name="" time="0"/>
  </testsuite>
</testsuites>`

// operatorJunit is a junit file whose root is a single suite.
const operatorJunit = `<testsuite name="step graph">
  <testcase name="Run multi-stage test e2e" time="7200"><failure>exit status 1</failure></testcase>
  <testcase classname="build" name="Build image tests" time="905"/>
</testsuite>`

func TestParseJunitAndSummarize(t *testing.T) {
	cases, err := parseJunit(strings.NewReader(e2eJunit))
	require.NoError(t, err)
	more, err := parseJunit(strings.NewReader(operatorJunit))
	require.NoError(t, err)

	tests := summarize(append(cases, more...))

	names := func(s testState) []string {
		var out []string
		for _, t := range tests[s] {
			out = append(out, t.suite+"/"+t.name)
		}
		return out
	}
	require.Equal(t, []string{"openshift-tests/fails", "step graph/Run multi-stage test e2e"}, names(testFailed))
	require.Equal(t, []string{"openshift-tests/flakes"}, names(testFlaky))
	require.Equal(t, []string{"openshift-tests/passes", "build/Build image tests"}, names(testPassed))
	require.Equal(t, []string{"nested/is skipped"}, names(testSkipped))

	fails := tests[testFailed][0]
	require.Equal(t, 2, fails.runs)
	require.Equal(t, 2, fails.failures)
	// The last failure is kept
	require.Equal(t, "panic", fails.message)
	require.Equal(t, "second try", fails.output)
	require.Equal(t, 5*time.Second, fails.duration)

	flakes := tests[testFlaky][0]
	require.Equal(t, 2, flakes.runs)
	require.Equal(t, 1, flakes.failures)
	require.Equal(t, "timed out", flakes.message)

	require.Equal(t, "not on this platform", tests[testSkipped][0].message)
	require.Empty(t, tests[testPassed][0].message)
}

func TestParseJunitRejectsBrokenXML(t *testing.T) {
	_, err := parseJunit(strings.NewReader(`<testsuite><testcase name="x">`))
	require.Error(t, err)
}

// fakeGCS serves a job's artifacts the way Google Cloud Storage does, listing
// them a page at a time.
func fakeGCS(t *testing.T, files map[string]string, public bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var listings atomic.Int32
	names := []string{}
	for name := range files {
		if strings.Contains(name, "junit") {
			names = append(names, name)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !public {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/storage/v1/b/bucket/o") {
			listings.Add(1)
			require.Equal(t, "job/1/artifacts/", r.URL.Query().Get("prefix"))
			require.Equal(t, "**/*junit*.xml", r.URL.Query().Get("matchGlob"))
			// One file per page
			i := 0
			if tok := r.URL.Query().Get("pageToken"); tok != "" {
				fmt.Sscan(tok, &i)
			}
			next := ""
			if i+1 < len(names) {
				next = fmt.Sprintf(`,"nextPageToken":"%d"`, i+1)
			}
			items := ""
			if i < len(names) {
				items = fmt.Sprintf(`{"name":%q,"size":"%d"}`, names[i], len(files[names[i]]))
			}
			fmt.Fprintf(w, `{"items":[%s]%s}`, items, next)
			return
		}
		body, ok := files[strings.TrimPrefix(r.URL.Path, "/bucket/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &listings
}

func TestFetchResults(t *testing.T) {
	srv, listings := fakeGCS(t, map[string]string{
		"job/1/started.json":                      `{"timestamp": 1000}`,
		"job/1/finished.json":                     `{"timestamp": 4600, "result": "FAILURE"}`,
		"job/1/artifacts/e2e/junit/junit_e2e.xml": e2eJunit,
		"job/1/artifacts/junit_operator.xml":      operatorJunit,
		"job/1/artifacts/junit_broken.xml":        `<testsuite>`,
	}, true)
	job := jobRef{bucket: "bucket", path: "job/1"}

	res, err := newFetcher(srv.URL).fetchResults(job)

	require.NoError(t, err)
	require.EqualValues(t, 3, listings.Load(), "every page is listed")
	require.Equal(t, "FAILURE", res.result)
	require.Equal(t, time.Hour, res.finished.Sub(res.started))
	// The broken file is left out
	require.Equal(t, 2, res.files)
	require.Len(t, res.tests[testFailed], 2)
	require.Len(t, res.tests[testFlaky], 1)
}

func TestFetchResultsOfPrivateJob(t *testing.T) {
	srv, _ := fakeGCS(t, nil, false)

	_, err := newFetcher(srv.URL).fetchResults(jobRef{bucket: "bucket", path: "job/1"})

	require.ErrorIs(t, err, errNotPublic)
}
