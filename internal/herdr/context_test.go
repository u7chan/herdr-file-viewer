package herdr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRootUsesFocusedPaneBeforeWorkspaceAndProcess(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	focused := filepath.Join(dir, "focused")
	workspace := filepath.Join(dir, "workspace")
	mustMkdir(t, focused)
	mustMkdir(t, workspace)

	got, err := ResolveRootAt(dir, lookup(map[string]string{
		contextJSONEnv: `{"focused_pane_cwd":"` + focused + `","workspace_cwd":"` + workspace + `"}`,
	}))
	if err != nil {
		t.Fatalf("ResolveRootAt() error = %v", err)
	}
	if got.Path != focused || got.Source != FocusedPaneRoot {
		t.Fatalf("ResolveRootAt() = %#v, want focused root %q", got, focused)
	}
	if got.Warning != "" {
		t.Fatalf("ResolveRootAt() warning = %q, want empty", got.Warning)
	}
}

func TestResolveRootResolvesRelativeContextAgainstProcessCWD(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	focused := filepath.Join(dir, "focused")
	mustMkdir(t, focused)

	got, err := ResolveRootAt(dir, lookup(map[string]string{
		contextJSONEnv: `{"focused_pane_cwd":"focused"}`,
	}))
	if err != nil {
		t.Fatalf("ResolveRootAt() error = %v", err)
	}
	if got.Path != focused || got.Source != FocusedPaneRoot {
		t.Fatalf("ResolveRootAt() = %#v, want focused root %q", got, focused)
	}
}

func TestResolveRootFallsBackFromNonDirectoryToWorkspace(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-directory")
	workspace := filepath.Join(dir, "workspace")
	mustWriteFile(t, file)
	mustMkdir(t, workspace)

	got, err := ResolveRootAt(dir, lookup(map[string]string{
		contextJSONEnv: `{"focused_pane_cwd":"` + file + `","workspace_cwd":"` + workspace + `"}`,
	}))
	if err != nil {
		t.Fatalf("ResolveRootAt() error = %v", err)
	}
	if got.Path != workspace || got.Source != WorkspaceRoot {
		t.Fatalf("ResolveRootAt() = %#v, want workspace root %q", got, workspace)
	}
	if !strings.Contains(got.Warning, "not a directory") {
		t.Fatalf("ResolveRootAt() warning = %q, want non-directory reason", got.Warning)
	}
}

func TestResolveRootFallsBackToProcessCWDWhenContextMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	got, err := ResolveRootAt(dir, lookup(nil))
	if err != nil {
		t.Fatalf("ResolveRootAt() error = %v", err)
	}
	if got.Path != dir || got.Source != ProcessRoot {
		t.Fatalf("ResolveRootAt() = %#v, want process root %q", got, dir)
	}
	if !strings.Contains(got.Warning, "missing") {
		t.Fatalf("ResolveRootAt() warning = %q, want missing-context reason", got.Warning)
	}
}

func TestResolveRootFallsBackToProcessCWDWhenContextJSONIsInvalid(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	got, err := ResolveRootAt(dir, lookup(map[string]string{
		contextJSONEnv: "not json",
	}))
	if err != nil {
		t.Fatalf("ResolveRootAt() error = %v", err)
	}
	if got.Path != dir || got.Source != ProcessRoot {
		t.Fatalf("ResolveRootAt() = %#v, want process root %q", got, dir)
	}
	if !strings.Contains(got.Warning, "invalid JSON") {
		t.Fatalf("ResolveRootAt() warning = %q, want invalid-JSON reason", got.Warning)
	}
}

func TestResolveRootFallsBackToProcessCWDWhenContextDirectoriesAreUnavailable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	got, err := ResolveRootAt(dir, lookup(map[string]string{
		contextJSONEnv: `{"focused_pane_cwd":"` + missing + `","workspace_cwd":"` + missing + `"}`,
	}))
	if err != nil {
		t.Fatalf("ResolveRootAt() error = %v", err)
	}
	if got.Path != dir || got.Source != ProcessRoot {
		t.Fatalf("ResolveRootAt() = %#v, want process root %q", got, dir)
	}
	if !strings.Contains(got.Warning, "unavailable") {
		t.Fatalf("ResolveRootAt() warning = %q, want unavailable reason", got.Warning)
	}
}

func lookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir(%q): %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("file"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
