package data

import (
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	graphql "github.com/cli/shurcooL-graphql"
	"github.com/shurcooL/githubv4"
	"golang.org/x/sync/errgroup"
)

// ConnectionPage says where a fetched page of a GraphQL connection sits in
// the whole list, so the rest of the list can be fetched.
type ConnectionPage struct {
	HasPreviousPage bool
	HasNextPage     bool
	StartCursor     string
	EndCursor       string
}

// fetchRestOfPullRequest fetches the pages of pr's connections that its
// query left out. The connections are fetched in parallel, as huge PRs need
// many pages.
func fetchRestOfPullRequest(prUrl *url.URL, pr *EnrichedPullRequestData) error {
	var g errgroup.Group
	g.SetLimit(8)
	for _, field := range []string{
		"Comments", "Reviews", "ReviewRequests", "TimelineItems", "AllCommits", "Files",
	} {
		g.Go(func() error {
			return fetchRestOfConnection(prUrl, "PullRequest", pr, field)
		})
	}
	g.Go(func() error {
		if err := fetchRestOfConnection(prUrl, "PullRequest", pr, "ReviewThreads"); err != nil {
			return err
		}
		var threads errgroup.Group
		threads.SetLimit(4)
		for i := range pr.ReviewThreads.Nodes {
			thread := &pr.ReviewThreads.Nodes[i]
			threads.Go(func() error {
				return fetchRestOfThreadComments(thread.Id, &thread.Comments)
			})
		}
		return threads.Wait()
	})
	return g.Wait()
}

var pageSizeRegex = regexp.MustCompile(`\b(first|last): \d+`)

// fetchRestOfConnection fetches the pages of a connection that the query of
// the PR or issue it belongs to left out, and adds their nodes to it, so it
// holds the whole list.
//
// resource points at the struct that was fetched with resource(url: url) as
// typeName, e.g. a PullRequest, and field names its connection, whose type
// has Nodes and PageInfo ConnectionPage fields. The extra pages are fetched
// with the field's own graphql tag, so they have the same arguments, e.g. the
// timeline's item types. A connection fetched with last: N is completed
// backwards, one fetched with first: N forwards.
func fetchRestOfConnection(resourceUrl *url.URL, typeName string, resource any, field string) error {
	value := reflect.ValueOf(resource).Elem()
	structField, ok := value.Type().FieldByName(field)
	if !ok {
		return fmt.Errorf("%s has no field %s", value.Type(), field)
	}
	tag := structField.Tag.Get("graphql")
	backwards := strings.Contains(tag, "last:")
	cursorArg := "after"
	if backwards {
		cursorArg = "before"
	}
	tag = pageSizeRegex.ReplaceAllString(tag, "$1: 100")
	tag = strings.Replace(tag, "(", "("+cursorArg+": $cursor, ", 1)

	queryType := reflect.StructOf([]reflect.StructField{{
		Name: "Resource",
		Tag:  graphqlTag("resource(url: $url)"),
		Type: reflect.StructOf([]reflect.StructField{{
			Name: "On",
			Tag:  graphqlTag("... on " + typeName),
			Type: reflect.StructOf([]reflect.StructField{{
				Name: field,
				Tag:  graphqlTag(tag),
				Type: structField.Type,
			}}),
		}}),
	}})

	conn := value.FieldByIndex(structField.Index)
	nodes := conn.FieldByName("Nodes")
	page := conn.FieldByName("PageInfo").Addr().Interface().(*ConnectionPage)
	for {
		more, cursor := page.HasNextPage, page.EndCursor
		if backwards {
			more, cursor = page.HasPreviousPage, page.StartCursor
		}
		if !more {
			return nil
		}
		result := reflect.New(queryType)
		variables := map[string]any{
			"url":    githubv4.URI{URL: resourceUrl},
			"cursor": graphql.String(cursor),
		}
		if err := client.Query("Fetch"+typeName+field, result.Interface(), variables); err != nil {
			return err
		}
		got := result.Elem().Field(0).Field(0).Field(0)
		if backwards {
			nodes.Set(reflect.AppendSlice(got.FieldByName("Nodes"), nodes))
		} else {
			nodes.Set(reflect.AppendSlice(nodes, got.FieldByName("Nodes")))
		}
		*page = got.FieldByName("PageInfo").Interface().(ConnectionPage)
	}
}

func graphqlTag(value string) reflect.StructTag {
	return reflect.StructTag("graphql:" + strconv.Quote(value))
}

// fetchRestOfThreadComments fetches the comments of the review thread with
// id that comments, its first page, doesn't have.
func fetchRestOfThreadComments(id string, comments *ReviewComments) error {
	for comments.PageInfo.HasNextPage {
		var queryResult struct {
			Node struct {
				Thread struct {
					Comments ReviewComments `graphql:"comments(first: 100, after: $cursor)"`
				} `graphql:"... on PullRequestReviewThread"`
			} `graphql:"node(id: $id)"`
		}
		variables := map[string]any{
			"id":     graphql.ID(id),
			"cursor": graphql.String(comments.PageInfo.EndCursor),
		}
		if err := client.Query("FetchReviewThreadComments", &queryResult, variables); err != nil {
			return err
		}
		more := queryResult.Node.Thread.Comments
		comments.Nodes = append(comments.Nodes, more.Nodes...)
		comments.PageInfo = more.PageInfo
	}
	return nil
}
