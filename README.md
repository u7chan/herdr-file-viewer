# Herdr File Viewer

[![CI](https://github.com/u7chan/herdr-file-viewer/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/u7chan/herdr-file-viewer/actions/workflows/ci.yml)
[![Go](https://badgen.net/static/Go/1.25%2B/00ADD8?icon=go)](https://go.dev/)
[![Herdr](https://badgen.net/static/Herdr/%3E%3D0.8.2/6E56CF)](https://herdr.dev/)

The read-only file viewer plugin for Herdr resolves its launch root once and
shows a lazy, keyboard-driven filesystem tree. Directory reads run
asynchronously so navigation, scrolling, and resize remain responsive.

## Minimum scope

This is the minimum, read-only viewer used inside a Herdr pane. It supports
lazy directory expansion, cell-aware single-line rendering, recoverable
directory errors, symlink-as-entry handling, keyboard and mouse scrolling,
visible scrollbar dragging, left-click selection/toggle, and OSC 52 path
copying. It does not provide preview, external actions, editing,
install/distribution automation, or release tags.

The supported validation target is Linux under WSL2 with Herdr 0.8.2 or newer.
Native macOS and Windows validation is out of scope. A terminal used for
copying must support OSC 52; otherwise the viewer still navigates, but the
clipboard operation cannot be expected to reach the host clipboard.
No install package, distribution channel, or release artifact is provided.

## Requirements

- Go 1.25.0 or newer for source compatibility
- Go 1.27.0 is the latest stable development and primary CI toolchain
- The minimum compatibility CI lane runs Go 1.25.14 with `GOTOOLCHAIN=local`
- Herdr 0.8.2 or newer for local plugin smoke tests
- golangci-lint 2.13.1, pinned in `.mise.toml` so local linting matches CI

`go.mod` keeps `go 1.25.0` as the minimum compatibility directive and pins
`toolchain go1.27.0` for the recommended development toolchain. Go 1.27.0 is
the latest stable release confirmed from the official Go release information
at implementation time. The supported Go 1.25 patch release used by the
minimum lane is Go 1.25.14.

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

The manifest also provides an Action for opening the same pane from the
current Herdr context:

```bash
herdr plugin action invoke u7chan.file-viewer.open-file-viewer
```

The launch root is the `focused_pane_cwd` snapshot captured at startup. If the
Herdr context is missing, invalid, or points to an unavailable directory, the
viewer reports a warning and falls back to the workspace or process cwd.

### Operations

- `Up` / `Down` or `j` / `k`: move the selection one row without reading the filesystem; the viewport follows it.
- `Ctrl+u` / `Ctrl+d`: move by half a viewport.
- `Ctrl+b` / `Ctrl+f` or `PageUp` / `PageDown`: move by one viewport with a one-row overlap.
- `Home` / `End`: move to the first / last visible row.
- `Right` / `Left`: expand/collapse a directory or move to its parent.
- Left click: toggle a directory; files, symlinks, and the root are selection-only.
- Mouse wheel over the tree: scroll three rows per wheel event.
- The rightmost tree column is a scrollbar. Click its track to jump, or drag its thumb to scroll. Scrollbar movement keeps the selected row in the viewport and does not read the filesystem.
- `Space` / full-width `U+3000`: copy the selected absolute path through OSC 52.
- `r`: reload the tree: every previously read directory is re-scanned and the
  Git status snapshot is refreshed together. Expansion state survives and the
  selection stays on its path, falling back to the last visible row when the
  entry no longer exists. Completion is confirmed by a brief in-app toast in
  the footer, so the reload feedback does not depend on Herdr settings.
- `Enter`: reserved for a future action and intentionally has no effect.
- Right click and other unassigned input: no effect.
- `q` / `Ctrl+C`: quit. Bubble Tea restores the alternate screen, mouse mode,
  cursor, and input state when the pane exits.

The title occupies the first row and is centered within the pane. On a normal
pane, a full-width `─` divider follows the title and another one separates the
tree from the bottom-fixed Footer. The root HOME path is pinned immediately
below the title divider; only its descendants scroll. The tree and Footer have
a small left inset. The Footer contains
`space copy    r reload    q quit` during normal operation, with the key labels emphasized
and the action labels muted; loading, warning, or error status replaces those
hints when relevant, and a brief toast (`Reloaded`) appears for a few
seconds after `r`. Very small panes omit dividers when there is no room for
them.
The divider and scrollbar use portable box-drawing/block glyphs rather than a
Nerd Font-specific glyph; the file tree icons remain Nerd Font glyphs.

Names containing C0/C1 controls, ESC, invalid UTF-8, CJK, emoji, or combining
characters are sanitized and truncated by terminal cell width. Widths 0, 1,
and 2 are handled without allowing a row, selection bar, or status line to
exceed the pane width. Symlinks are displayed but never followed.
Entries named `.git` are hidden at every tree depth; other dotfiles remain
visible.

To remove the local link after testing:

```bash
herdr plugin unlink u7chan.file-viewer
```

## Verification

The primary quality lane uses Go 1.27.0 and runs:

```bash
test -z "$(gofmt -l $(git ls-files '*.go'))"
mise x go@1.27.0 golangci-lint -- golangci-lint run
go vet ./...
go test ./...
go test -race ./...
CGO_ENABLED=0 go build -trimpath ./cmd/herdr-file-viewer
CGO_ENABLED=0 go build -trimpath -o bin/herdr-file-viewer ./cmd/herdr-file-viewer
```

`mise x go@1.27.0 golangci-lint -- golangci-lint run` installs and runs the golangci-lint
version pinned in `.mise.toml` (v2.13.1), the same version CI installs, alongside the
Go 1.27.0 development toolchain. Without mise, install the same golangci-lint version with `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.1`, then run `$(go env GOPATH)/bin/golangci-lint run` (or `$(go env GOBIN)/golangci-lint` when `GOBIN` is configured, since `go install` installs there instead).

The minimum compatibility lane uses the actual Go 1.25.14 toolchain and
disables automatic toolchain switching:

```bash
GOTOOLCHAIN=local go test ./...
GOTOOLCHAIN=local CGO_ENABLED=0 go build -trimpath ./cmd/herdr-file-viewer
```

The CI checks are deterministic; performance observations are not CI failure
thresholds. Cursor movement is required to avoid filesystem reads and visible
row rebuilds by invariant rather than by a fixed time limit.

## Benchmarks

The hot-path benchmarks use deterministic in-memory fixtures. Visible-row
rebuilds and cache hits are measured separately, and viewport rendering uses
many already-loaded visible rows without filesystem access. Run them with the
primary Go 1.27.0 toolchain:

```bash
go test -run '^$' -bench '^(BenchmarkVisibleRows|BenchmarkViewportRendering)' -benchmem ./internal/browser ./internal/app
```

The output is a comparison baseline, not a fixed performance threshold.
