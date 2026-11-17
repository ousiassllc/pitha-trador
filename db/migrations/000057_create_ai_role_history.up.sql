-- ai_role_history: どの AI の役割（Jev の売買判断・Sol・Opus・Luna）が、どのプロバイダーのどのモデルで動いているかの記録と、
-- Settings 画面で役割のプロバイダー・モデルを変えた履歴（issue #1080）。
-- 役割の設定そのものは runtime_settings の ai.role.<role> に持ち、この表は「いつ・何から何へ・誰が」と
-- 「アプリが起動したとき実際に使い始めた組」を残す。調整の実験（tuning_experiments）の交絡の検出（#885 の jev_model_changes と
-- 同じ用途）と、保存した設定と今動いている組のずれ（反映には再起動が要る）の確認に使う。
--
-- 1 行 = 1 件。kind で 2 種類を分ける:
--   kind='saved'   Settings 画面で役割の設定を保存・既定に戻したとき（actor='gui'）。runtime_settings の保存と同じ
--                  トランザクションで書く。値が変わらない保存は行を作らない。before_* は保存前の組、provider/model は保存後の組。
--   kind='started' アプリの起動時に、その役割が実際に使う組を記録（actor='app'）。直前の 'started' と同じなら行を作らない。
--                  before_* は NULL。
--   role           'jev' | 'sol' | 'opus' | 'luna'
--   provider       'custom'（独自 API）| 'openrouter'
--   model          モデル名。'' は「その役割にモデルの指定なし」（独自 API の既定・Jev 以外の独自 API）。
--     プロバイダー名とモデル名は秘密ではない。API キー・接続先 URL など秘密情報の値はこの表に入れない。
--     書くのは Settings 画面の保存の経路と起動時の記録だけで、MCP からは書けない。
CREATE TABLE ai_role_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    at TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('saved', 'started')),
    actor TEXT NOT NULL CHECK (actor IN ('gui', 'app')),
    role TEXT NOT NULL CHECK (role IN ('jev', 'sol', 'opus', 'luna')),
    before_provider TEXT CHECK (before_provider IN ('custom', 'openrouter')),
    before_model TEXT,
    provider TEXT NOT NULL CHECK (provider IN ('custom', 'openrouter')),
    model TEXT NOT NULL DEFAULT '',
    CHECK ((kind = 'saved' AND actor = 'gui' AND before_provider IS NOT NULL AND before_model IS NOT NULL
            AND NOT (before_provider = provider AND before_model = model))
        OR (kind = 'started' AND actor = 'app' AND before_provider IS NULL AND before_model IS NULL))
);

CREATE INDEX ai_role_history_role_idx ON ai_role_history (role, id);
