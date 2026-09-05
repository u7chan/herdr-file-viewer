package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/u7chan/herdr-file-viewer/internal/app"
	"github.com/u7chan/herdr-file-viewer/internal/herdr"
)

// stubPaneClient is the herdr-side test double for the composition root
// adapter tests.
type stubPaneClient struct {
	openRequest herdr.OpenPaneRequest
	openID      string
	openErr     error

	closeErr error
	closed   []string

	getInfo  herdr.PaneInfo
	getFound bool
	getErr   error

	listInfo []herdr.PaneInfo
	listErr  error

	reportRequest herdr.ReportMetadataRequest
	reportErr     error
}

func (s *stubPaneClient) OpenPane(request herdr.OpenPaneRequest) (string, error) {
	s.openRequest = request
	return s.openID, s.openErr
}

func (s *stubPaneClient) ClosePane(paneID string) error {
	s.closed = append(s.closed, paneID)
	return s.closeErr
}

func (s *stubPaneClient) ClosePreviewPane(paneID string) error {
	s.closed = append(s.closed, paneID)
	return s.closeErr
}

func (s *stubPaneClient) GetPane(paneID string) (herdr.PaneInfo, bool, error) {
	return s.getInfo, s.getFound, s.getErr
}

func (s *stubPaneClient) ListPanes(workspaceID string) ([]herdr.PaneInfo, error) {
	return s.listInfo, s.listErr
}

func (s *stubPaneClient) ReportMetadata(request herdr.ReportMetadataRequest) error {
	s.reportRequest = request
	return s.reportErr
}

func TestPreviewClientAdapterOpenPreviewFixesPluginIdentityAndEnv(t *testing.T) {
	stub := &stubPaneClient{openID: "wY:p9Z"}
	client := paneClientAdapter{client: stub}

	paneID, err := client.OpenPreview("/abs/file.md", "wY:p3K")
	if err != nil || paneID != "wY:p9Z" {
		t.Fatalf("OpenPreview() = %q, %v; want pane id without error", paneID, err)
	}
	want := herdr.OpenPaneRequest{
		Plugin:     pluginID,
		Entrypoint: herdr.PreviewEntrypointID,
		Placement:  "split",
		TargetPane: "wY:p3K",
		Direction:  "right",
		Env:        []string{herdr.PreviewFileEnv + "=/abs/file.md"},
	}
	if !reflect.DeepEqual(stub.openRequest, want) {
		t.Fatalf("OpenPane() request = %#v, want %#v", stub.openRequest, want)
	}

	stub.openErr = errors.New("daemon down")
	if _, err := client.OpenPreview("/a", "wY:p3K"); err == nil {
		t.Fatal("OpenPreview() error = nil, want propagated error")
	}
}

func TestPreviewClientAdapterGetPaneExtractsPreviewToken(t *testing.T) {
	stub := &stubPaneClient{
		getInfo:  herdr.PaneInfo{PaneID: "wY:p9Z", Tokens: map[string]string{app.PreviewMetadataToken: "/abs/file.md"}},
		getFound: true,
	}
	client := paneClientAdapter{client: stub}

	pane, found, err := client.GetPane("wY:p9Z")
	if err != nil || !found || pane.PaneID != "wY:p9Z" || pane.File != "/abs/file.md" {
		t.Fatalf("GetPane() = %#v, %v, %v; want token-extracted pane", pane, found, err)
	}

	untagged := &stubPaneClient{getInfo: herdr.PaneInfo{PaneID: "wY:p9Z"}, getFound: true}
	pane, found, err = paneClientAdapter{client: untagged}.GetPane("wY:p9Z")
	if err != nil || !found || pane.File != "" {
		t.Fatalf("GetPane(untagged) = %#v, %v, %v; want empty file", pane, found, err)
	}

	missing := &stubPaneClient{getFound: false}
	pane, found, err = paneClientAdapter{client: missing}.GetPane("wY:p9Z")
	if found || err != nil || pane != (app.PreviewPane{}) {
		t.Fatalf("GetPane(missing) = %#v, %v, %v; want not found without error", pane, found, err)
	}
}

