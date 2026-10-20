-- mcp_settings_audit: Paper 実績 MCP の設定書き込み（ホワイトリスト）と書き込み許可の切替の監査ログ（issue #806、#807）。
-- 1 変更 = 1 行。MCP からの変更（actor='mcp'）に加え、Settings 画面（actor='gui'）からのホワイトリスト対象キーの
-- 変更と書き込み許可の切替も同じ表に残す（settings_diff が「いつの時点の値か」を再構成するため）。
--   at           変更時刻（RFC3339 UTC）。
--   actor        'mcp'（Bearer 認証を通った MCP クライアント）／'gui'（Settings 画面）。
--   client       actor='mcp' のときの接続元ホスト（IP のみ。ポート・ヘッダ値・鍵は含まない）。'gui' は空。
--   action       'set'（値の保存）／'reset'（保存値を消して既定へ。GUI のみ）／'write_enable'／'write_disable'。
--   setting_key  変更されたキー。write_enable／write_disable は 'mcp.settings_write_enabled'。
--   before_value / after_value  変更前後の値を Settings 画面と同じ文字列で。NULL は「保存値なし（既定が効く）」。
--     書き込み許可の切替は有効な値（'1'/'0'）を入れる。秘密に分類されるキーの値は '****' に伏せて入れる
--     （いまの許可キーに秘密はない。将来の追加キーは既定で伏せる。docs/architecture/paper-results-mcp.md §書き込み）。
CREATE TABLE mcp_settings_audit (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    at TEXT NOT NULL,
    actor TEXT NOT NULL CHECK (actor IN ('mcp', 'gui')),
    client TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL CHECK (action IN ('set', 'reset', 'write_enable', 'write_disable')),
    setting_key TEXT NOT NULL CHECK (setting_key <> ''),
    before_value TEXT,
    after_value TEXT
);

CREATE INDEX mcp_settings_audit_key_idx ON mcp_settings_audit (setting_key, id);
