package prow

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// jobRef locates a Prow job's artifacts, e.g. from
// https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/org_repo/1/job-name/123
type jobRef struct {
	url    string
	bucket string
	// path is the build's directory within the bucket
	path    string
	name    string
	buildID string
}

// parseJobURL parses a link to a Prow job whose artifacts are in Google
// Cloud Storage.
func parseJobURL(raw string) (jobRef, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return jobRef{}, false
	}
	rest, ok := strings.CutPrefix(u.Path, "/view/gs/")
	if !ok {
		if rest, ok = strings.CutPrefix(u.Path, "/view/gcs/"); !ok {
			return jobRef{}, false
		}
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 3 {
		return jobRef{}, false
	}
	return jobRef{
		url:     raw,
		bucket:  parts[0],
		path:    strings.Join(parts[1:], "/"),
		name:    parts[len(parts)-2],
		buildID: parts[len(parts)-1],
	}, true
}

// testState is how a test went across its runs.
type testState int

const (
	testFailed testState = iota
	testFlaky
	testPassed
	testSkipped
	numTestStates
)

// testResult is a test's result, across its runs when it was retried.
type testResult struct {
	suite    string
	name     string
	state    testState
	duration time.Duration
	runs     int
	failures int
	// message and output are a failure's, or why a test was skipped
	message string
	output  string
}

func (t *testResult) key() string {
	return t.suite + "\x00" + t.name
}

// jobResults are what's known about a job and its tests.
type jobResults struct {
	started  time.Time
	finished time.Time
	// result is e.g. "SUCCESS", "FAILURE" or "ABORTED", or "" while running
	result string
	// tests are listed by state, and within that in the order they ran
	tests [numTestStates][]*testResult
	// files are how many junit files were read
	files int
	// skippedFiles are junit files too big to read
	skippedFiles int
}

// maxJunitSize is the size of the biggest junit file read, as reading
// bigger ones would take too long and too much memory.
const maxJunitSize = 256 << 20

// maxFailureText is how much of a failure's message and output is kept.
const maxFailureText = 64 << 10

var errNotPublic = errors.New("not public")

type fetcher struct {
	client  *http.Client
	baseURL string
}

func newFetcher(baseURL string) *fetcher {
	return &fetcher{client: &http.Client{Timeout: 2 * time.Minute}, baseURL: baseURL}
}

// fetchResults fetches the job's metadata and all of its junit files and
// sums them up.
func (f *fetcher) fetchResults(job jobRef) (*jobResults, error) {
	res := &jobResults{}
	var started struct {
		Timestamp int64 `json:"timestamp"`
	}
	if err := f.getJSON(job.bucket, job.path+"/started.json", &started); err == nil {
		res.started = time.Unix(started.Timestamp, 0)
	}
	var finished struct {
		Timestamp int64  `json:"timestamp"`
		Result    string `json:"result"`
	}
	if err := f.getJSON(job.bucket, job.path+"/finished.json", &finished); err == nil {
		res.finished = time.Unix(finished.Timestamp, 0)
		res.result = finished.Result
	}

	files, err := f.listJunitFiles(job)
	if err != nil {
		return nil, err
	}

	type parsed struct {
		read  bool
		cases []testCase
		err   error
	}
	results := make([]parsed, len(files))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, file := range files {
		if file.size > maxJunitSize {
			res.skippedFiles++
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			body, err := f.get(job.bucket, file.name)
			if err != nil {
				results[i].err = err
				return
			}
			defer body.Close()
			results[i].read = true
			results[i].cases, results[i].err = parseJunit(body)
		}()
	}
	wg.Wait()

	var cases []testCase
	for _, r := range results {
		if !r.read || r.err != nil {
			// A junit file that fails to parse, e.g. one cut short, is left
			// out rather than failing the whole job
			continue
		}
		res.files++
		cases = append(cases, r.cases...)
	}
	res.tests = summarize(cases)
	return res, nil
}

type object struct {
	name string
	size int64
}

