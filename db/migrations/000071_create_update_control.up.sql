-- update_control_status / update_control_requests: デスクトップアプリの自動更新（internal/service/updater）を、
-- 別プロセスの Paper 実績 MCP（update_status・update_check_now・update_apply。issue #1222）から見る・動かすための
-- 受け渡しの 2 表。MCP プロセスはアプリの Checker・SafeGate・QuitForUpdate を持たず（アプリの DB 接続もハンドルも
-- 環境変数も渡さない、という起動の安全条件は変えない）、注文案（order_proposals）と同じく DB を窓口にする。
-- 状態を書き、依頼を受けて実行するのはアプリ（internal/service/updater/mcpbridge）だけ。MCP が書くのは依頼の 1 行
-- INSERT と、時間切れの依頼を取り下げる 1 回の UPDATE だけで、状態の表・設定・発注・ポジションには書かない。
-- cmd/server にはアップデーターが無いので、この 2 表は何も書かれない（MCP は「アップデーターなし」と答える）。
--
-- update_control_status（アプリが書く現在の状態。常に 1 行 = id 1。アプリの終了時に消える）
--   heartbeat_at     アプリが最後に書いた時刻（RFC3339 UTC）。一定時間更新が無ければ MCP はアップデーターなしと答える
--                    （アプリが落ちて行が残ったとき用）。
--   current_version  動いているアプリの版（internal/version.Version）。
--   checked_at       最後の確認が終わった時刻。まだ確認していなければ NULL。
--   dev_build / no_release / available / blocked / ready  updater.Status の同名のフラグ（0/1）。
--   latest_version   available のときの最新リリースのタグ。それ以外は空。
--   blocked_kind     SafeGate が止めた条件（updater.BlockKind）。blocked でなければ空。
--   error_kind       最後の確認の失敗の種別（updater.ErrorKind）。失敗していなければ空。
--   生のエラー文・インストーラーのパス・URL・公開鍵は持たない（種別だけ。Settings のパネルと同じ方針、issue #241）。
--
-- update_control_requests（MCP の依頼。1 依頼 1 行。update_apply の行は導入の依頼の監査も兼ねる）
--   kind          'check'（今すぐ確認。Checker.CheckForUpdate と同じ経路。終了・導入はしない）／'apply'（導入の依頼。
--                 検証済みのインストーラーがあり SafeGate を通るときだけ既存の QuitForUpdate を呼ぶ）。
--   actor         'mcp' のみ。
--   client        接続元ホスト（IP のみ）。
--   requested_at  依頼時刻（RFC3339 UTC）。
--   state         'pending'（未着手）／'running'（アプリが着手）／'done'。
--   outcome       'checked'／'check_failed'（確認の結果。種別は status 側）、'accepted'／'deferred'（導入の結果）、
--                 'expired'（アプリが一定時間内に着手せず、取り下げた）。done になるまで空。
--   reason        deferred の理由（SafeGate の種別、'not_ready'＝検証済みのインストーラーが無い）。それ以外は空。
--   finished_at   done にした時刻。
-- update_apply はアプリが着手する前に MCP が待ち切れなかったとき、pending のまま取り下げる（遅れて導入が動かない）。
CREATE TABLE update_control_status (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    heartbeat_at TEXT NOT NULL,
    current_version TEXT NOT NULL,
    checked_at TEXT,
    dev_build INTEGER NOT NULL CHECK (dev_build IN (0, 1)),
    no_release INTEGER NOT NULL CHECK (no_release IN (0, 1)),
    available INTEGER NOT NULL CHECK (available IN (0, 1)),
    latest_version TEXT NOT NULL DEFAULT '',
    blocked INTEGER NOT NULL CHECK (blocked IN (0, 1)),
    blocked_kind TEXT NOT NULL DEFAULT ''
        CHECK (blocked_kind IN ('', 'open_positions', 'kill_switch', 'recent_order', 'check_failed')),
    ready INTEGER NOT NULL CHECK (ready IN (0, 1)),
    error_kind TEXT NOT NULL DEFAULT ''
        CHECK (error_kind IN ('', 'network', 'rate_limit', 'access', 'verification', 'release', 'other'))
);

CREATE TABLE update_control_requests (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    kind TEXT NOT NULL CHECK (kind IN ('check', 'apply')),
    actor TEXT NOT NULL CHECK (actor = 'mcp'),
    client TEXT NOT NULL DEFAULT '',
    requested_at TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'running', 'done')),
    outcome TEXT NOT NULL DEFAULT ''
        CHECK (outcome IN ('', 'checked', 'check_failed', 'accepted', 'deferred', 'expired')),
    reason TEXT NOT NULL DEFAULT ''
        CHECK (reason IN ('', 'open_positions', 'kill_switch', 'recent_order', 'check_failed', 'not_ready')),
    finished_at TEXT
);

CREATE INDEX update_control_requests_state_idx ON update_control_requests (state, id);
