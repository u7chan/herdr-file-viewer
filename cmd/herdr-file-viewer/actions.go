package main

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/u7chan/herdr-file-viewer/internal/app"
)

// defaultActionRunner starts default-action commands in the user's
// interactive shell environment (bash -lic, so aliases and functions from
// ~/.bashrc and its sources like ~/.config/workstation/shell/init.bash
// resolve) with setsid and all standard streams redirected to /dev/null.
// The new session detaches the child from the TUI's process group and the
// stream redirection keeps its output off the alternate screen, so the
// viewer stays responsive and uncorrupted while the launched program runs
// or fails on its own. Hand-edited command strings are evaluated by the
// shell by design (documented in the README).
type defaultActionRunner struct{}

func (defaultActionRunner) Run(command string) error {
	cmd := exec.Command("bash", "-lic", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() { _ = devNull.Close() }()
	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	return cmd.Start()
}

// newDefaultActionRunner wires the detached bash -lic launcher. It is a
// function so composition tests can identify the wired capability without
// starting processes.
func newDefaultActionRunner() app.DefaultActionRunner {
	return defaultActionRunner{}
}