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
	return launchDefaultAction(exec.Command("bash", "-lic", command))
}

// launchDefaultAction starts cmd detached from the viewer and reaps it in
// the background: the child gets its own session (setsid), every stream
// goes to /dev/null, and Wait runs on a separate goroutine so a finished
// child releases its process-table entry instead of lingering as a zombie
// in the long-lived viewer. Wait is never observable by the TUI — the
// child is in its own session and its streams are already redirected — so
// the detach behavior is unchanged.
func launchDefaultAction(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() { _ = devNull.Close() }()
	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// newDefaultActionRunner wires the detached bash -lic launcher. It is a
// function so composition tests can identify the wired capability without
// starting processes.
func newDefaultActionRunner() app.DefaultActionRunner {
	return defaultActionRunner{}
}
