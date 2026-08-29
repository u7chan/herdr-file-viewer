# Development rules

## Tech Stack

- Go 1.25.0 source compatibility minimum
- Go 1.27.0 latest stable for development and the primary CI lane
- Go 1.25.14 minimum compatibility CI lane with `GOTOOLCHAIN=local`
- Bubble Tea v2
- Lip Gloss v2
- Herdr Plugin v1

## Why / What / Constraints First

Before implementation, briefly clarify when necessary:

- Why: why the change is needed
- What: what behavior or outcome is required
- Constraints: which boundaries must remain intact

Keep How in the design and code rather than this document.

## Behavior / Spec Consultation Policy

When consulted about behavior or specifications, do not implement immediately.
If the observed result does not match the implementation, file an Issue or
confirm the intended direction first. Never jump straight into
implementation.

## Architecture Policy

- `cmd/herdr-file-viewer` is the composition root.
- Herdr-specific environment access belongs in `internal/herdr`.
- UI state and rendering belong in `internal/app`; `View` must remain pure.
- Preserve the dependency direction `cmd -> app -> browser -> filesystem`, with `cmd -> herdr` for launch context resolution.
- Add packages only when required by implemented behavior. Avoid empty future packages, DI frameworks, generic SDKs, and speculative abstractions.

## Test Policy

- Treat tests as executable specifications.
- Prefer deterministic invariants over timing-based assertions.
- Required formatting, lint, vet, test, and static-build checks must pass.
- The lint check is `golangci-lint run`; the version is pinned in `.mise.toml` (local) and `.github/workflows/ci.yml` (CI) to v2.13.1. Treat it as required, not optional.
- The primary quality lane uses the pinned latest stable Go toolchain; the
  minimum compatibility lane uses the actual supported Go 1.25 patch release
  with toolchain auto-switching disabled.

## Comment Policy

- Avoid comments that merely restate the code.
- Document only non-obvious intent, constraints, and safety assumptions.
- Use `TODO:` or `TBD:` for explicitly deferred work.

## Commit Messages

- Commit messages are English-only with a conventional commit prefix (`feat:`, `fix:`, `docs:`, `chore:`, `refactor:`, `test:`, `ci:`).

## Review Policy

- Write review findings and review results in Japanese.
- After the feedback loop (review findings + fixes) completes, rebuild
  `bin/herdr-file-viewer` from the final code and report it as built; the user
  verifies the result themselves.

## References

- [`docs/pull-request.md`](docs/pull-request.md) — required Draft PR structure, verification record, and orchestration-team reporting.
- [`README.md`](README.md) — build, link, launch, and verification commands.
