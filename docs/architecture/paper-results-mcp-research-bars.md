# 設計: 立花の日足の研究用収集と状況ツール（Paper 実績 MCP の分割章）

`docs/architecture/paper-results-mcp.md` §5 の分割章。issue #1082。API は `docs/api/endpoints/paper-results-mcp-research-bars.md`（§3.36）、表は `docs/architecture/er/tables-research.md`（`research_tachibana_status` / `research_daily_bar_runs` / `research_daily_bar_progress`。マイグレーション `000059`）。

## 1. 目的

「立花の日足は 1 銘柄 1 リクエストで最大 20 年分が取れる」ので、ブローカーを立花に切り替えなくても、夜間に立花の日足を `daily_bars` に貯めておけば、後から研究（スクリーニング・特徴量の検証）に使える。これを**研究専用・既定オフ**の収集として足す。Live の Jev・Risk・発注・実行・ブローカーの選択・Paper のガード・既定値・kabu の動作・監視銘柄の選び方は**変えない**。

## 2. 収集（`internal/bootstrap/dailybars` の `Research`）

- 入口: `dailybars.NewResearch(ResearchConfig)`。設定・立花のセッション（`Acquire`）・`daily_bars`・研究用の記録・状態は**関数・インターフェース**で受け取り、`researchhub` には依存しない。配線は `internal/bootstrap/researchwire/`。
- スイッチ: Settings の `research.tachibana_bars.enabled`（`runtime_settings`。JSON 整数 0/1、**既定 0**）。毎サイクル（1 分）読む。実行時刻・速度・市場区分・最低価格・除外銘柄は夜間バッチの `broker.tachibana.nightly.*` を共用する。
- 1 サイクル: スイッチがオフなら状態 `disabled` を書くだけで `Acquire` もリクエストもしない。実行時刻前・日中（8:00〜15:30）・閉局（3:30〜5:30）は `waiting`。実行できるときだけ `Acquire`: 認証情報が未設定なら `not_configured`、立花がブローカーなら `broker_is_tachibana`（立花自身の夜間バッチが記録する。認証 ID は 1 セッションしか持てない）で、どちらもリクエスト・ログインなしで終わる。
- 夜の歩き方は夜間バッチと同じ関数（`batch.night`）。銘柄は `Source.DailyBarTargets` から夜間バッチの絞り込み（`universe`）で決め、1 銘柄 1 リクエスト。初回の成功が全履歴（埋め戻し）で、以後の夜は `store` の差分（新しい日だけ。株式分割で調整値が変わったら置き換え）。
- 再開: 銘柄ごとに `research_daily_bar_progress.last_night` を持つ。**その夜に試みた銘柄は同じ夜に取り直さない**（再起動・セッション切れ・8:00 の中断をまたいでも）。未着手・古い夜の銘柄から先に歩くので、短い夜が続いても全銘柄に順に行き渡る。夜が成功 = 対象の全銘柄を試みた（すべて失敗なら `failed`）。
- 速度・時間: `max_per_second` の間隔を空ける。8:00〜15:30、ctx の終了、セッションの喪失（`sessionLost`。夜は `failed`、10 分後に再開）で直ちに止める。夜は 1 行の `research_daily_bar_runs`（100 銘柄ごとに保存）。状態は `running`（夜の間）→ `ok` / `error`（短い・秘密を含まない理由）。
- 保存先は `daily_bars`（夜間バッチと同じ表。`market.DailyBarRepository`）。立花の日足スクリーニング（`tachibanawatch`）は立花がブローカーのときだけ動き、夜間バッチの記録（`daily_bar_runs`）の成功を条件に `daily_bars` を読む。研究用の収集は `daily_bar_runs` を書かないので、**監視銘柄の選び方を変えない**（kabu の間、監視銘柄は kabu のランキング監視のまま）。

## 3. 状況ツール

`research_daily_bars_status`（`internal/service/paperresultsmcp/researchbarsdiag`。登録は `research_bars_tool.go`）。読み取り専用接続・SELECT のみ。`daily_bars` の銘柄数と日付の範囲は、主キー `(symbol, trade_date)` の索引の飛び読み（再帰 CTE）で求め、全表を数えない。

## 4. 隔離

`internal/repository/researchbars/isolation_test.go` が、`research_daily_bar_*` と `research_tachibana_status` の表・リポジトリ・収集・MCP ツールを参照できるファイルを、収集・配線・読み取り専用ツール・マイグレーションに固定し、リポジトリの書き込み文が自分の表だけを書くことを検証する。
