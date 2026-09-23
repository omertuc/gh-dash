package inputbox

import (
	"os"
	"os/exec"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

var openEditorKey = key.NewBinding(
	key.WithKeys("ctrl+o"),
	key.WithHelp("Ctrl+o", "open in $EDITOR"),
)

type editorFinishedMsg struct {
	path string
	err  error
}

// editorCommand returns the user's preferred editor, split into its arguments
// so values like "code --wait" work.
func editorCommand() []string {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if fields := strings.Fields(os.Getenv(env)); len(fields) > 0 {
			return fields
		}
	}
	return []string{"vi"}
}

// openInEditor writes value to a temporary markdown file and opens it in the
// user's editor, suspending the TUI until the editor exits.
func openInEditor(value string) tea.Cmd {
	f, err := os.CreateTemp("", "gh-dash-*.md")
	if err != nil {
		return func() tea.Msg { return editorFinishedMsg{err: err} }
	}
	path := f.Name()
	_, err = f.WriteString(value)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
		return func() tea.Msg { return editorFinishedMsg{err: err} }
	}

	args := append(editorCommand(), path)
	c := exec.Command(args[0], args[1:]...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{path: path, err: err}
	})
}

// readEditorResult reads back the file the editor saved and removes it.
func readEditorResult(msg editorFinishedMsg) (string, error) {
	if msg.path == "" {
		return "", msg.err
	}
	defer os.Remove(msg.path)
	if msg.err != nil {
		return "", msg.err
	}
	content, err := os.ReadFile(msg.path)
	if err != nil {
		return "", err
	}
	// Editors usually add a trailing newline on save
	return strings.TrimSuffix(string(content), "\n"), nil
}
