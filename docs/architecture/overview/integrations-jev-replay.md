# アーキテクチャ設計: Jev リプレイ評価（保存済みの入力を後からアームに聞く。issue #1085）

`docs/architecture/overview/integrations.md` §6（Jev API連携）から分割した節。お試し記録（`integrations-jev-trial.md`。issue #1059）の兄弟で、ワイヤ層・アーム・キー・費用の数え方・日次上限・キーの伏せ方はそれをそのまま使う。テーブルは `docs/architecture/er/tables-jev-replay.md`、MCP ツールは `docs/api/endpoints/paper-results-mcp-jev-replay.md` を参照。

## 位置づけ

お試し記録は「これから起きる判断」の時点でしかモデルを比べられない。`jev_decisions` には過去の判断の入力（`state_json`＝ライブの Jev に送った状態。エクスポートでは `request_state_json`）が残っているので、期間・件数を決めて**過去の判断をお試しのアームに後から聞き直し**、同じ入力での差を何日も待たずに見る。Paper と研究用だけで、結果は**ライブの判断・取引・Policy・Risk・RAG の索引・Calibration・メタモデルの学習のどれにも入らない**。

- 実装: `internal/service/jevreplay`（ランナー・見積もり。テストは `replayflow/`）・`internal/repository/jevreplay`・`internal/domain/jevreplay`・`internal/config/jevreplaysettings`（`jev.replay.enabled`）・`internal/service/paperresultsmcp/replaytool`（MCP ツール。`replay_tools.go` が登録）・`internal/bootstrap/replaywire`（組み立て。`Services.Start` に 1 行）。
- **お試しのランナー（`internal/service/jev/trial`）の動きは変えない**。リプレイは `trial` の純粋な関数（`AnsweredCost`・`FailedCost`・`BuildDecision`・`ReservedUSD`・`ProviderFor`。`replay_api.go`）と `trialwire`（`TraderRaw`）を呼ぶ。`jev_trial_decisions`・`jev_decisions`・`jev_shadow_decisions`・`jev_trial_skips` には**書かない**。既存のシャドー（trader-v5）・既存の適時開示の見送りルール（`disclosureskip`）・ライブの Jev・Risk・発注には触れない。
- 実行には有料の呼び出しが要る。**既定はオフ**で、オンにするのはマージ後に人が Settings 画面で行う（キーと 1 日の上限は #1063 で決める）。この Issue の作業とテストは、モック・`httptest`・カウントするフェイクだけで、外部へは 1 回も呼ばない。

## スイッチと入口

| 項目 | 値・動き |
|------|----------|
| スイッチ | `runtime_settings` の `jev.replay.enabled`（JSON の整数 1 / 0）。**行が無い・壊れている・読めないときはオフ**（`jevreplaysettings.Enabled`）。`jev.trial.enabled` と同じく `settings_set` のホワイトリスト（`mcpwritesettings`）・汎用の Settings キー・自己改善ループの変更経路のいずれにも**無い**（`replayflow/isolation_test.go` が固定）。MCP からオンにはできない |
| 外部呼び出し | スイッチがオフ・API キー（`OPENROUTER_API_KEY`。お試しと同じ 1 つ）が無い・アームが無いときは **0 回**。オフのときはランナーの 1 パスが設定を 1 行読んで戻るだけで、run の行も触らない |
| Paper 以外 | `metalabelsettings.RuntimeMode` が Paper 以外なら何もしない（お試しと同じ） |
| run の入口 | MCP の `replay_start`（`jev_replay_runs` に `pending` の行を 1 件入れるだけ）。呼び出しは**アプリ内のランナー**が行う（MCP は何も送らない） |
| 組み立て | `researchwire.Wire.Start`（`replaywire`）。ランナー専用の DB 接続（`sidecar.Open`）で動き、Risk が読む DB 書き込み失敗の連続（`DBWriteFailures`）に影響しない。30 秒ごとに 1 パス。`ctx` の終了で進行中の呼び出しを取り消して閉じる |

## 開始前の見積もり（`replay_estimate`）と開始の条件

`replay_start` は、先に `replay_estimate` を取らないと開始できない。

