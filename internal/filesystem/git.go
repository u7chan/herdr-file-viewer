package filesystem

import (
	"bytes"
	"os/exec"
	"path/filepath"
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

// GitIgnoreReader is an optional filesystem capability for .gitignore
// matching, kept separate from GitStatusReader so deterministic callers can
// opt into each subprocess independently.
type GitIgnoreReader interface {
	ReadGitIgnore(path string, candidates []string) ([]string, error)
}

// GitStatusReader is an optional filesystem capability. FileSystem stays
// focused on directory reads so deterministic callers that do not model Git
// can continue to implement it without a subprocess dependency.
type GitStatusReader interface {
	ReadGitStatus(path string) ([]GitStatusEntry, error)
}

// WorktreeInfo is the branch and linked-worktree snapshot shown on the
// sticky Git info line. Branch and ShortSHA are mutually exclusive: Branch
// is the current branch name, ShortSHA the abbreviated HEAD used in place of
// the branch when the checkout is detached. IsLinked reports that the
// directory is a linked worktree rather than the main checkout, in which
// case RepoName is the repository display name (the main checkout directory
// name, mirroring Herdr's label).
type WorktreeInfo struct {
	Branch   string
	ShortSHA string
	RepoName string
	IsLinked bool
}

// GitWorktreeReader is an optional filesystem capability for branch and
// worktree lookup, kept separate from GitStatusReader so deterministic
// callers can opt into each subprocess independently.
type GitWorktreeReader interface {
	ReadWorktreeInfo(path string) (WorktreeInfo, error)
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

// ReadWorktreeInfo obtains the branch and linked-worktree state of a working
// tree root. A command error means the directory is not a usable Git working
// tree (or Git is unavailable), and callers may hide the info line entirely.
func (Local) ReadWorktreeInfo(path string) (WorktreeInfo, error) {
	branch, err := runGitIn(path, "branch", "--show-current")
	if err != nil {
		return WorktreeInfo{}, err
	}

	info := WorktreeInfo{Branch: strings.TrimSpace(string(branch))}
	if info.Branch == "" {
		sha, shaErr := runGitIn(path, "rev-parse", "--short", "HEAD")
		if shaErr == nil {
			info.ShortSHA = strings.TrimSpace(string(sha))
		}
		// A detached HEAD without any commit has no abbreviated SHA; the
		// branch slot stays empty so callers render whatever remains.
	}

	porcelain, err := runGitIn(path, "worktree", "list", "--porcelain")
	if err != nil {
		return WorktreeInfo{}, err
	}
	paths := parseWorktreePorcelain(porcelain)
	if len(paths) == 0 {
		return info, nil
	}

	for index, worktreePath := range paths {
		if !worktreePathMatches(worktreePath, path) {
			continue
		}
		if index == 0 {
			// The first record is the main checkout: no worktree column.
			return info, nil
		}
		info.IsLinked = true
		info.RepoName = filepath.Base(paths[0])
		return info, nil
	}
	// No porcelain entry matches the directory (for example the root is a
	// subdirectory of a worktree), so the worktree column stays empty.
	return info, nil
}

func runGitIn(path string, args ...string) ([]byte, error) {
	command := exec.Command("git", args...)
	command.Dir = path
	return command.Output()
}

// parseWorktreePorcelain returns the worktree paths in listing order. Every
// record begins with a "worktree <path>" attribute, so the first path is the
// main checkout and later paths are linked worktrees; the remaining
// attributes (HEAD, branch, detached, bare, ...) are skipped.
func parseWorktreePorcelain(output []byte) []string {
	var paths []string
	for _, line := range strings.Split(string(output), "\n") {
		if rest, ok := strings.CutPrefix(line, "worktree "); ok {
			paths = append(paths, strings.TrimSpace(rest))
		}
	}
	return paths
}

// worktreePathMatches reports whether a porcelain worktree path refers to
// the same directory as root. Symlinks on either side are resolved so a
// launch root reached through a symlink still matches its working tree.
func worktreePathMatches(worktreePath, root string) bool {
	candidates := []string{filepath.Clean(worktreePath)}
	if resolved, err := filepath.EvalSymlinks(worktreePath); err == nil {
		candidates = append(candidates, filepath.Clean(resolved))
	}
	targets := []string{filepath.Clean(root)}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		targets = append(targets, filepath.Clean(resolved))
	}
	for _, candidate := range candidates {
		for _, target := range targets {
			if candidate == target {
				return true
			}
		}
	}
	return false
}

// ReadGitIgnore reports which of the candidate paths (relative to path, the
// working-tree root) match .gitignore rules. Tracked files never match,
// which is the same behavior as VSCode's git.isIgnored. A non-working-tree
// directory (or an absent Git executable) yields an error that callers may
// treat as "no ignore rules"
func (Local) ReadGitIgnore(path string, candidates []string) ([]string, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	command := exec.Command("git", "check-ignore", "--stdin", "-z", "--")
	command.Dir = path
	command.Stdin = strings.NewReader(strings.Join(candidates, "\x00") + "\x00")
	output, err := command.Output()
	if err != nil {
		// Exit code 1 means nothing matched; every other failure means the
		// directory is not a usable Git working tree.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}

	return parseGitIgnoreOutput(output), nil
}

func parseGitIgnoreOutput(output []byte) []string {
	output = bytes.TrimSuffix(output, []byte{0})
	if len(output) == 0 {
		return nil
	}
	records := bytes.Split(output, []byte{0})
	matches := make([]string, 0, len(records))
	for _, record := range records {
		if len(record) > 0 {
			matches = append(matches, string(record))
		}
	}
	return matches
}
