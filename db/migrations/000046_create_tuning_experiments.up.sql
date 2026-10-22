-- tuning_experiments: 調整の実験の記録（issue #871）。1 実験 1 行。
-- 「何を変えたら、結果がどう変わったか」を同じ物差しで比べるための記録で、変えるのは変数 1 つだけ
-- （2 つ以上を同時に変える実験は MCP の experiment_create / GUI が作成時に弾く。表は 1 行に 1 変数だけを持てる形）。
-- 前後の成績・拒否理由・交絡の検出は表に持たず、experiment_compare / GUI が positions・trade_signals・
-- mcp_settings_audit・jev_decisions から毎回計算する（paper_performance_summary / risk_rejections と同じ計算）。
--   created_at     作成時刻（RFC3339 UTC）。
--   actor          'mcp'（experiment_create。Bearer 認証を通った MCP クライアント）/ 'gui'（Settings 画面）。
--                  mcp_settings_audit.actor と同じ扱い。
--   client         actor='mcp' のとき接続元ホストの IP。ヘッダの値・API キーは入れない。'gui' は空。
--   hypothesis     仮説（自由文。1〜500 文字）。
--   variable_kind  変えた変数の種類。'setting'（mcpwritesettings のホワイトリストの設定キー）/
--                  'model'（Jev のモデル。jev_decisions.model_id）/ 'prompt'（Jev のプロンプトの版。jev_decisions.question_version）。
--   variable_name  変えた変数の名前（設定キー・モデルの呼び名・プロンプトの版の呼び名。1〜200 文字）。
--   before_value / after_value  変更前・変更後の値（1〜200 文字。互いに異なる）。設定は設定画面と同じ文字列。
--   audit_id       kind='setting' のとき、その変更の mcp_settings_audit の行 ID（任意。外部キーにしないのは、
--                  監査表がマイグレーションで作り直されうるため）。変更前後の設定一式は settings_diff が監査ログから再構成できる。
--   started_at     実験の開始時刻（RFC3339 UTC）。この時刻から終了までを「後」、同じ長さだけ前を「前」として比べる。
--   ended_at       終了時刻。実行中は NULL。
--   status         'running'（実行中）/ 'closed'（終了）/ 'aborted'（中止。判定を出さない）。
--                  ended_at は status='running' のときだけ NULL。
CREATE TABLE tuning_experiments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at TEXT NOT NULL,
    actor TEXT NOT NULL CHECK (actor IN ('mcp', 'gui')),
    client TEXT NOT NULL DEFAULT '',
    hypothesis TEXT NOT NULL CHECK (length(trim(hypothesis)) > 0 AND length(hypothesis) <= 500),
    variable_kind TEXT NOT NULL CHECK (variable_kind IN ('setting', 'model', 'prompt')),
    variable_name TEXT NOT NULL CHECK (length(variable_name) BETWEEN 1 AND 200),
    before_value TEXT NOT NULL CHECK (length(before_value) BETWEEN 1 AND 200),
    after_value TEXT NOT NULL CHECK (length(after_value) BETWEEN 1 AND 200),
    audit_id INTEGER,
    started_at TEXT NOT NULL,
    ended_at TEXT,
    status TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'closed', 'aborted')),
    CHECK (before_value <> after_value),
    CHECK ((status = 'running') = (ended_at IS NULL))
);

CREATE INDEX tuning_experiments_status_idx ON tuning_experiments (status, id);