1. **見積もり**: 期間（ライブの判断の時刻）・`max_rows`（**必須**。1〜1000。古い順に数える）・アーム（既定は `jev.trial.arms` の全アーム）から、`BuildPlan` が対象の判断と呼び出し（**判断 × アーム**。アームごとの学習カットオフの規則を適用した後の数）を決め、費用の見込みを返す。
   - **費用の見込み** `max_cost_usd` = 呼び出しごとの**予約と同じ最悪値側の単価**（リクエスト本文から推定した入力トークン × `jevtrialsettings.WorstCaseUSDPerMtok`。`trial.ReservedUSD`）の合計。`list_price_usd` は定価での目安（定価が分からないアームがあれば `null`）。
   - **`confirm_token`** = 見積もりの内容（指定・アームの設定（キーは含まない）・対象の判断の数と最大の id・呼び出し数・最悪値側の費用）のハッシュ。
2. **開始**: `replay_start` は同じ引数と `confirm_token` を受け、トランザクションの中で**もう一度見積もり直し**、(a) `jev.replay.enabled` がオン、(b) 動いている run が無い（**同時に 1 件**）、(c) 呼び出しが 1 件以上、(d) `confirm_token` が今の見積もりと一致、をすべて確かめてから 1 行入れる。見積もりの後に判断・アーム・サイズが変わると不一致で拒否する（見積もりを経ない・上限を超える開始はできない）。MCP の書き込み許可がオフのときも拒否する。
3. run には、開始時の見積もり（件数・呼び出し数・最悪値側の費用）・アームの設定の写し・`max_decision_id`（見積もり後に増えた判断を対象に入れない）が残る。**ランナーはこの最悪値側の費用を超えたら run を止める**（`stopped` / `run_cost_cap`）。

## 学習カットオフの規則

- アームに任意の `llm_cutoff`（`YYYY-MM-DD`。ベンダーが公表した学習データの終端日。`jevtrialsettings.Arm.LLMCutoff`）を持てる。無いアームは従来どおりで、既存の設定は変わらない（後方互換。検証は空か日付だけ）。**日付を推測で入れない**。公開日（Gemini Decision 1: 2026-11-11、Clef 系: 2026-10-01。`integrations-jev-trial.md`）は学習の終端日ではないので `llm_cutoff` には入れない。
- **`llm_cutoff` があるアーム**には、その日**より後**（翌日 0 時 UTC 以降）の判断だけを聞く。それ以前の判断は対象に入らない（見積もりの `excluded_before_cutoff` に件数が出る）。結果の `llm_cutoff_status` は `verified`、`reference_only` は 0。
- **`llm_cutoff` が無いアーム**は、`llm_cutoff_status = unverified`（画面・MCP では**未確認**）。判断は全部聞くが、**結果は参考扱い**（`reference_only = 1`。DB の CHECK でも `unverified` の行は必ず 1）。学習データに入っていた可能性を排除できないため、これだけで優劣を決めない。

## 送る入力

- **保存された入力そのまま**を送る: `jev_decisions.state_json` を `trialwire.Client.TraderRaw` が**デコードせずに** `state.market` の値としてそのまま載せる（キーの順序・未知のキーも変えない）。判断より後のデータを足さない。
- ライブのお試しは同時点の RAG 文脈（類似事例）も送るが、保存されていない RAG 文脈を今作ると**その判断より後の判断が入る**ので、リプレイの `similar_past_cases` は**常に空**（`{}`）。したがってライブのお試しとは入力が少し違う（RAG 無し）。行の `rag_includes_live_model_answers` は常に 0。
- 質問群はアームのプロンプト版（登録済みの Trader 版）のものを使う（お試しと同じ）。

## 費用の数え方・上限（お試しと同じ仕組み）

