# Pull request guide

Use this guide for every Issue-to-Draft-PR handoff. Record the actual values
from the current run; do not turn run-specific values (e.g., model
assignments) into a permanent repository rule.

## Required PR body

Use the template below. Include the Orchestration team section only when the
Issue was developed via Herdr orchestration (pi-issue-pr-workflow); otherwise
omit that section and its responsibility note entirely.

```markdown
## Scope
- <Issue-scoped changes only>

## Summary
- <concise implementation summary>

## Issue linkage
Closes #<sub-issue>
Parent context: #<parent-issue>

## Verification
- `<command>` — PASS: <result>
- `<command>` — PASS: <result>
- <include every required check and its actual result>

## Orchestration team
<Herdr orchestration only; omit this entire section and the responsibility note below it otherwise>
| role | assignee | provider | model | thinking |
| --- | --- | --- | --- | --- |
| orchestrator | <agent/pane; shared agent if applicable> | <run value> | <run value> | <run value> |
| impl | <agent/pane; shared agent if applicable> | <run value> | <run value> | <run value> |
| review | <agent/pane; shared agent if applicable> | <run value> | <run value> | <run value> |
| pr-fix | <agent/pane; shared agent if applicable> | <run value> | <run value> | <run value> |

Implementation and review responsibilities are separate. Review findings are
handled by the designated implementation/pr-fix owner.

## Unresolved conditions
- None, or list the exact condition and required decision.
```

This PR is Draft. Do not merge, mark ready, or close the linked issue as part
of implementation handoff.

## Verification record

List every command that was required by the Issue, its comments, and the
repository instructions, with the actual result. For this foundation that
includes, as applicable:

```text
test -z "$(gofmt -l $(git ls-files '*.go'))"
golangci-lint run
go vet ./...
go test ./...
CGO_ENABLED=0 go build -trimpath ./cmd/herdr-file-viewer
CGO_ENABLED=0 go build -trimpath -o bin/herdr-file-viewer ./cmd/herdr-file-viewer
herdr plugin link "$PWD"
herdr plugin pane open --plugin <id> --entrypoint <id> --placement split --direction right
<explicit q or Ctrl+C quit command and result>
<terminal-state restoration smoke command and captured evidence>
```

Do not label a skipped, unavailable, or failed command as PASS. Include
manifest/API validation evidence when a plugin manifest is part of the Issue.

Keep the PR scoped to its Issue. A completion report requires a pushed head,
passing required verification, and a confirmed Draft state; otherwise report
the blocking command/result and decision needed.
