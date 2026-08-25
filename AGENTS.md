# Development rules

## Architecture

- `cmd/herdr-file-viewer` is the composition root. It acquires the Herdr
  launch context, assembles the model, starts Bubble Tea, and handles the
  top-level error.
- `internal/herdr` owns access to Herdr environment variables and the
  `HERDR_PLUGIN_CONTEXT_JSON` snapshot. The rest of the application must not
  read environment variables directly.
- `internal/app` owns the Bubble Tea model and rendering. `View` is pure: it
  does not perform filesystem I/O or mutate state.
- The intended future dependency direction is `cmd -> app -> browser ->
  filesystem`, with `cmd -> herdr` for launch-context resolution. Add a
  package only when the corresponding feature is implemented; do not create
  empty future packages or add a DI framework, Bubbles, or a generic SDK.

## Current foundation scope

- This foundation resolves a root once at startup and renders a status shell.
  Filesystem scanning, tree state, navigation, clipboard actions, and polling
  belong to later Sub-Issues.
- Bubble Tea commands are the boundary for future asynchronous I/O. Do not
  start a polling loop or put I/O in `View`.
- `q` and `Ctrl+C` quit, and the Bubble Tea v2 view declares the alternate
  screen and cell-motion mouse mode so the framework can restore terminal
  state on exit.

## Testing and performance

- Keep deterministic tests for context precedence, invalid or missing context,
  non-directory fallback, process-cwd fallback, quit messages, and zero-size
  window messages.
- The CI quality gate is `gofmt` diff checking, `golangci-lint run`, `go vet
  ./...`, `go test ./...`, and a `CGO_ENABLED=0` static build.
- Prefer deterministic invariants and bounded state over timing thresholds.
  Performance measurement and filesystem-call invariants will be added with
  the features that need them; do not add fixed-duration CI gates.

## Pull requests

- Before creating a pull request, read and follow [`docs/pull-request.md`](docs/pull-request.md).
  It is the required report template for this repository.