| 項目 | 動き |
|------|------|
| 費用の数え方 | `trial.BuildDecision`（`AnsweredCost` / `FailedCost`）をそのまま使う: 報告値 → 定価での推定 → 不明（最悪値側で数える）。HTTP 4xx は課金されない前提で 0。行にはお試しと同じ列（`cost_usd`・`cost_source`・`counted_cost_usd`）で残す |
| 予約 | 呼び出しの前に、最悪値側の費用を**飛んでいる呼び出しの分として予約**し、行を保存してから解放する（`trial.reserve` と同じ。メモリのみ）。上限の判定は「保存済みの行 + 予約 + この呼び出し」 |
| 1 日の上限（JST） | **全体**（`jev.trial.daily_cost_cap_usd`）と**アームごと**（`DailyCostCapUSD`）。保存済みの費用は**お試しの行（`jev_trial_decisions`）とリプレイの行（`jev_replay_results`）の合計**で数える（リプレイがお試しと同じ上限を超えない）。上限に達したら**新しい呼び出しを出さず**、run は `running` のまま `blocked_reason`（`daily_cap_overall` / `daily_cap_arm`）を残して待ち、翌日（JST）に自動で続きから再開する |
| 上限の残り | お試しの予約（`trial.Trial` のメモリ）はリプレイから見えないため、同時に動くときは両方が上限の手前の判定を通って、飛んでいる呼び出しの分（最大 4 本×最悪値）だけ超えうる。超過の幅は飛んでいる呼び出しで抑えられる |
| 停止の記録 | お試しの停止記録（`jev.trial.halt` の全体停止・アームの停止・429/529 の待機）は**読んで従う**が、**書かない**（リプレイが書くのは自分の 2 表だけ）。HTTP 402 は run を止める（`payment_required`）。403 はそのアームをその日止める（メモリ）。429/529 はそのアームを 15 分待つ（メモリ）。失敗した行が 1 日 20 回（`DailyFailureCap`）に達したアームはその日止める（行から数える） |
| 間隔・並列 | アームごとに最短 2 秒（`MinCallInterval`）。リプレイは捨てずに**待つ**。同時 4 本まで。再試行はしない（有料の呼び出しは 1 回きり。1 回 45 秒まで） |
| キーの伏せ方 | エラーの抜粋は `trial.BuildDecision` が、設定済みのキーの文字列と API キーらしい文字列を `[redacted]` に置き換えてから 200 バイトに切る。キーはコード・ログ・エラー・MCP の応答・テストの値（`test-key-xxxx`）のどこにも入れない。応答本文（`response_json`）は表に残すが MCP は返さない |

## run の状態と再開

| `status` | 意味 |
|----------|------|
| `pending` | 入れた（`replay_start`）。ランナーはまだ拾っていない |
| `running` | ランナーが進めている。上限・停止中・待機で呼べないあいだも `running` で、理由は `blocked_reason` |
| `completed` | 計画した呼び出しすべてに結果の行がある（呼べなかったアームが残っていれば完了にせず待つ） |
| `stopped` | 止めた。`stop_reason` = `requested`（`replay_stop`）/ `run_cost_cap`（見積もりの最悪値）/ `payment_required`（402） |
| `failed` | 始められなかった。`stop_reason` = `key_not_set`（キー無し。**何も送らない**）/ `no_arms`（アームが無い・プロンプト版が未登録） |

- **停止**: `replay_stop` は `stop_requested` を 1 にするだけ。ランナーは次の呼び出しの前にそれを見て `stopped`（`requested`）にする。記録済みの結果は残る。止める操作は費用を減らすだけなので MCP の書き込み許可のスイッチを見ない。
- **再開・取り消しの安全**: 結果の行は (run, 判断, アーム) が一意で、行の保存と run の進み具合（`calls_done`・`calls_failed`・`counted_cost_usd`）の更新は 1 トランザクション。アプリの終了（`Close`）で取り消された呼び出しは行を残さず数えない。run は `running` のまま残り、次の起動で、**結果の行が無い組だけ**を聞く（同じ組を 2 回聞かない）。スイッチをオフにしたときも新しい呼び出しは出ない。

## 結果の読み方（`replay_results`）

`jev_trial_compare` と**同じ指標**（`trialquery.CompareReplay`。cohort・共通の成功数 `common_success_count`・欠測率 `missing_rate`・ラベル（`calibration_outcomes`）での当たり率と `live_baseline`・見送りの反実仮想ラベル（コスト控除後の `net_return_bps`）の proxy）を、run の行と**同じライブの判断**について出す（コードは共通）。アームごとの行数・参考扱いの行数・学習カットオフの確認状態（未確認など）・API の費用を添える。応答本文・エラー本文・キーは返さない。リプレイにはお試しの見送り記録が無いので `fairness.skipped` は空で、学習カットオフ以前の判断の除外は `replay_estimate` の `excluded_before_cutoff` で見る。
