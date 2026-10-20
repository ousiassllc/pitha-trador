-- health_alerts / health_alert_checks: 立会中の市況データ停止の警告状態（issue #802）。
-- アプリ内のヘルス検知（internal/service/healthalert）が一定間隔で書き、Paper 実績 MCP（別プロセス・
-- 読み取り専用接続）と全ページ共通バナーが読む。MCP は書けないため、状態は DB に持つ。
-- health_alerts: いま出ている警告。1コード=1行。条件が解けると行ごと消える（履歴は持たない）。
--   code は固定コード（自由文ではない）:
--     kabu_not_logged_in  取引日の 08:45〜15:30 に kabuステーションが未ログイン（08:45 の時点で未ログイン、
--                         または立会中に未ログイン〔4001007〕が5分続いた）
--     market_data_idle    立会中に market-data ジョブが（成功も失敗も）5分間ゼロ（アプリ停止・ワーカー停止）
--   since は警告を出した時刻（RFC3339 UTC）、checked_at は検知が最後に条件を確かめた時刻。
-- health_alert_checks: 検知の心拍。1行だけ（id=1）。checked_at が古い間はアプリが動いていないので、
--   MCP は health_alerts の行を信用せず、立会中なら market_data_idle を導出する。
CREATE TABLE health_alerts (
    code TEXT PRIMARY KEY CHECK (code <> ''),
    since TEXT NOT NULL,
    checked_at TEXT NOT NULL
);

CREATE TABLE health_alert_checks (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    checked_at TEXT NOT NULL
);
