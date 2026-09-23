package tui

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cli/go-gh/v2/pkg/browser"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

// openUrlInBrowser opens the given URL in the browser, e.g. a check's page.
func (m *Model) openUrlInBrowser(url string) tea.Cmd {
	taskId := fmt.Sprintf("open_browser_%d", time.Now().Unix())
	task := context.Task{
		Id:           taskId,
		StartText:    "Opening in browser",
		FinishedText: "Opened in browser",
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := m.ctx.StartTask(task)
	openCmd := func() tea.Msg {
		// Discard the launcher's output, as openBrowser does
		b := browser.New("", io.Discard, io.Discard)
		err := b.Browse(url)
		return constants.TaskFinishedMsg{TaskId: taskId, Err: err}
	}
	return tea.Batch(startCmd, openCmd)
}

func (m *Model) openBrowser() tea.Cmd {
	taskId := fmt.Sprintf("open_browser_%d", time.Now().Unix())
	task := context.Task{
		Id:           taskId,
		StartText:    "Opening in browser",
		FinishedText: "Opened in browser",
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := m.ctx.StartTask(task)
	openCmd := func() tea.Msg {
		// Discard the launcher's stdout/stderr so any noise (e.g. GTK / GVFS
		// warnings from xdg-open / gnome-open) does not leak into the TUI's
		// terminal and corrupt the display. See #829, #584, #679.
		b := browser.New("", io.Discard, io.Discard)
		currRow := m.getCurrRowData()
		if currRow == nil || reflect.ValueOf(currRow).IsNil() {
			return constants.TaskFinishedMsg{
				TaskId: taskId,
				Err:    errors.New("current selection doesn't have a URL"),
			}
		}
		err := b.Browse(currRow.GetUrl())
		return constants.TaskFinishedMsg{TaskId: taskId, Err: err}
	}
	return tea.Batch(startCmd, openCmd)
}
