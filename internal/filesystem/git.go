package filesystem

import (
	"bytes"
	"os/exec"
	"strings"
)

// GitStatus identifies the subset of porcelain statuses shown by the viewer.
type GitStatus uint8

const (
	GitStatusNone GitStatus = iota
	GitStatusModified
	GitStatusUntracked
	GitStatusAdded
	GitStatusUnmerged
	GitStatusDeleted
)

// GitStatusEntry associates a working-tree status with a path. Path is
// relative to the directory supplied to GitStatusReader.
type GitStatusEntry struct {
	Path   string
	Status GitStatus
}

// GitStatusReader is an optional filesystem capability. FileSystem stays
// focused on directory reads so deterministic callers that do not model Git
// can continue to implement it without a subprocess dependency.
type GitStatusReader interface {
	ReadGitStatus(path string) ([]GitStatusEntry, error)
}

// ReadGitStatus obtains one NUL-delimited porcelain snapshot. A command error
// means that the directory is not a usable Git working tree (or Git is not
// available), and callers may safely treat that as no statuses.
func (Local) ReadGitStatus(path string) ([]GitStatusEntry, error) {
	command := exec.Command(
		"git",
		"-c", "status.relativePaths=true",
		"status",
		"--porcelain=v1",
		"--untracked-files=all",
		"-z",
		"--",
	)
	command.Dir = path
	output, err := command.Output()
	if err != nil {
		return nil, err
	}
	return parseGitStatusPorcelain(output), nil
}

func parseGitStatusPorcelain(output []byte) []GitStatusEntry {
	if len(output) == 0 {
		return nil
	}

	records := bytes.Split(output, []byte{0})
	entries := make([]GitStatusEntry, 0, len(records))
	for index := 0; index < len(records); index++ {
		record := records[index]
		if len(record) < 4 || record[2] != ' ' {
			continue
		}

		status := gitStatusForXY(record[0], record[1])
		if status == GitStatusNone {
			continue
		}

		path := string(record[3:])
		if path == "" {
			continue
		}
		path = strings.TrimSuffix(path, "/")
		entries = append(entries, GitStatusEntry{Path: path, Status: status})

		// With -z, a rename/copy record contains the new path first and the
		// old path in the following NUL-delimited record. The new path is the
		// entry that can be displayed in the current tree.
		if record[0] == 'R' || record[0] == 'C' {
			index++
		}
	}
	return entries
}

func gitStatusForXY(index, worktree byte) GitStatus {
	if index == 'U' || worktree == 'U' || (index == 'D' && worktree == 'D') || (index == 'A' && worktree == 'A') {
		return GitStatusUnmerged
	}
	if index == 'D' || worktree == 'D' {
		return GitStatusDeleted
	}
	if index == 'A' || worktree == 'A' {
		return GitStatusAdded
	}
	if index == 'M' || worktree == 'M' || index == 'R' || worktree == 'R' || index == 'C' || worktree == 'C' || index == 'T' || worktree == 'T' {
		return GitStatusModified
	}
	if index == '?' && worktree == '?' {
		return GitStatusUntracked
	}
	return GitStatusNone
}
