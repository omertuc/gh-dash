// Package plugins lets optional features hook into gh-dash. Plugins are
// built in and registered by name, but stay off until the user opts into
// them in their config:
//
//	plugins:
//	  prow:
//	    enabled: true
//	    options: {}
//
// A plugin implements Plugin and any of the capability interfaces that it
// needs: CommentCommander, PanelProvider, CheckOpener and CheckCommander.
package plugins

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
)

// Plugin is a feature that can be opted into.
type Plugin interface {
	// Name is what the plugin is enabled by in the config, e.g. "prow"
	Name() string
	// Configure applies the plugin's options from the config. It's called
	// once the plugin is enabled, before it's asked for anything.
	Configure(options map[string]any) error
}

// CommentCommander offers commands to autocomplete while writing a comment
// on a PR, e.g. "/lgtm". They're suggested when a line starts with "/".
type CommentCommander interface {
	CommentCommands(pr PR) []CommentCommand
}

// CommentCommand is a command offered while writing a comment.
type CommentCommand struct {
	// Text is what's inserted, e.g. "/test e2e-aws"
	Text string
	// Description is shown next to it, e.g. "Run the e2e-aws job"
	Description string
}

// PanelProvider shows panels in a PR's overview, e.g. who approved it.
type PanelProvider interface {
	Panels(pr PR) []Panel
}

// CheckOpener opens some of a PR's checks in a tab of their own, rather than
// in the browser, e.g. to browse a CI job's test results.
type CheckOpener interface {
	// CheckAction reports whether the plugin opens the check, along with what
	// opening it does, e.g. "view tests", shown as a hint.
	CheckAction(check Check) (label string, ok bool)
	// OpenCheck returns the view shown in the check's tab.
	OpenCheck(pr PR, check Check) View
}

// CheckCommander offers commands about one of a PR's checks, e.g. "/test
// e2e-aws" to rerun its job. They're offered while the check is focused in
// the PR's checks tab, and posted as a comment on the PR once picked.
type CheckCommander interface {
	CheckCommands(pr PR, check Check) []CheckCommand
}

// CheckCommand is a command offered on a check.
type CheckCommand struct {
	// Key picks the command once the check's commands are shown, e.g. "t"
	Key string
	// Label is what the command does, shown after its key, e.g. "rerun"
	Label string
	// Comment is what's posted on the PR, e.g. "/test e2e-aws"
	Comment string
	// Confirm asks before posting the comment, for commands that are hard
	// to take back, e.g. overriding a status
	Confirm bool
}

var (
	mu        sync.RWMutex
	factories = map[string]func() Plugin{}
	enabled   []Plugin
)

// Register makes a plugin available to be enabled by name. It's meant to be
// called from the plugin's init.
func Register(name string, factory func() Plugin) {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := factories[name]; ok {
		panic(fmt.Sprintf("plugin %q registered twice", name))
	}
	factories[name] = factory
}

// Available lists the names of the plugins that can be enabled.
func Available() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(factories))
	for name := range factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Configure enables the plugins the config opts into, replacing those enabled
// before. Plugins that fail to configure, or that don't exist, are left out
// and reported in the error, while the rest are still enabled.
func Configure(cfg map[string]config.PluginConfig) error {
	mu.Lock()
	defer mu.Unlock()

	names := make([]string, 0, len(cfg))
	for name := range cfg {
		names = append(names, name)
	}
	sort.Strings(names)

	var errs []string
	enabled = nil
	for _, name := range names {
		if !cfg[name].Enabled {
			continue
		}
		factory, ok := factories[name]
		if !ok {
			errs = append(errs, fmt.Sprintf("unknown plugin %q", name))
			continue
		}
		p := factory()
		if err := p.Configure(cfg[name].Options); err != nil {
			errs = append(errs, fmt.Sprintf("plugin %q: %v", name, err))
			continue
		}
		enabled = append(enabled, p)
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// Enabled returns the plugins the user opted into.
func Enabled() []Plugin {
	mu.RLock()
	defer mu.RUnlock()
	return slices.Clone(enabled)
}

// CommentCommands gathers the commands the enabled plugins offer while
// writing a comment on pr.
func CommentCommands(pr PR) []CommentCommand {
	var commands []CommentCommand
	for _, p := range Enabled() {
		if c, ok := p.(CommentCommander); ok {
			commands = append(commands, c.CommentCommands(pr)...)
		}
	}
	return commands
}

// Panels gathers the panels the enabled plugins show in pr's overview.
func Panels(pr PR) []Panel {
	var panels []Panel
	for _, p := range Enabled() {
		if pp, ok := p.(PanelProvider); ok {
			panels = append(panels, pp.Panels(pr)...)
		}
	}
	return panels
}

// CheckOpenerFor returns the enabled plugin that opens check, if any, along
// with what opening it does.
func CheckOpenerFor(check Check) (CheckOpener, string, bool) {
	for _, p := range Enabled() {
		if o, ok := p.(CheckOpener); ok {
			if label, ok := o.CheckAction(check); ok {
				return o, label, true
			}
		}
	}
	return nil, "", false
}

// CheckCommands gathers the commands the enabled plugins offer on pr's
// check. A key picks the first command offered with it.
func CheckCommands(pr PR, check Check) []CheckCommand {
	var commands []CheckCommand
	seen := map[string]bool{}
	for _, p := range Enabled() {
		c, ok := p.(CheckCommander)
		if !ok {
			continue
		}
		for _, command := range c.CheckCommands(pr, check) {
			if command.Key == "" || seen[command.Key] {
				continue
			}
			seen[command.Key] = true
			commands = append(commands, command)
		}
	}
	return commands
}

// OpenURLMsg asks gh-dash to open a URL in the browser. Views return it from
// their commands.
type OpenURLMsg struct {
	URL string
}

// OpenURL returns a command asking gh-dash to open url in the browser.
func OpenURL(url string) tea.Cmd {
	return func() tea.Msg { return OpenURLMsg{URL: url} }
}

// WrapCmd wraps the messages cmd produces with wrap, including those of
// batched commands, so they find their way back to where cmd came from.
// Commands sequenced with tea.Sequence can't be wrapped, so views mustn't
// use it.
func WrapCmd(cmd tea.Cmd, wrap func(tea.Msg) tea.Msg) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := cmd()
		switch msg := msg.(type) {
		case nil:
			return nil
		case tea.BatchMsg:
			wrapped := make([]tea.Cmd, 0, len(msg))
			for _, c := range msg {
				wrapped = append(wrapped, WrapCmd(c, wrap))
			}
			return tea.BatchMsg(wrapped)
		case OpenURLMsg:
			// Meant for gh-dash itself
			return msg
		default:
			return wrap(msg)
		}
	}
}