func TestPreviewClientAdapterListPanesMapsTokens(t *testing.T) {
	stub := &stubPaneClient{listInfo: []herdr.PaneInfo{
		{PaneID: "wY:p1"},
		{PaneID: "wY:p2", Tokens: map[string]string{app.PreviewMetadataToken: "/a.md"}},
	}}
	panes, err := paneClientAdapter{client: stub}.ListPanes("wY")
	if err != nil {
		t.Fatalf("ListPanes() error = %v", err)
	}
	if len(panes) != 2 || panes[1].PaneID != "wY:p2" || panes[1].File != "/a.md" || panes[0].File != "" {
		t.Fatalf("ListPanes() = %#v, want both panes with extracted token", panes)
	}
}

func TestPreviewClientAdapterTagPreviewReportsMetadata(t *testing.T) {
	stub := &stubPaneClient{}
	err := paneClientAdapter{client: stub}.TagPreview("wY:p9Z", "/abs/file.md")
	if err != nil {
		t.Fatalf("TagPreview() error = %v", err)
	}
	want := herdr.ReportMetadataRequest{
		PaneID: "wY:p9Z",
		Source: pluginID,
		Tokens: []string{app.PreviewMetadataToken + "=/abs/file.md"},
	}
	if !reflect.DeepEqual(stub.reportRequest, want) {
		t.Fatalf("ReportMetadata() request = %#v, want %#v", stub.reportRequest, want)
	}
}

func TestHelpClientAdapterOpenHelpFixesPluginManifestPopupAndContext(t *testing.T) {
	stub := &stubPaneClient{openID: "wY:h1"}
	paneID, err := (helpClientAdapter{client: stub}).OpenHelp(app.HelpOpenRequest{Context: "preview"})
	if err != nil || paneID != "wY:h1" {
		t.Fatalf("OpenHelp() = %q, %v; want pane id without error", paneID, err)
	}
	want := herdr.OpenPaneRequest{
		Plugin:     pluginID,
		Entrypoint: herdr.HelpEntrypointID,
		// placement stays empty: the popup declaration (placement and
		// size) lives in herdr-plugin.toml and the CLI invocation must not
		// override it with --placement.
		Focus: true,
		Env:   []string{herdr.HelpContextEnv + "=preview"},
	}
	if !reflect.DeepEqual(stub.openRequest, want) {
		t.Fatalf("OpenPane() request = %#v, want %#v (popup targets the active pane, so no target)", stub.openRequest, want)
	}

	stub.openErr = errors.New("daemon down")
	if _, err := (helpClientAdapter{client: stub}).OpenHelp(app.HelpOpenRequest{Context: "tree"}); err == nil {
		t.Fatal("OpenHelp() error = nil, want propagated error")
	}
}

func TestHelpClientAdapterOpenHelpKeepsEveryCallerContextDistinct(t *testing.T) {
	stub := &stubPaneClient{openID: "wY:h1"}
	adapter := helpClientAdapter{client: stub}

	if _, err := adapter.OpenHelp(app.HelpOpenRequest{Context: "tree"}); err != nil {
		t.Fatalf("OpenHelp(tree) error = %v", err)
	}
	treeWant := herdr.OpenPaneRequest{
		Plugin:     pluginID,
		Entrypoint: herdr.HelpEntrypointID,
		Focus:      true,
		Env:        []string{herdr.HelpContextEnv + "=tree"},
	}
	if !reflect.DeepEqual(stub.openRequest, treeWant) {
		t.Fatalf("OpenPane(tree) request = %#v, want %#v", stub.openRequest, treeWant)
	}
}

func TestNewHelpConfigWiresTheClientOnlyInsideAHerdrPane(t *testing.T) {
	if err := os.Unsetenv(herdr.PaneIDEnv); err != nil {
		t.Fatalf("Unsetenv(%s): %v", herdr.PaneIDEnv, err)
	}
	if config := newHelpConfig(); config.Client != nil {
		t.Fatalf("newHelpConfig() outside a Herdr pane = %#v, want nil client", config)
	}
	t.Setenv(herdr.PaneIDEnv, "wY:p3K")
	if config := newHelpConfig(); config.Client == nil {
		t.Fatal("newHelpConfig() inside a Herdr pane = nil client, want the wired client")
	}
}

func TestPreviewClientAdapterClosePanePassesThrough(t *testing.T) {
	stub := &stubPaneClient{}
	if err := (paneClientAdapter{client: stub}).ClosePane("wY:p9Z"); err != nil {
		t.Fatalf("ClosePane() error = %v", err)
	}
	if !reflect.DeepEqual(stub.closed, []string{"wY:p9Z"}) {
		t.Fatalf("closed = %v, want the pane id", stub.closed)
	}
}

