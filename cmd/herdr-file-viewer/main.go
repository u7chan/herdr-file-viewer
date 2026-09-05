package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/u7chan/herdr-file-viewer/internal/app"
	"github.com/u7chan/herdr-file-viewer/internal/herdr"
)

const (
	pluginID = "u7chan.file-viewer"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	// The startup hook runs once per session restore and must be handled
	// before any entrypoint check: it restores panes and exits without
	// starting a TUI.
	if herdr.IsStartupEvent() {
		return runStartupRestore()
	}
	if herdr.IsPreviewEntrypoint() {
		return runPreview()
	}
	if herdr.IsHelpEntrypoint() {
		return runHelp()
	}

	prefs, prefsWarning := loadPreferences()
	root, err := herdr.ResolveRoot()
	if err != nil {
		return err
	}
	if err := herdr.ChdirRoot(root.Path); err != nil {
		return err
	}

	model := app.NewModelConfigured(root.Path, root.Warning, app.ModelConfig{
		Preview: app.PreviewConfig{
			Client:      newPreviewClient(),
			TargetPane:  herdr.PaneID(),
			WorkspaceID: herdr.WorkspaceID(),
		},
		Help: newHelpConfig(),
		// Root moves keep the process working directory in sync with the
		// display root so Herdr attributes the viewed directory to this pane.
		Chdir: herdr.ChdirRoot,
		Preferences: app.Preferences{
			AppearanceMode: prefs.AppearanceMode,
			IconBaseSet:    prefs.IconBaseSet,
		},
		PreferencesWarning: prefsWarning,
	})
	return runProgram(model)
}

// runStartupRestore is a variable so the composition tests can substitute a
// deterministic implementation and prove that the startup event is handled
// before the TUI entrypoints.
var runStartupRestore = func() error {
	env := herdr.RestoreLaunchEnv{
		PluginRoot:      os.Getenv(herdr.PluginRootEnv),
		PluginConfigDir: os.Getenv(herdr.PluginConfigDirEnv),
		PluginStateDir:  os.Getenv(herdr.PluginStateDirEnv),
		SocketPath:      os.Getenv(herdr.SocketPathEnv),
		BinPath:         os.Getenv(herdr.BinPathEnv),
	}
	if executable, err := os.Executable(); err == nil {
		env.Executable = executable
	}
	client := herdr.NewCLIPaneClient()
	restorer := herdr.NewRestorer(herdr.RestoreConfig{
		Client: client,
		State:  herdr.NewPreviewStateStore(env.PluginStateDir, env.SocketPath),
		Log:    os.Stdout,
		Env:    env,
		Poll:   time.Second,
		// The 30-second window covers the shell-spawn race right after the
		// session restore; panes that never become ready stay untouched.
		Timeout: 30 * time.Second,
	})
	restorer.Run()
	return nil
}

// loadPreferences resolves preferences.json once per process from
// HERDR_PLUGIN_CONFIG_DIR. A missing file is a first run: the defaults are
// written so the hand-editing entry point exists, and no warning is shown. A
// rejected file falls back to the defaults and returns the rejection as a
// warning the model shows as a startup toast. Detached runs (direct
// terminal, tests) read nothing and never write.
func loadPreferences() (herdr.Preferences, string) {
	prefs, err := herdr.NewPreferencesStore(os.Getenv(herdr.PluginConfigDirEnv)).Load()
	if err != nil {
		return prefs, err.Error()
	}
	return prefs, ""
}

func runPreview() error {
	file := herdr.PreviewFile()
	if file != "" {
		parent := filepath.Dir(file)
		if err := chdirPreview(parent); err != nil {
			fmt.Fprintf(os.Stderr, "enter preview directory %q: %v\n", parent, err)
		}
	}
	// The preview file must survive a cold session restore, so it is saved
	// durably per pane before the TUI starts; the startup restore reads it
	// back into the same pane.
	if err := herdr.NewPreviewStateStore(os.Getenv(herdr.PluginStateDirEnv), os.Getenv(herdr.SocketPathEnv)).Save(herdr.PaneID(), file); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	prefs, prefsWarning := loadPreferences()
	prefsStore := herdr.NewPreferencesStore(os.Getenv(herdr.PluginConfigDirEnv))
	model := app.NewPreviewModelConfigured(file, newPreviewClient(), herdr.PaneID(), app.PreviewModelConfig{
		Help: newHelpConfig(),
		Preferences: app.Preferences{
			AppearanceMode: prefs.AppearanceMode,
		},
		Wrap:               prefs.PreviewWrap,
		ShowWhitespace:     prefs.PreviewShowWhitespace,
		SaveWrap:           prefsStore.SavePreviewWrap,
		SaveShowWhitespace: prefsStore.SavePreviewShowWhitespace,
		PreferencesWarning: prefsWarning,
	})
	return runProgram(model)
}

