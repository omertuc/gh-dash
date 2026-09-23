package common

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
)

// RunCmdInDir runs a command in dir. On failure, the returned error carries
// the last line the command wrote to stderr, which usually explains why.
func RunCmdInDir(dir string, name string, args ...string) error {
	var stderr bytes.Buffer
	c := exec.Command(name, args...)
	c.Dir = dir
	c.Stderr = &stderr
	err := c.Run()
	if err == nil {
		return nil
	}

	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
		return errors.New(last)
	}
	return err
}