// listJunitFiles lists the junit files among the job's artifacts.
func (f *fetcher) listJunitFiles(job jobRef) ([]object, error) {
	var objects []object
	pageToken := ""
	for {
		q := url.Values{}
		q.Set("prefix", job.path+"/artifacts/")
		q.Set("matchGlob", "**/*junit*.xml")
		q.Set("fields", "items(name,size),nextPageToken")
		q.Set("maxResults", "1000")
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		u := fmt.Sprintf("%s/storage/v1/b/%s/o?%s", f.baseURL, url.PathEscape(job.bucket), q.Encode())
		var page struct {
			Items []struct {
				Name string `json:"name"`
				Size string `json:"size"`
			} `json:"items"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := f.fetchJSON(u, &page); err != nil {
			return nil, err
		}
		for _, it := range page.Items {
			size, _ := strconv.ParseInt(it.Size, 10, 64)
			objects = append(objects, object{name: it.Name, size: size})
		}
		if page.NextPageToken == "" {
			return objects, nil
		}
		pageToken = page.NextPageToken
	}
}

func (f *fetcher) objectURL(bucket, name string) string {
	segments := strings.Split(name, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return f.baseURL + "/" + url.PathEscape(bucket) + "/" + path.Join(segments...)
}

func (f *fetcher) get(bucket, name string) (io.ReadCloser, error) {
	return f.open(f.objectURL(bucket, name))
}

func (f *fetcher) getJSON(bucket, name string, v any) error {
	return f.fetchJSON(f.objectURL(bucket, name), v)
}

func (f *fetcher) fetchJSON(u string, v any) error {
	body, err := f.open(u)
	if err != nil {
		return err
	}
	defer body.Close()
	return json.NewDecoder(body).Decode(v)
}

func (f *fetcher) open(u string) (io.ReadCloser, error) {
	resp, err := f.client.Get(u)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, errNotPublic
		}
		return nil, fmt.Errorf("fetching %s: %s", u, resp.Status)
	}
	return resp.Body, nil
}

// testCase is a run of a test, as a junit file reports it.
type testCase struct {
	suite    string
	name     string
	duration time.Duration
	failed   bool
	skipped  bool
	message  string
	output   string
}

type xmlTestCase struct {
	Name      string      `xml:"name,attr"`
	Classname string      `xml:"classname,attr"`
	Time      string      `xml:"time,attr"`
	Failure   *xmlFailure `xml:"failure"`
	Error     *xmlFailure `xml:"error"`
	Skipped   *xmlFailure `xml:"skipped"`
}

type xmlFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

// parseJunit reads the test cases of a junit file, whether its root is a
// <testsuites> or a <testsuite>. Suites may be nested.
func parseJunit(r io.Reader) ([]testCase, error) {
	dec := xml.NewDecoder(r)
	var cases []testCase
	var suites []string
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return cases, nil
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "testsuite":
				name := ""
				for _, a := range t.Attr {
					if a.Name.Local == "name" {
						name = a.Value
					}
				}
				suites = append(suites, name)
			case "testcase":
				var tc xmlTestCase
				if err := dec.DecodeElement(&tc, &t); err != nil {
					return nil, err
				}
				if strings.TrimSpace(tc.Name) == "" {
					continue
				}
				suite := tc.Classname
				if suite == "" && len(suites) > 0 {
					suite = suites[len(suites)-1]
				}
				c := testCase{suite: suite, name: tc.Name}
				if secs, err := strconv.ParseFloat(tc.Time, 64); err == nil {
					c.duration = time.Duration(secs * float64(time.Second))
				}
				failure := tc.Failure
				if failure == nil {
					failure = tc.Error
				}
				switch {
				case failure != nil:
					c.failed = true
					c.message = truncate(strings.TrimSpace(failure.Message))
					c.output = truncateStart(strings.TrimSpace(failure.Text))
				case tc.Skipped != nil:
					c.skipped = true
					c.message = truncate(strings.TrimSpace(tc.Skipped.Message))
				}
				cases = append(cases, c)
			}
		case xml.EndElement:
			if t.Name.Local == "testsuite" && len(suites) > 0 {
				suites = suites[:len(suites)-1]
			}
		}
	}
}

// summarize sums up the runs of each test: one that failed every time it ran
// failed, one that failed but then passed is flaky.
func summarize(cases []testCase) [numTestStates][]*testResult {
	byKey := map[string]*testResult{}
	var order []*testResult
	passes := map[string]int{}
	skips := map[string]int{}
	for _, c := range cases {
		t := &testResult{suite: c.suite, name: c.name}
		k := t.key()
		if existing, ok := byKey[k]; ok {
			t = existing
		} else {
			byKey[k] = t
			order = append(order, t)
		}
		t.runs++
		t.duration = c.duration
		switch {
		case c.failed:
			t.failures++
			// Keep the last failure, which is the most relevant one
			t.message, t.output = c.message, c.output
		case c.skipped:
			skips[k]++
			if t.failures == 0 && t.message == "" {
				t.message = c.message
			}
		default:
			passes[k]++
		}
	}

	var tests [numTestStates][]*testResult
	for _, t := range order {
		k := t.key()
		switch {
		case t.failures > 0 && passes[k] > 0:
			t.state = testFlaky
		case t.failures > 0:
			t.state = testFailed
		case passes[k] == 0 && skips[k] > 0:
			t.state = testSkipped
		default:
			t.state = testPassed
			t.message = ""
		}
		tests[t.state] = append(tests[t.state], t)
	}
	return tests
}

// truncate keeps the start of s, e.g. a failure's message.
func truncate(s string) string {
	if len(s) <= maxFailureText {
		return s
	}
	return strings.ToValidUTF8(s[:maxFailureText], "") + "…"
}

// truncateStart keeps the end of s, e.g. a failure's output, which ends with
// what went wrong.
func truncateStart(s string) string {
	if len(s) <= maxFailureText {
		return s
	}
	return "…" + strings.ToValidUTF8(s[len(s)-maxFailureText:], "")
}
