# Issue #6 検証記録

Issue #6 の変更だけを対象に、WSL2 上で実施した記録である。時間値は
環境依存の観測値であり、CI の固定失敗閾値には使わない。

## 実施環境

- Linux x86_64 / WSL2: `6.6.87.2-microsoft-standard-WSL2`
- Ubuntu 26.04、Go 1.25.0、golangci-lint 2.12.0、Herdr 0.8.2
- Windows PowerShell 5.1.26100.9168、`powershell.exe` と `clip.exe`
- ブランチ: `feature/6-smoke-and-hardening`

## 自動検証

実行した必須コマンド:

```bash
mise x go@1.25.0 golangci-lint@2.12.0 -- bash -c 'test -z "$(gofmt -l $(git ls-files "*.go"))" && golangci-lint run && go vet ./... && go test ./... && go test -race ./... && CGO_ENABLED=0 go build -trimpath ./cmd/herdr-file-viewer && CGO_ENABLED=0 go build -trimpath -o bin/herdr-file-viewer ./cmd/herdr-file-viewer'
```

結果: **PASS**。`0 issues.`、全パッケージの `go vet`、通常テスト、race
テストが成功し、通常ビルドと `bin/herdr-file-viewer` の静的ビルドも成功した。
`bin/herdr-file-viewer` は ignored のローカル成果物であり、commit 対象ではない。

追加した決定的な仕様テスト:

- CJK、emoji、combining character と width 0/1/2 の cell-aware truncate
- 行、選択 bar、status の pane 幅上限と control character の sanitize
- root/directory の読み込み失敗、消失後の retry、symlink の非追跡
- `View`、矢印移動、resize 中に filesystem read を開始しないこと
- invalid load request、context の relative path、missing/invalid context の fallback

## Herdr link / open / unlink

### link

pane list で `wY:p1`（司令塔）、`wY:pP`（impl）、`wY:pQ`（review）を確認した後、
自分で次を実行した。

```bash
herdr plugin link "$PWD"
```

結果: **PASS**。manifest warning は出ず、`plugin_linked` として次を確認した。

```text
plugin_id=u7chan.file-viewer
manifest_path=/home/u7dev/workspace/herdr-file-viewer/herdr-plugin.toml
min_herdr_version=0.8.2
platforms=["linux"]
panes=[{id=files,title=Files,placement=split,command=["./bin/herdr-file-viewer"]}]
source.kind=local
```

`herdr plugin list --plugin u7chan.file-viewer --json` でも同じ manifest を確認した。

### 明示 pane / right split / focus entrypoint

pane list で解決した `HERDR_PANE_ID=wY:pP` を対象に、次を実行した。

```bash
herdr plugin pane open \
  --plugin u7chan.file-viewer \
  --entrypoint files \
  --placement split \
  --target-pane "$HERDR_PANE_ID" \
  --direction right \
  --cwd "$PWD" \
  --focus
```

結果: **PASS**。`plugin_pane_opened`、entrypoint `files`、pane `wY:pT`、label
`Files`、`focused=true` を確認した。layout は `wY:pP` の右に `wY:pT` を置いた
right split になった。

### unlink

smoke 完了後に次を実行した。

```bash
herdr plugin unlink u7chan.file-viewer
herdr plugin list --plugin u7chan.file-viewer --json
```

結果: **PASS**。`plugin_unlinked` (`removed=true`) の後、plugin list は
`plugins=[]` になった。

## Herdr 0.8.2 / WSL2 smoke

fixture は repository の ignored な `tmp/issue6-fixture` に一時作成した。
通常ファイル、`.git` 等の hidden directory/entry、CJK `日本語`、emoji `🙂`、
combining `é`、C0/ESC/newline/tab を含む名前、symlink-to-directory、broken
symlink、permission denied directory、消失させる directory を含めた。

