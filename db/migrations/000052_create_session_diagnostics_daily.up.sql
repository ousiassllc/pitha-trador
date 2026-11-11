-- session_diagnostics_daily: 立会日ごとの「セッションのそろい」の日次診断（issue #1054）。
--
-- 成功ジョブは 7 日（retention.DefaultSucceededJobDays）、health_alerts は「いま出ている警告」しか持たないため、
-- 1 週間より前の欠けは原因をたどれなかった。この表は、1 立会日 = 1 行の短い診断を、ジョブ保持期間より長く
-- （削除しない。1 日 1 行で小さい）残す。価格・出来高・生データは入れない（件数・時刻・固定コードだけ）。
--
-- 書くのはアプリ（internal/service/sessiongap.Recorder。1 分ごと）だけで、読むのは Paper 実績 MCP
-- （health_today / collection_status / session_diagnostics）。取引・リスク・ポリシー・Jev のプロンプトは読まない。
-- 欠けた日の市況を後から埋めることはしない: 行に書くのは、その時点の market_snapshots が示す事実だけ。
--
--   session_date   立会日（JST の YYYY-MM-DD）。主キー。休業日の行は作らない。
--   gap_kind       'in_progress' = 当日の観測中（欠けの判定はまだ無い。読み手は無視する）。
--                  'none' = そろっている／'whole_day' = 終日 0 件／'intraday' = 日中の欠け（前場だけ無い・開始の遅れ・
--                  場中の長い途切れ・早い終わり）／'insufficient_history' = 履歴の始まりより前（#814。実欠けではない）。
--   gap_codes_json 日中の欠けの固定コード（JSON 配列。例: ["morning_missing"]）。欠けが無ければ []。
--   gaps_json      欠けた区間（JSON 配列。{code, from, to, minutes}。from/to は RFC3339 UTC。多くても 10 件）。
--   alert_codes_json 当日アプリが観測した健康警告の固定コード（JSON 配列。kabu_not_logged_in 等。立会外では消えるため
--                  観測中に積み上げる）。アプリが動いていない時間の警告は残らない。
--   watch_checks   アプリが立会中に観測した回数（1 分に 1 回）。0 や極端に少ない日はアプリが止まっていた手がかり。
--   snapshot_rows / snapshot_minutes  その日の market_snapshots の行数／1 件以上ある分の数（前場・後場の立会内）。
--   first_snapshot_at / last_snapshot_at  最初／最後のスナップショットの時刻（RFC3339 UTC）。NULL = 0 件。
--   longest_gap_minutes  立会内の最長の途切れ（分。開始・終了の端を含む）。
--   market_data_jobs_succeeded / _failed  その日に終わった market-data ジョブの数（確定時点の jobs）。
--   recorded_at    最後に書いた時刻（RFC3339 UTC）。
CREATE TABLE session_diagnostics_daily (
    session_date TEXT PRIMARY KEY CHECK (length(session_date) = 10),
    gap_kind TEXT NOT NULL CHECK (gap_kind IN ('in_progress', 'none', 'whole_day', 'intraday', 'insufficient_history')),
    gap_codes_json TEXT NOT NULL DEFAULT '[]',
    gaps_json TEXT NOT NULL DEFAULT '[]',
    alert_codes_json TEXT NOT NULL DEFAULT '[]',
    watch_checks INTEGER NOT NULL DEFAULT 0 CHECK (watch_checks >= 0),
    snapshot_rows INTEGER NOT NULL DEFAULT 0 CHECK (snapshot_rows >= 0),
    snapshot_minutes INTEGER NOT NULL DEFAULT 0 CHECK (snapshot_minutes >= 0),
    first_snapshot_at TEXT,
    last_snapshot_at TEXT,
    longest_gap_minutes INTEGER NOT NULL DEFAULT 0 CHECK (longest_gap_minutes >= 0),
    market_data_jobs_succeeded INTEGER CHECK (market_data_jobs_succeeded IS NULL OR market_data_jobs_succeeded >= 0),
    market_data_jobs_failed INTEGER CHECK (market_data_jobs_failed IS NULL OR market_data_jobs_failed >= 0),
    recorded_at TEXT NOT NULL
);
