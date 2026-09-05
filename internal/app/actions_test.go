package app

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

// stubActionRunner records every launched default-action command and can
// force a launch failure.
type stubActionRunner struct {
	commands []string
	err      error
}

func (s *stubActionRunner) Run(command string) error {
	s.commands = append(s.commands, command)
	return s.err
}

func TestDefaultActionCommandSubstitutesEveryTokenWithShellQuote(t *testing.T) {
	tests := []struct {
		name     string
		template string
		token    string
		path     string
		want     string
	}{
		{
			name:     "file token with spaces",
			template: "zed <filepath>",
			token:    actionPathTokenFile,
			path:     "/home/u/My Docs/a.txt",
			want:     "zed '/home/u/My Docs/a.txt'",
		},
		{
			name:     "folder token",
			template: "open <dirpath>",
			token:    actionPathTokenFolder,
			path:     "/home/u/My Docs",
			want:     "open '/home/u/My Docs'",
		},
		{
			name:     "embedded single quote",
			template: "zed <filepath>",
			token:    actionPathTokenFile,
			path:     "/home/u/it's/a b.txt",
			want:     "zed '/home/u/it'\\''s/a b.txt'",
		},
		{
			name:     "repeated token",
			template: "echo <filepath> <filepath>",
			token:    actionPathTokenFile,
			path:     "/a b",
			want:     "echo '/a b' '/a b'",
		},
		{
			name:     "no token leaves template untouched",
			template: "echo hello",
			token:    actionPathTokenFile,
			path:     "/a b",
			want:     "echo hello",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := defaultActionCommand(test.template, test.token, test.path); got != test.want {
				t.Fatalf("defaultActionCommand() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestShellQuoteRoundTripsThroughShellEvaluation(t *testing.T) {
	// The quoted fragment must evaluate back to the original bytes inside
	// the configured command when the interactive shell runs it.
	tests := []struct {
		path string
		want string
	}{
		{path: "/home/u/plain.txt", want: "'/home/u/plain.txt'"},
		{path: "/home/u/My Docs/a.txt", want: "'/home/u/My Docs/a.txt'"},
		{path: "/home/u/it's/a.txt", want: "'/home/u/it'\\''s/a.txt'"},
		{path: `"/here"`, want: `'"/here"'`},
		{path: "", want: "''"},
	}
	for _, test := range tests {
		if got := shellQuote(test.path); got != test.want {
			t.Fatalf("shellQuote(%q) = %q, want %q", test.path, got, test.want)
		}
	}
}

func TestCtrlEnterIsADistinctKeyFromEnter(t *testing.T) {
	if got := (tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}).String(); got != "ctrl+enter" {
		t.Fatalf("ctrl+enter String() = %q, want %q", got, "ctrl+enter")
	}
	if got := (tea.KeyPressMsg{Code: tea.KeyEnter}).String(); got != "enter" {
		t.Fatalf("enter String() = %q, want %q", got, "enter")
	}
}

// newActionModel builds a tree with one directory (docs) and one file
// (notes.md) below the root and completes the initial load, so the
// selection order is root, docs, notes.md.
func newActionModel(t *testing.T, actions DefaultActions) (*Model, *stubActionRunner) {
	t.Helper()
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{
		{Name: "docs", Mode: fs.ModeDir},
		{Name: "notes.md", Mode: 0},
	})
	fake.set(filepath.Join(root, "docs"), nil)
	runner := &stubActionRunner{}
	model := NewModelConfigured(root, "", ModelConfig{
		DefaultAction: runner,
		Preferences:   Preferences{Actions: actions},
	}, fake)
	completeInitialLoad(t, model)
	return model, runner
}

func TestCtrlEnterRunsFileActionOnFileSelection(t *testing.T) {
	model, runner := newActionModel(t, DefaultActions{File: "zed <filepath>"})
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown}) // notes.md

	cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+enter returned nil command")
	}
	cmd() // the program loop launches the action by running the command
	if len(runner.commands) != 1 {
		t.Fatalf("launched commands = %d, want 1", len(runner.commands))
	}
	selected := model.selectedNode().Path()
	want := "zed " + shellQuote(selected)
	if runner.commands[0] != want {
		t.Fatalf("launched command = %q, want %q", runner.commands[0], want)
	}
}

func TestCtrlEnterRunsFolderActionOnDirectorySelection(t *testing.T) {
	model, runner := newActionModel(t, DefaultActions{Folder: "open <dirpath>"})
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown}) // docs

	cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+enter returned nil command")
	}
	cmd()
	if len(runner.commands) != 1 {
		t.Fatalf("launched commands = %d, want 1", len(runner.commands))
	}
	selected := model.selectedNode().Path()
	want := "open " + shellQuote(selected)
	if runner.commands[0] != want {
		t.Fatalf("launched command = %q, want %q", runner.commands[0], want)
	}
}

