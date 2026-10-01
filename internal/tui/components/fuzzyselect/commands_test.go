package fuzzyselect

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

func newCommandSource() *CommandSource {
	return &CommandSource{
		Commands: []Suggestion{{Value: "/lgtm"}, {Value: "/test e2e-aws"}},
		Fallback: &UserMentionSource{WithAtSymbol: true, Users: []data.User{{Login: "alice"}}},
	}
}

func TestCommandSourceSuggestsCommandsOnSlashLines(t *testing.T) {
	src := newCommandSource()
	input := "thanks!\n  /test e2"
	cursor := tea.Position{X: 10, Y: 1}

	require.True(t, src.InCommand(input, cursor))
	ctx := src.ExtractContext(input, cursor)
	require.Equal(t, Context{
		Start:   tea.Position{X: 2, Y: 1},
		End:     tea.Position{X: 10, Y: 1},
		Content: "/test e2",
	}, ctx)
	require.Equal(t, src.Commands, src.Suggestions(input, ctx.Start))
	require.Nil(t, src.ItemsToExclude(input, cursor))

	value, pos := src.InsertSuggestion(input, "/test e2e-aws", ctx.Start, ctx.End)
	require.Equal(t, "thanks!\n  /test e2e-aws", value)
	require.Equal(t, tea.Position{X: 15, Y: 1}, pos)
}

func TestCommandSourceFallsBackElsewhere(t *testing.T) {
	src := newCommandSource()
	input := "/lgtm\nping @al"
	cursor := tea.Position{X: 8, Y: 1}

	require.False(t, src.InCommand(input, cursor))
	ctx := src.ExtractContext(input, cursor)
	require.Equal(t, "al", ctx.Content)
	require.Equal(t, []Suggestion{{Value: "alice"}}, src.Suggestions(input, ctx.Start))

	value, _ := src.InsertSuggestion(input, "alice", ctx.Start, ctx.End)
	require.Equal(t, "/lgtm\nping @alice ", value)
}

func TestCommandSourceIgnoresSlashesMidLine(t *testing.T) {
	src := newCommandSource()
	input := "see a/b"

	require.False(t, src.InCommand(input, tea.Position{X: 7}))
	require.Equal(t, Context{}, src.ExtractContext(input, tea.Position{X: 7}))
}

func TestCommandSourceWithoutCommandsIsItsFallback(t *testing.T) {
	src := newCommandSource()
	src.Commands = nil

	require.False(t, src.InCommand("/lgtm", tea.Position{X: 5}))
	require.Equal(t, Context{}, src.ExtractContext("/lgtm", tea.Position{X: 5}))
}
