package main

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// procState reports the current state of pid by reading /proc/<pid>/stat
// on Linux: "gone" once the process has been reaped and its slot released,
// otherwise the process state letter (e.g. "S" for sleeping, "Z" for a
// zombie).
func procState(pid int) string {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "gone"
	}
	// The command name in parentheses may contain spaces, so the state is
	// the first field after the last closing parenthesis.
	text := string(data)
	close := strings.LastIndexByte(text, ')')
	if close < 0 || close+2 >= len(text) {
		return "unknown"
	}
	return text[close+2 : close+3]
}

func TestDefaultActionRunnerReapsFinishedChild(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("reaping check reads /proc/<pid>/stat")
	}
	cmd := exec.Command("bash", "-lic", "exit 0")
	if err := launchDefaultAction(cmd); err != nil {
		t.Fatalf("launchDefaultAction() error = %v", err)
	}
	pid := cmd.Process.Pid

	// The child must vanish from /proc shortly after it exits: without the
	// background Wait it would linger in state Z until the viewer exits, so
	// the loop only terminates on "gone".
	deadline := time.Now().Add(10 * time.Second)
	for {
		state := procState(pid)
		if state == "gone" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("child %d still in state %q after exit; finished children must be reaped, not left as zombies", pid, state)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
