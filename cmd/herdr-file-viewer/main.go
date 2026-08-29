package main

import (
	"errors"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/u7chan/herdr-file-viewer/internal/app"
	"github.com/u7chan/herdr-file-viewer/internal/herdr"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	root, err := herdr.ResolveRoot()
	if err != nil {
		return err
	}
	if err := herdr.ChdirRoot(root.Path); err != nil {
		return err
	}

	model := app.NewModel(root.Path, root.Warning)
	_, err = tea.NewProgram(model).Run()
	if errors.Is(err, tea.ErrInterrupted) {
		return nil
	}
	return err
}
