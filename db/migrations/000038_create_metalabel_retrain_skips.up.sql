-- metalabel_retrain_skips: 夜間のメタモデル再学習が「データ不足」で見送られた実行記録（issue #799、親 #752）。
-- 見送りはモデルを保存しないため、これが無いと外から気づけない。1見送り=1行。
-- reason_code は固定コード（自由文ではない）:
--   too_few_rows_before_oos   検証期間（OOS窓）より前の使えるラベル付き判断が、fit+較正の最小件数に足りない
--   too_few_rows_after_purge  purge/embargo 後の fit または較正が最小件数に足りない
--   training_data_unusable    件数は足りるが学習・較正できない（ラベルが片側だけ等）
-- usable_rows: 検証期間（OOS窓）より前にあった、使えるラベル付き判断の件数。
-- required_rows: 学習に必要な件数（fit と較正の最小件数の合計。「必要130件中 x件」の130）。
-- skipped_at は見送りの判定時刻（RFC3339 UTC）。件数と固定コードだけで、板・価格・自由文は含まない。
CREATE TABLE metalabel_retrain_skips (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    skipped_at TEXT NOT NULL,
    reason_code TEXT NOT NULL CHECK (reason_code <> ''),
    usable_rows INTEGER NOT NULL CHECK (usable_rows >= 0),
    required_rows INTEGER NOT NULL CHECK (required_rows >= 0),
    created_at TEXT NOT NULL
);

CREATE INDEX metalabel_retrain_skips_skipped_at_idx ON metalabel_retrain_skips (skipped_at);
