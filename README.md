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
visible scrollbar dragging, left-click selection/toggle, OSC 52 path
copying, and a text preview pane opened from the tree. Recognized source,
configuration, and markup filenames receive syntax highlighting, while files
without a matching lexer remain plain text. It does not provide markdown
rendering, external actions, editing, install/distribution automation, or
release tags.

The supported validation target is Linux under WSL2 with Herdr 0.8.2 or newer.
Native macOS and Windows validation is out of scope. Native Windows host
support is not planned: the manifest is `platforms = ["linux"]`, and windows
cross-builds are not expected to compile (`internal/herdr` uses
`golang.org/x/sys/unix`). A terminal used for copying must support OSC 52;
otherwise the viewer still navigates, but the clipboard operation cannot be
expected to reach the host clipboard.
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

The viewer changes its process working directory into the launch root. Herdr
launches plugin pane commands with the plugin directory as their working
directory and pane splits inherit that pane's working directory, so without
this change a split opened from the viewer pane would start in the plugin
directory instead of the directory being browsed.

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
- `Enter`: open a text preview of the selected file in a right split pane and
  keep the keyboard focus in the tree. Directories and symlinks whose target
  is a directory or missing are ignored. The preview pane is tracked by its
  pane ID and re-discovered through its `preview=<path>` metadata token after
  a tree restart; pressing `Enter` on the file already shown keeps the
  existing pane, and pressing it on another file closes and reopens the pane.
  Without a Herdr context (`HERDR_PANE_ID` missing) `Enter` stays a no-op;
  CLI failures surface as a footer warning and the tree keeps working.
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

### Preview pane

`Enter` in the tree opens the `preview` entrypoint as a right split without
stealing the keyboard focus. The preview reads the file path passed through
`HERDR_PREVIEW_FILE` from disk at startup and on manual reload (the tree cache
is not used), and shows a snapshot of its head.
The layout mirrors the tree:
a centered title (absolute path, tail-truncated with `…`), dividers, a body
with a line-number gutter and a vertical divider, and the footer
`w wrap    space copy    r reload    q close`.

Previewability is classified before rendering: known image, video, audio, and
binary extensions (`png`, `mp4`, `mp3`, `zip`, `exe`, `so`, `pdf`, ...) show
an `Unsupported preview: <category>` label instead of content; unknown or
extensionless files are sniffed for NUL bytes or invalid UTF-8 in their head
and are treated as binary when either is found. Everything else renders as
text: `svg`, `json`, markdown, and plain files all count. Text is displayed
with right-aligned line numbers (the gutter width follows the largest line
number, and wrapped continuation lines leave the gutter blank), tabs expand
to four spaces, CRLF is normalized, and every line passes the same
terminal-safe sanitization as the tree. Files whose base name matches a Chroma
lexer are syntax-highlighted with the fixed `github-dark` / `github` themes;
files without a matching lexer fall back to plain text. Files larger than 2 MiB
are cut at the head and end with a `… truncated (2 MiB limit)` marker.
Markdown is treated as source and highlighted, not rendered.

Scrolling in the preview:

- `j` / `k`, `Up` / `Down`, `Ctrl+u` / `Ctrl+d`, `Ctrl+b` / `Ctrl+f`,
  `PageUp` / `PageDown`, `Home` / `End`: vertical movement, identical to the
  tree.
- Mouse wheel over the body: scroll three rows per wheel event. The rightmost
  body column is the vertical scrollbar with track click and thumb drag.
- `w`: toggle hard wrap (default off). With wrap on, lines break at the pane
  width, continuation rows keep a blank gutter, and the horizontal offset is
  reset. With wrap off, lines scroll horizontally instead of truncating.
- `r`: reload the displayed path from disk using the same 2 MiB head and text
  pipeline. The scroll position is clamped to the new content, wrap mode is
  preserved, and any selection is cleared on a successful reload. A successful
  reload shows a brief `Reloaded` toast; if a reload fails, the last
  successfully displayed content is kept while a warning is shown. If the
  initial read failed, `r` retries it. When `HERDR_PREVIEW_FILE` is unset, `r`
  does nothing.
- Left-drag across preview body text selects and highlights it. Selection is
  visual only until `space` copies it (below). Clicking again resets the
  selection; vertical and horizontal scrolling keep it attached to the text,
  while `w`, a resize, and a successful reload clear it. Gutter clicks anchor
  at the start of the line, and clicks past a line's end clamp to that end.
  Unsupported preview labels and the truncation marker are not selectable.
  Right/middle clicks, keyboard selection, double-click word selection, and
  drag auto-scroll are not assigned.
- `space`: copy the selected text to the clipboard (OSC 52), with the same
  terminal limitations as the tree's `space` path copy. A brief toast in the
  footer row reports `Copied N chars` (single line) or `Copied N chars (M
  lines)` across lines, where N is the rune count and M the line count; with
  no selection it shows `No selection` and copies nothing. The toast
  disappears after a few seconds and the help row returns. The highlight
  stays after copying so `space` can be pressed again to re-copy. Toggling
  `w` still clears the selection as described above, so select the text again
  before copying; horizontal scrolling keeps it. Because the selection is
  kept in original line coordinates, the same selected content produces the
  identical copied text in either wrap mode. The copied text is what is
  displayed (tabs expanded, sanitized).
- `Left` / `Right`: horizontal scrolling (only while wrap is off). A
  horizontal scrollbar row appears above the footer with the same track
  click and thumb drag behavior.
- `q` / `Ctrl+C`: quit. The split pane disappears and focus returns to the
  tree (Herdr standard behavior).

A missing or unreadable `HERDR_PREVIEW_FILE` shows a footer warning and waits
for `q`. The preview tags its own pane with a `preview=<path>` metadata token
at startup so a restarted tree can re-discover the pane without opening a
duplicate.

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
