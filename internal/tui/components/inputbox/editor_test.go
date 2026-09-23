package inputbox

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEditorCommand(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	require.Equal(t, []string{"vi"}, editorCommand())

	t.Setenv("EDITOR", "code --wait")
	require.Equal(t, []string{"code", "--wait"}, editorCommand())

	t.Setenv("VISUAL", "nvim")
	require.Equal(t, []string{"nvim"}, editorCommand())
}

func TestReadEditorResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "comment.md")
	require.NoError(t, os.WriteFile(path, []byte("hello\n\nworld\n"), 0o600))

	value, err := readEditorResult(editorFinishedMsg{path: path})
	require.NoError(t, err)
	require.Equal(t, "hello\n\nworld", value)

	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err), "temp file should be removed")
}