// chdirPreview is a variable so composition tests can cover the best-effort
// failure path without depending on filesystem permissions.
var chdirPreview = os.Chdir

func runHelp() error {
	model := app.NewHelpModel(herdr.HelpContext())
	return runProgram(model)
}

// runProgram is a variable so composition tests can exercise entrypoints
// without starting Bubble Tea.
var runProgram = func(model tea.Model) error {
	_, err := tea.NewProgram(model).Run()
	if errors.Is(err, tea.ErrInterrupted) {
		return nil
	}
	return err
}

// newPreviewClient adapts the herdr CLI implementation to the app-side
// preview interface, fixing the plugin identity, the split placement, the
// metadata token name, and the durable restore state at the composition
// root.
func newPreviewClient() app.PreviewClient {
	return paneClientAdapter{
		client: herdr.NewCLIPaneClient(),
		state:  herdr.NewPreviewStateStore(os.Getenv(herdr.PluginStateDirEnv), os.Getenv(herdr.SocketPathEnv)),
	}
}

type paneClientAdapter struct {
	client herdr.PaneClient
	state  *herdr.PreviewStateStore
}

func (a paneClientAdapter) OpenPreview(file, targetPane string) (string, error) {
	return a.client.OpenPane(herdr.OpenPaneRequest{
		Plugin:     pluginID,
		Entrypoint: herdr.PreviewEntrypointID,
		Placement:  "split",
		TargetPane: targetPane,
		Direction:  "right",
		Env:        []string{herdr.PreviewFileEnv + "=" + file},
	})
}

func (a paneClientAdapter) ClosePane(paneID string) error {
	return a.client.ClosePreviewPane(paneID)
}

// RemovePreviewState forgets the durable restore state of a preview pane
// the tree just closed. The swap then reopens the preview, whose fresh
// process writes the new state, so a later session restore never brings the
// replaced file back.
func (a paneClientAdapter) RemovePreviewState(paneID string) error {
	if a.state == nil {
		return nil
	}
	return a.state.Remove(paneID)
}

func (a paneClientAdapter) GetPane(paneID string) (app.PreviewPane, bool, error) {
	info, found, err := a.client.GetPane(paneID)
	if err != nil || !found {
		return app.PreviewPane{}, found, err
	}
	return app.PreviewPane{
		PaneID: info.PaneID,
		File:   info.Tokens[app.PreviewMetadataToken],
	}, true, nil
}

func (a paneClientAdapter) ListPanes(workspaceID string) ([]app.PreviewPane, error) {
	panes, err := a.client.ListPanes(workspaceID)
	if err != nil {
		return nil, err
	}
	previews := make([]app.PreviewPane, 0, len(panes))
	for _, pane := range panes {
		previews = append(previews, app.PreviewPane{
			PaneID: pane.PaneID,
			File:   pane.Tokens[app.PreviewMetadataToken],
		})
	}
	return previews, nil
}

func (a paneClientAdapter) TagPreview(paneID, file string) error {
	return a.client.ReportMetadata(herdr.ReportMetadataRequest{
		PaneID: paneID,
		Source: pluginID,
		Tokens: []string{app.PreviewMetadataToken + "=" + file},
	})
}

// newHelpConfig wires the help popup capability when the viewer runs
// inside a Herdr pane. Popup panes always target the active pane, so
// outside a pane there is no active target; the nil client makes h a
// warning-only no-op there.
func newHelpConfig() app.HelpConfig {
	if herdr.PaneID() == "" {
		return app.HelpConfig{}
	}
	return app.HelpConfig{Client: newHelpClient()}
}

// newHelpClient adapts the herdr CLI implementation to the app-side help
// interface, fixing the plugin identity, the focus, and the help context
// environment value at the composition root; the popup placement itself
// stays in the manifest.
func newHelpClient() app.HelpClient {
	return helpClientAdapter{client: herdr.NewCLIPaneClient()}
}

type helpClientAdapter struct {
	client herdr.PaneClient
}

func (a helpClientAdapter) OpenHelp(request app.HelpOpenRequest) (string, error) {
	// The help pane is declared popup with a fixed size in herdr-plugin.toml;
	// the placement and size stay in the manifest, and the CLI invocation
	// omits --placement so herdr uses the declared popup definition. Popup
	// panes always target the active pane, so no target is passed either.
	return a.client.OpenPane(herdr.OpenPaneRequest{
		Plugin:     pluginID,
		Entrypoint: herdr.HelpEntrypointID,
		Focus:      true,
		Env:        []string{herdr.HelpContextEnv + "=" + request.Context},
	})
}
