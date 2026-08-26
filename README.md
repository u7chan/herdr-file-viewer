# Herdr File Viewer

[![CI](https://github.com/u7chan/herdr-file-viewer/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/u7chan/herdr-file-viewer/actions/workflows/ci.yml)
[![Go](https://badgen.net/static/Go/1.25%2B/00ADD8?icon=go)](https://go.dev/)
[![Herdr](https://badgen.net/static/Herdr/%3E%3D0.8.2/6E56CF)](https://herdr.dev/)

The read-only file viewer plugin for Herdr resolves its launch root once and
shows a lazy, keyboard-driven filesystem tree. Directory reads run
asynchronously so navigation and resize remain responsive.

## Minimum scope

This is the minimum, read-only viewer used inside a Herdr pane. It supports
lazy directory expansion, cell-aware single-line rendering, recoverable
directory errors, symlink-as-entry handling, keyboard navigation, left-click
selection/toggle, and OSC 52 path copying. It does not provide preview,
external actions, editing, install/distribution automation, or release tags.

The supported validation target is Linux under WSL2 with Herdr 0.8.2 or newer.
Native macOS and Windows validation is out of scope. A terminal used for
copying must support OSC 52; otherwise the viewer still navigates, but the
clipboard operation cannot be expected to reach the host clipboard.
No install package, distribution channel, or release artifact is provided.

## Requirements

- Go 1.25 or newer
- Herdr 0.8.2 or newer for local plugin smoke tests

## Build

```bash
mkdir -p bin
CGO_ENABLED=0 go build -trimpath -o bin/herdr-file-viewer ./cmd/herdr-file-viewer
```

`bin/herdr-file-viewer` is a local build artifact and must not be committed.

## Link and open in Herdr

From the repository root, link the manifest and explicitly open its split
entrypoint to the right of the focused pane:

```bash
herdr plugin link "$PWD"
herdr plugin pane open \
  --plugin u7chan.file-viewer \
  --entrypoint files \
  --placement split \
  --direction right \
  --no-focus
```

The launch root is the `focused_pane_cwd` snapshot captured at startup. If the
Herdr context is missing, invalid, or points to an unavailable directory, the
viewer reports a warning and falls back to the workspace or process cwd.

### Operations

- `Up` / `Down`: move the selection without reading the filesystem.
- `Right` / `Left`: expand/collapse a directory or move to its parent.
- Left click: toggle a directory; files, symlinks, and the root are selection-only.
- `Space` / full-width `U+3000`: copy the selected absolute path through OSC 52.
- `Enter`: reserved for a future action and intentionally has no effect.
- Mouse wheel, right click, and other unassigned input: no effect.
- `q` / `Ctrl+C`: quit. Bubble Tea restores the alternate screen, mouse mode,
  cursor, and input state when the pane exits.

Names containing C0/C1 controls, ESC, invalid UTF-8, CJK, emoji, or combining
characters are sanitized and truncated by terminal cell width. Widths 0, 1,
and 2 are handled without allowing a row, selection bar, or status line to
exceed the pane width. Symlinks are displayed but never followed.

To remove the local link after testing:

```bash
herdr plugin unlink u7chan.file-viewer
```

The complete WSL2/Herdr smoke record and the non-gating performance
measurements are in [`docs/issue-6-verification.md`](docs/issue-6-verification.md).

## Verification

```bash
test -z "$(gofmt -l $(git ls-files '*.go'))"
golangci-lint run
go vet ./...
go test ./...
go test -race ./...
CGO_ENABLED=0 go build -trimpath ./cmd/herdr-file-viewer
CGO_ENABLED=0 go build -trimpath -o bin/herdr-file-viewer ./cmd/herdr-file-viewer
```

The CI checks are deterministic; performance observations are not CI failure
thresholds. Cursor movement is required to avoid filesystem reads and visible
row rebuilds by invariant rather than by a fixed time limit.
