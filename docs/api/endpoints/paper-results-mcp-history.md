# API 仕様: Paper 実績 MCP のツール: 改訂履歴

`docs/api/endpoints/paper-results-mcp.md`から分割した改訂履歴（linterlyの1ファイル300行上限のため）。`paper-results-mcp.md`の変更はここの末尾に1行ずつ追記する。

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-17 | 新規作成 | issue #783。ツールの入出力・上限・出さない列を、既存マイグレーションの列名で固定 |
| 1.1 | 2026-10-17 | 受け入れ条件の実機確認を、選んだ経路（Tailscale または Cloudflare Tunnel）経由に変えた。Tailscale は tailnet 外から届かないことも確認する。必須の 401 は API キーだけ | 悠太さん指示。Cloudflare を必須にしない |
| 1.2 | 2026-10-18 | トランスポートの要確認を、Streamable HTTP（POST のみ・JSON 応答）で実装した旨の回答に改めた。ツール・上限・ホワイトリストは変更なし | issue #783 の実装 |
| 1.3 | 2026-10-20 | §3.8 `collection_status` に `alerts[]`（`code`・`since`。`kabu_not_logged_in` / `market_data_idle`）を追加。マイグレーション `000039` の `health_alerts` / `health_alert_checks` を読む | issue #802 |
| 1.4 | 2026-10-20 | §3.1 `capital_jpy` の既定を 500,000 に更新し、Settings で保存した Paper の運用資金（`runtime_settings`）には追従しない旨（`runtime_settings` を読まない方針を維持）を明記 | issue #804 |
| 1.5 | 2026-10-20 | §3.9 `health_today` を追加（今日の Paper 取引数・メタモデル・ブートストラップ・kabu/market-data・欠損・日次ガード・Kill Switch・再学習の見送りと、要対応フラグ `needs_attention` / 固定コード `attention_codes[]`）。§4 の `runtime_settings` を、固定キーのみ読み取りで読む記述に改めた | issue #808 |
| 1.6 | 2026-10-20 | §3.1 `capital_jpy` を、Settings の保存値（`runtime_settings` の `risk.paper.initial_capital_jpy`）に追従し、なければ risk.yaml とする仕様に更新。§3.10〜§3.15 に設定ツール（`settings_get` / `settings_set` / `settings_diff` / `settings_audit` / `settings_write_enable` / `settings_write_disable`）とホワイトリスト 15 キー、§4 の `runtime_settings` の行、§5 の受け入れ条件を追加 | issue #806・#807 |
| 1.7 | 2026-10-20 | §3.11 `settings_get` に `llm_cutoff_status`（`placeholder` / `unverified` / `confirmed`）を追加。§3.12 `settings_set` の LLM cutoff 4 キーの応答に `llm_cutoff_status` を付け、MCP だけで日付と出典を更新する手順を追記 | issue #809 |
| 1.8 | 2026-10-20 | §3.9 `health_today` の `missing_session_dates[]` を、それ以前のスナップショットがある日付だけ（実際の欠損）に改め、履歴が足りず判定できない立会日を `insufficient_history_dates[]` に分けた。`attention_codes` の `missing_session` は `insufficient_history_dates[]` だけが空でないときは出さない | issue #814 |
| 1.9 | 2026-10-20 | §3.10 のホワイトリストに Paper の予算枠 2 キー（`risk.bucket.stock.budget_jpy`・`risk.bucket.bond_etf.budget_jpy`）を足して 17 キーにした（`settings_get` / `settings_diff` にも出る）。§3.12 に、枠の合計が運用資金を超える保存の拒否（監査行なし）と、枠の間で予算を移すときは減らす方を先に保存する手順を追記。§4・§5 の 15 キーを 17 キーに改めた。ツールの数・入出力の形は変更なし | issue #824（親 #823） |
| 1.10 | 2026-10-21 | §3.16〜§3.18（分割章 `paper-results-mcp-proposals.md`）に注文案ツール（`order_proposal_submit` / `order_proposal_get` / `order_proposals_disable`。Paper の注文案を受け付けて記録するだけ）を追加し、ツール数を 18 にした。§3.14 の `action` に `order_proposals_enable` / `order_proposals_disable` を追加。§4 に受け付けスイッチの読み取りと `order_proposals.request_hash` / `client` を、§5 に注文案の受け入れ条件を追加。ホワイトリストは 17 キーのまま | issue #827（親 #826） |
| 1.11 | 2026-10-21 | §3.16〜§3.19（分割章）に `order_proposal_cancel`（取消の依頼を記録するだけ）を追加しツール数を 19 にした。§3.6 の `risk_rejections` に `proposal_risk_passed_count` / `proposal_risk_rejected_count` を追加。§3.3 の `exit_reason` の既知の値に `proposal_close` を追加 | issue #828（親 #826） |
| 1.12 | 2026-10-21 | §3.17〜§3.22（分割章）に `order_proposals_list` / `order_proposal_summary` を追加し、`order_proposal_get` に結果（signal・order・position・最大損失・損切りと利確に届いたか）を足してツール数を 21 にした（読み取り 9・設定 6・注文案 6）。§3.1 `paper_performance_summary` に `by_origin`、§3.3 `paper_trades` に `origin` を追加。§3.9 `health_today` に `order_proposals` と要対応コード `order_proposal_stalled` を追加（9 種）。読み取り接続のままで、新規テーブルなし | issue #829（親 #826） |
| 1.13 | 2026-10-21 | §3.6 `risk_rejections` に `proposal_rejected_after_risk_count` と `proposal_rejected_after_risk_breakdown[]` を追加。Risk を通ったが注文にならなかった注文案を `risk_passed_count` / `other_reject_count` から外した（`reject_reason` は `proposal_rejected_after_risk: <コード>`。`order_proposal_get` の `result.signal` は `rejected_after_risk` で Risk 通過後の拒否を区別し、`reject_code` に同じ接頭辞を扱う）。新規テーブルなし | issue #860（親 #826） |
| 1.14 | 2026-10-21 | §3.8 `collection_status.alerts[]` と §3.9 `health_today` に、kabu ステーションへの接続拒否の固定コード `kabu_unreachable` を追加（`attention_codes` の並びは `kabu_not_logged_in` の次。`market_data.kabu_login` に `unreachable`）。アプリが止まっているときの `market_data_idle` の導出は変更なし。スリープ防止のキー `power.sleep_guard_enabled` は設定ツールのホワイトリストに入れない | issue #863 |