func TestCtrlEnterRunsFolderActionOnRootRow(t *testing.T) {
	model, runner := newActionModel(t, DefaultActions{Folder: "open <dirpath>"})
	// Selection starts on the sticky root row, which is a directory.
	cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+enter returned nil command")
	}
	cmd()
	if len(runner.commands) != 1 {
		t.Fatalf("launched commands = %d, want 1", len(runner.commands))
	}
	want := "open " + shellQuote(model.selectedNode().Path())
	if runner.commands[0] != want {
		t.Fatalf("launched command = %q, want %q", runner.commands[0], want)
	}
}

func TestCtrlEnterIsSilentWhenBothActionsUnset(t *testing.T) {
	model, runner := newActionModel(t, DefaultActions{})
	for _, key := range []tea.KeyPressMsg{
		{Code: tea.KeyEnter, Mod: tea.ModCtrl}, // root row, folder action unset
		{Code: tea.KeyDown},                    // docs
		{Code: tea.KeyEnter, Mod: tea.ModCtrl}, // folder action unset
		{Code: tea.KeyDown},                    // notes.md
		{Code: tea.KeyEnter, Mod: tea.ModCtrl}, // file action unset
	} {
		if cmd := model.UpdateKey(key); cmd != nil {
			t.Fatalf("UpdateKey(%q) returned command %v, want silent no-op", key.String(), cmd)
		}
	}
	if len(runner.commands) != 0 {
		t.Fatalf("launched commands = %d, want 0 for unset actions", len(runner.commands))
	}
}

func TestCtrlEnterHonorsEachActionIndependently(t *testing.T) {
	// File action set only: files launch, folders stay silent.
	model, runner := newActionModel(t, DefaultActions{File: "zed <filepath>"})

	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown}) // docs
	if cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}); cmd != nil {
		t.Fatalf("folder ctrl+enter with unset folder action returned command %v, want no-op", cmd)
	}

	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown}) // notes.md
	if cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}); cmd == nil {
		t.Fatal("file ctrl+enter with set file action returned nil command")
	} else {
		cmd()
	}
	if len(runner.commands) != 1 {
		t.Fatalf("launched commands = %d, want 1", len(runner.commands))
	}

	// Folder action set only: folders launch, files stay silent.
	model, runner = newActionModel(t, DefaultActions{Folder: "open <dirpath>"})

	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown}) // docs
	if cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}); cmd == nil {
		t.Fatal("folder ctrl+enter with set folder action returned nil command")
	} else {
		cmd()
	}
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown}) // notes.md
	if cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}); cmd != nil {
		t.Fatalf("file ctrl+enter with unset file action returned command %v, want no-op", cmd)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("launched commands = %d, want 1", len(runner.commands))
	}
}

func TestCtrlEnterWithoutRunnerIsANoOp(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFileSystem()
	fake.set(root, []filesystem.Entry{{Name: "notes.md", Mode: 0}})
	model := NewModelConfigured(root, "", ModelConfig{
		Preferences: Preferences{Actions: DefaultActions{File: "zed <filepath>"}},
	}, fake)
	completeInitialLoad(t, model)
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})

	if cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}); cmd != nil {
		t.Fatalf("ctrl+enter without runner returned command %v, want no-op", cmd)
	}
}

func TestPlainEnterNeverLaunchesDefaultAction(t *testing.T) {
	model, runner := newActionModel(t, DefaultActions{File: "zed <filepath>", Folder: "open <dirpath>"})
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown}) // docs
	if cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatalf("plain enter on a directory returned command %v, want no preview activation", cmd)
	}
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown}) // notes.md
	// Without a preview client Enter is a no-op, but it must never reach
	// the default-action runner either.
	_ = model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(runner.commands) != 0 {
		t.Fatalf("plain enter launched %d default actions, want 0", len(runner.commands))
	}
}

func TestDefaultActionLaunchFailureSurfacesFooterWarning(t *testing.T) {
	model, runner := newActionModel(t, DefaultActions{File: "zed <filepath>"})
	runner.err = errors.New("spawn failed")

	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown})
	model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown}) // notes.md
	cmd := model.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+enter returned nil command")
	}
	model.Update(cmd())
	if !strings.Contains(model.warning, "Default action: spawn failed") {
		t.Fatalf("warning = %q, want the launch failure warning", model.warning)
	}
}
