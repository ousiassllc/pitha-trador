-- jev_model_changes: Jev のモデル（Settings 画面の秘密情報 JEV_MODEL）を切り替えた記録（issue #885）。
-- 1 切り替え = 1 行。「いつ・何から何へ・誰が」を残し、調整の実験（tuning_experiments）の交絡の検出と、
-- モデル実験（variable_kind='model'）の開始時刻の確認に使う。jev_decisions.model_id は判断が走った後にしか
-- 残らないため、判断が 1 件も無い期間の切り替えはこの表でしか分からない。
--
-- 書くのは Settings 画面の保存・削除の経路（system.SecretsRepository）だけで、JEV_MODEL のときだけ、秘密情報の
-- 保存と同じ 1 トランザクションで書く。値が変わらない保存（同じモデル名の保存し直し・未設定の削除）は行を作らない。
-- 記録の開始より前の切り替えは遡れない（この表に行が無い = 「記録なし」で、「切り替えなし」とは限らない）。
--   at           保存した時刻（RFC3339 UTC）。反映にはアプリの再起動が要るため、判断の model_id が変わるのはこれ以降。
--   actor        'gui'（Settings 画面）。Settings 画面のほかに秘密情報を書く経路は無い。
--   before_value 切り替え前のモデル名。NULL は「未設定（既定のモデルが効く）」。
--   after_value  切り替え後のモデル名。NULL は「削除して未設定へ戻した」。
--     モデル名は秘密ではない。JEV_MODEL 以外の秘密情報（API キーなど）の値はこの表に入れない。
CREATE TABLE jev_model_changes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    at TEXT NOT NULL,
    actor TEXT NOT NULL CHECK (actor IN ('gui')),
    before_value TEXT,
    after_value TEXT,
    CHECK (before_value IS NOT after_value)
);

CREATE INDEX jev_model_changes_at_idx ON jev_model_changes (at);
