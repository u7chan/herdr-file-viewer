# セッション復元スモーク記録 (Issue #95)

Issue #95 の変更（startup hook による Files / Preview のその場復元）だけを
対象に、Herdr 0.8.2 / WSL2 上で実施した記録である。検証用 server は短い別
`XDG_CONFIG_HOME` / `XDG_STATE_HOME` と名前付き session `hfv95` を使い、
本番の socket (`/home/u7dev/.config/herdr/herdr.sock`) と plugin state には
一切触れていない。時間値は環境依存の観測値であり、CI の固定失敗閾値には
使わない。

## 実施環境

- Linux x86_64 / WSL2、Herdr 0.8.2（mise 経由）
- ブランチ: `feat/issue-95-restore-panes`
- 検証用 fixture: `/tmp/hfv95-smoke`（config / state / fixtures、実施後に削除）

## 決定的テスト（自動）

```bash
test -z "$(gofmt -l $(git ls-files '*.go'))"
mise x go@1.27.0 -- golangci-lint run
go vet ./...
go test ./...
go test -race ./...
CGO_ENABLED=0 go build -trimpath ./cmd/herdr-file-viewer
GOTOOLCHAIN=local go test ./...          # go1.25.14 実ツールチェーン
GOTOOLCHAIN=local CGO_ENABLED=0 go build -trimpath ./cmd/herdr-file-viewer
```

結果: すべて **PASS**（lint `0 issues.`）。追加仕様テスト:

- startup event が preview / help / files 判定より先に処理され TUI を起動しない
- 複数 workspace の `Files` / `Preview` 候補列挙と他 label の非変更
- `pending -> ready`、`already`、`timeout`、API failure（fake runner /
  fake retry、timing assertion なし）
- Files context が pane ごとの保存 cwd から生成される
- Preview state の server 別・pane 別分離と欠落・破損時の safe skip
- 空白 / single quote / `=` を含む root・preview・plugin path の `/bin/sh`
  による quote round-trip
- 復元 Preview の別ファイル切替での close → reopen（通常 `pane close`
  fallback を含む）
- `View` / resize が PreviewClient（Herdr CLI・state ファイルの実装）へ
  一切アクセスしない

`bin/herdr-file-viewer` は最終コードから再ビルドし、ignored のまま commit しない。

## Manifest smoke

```text
herdr plugin link /home/u7dev/workspace/herdr-file-viewer
PASS: plugin_linked。startup=[{command=["./bin/herdr-file-viewer"]}]、
warnings なし、panes=[files,preview,help]。plugin list --json でも同内容。
```

## Herdr 0.8.2 / WSL2 smoke（isolated server）

1. 8 workspace（`w1`..`w8`）+ neg workspace（`wB`）を作成。`w1`..`w6` は
   Files + Preview、`w7` は Files のみ、`w8` は root + Files、`wB` は
   「Files」label の shell + 「Shell」「Terminal」label の shell。
2. Preview 対象は空白・`=`・single quote を含むパス。
3. Preview 起動時に per-pane state が
   `$XDG_STATE_HOME/herdr/plugins/u7chan.file-viewer/preview/<socket hash>/<pane>.json`
   に保存されることを確認。
4. `C` で `w6:p2` の表示 root を `ws6/sub6` へ移動 → pane cwd も同期。

### stop → start 1 回目

- hook ログ: Files 8 pane が `restored`。Preview 6 pane はこの環境の
  prompt helper の問題（後述）により `timeout reason=foreground not ready
  within 30s` を plugin log に記録。
- helper 復帰後、同一 hook 実行で Preview 6 pane が `restored`。
  `w1:p3` の title が `/tmp/hfv95-smoke/fixtures/pre view=one.md`（空白と
  `=` を含む）、`w3:p3` が `plain =quote'file.txt`（single quote を含む）、
  内容表示と `preview=<path>` token の再報告を確認。

### stop → start 2 回目（全 pane 復元済み状態から）

- hook ログ: Files 8 が `restored`。Preview は再び起動直後の prompt
  helper 滞留で `timeout`（plugin log で reason 確認）→ helper 復帰後に
  同一機構で `restored`。
- stop 前後の snapshot（pane_id / label / cwd / tab_id / focused）を
  workspace 別に diff → **完全一致**。`w6:p2` の移動後 root
  （`ws6/sub6`）も一致。
- 二重起動なし: 各 pane の foreground process は viewer 1 プロセスのみ。
- `w7`（Preview なし Space）に Preview は復元されず、`wB:p2`（Shell）、
  `wB:p3`（Terminal）は未変更。

### live handoff

- `wB:p1`（「Files」label の shell）で `sleep 120` を foreground 実行中の
  まま `herdr server live-handoff` を実施。
- 新 server の startup hook: 生存中の Files/Preview 14 pane すべて
  `already reason=viewer already running`（二重起動なし、プロセスは
  handoff を生存）、`wB:p1` は `sleep` 実行中のため 30 秒 poll 後に
  `timeout` で未変更。shell / Terminal label の pane は候補外。
- focus (`w1:p1`) は handoff 後も不変。

### 環境ノート（観測値）

- この開発機は egress が不安定で、shell 起動時の `starship init bash` /
  `zoxide init bash`（update check の network 待ち）が foreground に
  滞留することがある。滞留時は foreground が
  `[bash, starship|zoxide]` となり、Issue #95 の定義どおり prompt helper
  として `pending -> timeout` になる（plugin log で reason 確認可能）。
  滞留 helper を終了させると shell は idle になり、hook の 1 秒 poll
  ウィンドウ内で `ready` に遷移して復元される。検証用 server の
  `XDG_CONFIG_HOME/starship.toml` に `[update] disabled = true` を置く
  ことで starship 側の滞留は抑止できる（本番設定には触れない）。
- Herdr 0.8.2 の live handoff では pane metadata token は引き継がれない
  （`restore_handoff` が `MetadataTokens::default()` を復元する）。
  生存 viewer は tracked pane ID で動作を継続し、次の cold restore で
  preview が再起動したときに token を再報告するため、復元機能の動作は
  維持される。token 不在時の tracked pane は既存の
  「token なし tracked pane は close → reopen」経路に乗る。

## 対象外・制約

- TUI 内部状態（展開・選択・scroll など）の復元、Help popup の復元、
  消えた pane の新規作成、generic restore framework は Issue #95 の
  対象外どおり未実装。
- `bin/herdr-file-viewer` と検証用 fixture は commit しない。