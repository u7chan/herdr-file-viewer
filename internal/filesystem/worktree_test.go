package filesystem

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseWorktreePorcelain(t *testing.T) {
	output := []byte("worktree /home/u/dev/repo\nHEAD 20a491bf1be1f2d61689e149f3072dbbc1071c46\nbranch refs/heads/main\n\nworktree /home/u/.herdr/worktrees/repo/worktree-brave-forest-6cd8\nHEAD 4c4b3bee853d708b8a073fc3435d98d9f258d620\nbranch refs/heads/feature\n\nworktree /home/u/dev/detached\nHEAD 4c4b3bee853d708b8a073fc3435d98d9f258d620\ndetached\n\n")
	want := []string{
		"/home/u/dev/repo",
		"/home/u/.herdr/worktrees/repo/worktree-brave-forest-6cd8",
		"/home/u/dev/detached",
	}
	if got := parseWorktreePorcelain(output); !reflect.DeepEqual(got, want) {
		t.Fatalf("parseWorktreePorcelain() = %#v, want %#v", got, want)
	}
}

func TestParseWorktreePorcelainIgnoresAttributeLines(t *testing.T) {
	output := []byte("worktree /main/path\nHEAD 0000000000000000000000000000000000000000\nbranch refs/heads/main\nbare\nlocked by lock file\nprunable gitdir file points to non-existent location\n\nworktree /linked/path\nHEAD 4c4b3bee853d708b8a073fc3435d98d9f258d620\ndetached\n")
	want := []string{"/main/path", "/linked/path"}
	if got := parseWorktreePorcelain(output); !reflect.DeepEqual(got, want) {
		t.Fatalf("parseWorktreePorcelain() = %#v, want %#v", got, want)
	}
}

func TestWorktreePathMatchesResolvesSymlinks(t *testing.T) {
	root := t.TempDir()
	worktreePath := filepath.Join(root, "worktree")
	realPath := filepath.Join(root, "real")
	linkPath := filepath.Join(root, "link")
	if err := exec.Command("mkdir", "-p", worktreePath, realPath).Run(); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := exec.Command("ln", "-s", realPath, linkPath).Run(); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	for _, test := range []struct {
		name         string
		worktreePath string
		root         string
		want         bool
	}{
		{name: "exact", worktreePath: worktreePath, root: worktreePath, want: true},
		{name: "slashed", worktreePath: worktreePath + string(filepath.Separator), root: worktreePath, want: true},
		{name: "different", worktreePath: worktreePath, root: realPath, want: false},
		{name: "symlinked root", worktreePath: realPath, root: linkPath, want: true},
		{name: "symlinked worktree path", worktreePath: linkPath, root: realPath, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := worktreePathMatches(test.worktreePath, test.root); got != test.want {
				t.Fatalf("worktreePathMatches(%q, %q) = %v, want %v", test.worktreePath, test.root, got, test.want)
			}
		})
	}
}

func TestReadWorktreeInfoMainDetachedAndLinked(t *testing.T) {
	git := requireGit(t)
	root := t.TempDir()
	mainPath := filepath.Join(root, "repo")
	if err := os.MkdirAll(mainPath, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", mainPath, err)
	}
	runGit(t, git, mainPath, "init", "-q", "-b", "main")
	runGit(t, git, mainPath, "config", "user.email", "test@example.com")
	runGit(t, git, mainPath, "config", "user.name", "Test")
	writeFile(t, filepath.Join(mainPath, "file.txt"))
	runGit(t, git, mainPath, "add", ".")
	runGit(t, git, mainPath, "commit", "-q", "-m", "init")

	fullSHA := strings.TrimSpace(runGitOutput(t, git, mainPath, "rev-parse", "HEAD"))

	info, err := (Local{}).ReadWorktreeInfo(mainPath)
	if err != nil {
		t.Fatalf("main ReadWorktreeInfo() error = %v", err)
	}
	if info.Branch != "main" || info.ShortSHA != "" || info.IsLinked || info.RepoName != "" {
		t.Fatalf("main info = %#v, want branch main and no worktree column", info)
	}

	linkedPath := filepath.Join(root, "linked")
	runGit(t, git, mainPath, "worktree", "add", "-q", "-b", "feature", linkedPath)
	info, err = (Local{}).ReadWorktreeInfo(linkedPath)
	if err != nil {
		t.Fatalf("linked ReadWorktreeInfo() error = %v", err)
	}
	if info.Branch != "feature" || !info.IsLinked {
		t.Fatalf("linked info = %#v, want branch feature and linked", info)
	}
	if want := filepath.Base(mainPath); info.RepoName != want {
		t.Fatalf("linked repo name = %q, want %q", info.RepoName, want)
	}

	runGit(t, git, linkedPath, "checkout", "-q", "--detach")
	info, err = (Local{}).ReadWorktreeInfo(linkedPath)
	if err != nil {
		t.Fatalf("detached ReadWorktreeInfo() error = %v", err)
	}
	if info.Branch != "" || info.ShortSHA == "" {
		t.Fatalf("detached info = %#v, want short SHA and no branch", info)
	}
	if !strings.HasPrefix(fullSHA, info.ShortSHA) {
		t.Fatalf("detached short SHA %q is not a prefix of %q", info.ShortSHA, fullSHA)
	}
	if !info.IsLinked || info.RepoName == "" {
		t.Fatalf("detached linked info = %#v, want worktree column", info)
	}
}

func TestReadWorktreeInfoUnbornBranchAndNonRepository(t *testing.T) {
	git := requireGit(t)
	unbornPath := filepath.Join(t.TempDir(), "unborn")
	if err := os.MkdirAll(unbornPath, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", unbornPath, err)
	}
	runGit(t, git, unbornPath, "init", "-q", "-b", "main")
	info, err := (Local{}).ReadWorktreeInfo(unbornPath)
	if err != nil {
		t.Fatalf("unborn ReadWorktreeInfo() error = %v", err)
	}
	if info.Branch != "main" || info.IsLinked {
		t.Fatalf("unborn info = %#v, want branch main and no worktree column", info)
	}

	nonRepository := t.TempDir()
	if _, err := (Local{}).ReadWorktreeInfo(nonRepository); err == nil {
		t.Fatal("non-repository ReadWorktreeInfo() = nil error, want error")
	}
}

// requireGit skips the test when no git executable is available so the rest
// of the suite stays deterministic on minimal environments.
func requireGit(t *testing.T) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git executable not available")
	}
	return git
}

func runGit(t *testing.T, git, dir string, args ...string) {
	t.Helper()
	command := exec.Command(git, args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %q: %v\n%s", args, dir, err, output)
	}
}

func runGitOutput(t *testing.T, git, dir string, args ...string) string {
	t.Helper()
	command := exec.Command(git, args...)
	command.Dir = dir
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %v in %q: %v", args, dir, err)
	}
	return string(output)
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := exec.Command("sh", "-c", "echo content > "+shellQuote(path)).Run(); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}
