-- order_proposals / order_proposal_events: あいりが MCP（order_proposal_submit）で出した Paper の注文案の受け付けと、
-- 状態の変化の記録（issue #827、親 #826）。この migration は「受け付けて記録する」ところまで。Risk check・発注・
-- 期限切れ・取消の処理は #828 でアプリが行い、この 2 表の status と下の紐付け列を更新する。MCP が書くのはこの 2 表への
-- INSERT（案 1 行＋最初のイベント 1 行を同じトランザクションで）だけで、paper_orders / positions / trade_signals には書かない。
--
-- order_proposals（1 案 1 行）
--   idempotency_key  同じ案の再送を見分けるキー（8〜64 文字、[A-Za-z0-9_-]）。UNIQUE。
--   request_hash     引数を正規化したもの（idempotency_key を除く）の SHA-256（16 進）。同じキーで内容が違えば拒否する根拠。
--   received_at      受け付け時刻（UTC）。
--   client           接続元 IP だけ（ヘッダ・API キーは残さない）。
--   mode             'paper' のみ。Live の値は CHECK で入れられない（Live の発注経路は作らない）。
--   bucket           'stock' / 'bond_etf'（資産ごとの予算枠、issue #824 と同じ値）。
--   action           'open_long'（新規の買い）／'close'（保有中ポジションの決済）。SHORT は無い。
--   quantity / budget_jpy  open_long のときどちらか一方（株数／使う金額）。close は両方 NULL。
--   order_type / limit_price  'MARKET' / 'LIMIT'。LIMIT のときだけ limit_price。
--   stop_loss_price / take_profit_price / exit_plan / max_loss_jpy  open_long の損切り・利確（任意）・手じまいの計画・
--                    あいりが見込む最大損失（円）。close は NULL（exit_plan だけ任意）。
--   rationale / sources_json  判断の理由と、情報源（title・url・retrieved_at の配列）の JSON。
--   expires_at       有効期限（UTC）。受け付け時点から最大 24 時間。
--   status / status_reason  状態（最初は 'received'）と固定コードの理由（最初は空）。状態の一覧は #828。
--   instrument_id / trade_signal_id / paper_order_id / position_id  アプリが #828 で埋める紐付け。最初は NULL。
--   updated_at       最後に状態を変えた時刻（UTC）。
CREATE TABLE order_proposals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    idempotency_key TEXT NOT NULL UNIQUE,
    request_hash TEXT NOT NULL,
    received_at TEXT NOT NULL,
    client TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL DEFAULT 'paper'
        CHECK (mode IN ('paper')),
    symbol VARCHAR(10) NOT NULL,
    bucket VARCHAR(20) NOT NULL
        CHECK (bucket IN ('stock', 'bond_etf')),
    action TEXT NOT NULL
        CHECK (action IN ('open_long', 'close')),
    quantity INTEGER
        CHECK (quantity IS NULL OR quantity > 0),
    budget_jpy NUMERIC
        CHECK (budget_jpy IS NULL OR budget_jpy > 0),
    order_type VARCHAR(10) NOT NULL
        CHECK (order_type IN ('MARKET', 'LIMIT')),
    limit_price NUMERIC
        CHECK (limit_price IS NULL OR limit_price > 0),
    stop_loss_price NUMERIC
        CHECK (stop_loss_price IS NULL OR stop_loss_price > 0),
    take_profit_price NUMERIC
        CHECK (take_profit_price IS NULL OR take_profit_price > 0),
    exit_plan TEXT,
    max_loss_jpy NUMERIC
        CHECK (max_loss_jpy IS NULL OR max_loss_jpy > 0),
    rationale TEXT NOT NULL,
    sources_json TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'received'
        CHECK (status IN ('received', 'processing', 'waiting_session', 'rejected', 'submitted',
                          'filled', 'expired', 'cancelled', 'closed', 'failed')),
    status_reason TEXT NOT NULL DEFAULT '',
    instrument_id INTEGER REFERENCES instruments (id),
    trade_signal_id INTEGER REFERENCES trade_signals (id),
    paper_order_id INTEGER REFERENCES paper_orders (id),
    position_id INTEGER REFERENCES positions (id),
    updated_at TEXT NOT NULL
);

CREATE INDEX order_proposals_received_at_idx ON order_proposals (received_at);
CREATE INDEX order_proposals_status_received_at_idx ON order_proposals (status, received_at);

-- order_proposal_events: 状態の変化を 1 行ずつ。追記だけ（UPDATE / DELETE はトリガーで拒否する）。
--   from_status  変化前の状態。受け付け時の最初の行だけ NULL。
--   to_status    変化後の状態。order_proposals.status と同じ値域。
--   reason_code  固定コード（無いときは空）。
--   actor        'mcp'（order_proposal_submit）／'app'（アプリの処理役、#828）／'gui'（Settings 画面、#829 以降）。
CREATE TABLE order_proposal_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    proposal_id INTEGER NOT NULL REFERENCES order_proposals (id),
    at TEXT NOT NULL,
    from_status VARCHAR(20)
        CHECK (from_status IS NULL OR from_status IN ('received', 'processing', 'waiting_session', 'rejected', 'submitted',
                                                       'filled', 'expired', 'cancelled', 'closed', 'failed')),
    to_status VARCHAR(20) NOT NULL
        CHECK (to_status IN ('received', 'processing', 'waiting_session', 'rejected', 'submitted',
                             'filled', 'expired', 'cancelled', 'closed', 'failed')),
    reason_code TEXT NOT NULL DEFAULT '',
    actor TEXT NOT NULL
        CHECK (actor IN ('mcp', 'app', 'gui'))
);

CREATE INDEX order_proposal_events_proposal_idx ON order_proposal_events (proposal_id, id);

CREATE TRIGGER order_proposal_events_no_update
BEFORE UPDATE ON order_proposal_events
BEGIN
    SELECT RAISE(ABORT, 'order_proposal_events is append-only: UPDATE is not allowed');
END;

CREATE TRIGGER order_proposal_events_no_delete
BEFORE DELETE ON order_proposal_events
BEGIN
    SELECT RAISE(ABORT, 'order_proposal_events is append-only: DELETE is not allowed');
END;

-- mcp_settings_audit.action に、注文案の受け付けスイッチ（runtime_settings の mcp.order_proposals_enabled）の
-- 切替 'order_proposals_enable'（GUI だけ）／'order_proposals_disable'（GUI と MCP）を足す。
-- SQLite は CHECK 制約を ALTER できないため、新テーブルへ作り直して行を移す（id はそのまま）。
CREATE TABLE mcp_settings_audit_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    at TEXT NOT NULL,
    actor TEXT NOT NULL CHECK (actor IN ('mcp', 'gui')),
    client TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL CHECK (action IN ('set', 'reset', 'write_enable', 'write_disable',
                                           'order_proposals_enable', 'order_proposals_disable')),
    setting_key TEXT NOT NULL CHECK (setting_key <> ''),
    before_value TEXT,
    after_value TEXT
);

INSERT INTO mcp_settings_audit_new (id, at, actor, client, action, setting_key, before_value, after_value)
SELECT id, at, actor, client, action, setting_key, before_value, after_value FROM mcp_settings_audit;

DROP TABLE mcp_settings_audit;
ALTER TABLE mcp_settings_audit_new RENAME TO mcp_settings_audit;

CREATE INDEX mcp_settings_audit_key_idx ON mcp_settings_audit (setting_key, id);
