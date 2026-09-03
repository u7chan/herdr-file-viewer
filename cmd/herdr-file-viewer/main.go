package main

import (
	"errors"
	"fmt"
	"os"

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
	if herdr.IsPreviewEntrypoint() {
		return runPreview()
	}
	if herdr.IsHelpEntrypoint() {
		return runHelp()
	}

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
		Help: app.HelpConfig{
			Client:     newHelpClient(),
			TargetPane: herdr.PaneID(),
		},
		// Root moves keep the process working directory in sync with the
		// display root so Herdr attributes the viewed directory to this pane.
		Chdir: herdr.ChdirRoot,
	})
	return runProgram(model)
}

func runPreview() error {
	model := app.NewPreviewModelWithConfig(herdr.PreviewFile(), newPreviewClient(), herdr.PaneID(), app.HelpConfig{
		Client:     newHelpClient(),
		TargetPane: herdr.PaneID(),
	})
	return runProgram(model)
}

func runHelp() error {
	model := app.NewHelpModel(herdr.HelpContext())
	return runProgram(model)
}

func runProgram(model tea.Model) error {
	_, err := tea.NewProgram(model).Run()
	if errors.Is(err, tea.ErrInterrupted) {
		return nil
	}
	return err
}

// newPreviewClient adapts the herdr CLI implementation to the app-side
// preview interface, fixing the plugin identity, the split placement, and
// the metadata token name at the composition root.
func newPreviewClient() app.PreviewClient {
	return paneClientAdapter{client: herdr.NewCLIPaneClient()}
}

type paneClientAdapter struct {
	client herdr.PaneClient
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
	return a.client.ClosePane(paneID)
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

// newHelpClient adapts the herdr CLI implementation to the app-side help
// interface, fixing the plugin identity, the overlay placement, the focus,
// and the help context environment value at the composition root.
func newHelpClient() app.HelpClient {
	return helpClientAdapter{client: herdr.NewCLIPaneClient()}
}

type helpClientAdapter struct {
	client herdr.PaneClient
}

func (a helpClientAdapter) OpenHelp(request app.HelpOpenRequest) (string, error) {
	return a.client.OpenPane(herdr.OpenPaneRequest{
		Plugin:     pluginID,
		Entrypoint: herdr.HelpEntrypointID,
		Placement:  "overlay",
		TargetPane: request.TargetPane,
		Focus:      true,
		Env:        []string{herdr.HelpContextEnv + "=" + request.Context},
	})
}