| # | 操作・観測 | 判定と証拠 |
|---:|---|---|
| 1 | local plugin link | **PASS**。上記 `plugin_linked` の出力。 |
| 2 | manifest warning | **PASS**。link/list の manifest に warning なし。 |
| 3 | 明示対象 pane、right split、`files` entrypoint、focus | **PASS**。`wY:pT` の open 結果と layout。 |
| 4 | 起動時 root snapshot | **PASS**。`/proc/<pid>/environ` の `HERDR_PLUGIN_CONTEXT_JSON` が `focused_pane_cwd=/home/u7dev/workspace/herdr-file-viewer`、初期表示も同 root。 |
| 5 | missing / invalid context と fallback | **PASS**。同じ静的 binary を WSL2 pty で context 無し、`not json`、unavailable candidates で起動し、各 warning を表示して `q` で exit 0。通常テストの `ResolveRootAt` でも process fallback を確認。 |
| 6 | 起動後 focus 変更 | **PASS**。`herdr agent focus wY:p1` → `herdr plugin pane focus wY:pT` の後も root は変化せず、起動時 snapshot を保持。 |
| 7 | root load、lazy expand、collapse、parent、selection scroll | **PASS**。`.git` → `objects` の lazy expand、`left` の parent/collapse、30 回の down 後に viewport 上端が root から `logs/objects` へ移動した出力を確認。 |
| 8 | left click | **PASS**。SGR mouse click を `herdr pane send-text` で送り、`plain-file.txt` の selection-only と `large` の toggle + `alpha` 表示を確認。 |
| 9 | Space / `U+3000` の OSC 52 | **PASS**。Space で一意な `/home/u7dev/workspace/herdr-file-viewer/tmp/issue6-fixture` を copy し、`powershell.exe -NoProfile -NonInteractive -Command 'Get-Clipboard'` が同じ文字列を返した。full-width space は自動テストで確認。 |
| 10 | Enter、wheel、right-click、未割当入力 | **PASS**。`enter`、SGR wheel/right-click、`x` の後も tree/status が変化せず `Ready`。 |
| 11 | resize、極狭、hidden、symlink、permission、Unicode/control | **PASS**。width 6 の pane で `Herd`、`▾ /h`、`▸` のみの安全な表示。permission denied と retry 後の `secret`、消失 directory と再作成後の `before`、symlink の非展開、`control-�-�-tab�-name`、CJK/emoji/combining を確認。 |
| 12 | `q` / Ctrl+C と terminal 復元 | **PASS**。`wY:pT` は `q`、`wY:pV` は `ctrl+c` で pane が消え、focus が `wY:p1` に戻り、`wY:p1`/`wY:pQ`/`wY:pP` は存続。別 pty の ANSI 証拠でも `?1049h/l`、`?25l/h`、`?1002l`、`?1003l`、`?1006l` を確認。 |

Herdr の agent pane `wY:p1`、`wY:pQ`、`wY:pP` は smoke 中に終了していない。

## 性能記録

同一 WSL2 環境、`CGO_ENABLED=0` の同一 `bin/herdr-file-viewer`、pty 120x30、
ignored fixture `tmp/issue6-performance-fixture`（root は directory 1 個と file
2 個、large directory は実ファイル 2,000 個）で測定した。Go の一時 harness は
測定後に削除し、大量ファイル生成を通常テストへ持ち込んでいない。

| 測定 | 実測値 |
|---|---:|
| process start → 初回 View（pty） | 21.923 ms |
| 初回 View の process 内計測（5 回） | 15.9–57.6 µs、filesystem calls=0 |
| root `ReadDir` 完了（5 回） | 11.3–91.1 µs、filesystem calls=1 |
| root load の visible apply（5 回） | 1.9–22.8 µs |
| cached visible rows の up/down 20,000 回 | 940.1 µs、47 ns/move、calls=1 |
| large directory 初回 expand | total 2.1149 ms（ReadDir 1.2855 ms、apply 828.2 µs）、calls=2 |
| RSS | 5,888 kB |
| idle CPU | 0.5%（2.0 秒、`/proc/<pid>/stat` の utime+stime、HZ=100） |

50 ms は目標値であり、環境差のため CI 閾値にはしていない。代わりに、
filesystem call count、cached visible rows、control-safe cell width を決定的な
テストで保証している。

## 対象外・未提供

- macOS / Windows native の検証、preview、external action、編集は対象外。
- install package、distribution、release automation、release tag は未提供。
- `bin/herdr-file-viewer` と ignored fixture は commit しない。

## 人間確認

本記録の必須項目は pane snapshot/read、ANSI sequence、process state、Windows
clipboard の取得で客観確認できたため、追加の人間確認はない。