func TestPreviewClientAdapterClosePanePropagatesFailure(t *testing.T) {
	stub := &stubPaneClient{closeErr: errors.New("close failed")}
	if err := (paneClientAdapter{client: stub}).ClosePane("wY:p9Z"); err == nil {
		t.Fatal("ClosePane() error = nil, want propagated error")
	}
}

func TestLoadPreferencesReadsPreferencesFileFromConfigDir(t *testing.T) {
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "preferences.json"), []byte(`{
		"appearance": {"mode": "light"},
		"icons": {"base_set": "material"},
		"preview": {"wrap": true}
	}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv(herdr.PluginConfigDirEnv, configDir)

	prefs, warning := loadPreferences()
	if prefs.AppearanceMode != "light" || prefs.IconBaseSet != "material" || !prefs.PreviewWrap {
		t.Fatalf("loadPreferences() = %#v, want resolved light/material/wrap values", prefs)
	}
	if warning != "" {
		t.Fatalf("loadPreferences() warning = %q, want none for a valid file", warning)
	}
}

func TestLoadPreferencesCreatesDefaultFileOnFirstRun(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv(herdr.PluginConfigDirEnv, configDir)

	prefs, warning := loadPreferences()
	want := herdr.Preferences{AppearanceMode: "auto", IconBaseSet: "font-awesome-solid"}
	if prefs != want {
		t.Fatalf("loadPreferences() = %#v, want defaults %#v on first run", prefs, want)
	}
	if warning != "" {
		t.Fatalf("loadPreferences() warning = %q, want none when the default file is created", warning)
	}
	if _, err := os.Stat(filepath.Join(configDir, "preferences.json")); err != nil {
		t.Fatalf("preferences.json not created on first run: %v", err)
	}
}

func TestLoadPreferencesWarnsWhenDefaultFileCreationFails(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv(herdr.PluginConfigDirEnv, filepath.Join(blocker, "sub"))

	prefs, warning := loadPreferences()
	want := herdr.Preferences{AppearanceMode: "auto", IconBaseSet: "font-awesome-solid"}
	if prefs != want {
		t.Fatalf("loadPreferences() = %#v, want defaults %#v when creation fails", prefs, want)
	}
	if warning == "" {
		t.Fatalf("loadPreferences() warning = %q, want the creation-failure warning", warning)
	}
}

func TestLoadPreferencesFallsBackToDefaultsWithWarningOnRejectedFile(t *testing.T) {
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "preferences.json"), []byte(`{"preview": {`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv(herdr.PluginConfigDirEnv, configDir)

	prefs, warning := loadPreferences()
	want := herdr.Preferences{AppearanceMode: "auto", IconBaseSet: "font-awesome-solid"}
	if prefs != want {
		t.Fatalf("loadPreferences() = %#v, want defaults %#v on rejection", prefs, want)
	}
	if warning == "" {
		t.Fatalf("loadPreferences() warning = %q, want the rejection warning for the toast", warning)
	}
}

func TestLoadPreferencesIsDetachedWithoutStateDir(t *testing.T) {
	t.Setenv(herdr.PluginStateDirEnv, "")

	prefs, warning := loadPreferences()
	want := herdr.Preferences{AppearanceMode: "auto", IconBaseSet: "font-awesome-solid"}
	if prefs != want {
		t.Fatalf("loadPreferences() = %#v, want defaults %#v for a detached run", prefs, want)
	}
	if warning != "" {
		t.Fatalf("loadPreferences() warning = %q, want none for a missing state dir", warning)
	}
}

func TestRunTreatsStartupEventBeforeEntrypointChecks(t *testing.T) {
	// The startup event must win over every entrypoint: even with a preview
	// entrypoint and preview file set, run() replaces the TUI with the
	// restore and returns.
	t.Setenv(herdr.PluginEventEnv, "startup")
	t.Setenv(herdr.EntrypointIDEnv, herdr.PreviewEntrypointID)
	t.Setenv(herdr.PreviewFileEnv, "/abs/file.md")

	original := runStartupRestore
	runStartupRestore = func() error { return errors.New("startup restore ran") }
	t.Cleanup(func() { runStartupRestore = original })

	err := run()
	if err == nil || err.Error() != "startup restore ran" {
		t.Fatalf("run() error = %v, want the startup restore outcome (no TUI started)", err)
	}
}

func TestRunStartupRestorePassesWithoutTUI(t *testing.T) {
	// A successful restore returns nil immediately: the hook must exit
	// without entering the Bubble Tea program.
	t.Setenv(herdr.PluginEventEnv, "startup")
	t.Setenv(herdr.PluginRootEnv, "/tmp/plugin-root")
	t.Setenv(herdr.PluginConfigDirEnv, "/tmp/plugin-config")
	t.Setenv(herdr.PluginStateDirEnv, "/tmp/plugin-state")
	t.Setenv(herdr.SocketPathEnv, "/tmp/plugin.sock")
	t.Setenv(herdr.BinPathEnv, "/usr/bin/herdr")
	t.Setenv(herdr.EntrypointIDEnv, "files")

	original := runStartupRestore
	runStartupRestore = func() error { return nil }
	t.Cleanup(func() { runStartupRestore = original })

	if err := run(); err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
}

// TestRunPreviewChangesWorkingDirectoryToPreviewFileParent is intentionally
// sequential because runPreview changes the process working directory.
func TestRunPreviewChangesWorkingDirectoryToPreviewFileParent(t *testing.T) {
	parent := t.TempDir()
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore cwd %q: %v", original, err)
		}
	})

	t.Setenv(herdr.PreviewFileEnv, filepath.Join(parent, "preview.md"))
	t.Setenv(herdr.PluginStateDirEnv, "")
	t.Setenv(herdr.SocketPathEnv, "")

	started := false
	originalRunProgram := runProgram
	runProgram = func(tea.Model) error {
		started = true
		return nil
	}
	t.Cleanup(func() { runProgram = originalRunProgram })

	if err := runPreview(); err != nil {
		t.Fatalf("runPreview() error = %v, want preview to start", err)
	}
	if !started {
		t.Fatal("runPreview() did not start the preview program")
	}
	got, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	wantInfo, err := os.Stat(parent)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", parent, err)
	}
	gotInfo, err := os.Stat(got)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", got, err)
	}
	if !os.SameFile(wantInfo, gotInfo) {
		t.Fatalf("preview cwd = %q, want file parent %q", got, parent)
	}
}

func TestRunPreviewKeepsWorkingDirectoryWhenParentIsMissing(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}

	file := filepath.Join(t.TempDir(), "missing", "preview.md")
	t.Setenv(herdr.PreviewFileEnv, file)
	t.Setenv(herdr.PluginStateDirEnv, "")
	t.Setenv(herdr.SocketPathEnv, "")

	started := false
	originalRunProgram := runProgram
	runProgram = func(tea.Model) error {
		started = true
		return nil
	}
	t.Cleanup(func() { runProgram = originalRunProgram })

	if err := runPreview(); err != nil {
		t.Fatalf("runPreview() error = %v, want preview to start after missing parent", err)
	}
	if !started {
		t.Fatal("runPreview() did not start the preview program after missing parent")
	}
	got, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if got != original {
		t.Fatalf("cwd after missing preview parent = %q, want unchanged %q", got, original)
	}
}

func TestRunPreviewKeepsWorkingDirectoryWhenParentCannotBeEntered(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}

	file := filepath.Join(t.TempDir(), "preview.md")
	t.Setenv(herdr.PreviewFileEnv, file)
	t.Setenv(herdr.PluginStateDirEnv, "")
	t.Setenv(herdr.SocketPathEnv, "")

	var attempted string
	originalChdirPreview := chdirPreview
	chdirPreview = func(path string) error {
		attempted = path
		return os.ErrPermission
	}
	t.Cleanup(func() { chdirPreview = originalChdirPreview })

	started := false
	originalRunProgram := runProgram
	runProgram = func(tea.Model) error {
		started = true
		return nil
	}
	t.Cleanup(func() { runProgram = originalRunProgram })

	if err := runPreview(); err != nil {
		t.Fatalf("runPreview() error = %v, want preview to start after cwd failure", err)
	}
	if !started {
		t.Fatal("runPreview() did not start the preview program after cwd failure")
	}
	if want := filepath.Dir(file); attempted != want {
		t.Fatalf("chdir path = %q, want preview parent %q", attempted, want)
	}
	got, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if got != original {
		t.Fatalf("cwd after failed preview chdir = %q, want unchanged %q", got, original)
	}
}

func TestRunPreviewDoesNotChangeWorkingDirectoryWhenFileIsUnsetOrEmpty(t *testing.T) {
	for _, test := range []struct {
		name  string
		unset bool
	}{
		{name: "unset", unset: true},
		{name: "empty"},
	} {
		t.Run(test.name, func(t *testing.T) {
			original, err := os.Getwd()
			if err != nil {
				t.Fatalf("Getwd() error = %v", err)
			}
			if test.unset {
				previous, wasSet := os.LookupEnv(herdr.PreviewFileEnv)
				if err := os.Unsetenv(herdr.PreviewFileEnv); err != nil {
					t.Fatalf("Unsetenv(%s): %v", herdr.PreviewFileEnv, err)
				}
				t.Cleanup(func() {
					if wasSet {
						_ = os.Setenv(herdr.PreviewFileEnv, previous)
					} else {
						_ = os.Unsetenv(herdr.PreviewFileEnv)
					}
				})
			} else {
				t.Setenv(herdr.PreviewFileEnv, "")
			}
			t.Setenv(herdr.PluginStateDirEnv, "")
			t.Setenv(herdr.SocketPathEnv, "")

			called := false
			originalChdirPreview := chdirPreview
			chdirPreview = func(string) error {
				called = true
				return nil
			}
			t.Cleanup(func() { chdirPreview = originalChdirPreview })

			started := false
			originalRunProgram := runProgram
			runProgram = func(tea.Model) error {
				started = true
				return nil
			}
			t.Cleanup(func() { runProgram = originalRunProgram })

			if err := runPreview(); err != nil {
				t.Fatalf("runPreview() error = %v, want preview to start", err)
			}
			if !started {
				t.Fatal("runPreview() did not start the preview program")
			}
			if called {
				t.Fatal("runPreview() attempted a cwd change for an unset preview file")
			}
			got, err := os.Getwd()
			if err != nil {
				t.Fatalf("Getwd() error = %v", err)
			}
			if got != original {
				t.Fatalf("cwd for empty preview file = %q, want unchanged %q", got, original)
			}
		})
	}
}

func TestRunRestoredPreviewEntrypointChangesWorkingDirectoryToPreviewParent(t *testing.T) {
	// Session restore execs this same binary with the preview entrypoint and
	// HERDR_PREVIEW_FILE set; run() must reach the same runPreview path.
	parent := t.TempDir()
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore cwd %q: %v", original, err)
		}
	})

	t.Setenv(herdr.PluginEventEnv, "")
	t.Setenv(herdr.EntrypointIDEnv, herdr.PreviewEntrypointID)
	t.Setenv(herdr.PreviewFileEnv, filepath.Join(parent, "restored.md"))
	t.Setenv(herdr.PluginStateDirEnv, "")
	t.Setenv(herdr.SocketPathEnv, "")

	started := false
	originalRunProgram := runProgram
	runProgram = func(tea.Model) error {
		started = true
		return nil
	}
	t.Cleanup(func() { runProgram = originalRunProgram })

	if err := run(); err != nil {
		t.Fatalf("run() error = %v, want restored preview to start", err)
	}
	if !started {
		t.Fatal("run() did not start the restored preview program")
	}
	got, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	wantInfo, err := os.Stat(parent)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", parent, err)
	}
	gotInfo, err := os.Stat(got)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", got, err)
	}
	if !os.SameFile(wantInfo, gotInfo) {
		t.Fatalf("restored preview cwd = %q, want file parent %q", got, parent)
	}
}

func TestPreviewClientAdapterRemovePreviewStateDeletesDurableState(t *testing.T) {
	stateDir := t.TempDir()
	store := herdr.NewPreviewStateStore(stateDir, "/tmp/herdr.sock")
	if err := store.Save("wY:p9Z", "/abs/file.md"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	adapter := paneClientAdapter{client: &stubPaneClient{}, state: store}

	if err := adapter.RemovePreviewState("wY:p9Z"); err != nil {
		t.Fatalf("RemovePreviewState() error = %v", err)
	}
	if _, found, err := store.Load("wY:p9Z"); err != nil || found {
		t.Fatalf("state after removal = found %v, error %v; want gone (a later restore must not resurrect the closed preview)", found, err)
	}
}

func TestPreviewClientAdapterRemovePreviewStateToleratesMissingState(t *testing.T) {
	adapter := paneClientAdapter{
		client: &stubPaneClient{},
		state:  herdr.NewPreviewStateStore(t.TempDir(), "/tmp/herdr.sock"),
	}
	if err := adapter.RemovePreviewState("wY:p9Z"); err != nil {
		t.Fatalf("RemovePreviewState() error = %v, want nil for a pane without state", err)
	}
}
