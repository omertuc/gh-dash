package tasks

import (
	"slices"
)

// deleteCommentMutation deletes a PR's or Issue's comment, both being issue
// comments to GitHub
const deleteCommentMutation = `mutation($id: ID!) {
	deleteIssueComment(input: {id: $id}) { clientMutationId }
}`

// deleteCommentArgs are the gh arguments deleting the comment with the given
// id
func deleteCommentArgs(id string) []string {
	return []string{"api", "graphql", "-f", "query=" + deleteCommentMutation, "-f", "id=" + id}
}

// WithoutCommentId returns comments without the one with the given id, e.g. a
// deleted comment, leaving comments itself as is since it may be shared
func WithoutCommentId[T interface{ GetId() string }](comments []T, id string) []T {
	return slices.DeleteFunc(slices.Clone(comments), func(c T) bool {
		return c.GetId() == id
	})
}
