package plugins

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
)

type fakePlugin struct {
	name    string
	options map[string]any
}

func (p *fakePlugin) Name() string { return p.name }

func (p *fakePlugin) Configure(options map[string]any) error {
	if options["fail"] == true {
		return errors.New("bad options")
	}
	p.options = options
	return nil
}

func (p *fakePlugin) CommentCommands(pr PR) []CommentCommand {
	return []CommentCommand{{Text: "/" + p.name}}
}

func registerFake(t *testing.T, name string) {
	t.Helper()
	Register(name, func() Plugin { return &fakePlugin{name: name} })
	t.Cleanup(func() {
		mu.Lock()
		delete(factories, name)
		enabled = nil
		mu.Unlock()
	})
}

func TestPluginsAreOptIn(t *testing.T) {
	registerFake(t, "a")
	registerFake(t, "b")

	require.NoError(t, Configure(nil))
	require.Empty(t, Enabled())
	require.Empty(t, CommentCommands(PR{}))

	require.NoError(t, Configure(map[string]config.PluginConfig{
		"a": {Enabled: true, Options: map[string]any{"x": 1}},
		"b": {Enabled: false},
	}))
	require.Len(t, Enabled(), 1)
	require.Equal(t, "a", Enabled()[0].Name())
	require.Equal(t, map[string]any{"x": 1}, Enabled()[0].(*fakePlugin).options)
	require.Equal(t, []CommentCommand{{Text: "/a"}}, CommentCommands(PR{}))
}

func TestConfigureReportsBadPluginsButEnablesTheRest(t *testing.T) {
	registerFake(t, "a")
	registerFake(t, "b")

	err := Configure(map[string]config.PluginConfig{
		"a":       {Enabled: true, Options: map[string]any{"fail": true}},
		"b":       {Enabled: true},
		"missing": {Enabled: true},
	})

	require.ErrorContains(t, err, `plugin "a": bad options`)
	require.ErrorContains(t, err, `unknown plugin "missing"`)
	require.Len(t, Enabled(), 1)
	require.Equal(t, "b", Enabled()[0].Name())
}

type wrapped struct{ msg tea.Msg }

func TestWrapCmd(t *testing.T) {
	wrap := func(msg tea.Msg) tea.Msg { return wrapped{msg} }
	require.Nil(t, WrapCmd(nil, wrap))

	msg := WrapCmd(func() tea.Msg { return "hi" }, wrap)()
	require.Equal(t, wrapped{"hi"}, msg)

	// Meant for gh-dash, so it's left as is
	msg = WrapCmd(OpenURL("https://example.com"), wrap)()
	require.Equal(t, OpenURLMsg{URL: "https://example.com"}, msg)

	batch := WrapCmd(tea.Batch(func() tea.Msg { return 1 }, func() tea.Msg { return 2 }), wrap)()
	cmds, ok := batch.(tea.BatchMsg)
	require.True(t, ok)
	require.Len(t, cmds, 2)
	require.Equal(t, wrapped{1}, cmds[0]())
	require.Equal(t, wrapped{2}, cmds[1]())
}
