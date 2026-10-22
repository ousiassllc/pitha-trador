# Paper 実績 MCP（Grok Bot コネクタ）: 改訂履歴

`docs/architecture/paper-results-mcp.md`から分割した改訂履歴（linterlyの1ファイル300行上限のため）。`paper-results-mcp.md`の変更はここの末尾に1行ずつ追記する。

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-17 | 新規作成 | issue #783。読み取り専用 Paper 実績 MCP の仕様。実装は範囲外 |
| 1.1 | 2026-10-17 | 必須の認証を API キーだけにし、経路を Tailscale・任意の Cloudflare Tunnel・その他のプライベートトンネルへ一般化した。Cloudflare Access は §3.5 の任意 | 悠太さん指示。Cloudflare を必須にしない。Tailscale も検討する |
| 1.2 | 2026-10-18 | 実装を追加した（`cmd/paper-results-mcp`、`internal/service/paperresultsmcp`）。冒頭の「実装は範囲外」を改めた。トランスポートは Streamable HTTP（POST のみ・JSON 応答）とし、旧来の HTTP+SSE は実装しない（API 仕様の要確認事項への回答） | issue #783 の実装。仕様の変更はなし |
| 1.3 | 2026-10-19 | §3.4 に Settings 画面からの設定（有効/待受アドレス/API キー）を追加。原子的な書き込み、サーバー側の検証、鍵の一度きりの表示、DB・ログ・エクスポート・バックアップに入れないこと、プロセスは起動・停止せず反映に再起動が要ることを明記 | issue #792 |
| 1.4 | 2026-10-19 | §3.4 に、画面からの起動・停止・再起動とアプリ起動時の自動起動・アプリ終了時の停止を追記し、設計メモ `paper-results-mcp-lifecycle.md` への参照を追加。「画面はプロセスを起動も停止もしない」を改めた。DB 読み取り専用・既定オフ・キーが空なら待受しない・待受アドレス検証は変更なし | issue #795 |
| 1.5 | 2026-10-20 | §5 の `collection_status` に立会中の市況データ停止の警告 `alerts[]` を追加。状態はアプリの検知が `health_alerts`（`000039`）に保存し、MCP は読むだけ。アプリが止まっているときは MCP が `jobs` から `market_data_idle` を導く。DB は読み取り専用のまま | issue #802 |
| 1.6 | 2026-10-20 | §5 に `health_today`（今日の健康診断。取引数・メタモデル・ブートストラップ・kabu/market-data・欠損・日次ガード・Kill Switch・要対応コード）を追加し、ツール数を 9 にした。`runtime_settings` を固定キーの許可リストで読み取りだけ読む旨を追記。新規テーブルなし | issue #808 |
| 1.7 | 2026-10-20 | 設定の読み書きを追加（§7。ホワイトリスト 15 キー（当時。issue #824 で 17 キーになった。版 1.11）、書き込み許可のスイッチ＝`runtime_settings`、狭い書き手、監査ログ `mcp_settings_audit`、`settings_get` / `settings_set` / `settings_diff`）。「MCP は書かない」を「書くのはホワイトリストの設定ツールだけで、書き込み接続は別の狭い書き手」に改めた（§4・§5・§6）。Paper 資金の分母を Settings の保存値に追従させた（§7.6）。読み取り接続・待受・認証は変更なし | issue #806・#807 |
| 1.8 | 2026-10-20 | §7.5 に、`settings_get` の `llm_cutoff_status` と、`settings_set` の LLM cutoff 4 キー応答の同コードを追記 | issue #809 |
| 1.9 | 2026-10-20 | §7.4 の GUI の保存を、MCP と同じく設定と監査を 1 トランザクションにする形に改めた（監査の失敗は保存も取り消す）。MCP の書き込み経路は従来から 1 トランザクションで、変更なし | issue #813 |
| 1.10 | 2026-10-20 | §5 の `health_today` の欠損セッションを、それ以前のスナップショットがある日付だけ（実際の欠損）に改め、履歴が足りず判定できない立会日は `insufficient_history_dates[]` に分けて警告にしない旨を追記。DB は読み取り専用のまま | issue #814 |
| 1.11 | 2026-10-20 | §7.1 のホワイトリストに Paper の予算枠 2 キー（`risk.bucket.stock.budget_jpy`・`risk.bucket.bond_etf.budget_jpy`）を足して 17 キーにした。枠の合計が運用資金を超える保存（枠の保存・運用資金を下げる保存・既定に戻す）の拒否と、枠の間で予算を移すときは減らす方を先に保存する手順を追記。ツール数（15）・読み取り接続・待受・認証は変更なし | issue #824（親 #823） |
| 1.12 | 2026-10-21 | Paper の注文案の受け付けを追加（§8。`order_proposal_submit` / `order_proposal_get` / `order_proposals_disable`、受け付けスイッチ `mcp.order_proposals_enabled`、表 `order_proposals`・`order_proposal_events`、マイグレーション `000042`）。§5・§6 の「売買・発注のツールは作らない」を「Paper の注文案を出すツールだけ追加。発注はアプリ。注文の表には書かない。Live の発注・Live の値・Kill Switch の操作は不変」に改めた。§7.3 の書き込み経路を 4 種類に、§7.4 の監査の操作名を 6 つにした。ツール数は 18。ホワイトリストは 17 キーのまま | issue #827（親 #826） |
| 1.13 | 2026-10-21 | 注文案の取消の依頼 `order_proposal_cancel` を追加（§8.1・§8.5。取消の依頼だけを記録し、実際の取消はアプリ。書き手の変更は 5 種類、ツール数は 19）。注文案のアプリ側の処理（Risk check・Paper 発注・期限・取消・`close`・再起動の回復）を `order-proposal-processing.md` に分けて追記。`risk_rejections` に案の分の件数を追加。ホワイトリストは 17 キーのまま | issue #828（親 #826） |
| 1.14 | 2026-10-21 | 注文案の読み取りを追加（§5・§8.1。`order_proposals_list` / `order_proposal_summary` を足し、`order_proposal_get` に結果を足した。ツールは 21）。`paper_trades` / `paper_performance_summary` に入り口 `origin`、`health_today` に止まった注文案（要対応コード `order_proposal_stalled`）。すべて読み取り接続のままで、新しい表は無い。GUI の「注文案」画面は同じ読み取りモデルを使う | issue #829（親 #826） |
| 1.15 | 2026-10-21 | `risk_rejections`（§5）に、注文案が Risk を通ったが注文にならなかった行の件数と理由コード別内訳 `proposal_rejected_after_risk_*` を追加し、`risk_passed_count`・`other_reject_*` から外した（`trade_signals.reject_reason = 'proposal_rejected_after_risk: <コード>'`。`000045` が過去行も直す）。新規テーブルなし | issue #860（親 #826） |
