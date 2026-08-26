# Manifest スモーク記録 (Issue #21)

This record covers the `open-file-viewer` Action added to
`herdr-plugin.toml`. The checks were run from the Linux WSL2 development
environment with Herdr 0.8.2 and a locally built `bin/herdr-file-viewer`.

## Manifest and Action

```text
herdr --version
herdr 0.8.2

herdr plugin link "$PWD"
PASS: plugin linked with no manifest warning; plugin id u7chan.file-viewer

herdr plugin action list --plugin u7chan.file-viewer
PASS: lists open-file-viewer with the command
      herdr plugin pane open --plugin u7chan.file-viewer --entrypoint files
      --placement split --direction right --focus
```

The Action command follows the installed Herdr 0.8.2 `pane open` interface.
Its context is inherited by Herdr when the Action invokes the pane command;
the viewer smoke displayed the focused pane cwd captured in
`HERDR_PLUGIN_CONTEXT_JSON`.

## Action and pane smoke

```text
herdr plugin action invoke u7chan.file-viewer.open-file-viewer
PASS: action invocation returned `plugin-log-8`; Herdr opened Files pane
      `wY:p19` in a right split and the log finished with `status=succeeded`.
The action result reported `focused_pane_id=wY:p1`,
`focused_pane_cwd=/home/u7dev/workspace/herdr-file-viewer`, and
`workspace_id=wY`.

herdr plugin pane open --plugin u7chan.file-viewer --entrypoint files \
  --placement split --direction right --no-focus
PASS: direct pane opening returned Files pane `wY:p18`; its process was
      `./bin/herdr-file-viewer` and its initial root was
      `/home/u7dev/workspace/herdr-file-viewer`.

herdr pane send-keys wY:p18 q
herdr pane send-keys wY:p19 q
PASS: both viewers exited. Follow-up `herdr pane process-info` returned the
      expected `pane_not_found` for each removed smoke pane, while
      `herdr pane list --workspace wY` still contained `wY:p1`, `wY:p16`, and
      `wY:p17`.
```

The smoke panes were test artifacts. The orchestrator, implementation, and
review panes were not closed.
