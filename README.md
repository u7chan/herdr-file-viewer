# Herdr File Viewer

[![CI](https://github.com/u7chan/herdr-file-viewer/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/u7chan/herdr-file-viewer/actions/workflows/ci.yml)
[![Go](https://badgen.net/static/Go/1.25%2B/00ADD8?icon=go)](https://go.dev/)
[![Herdr](https://badgen.net/static/Herdr/%3E%3D0.8.2/6E56CF)](https://herdr.dev/)

The read-only file viewer plugin for Herdr resolves its launch root once and
shows a lazy, keyboard-driven filesystem tree. Directory reads run
asynchronously so navigation and resize remain responsive.

## Requirements

- Go 1.25 or newer
- Herdr 0.8.2 or newer for local plugin smoke tests

## Build

```bash
mkdir -p bin
CGO_ENABLED=0 go build -trimpath -o bin/herdr-file-viewer ./cmd/herdr-file-viewer
```

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

Use the arrow keys to select and expand/collapse directories. Press `q` or
`Ctrl+C` to quit. Bubble Tea restores the alternate screen, cursor, and other
terminal state when the pane exits.

To remove the local link after testing:

```bash
herdr plugin unlink u7chan.file-viewer
```

## Verification

```bash
test -z "$(gofmt -l $(git ls-files '*.go'))"
golangci-lint run
go vet ./...
go test ./...
go test -race ./...
CGO_ENABLED=0 go build -trimpath ./cmd/herdr-file-viewer
```
