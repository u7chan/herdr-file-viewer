package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestModelQuitsOnQAndCtrlC(t *testing.T) {
	t.Parallel()

	for _, key := range []tea.KeyPressMsg{
		{Code: 'q', Text: "q"},
		{Code: 'c', Mod: tea.ModCtrl},
	} {
		model := NewModel("/workspace", "")
		_, cmd := model.Update(key)
		if cmd == nil {
			t.Fatalf("Update(%q) returned nil command", key.String())
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("Update(%q) command = %T, want tea.QuitMsg", key.String(), cmd())
		}
	}
}

func TestModelKeepsZeroWindowSizeSafe(t *testing.T) {
	t.Parallel()

	model := NewModel("/workspace", "")
	updated, cmd := model.Update(tea.WindowSizeMsg{Width: 0, Height: 0})
	if cmd != nil {
		t.Fatalf("Update(zero size) command = %v, want nil", cmd)
	}

	got := updated.(Model)
	view := got.View()
	if !view.AltScreen {
		t.Fatal("View().AltScreen = false, want true")
	}
	if view.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("View().MouseMode = %v, want cell motion", view.MouseMode)
	}
	if !strings.Contains(view.Content, "Window: 0 x 0") {
		t.Fatalf("View().Content = %q, want zero size", view.Content)
	}
}

func TestModelClampsNegativeWindowSize(t *testing.T) {
	t.Parallel()

	model := NewModel("/workspace", "")
	updated, _ := model.Update(tea.WindowSizeMsg{Width: -1, Height: -2})
	if got := updated.(Model).View().Content; !strings.Contains(got, "Window: 0 x 0") {
		t.Fatalf("View().Content = %q, want clamped zero size", got)
	}
}

func TestModelViewShowsRootAndWarning(t *testing.T) {
	t.Parallel()

	view := NewModel("/workspace", "using process cwd").View()
	if !strings.Contains(view.Content, "Root: /workspace") {
		t.Fatalf("View().Content = %q, want root", view.Content)
	}
	if !strings.Contains(view.Content, "Warning: using process cwd") {
		t.Fatalf("View().Content = %q, want warning", view.Content)
	}
}
