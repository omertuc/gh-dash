package common

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/google/shlex"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
)

// DiffPR opens a diff view for a PR using the gh CLI.
// The env parameter should be the result of Config.GetFullScreenDiffPagerEnv().
func DiffPR(prNumber int, repoName string, env []string) tea.Cmd {
	c := exec.Command(
		"gh",
		"pr",
		"diff",
		fmt.Sprint(prNumber),
		"-R",
		repoName,
	)
	c.Env = env

	return keys.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return constants.ErrMsg{Err: err}
		}
		return nil
	})
}

// PageDiff shows a diff in the diff pager, the way DiffPR shows a PR's. The
// pager parameter should be the result of Config.GetDiffPager().
func PageDiff(diff string, pager string) tea.Cmd {
	args, err := shlex.Split(pager)
	if err == nil && len(args) == 0 {
		err = fmt.Errorf("it's empty")
	}
	if err != nil {
		return func() tea.Msg {
			return constants.ErrMsg{Err: fmt.Errorf("can't run the diff pager %q: %w", pager, err)}
		}
	}
	c := exec.Command(args[0], args[1:]...)
	c.Env = append(os.Environ(), "LESS=CRX")
	c.Stdin = strings.NewReader(diff)

	return keys.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return constants.ErrMsg{Err: err}
		}
		return nil
	})
}
