package app

import (
	"strconv"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/u7chan/herdr-file-viewer/internal/filesystem"
)

var benchmarkViewportLines []string

func BenchmarkViewportRendering(b *testing.B) {
	b.Run("ManyRows", func(b *testing.B) {
		model := benchmarkViewportModel(b)
		_, treeHeight, _ := layoutHeights(model.height)
		lines := model.renderTree(treeHeight)
		if len(lines) != treeHeight {
			b.Fatalf("fixture rendered rows = %d, want viewport height %d", len(lines), treeHeight)
		}
		b.ReportAllocs()

		for b.Loop() {
			benchmarkViewportLines = model.renderTree(treeHeight)
		}
	})
}

func benchmarkViewportModel(b *testing.B) *Model {
	b.Helper()

	const rowCount = 2048
	entries := make([]filesystem.Entry, 0, rowCount)
	for index := 0; index < rowCount; index++ {
		entries = append(entries, filesystem.Entry{
			Name: "file-" + strconv.Itoa(index),
		})
	}

	model := NewModel("/benchmark/viewport", "", benchmarkFileSystem{entries: entries})
	load := model.Init()
	if load == nil {
		b.Fatal("fixture Init() returned nil command")
	}
	result := loadResultFromInit(b, load)
	model.Update(result)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model.selected = len(model.visibleRows) / 2
	model.keepSelectionVisible()
	if model.tree.VisibleRowsDirty() {
		b.Fatal("fixture visible rows are dirty after setup")
	}
	return model
}

type benchmarkFileSystem struct {
	entries []filesystem.Entry
}

func (f benchmarkFileSystem) ReadDir(string) ([]filesystem.Entry, error) {
	return append([]filesystem.Entry(nil), f.entries...), nil
}
