-- 見送り候補の反実仮想ラベル（issue #1052、親 #752）。ガードで見送った LONG/SHORT の Jev 判断のうち、
-- calibration_outcomes にラベルが付かなかったもの（Outcome Labeling の再試行窓＝24 時間より古くなって
-- 取り残されたもの）に、「もし入っていたらどうなったか」を既存メタラベルと同じ定義で残す。
-- 取引には使わない記録: Policy・Risk・発注・Paper ガード・Live・Jev のプロンプトは読まない。読むのは
-- 夜間再学習（設定 metalabel.include_skipped_labels がオンのときだけ）と、書く後追い記録部品だけ。
-- calibration_outcomes・jev_decisions には既存の列も制約も足さない（既存行の数値は書き換えない）。
-- 時刻はすべて UTC の RFC3339（小数 9 桁固定。docs/architecture/er.md §型・規約「日時」）。
--
-- 行の定義は calibration_outcomes の label_version 2（トリプルバリア）と同じ。方向のない判断（NONE）は
-- 方向を補完しないので、この表には入らない（CHECK）。
--   jev_decision_id  見送った判断。(jev_decision_id, horizon_minutes) で一意。
--   horizon_minutes  ラベルの窓（分）。Settings のトリプルバリアのタイムアウト。
--   reject_reason    見送りの理由（trade_signals.reject_reason。シグナルが無ければ空）。
--   barrier_hit / net_return_bps / entry_ref_price / label_version  calibration_outcomes と同じ意味
--                    （net_return_bps はコスト控除後の bps）。
--   future_return / max_adverse_excursion / max_favorable_excursion  同上（%）。
--   llm_cutoff       記録したときの metalabel.llm_cutoff（確認済みの日付。仮置きのあいだは記録しない）。
--   created_at       記録した時刻。
CREATE TABLE counterfactual_labels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    jev_decision_id INTEGER NOT NULL REFERENCES jev_decisions (id),
    horizon_minutes INTEGER NOT NULL CHECK (horizon_minutes > 0),
    reject_reason TEXT NOT NULL DEFAULT '',
    barrier_hit TEXT NOT NULL CHECK (barrier_hit IN ('tp', 'sl', 'timeout')),
    net_return_bps NUMERIC NOT NULL,
    entry_ref_price NUMERIC NOT NULL CHECK (entry_ref_price > 0),
    label_version INTEGER NOT NULL CHECK (label_version = 2),
    future_return NUMERIC NOT NULL,
    max_adverse_excursion NUMERIC NOT NULL,
    max_favorable_excursion NUMERIC NOT NULL,
    llm_cutoff TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (jev_decision_id, horizon_minutes)
);

-- 夜間再学習の読み取り（判断ごとに最新の行）。
CREATE INDEX counterfactual_labels_decision_idx ON counterfactual_labels (jev_decision_id, id);

-- trade_signals.jev_decision_id の索引: 見送りかどうかの判定（上の表の対象選びと下の印）が判断ごとにシグナルを引くため。
-- 列も制約も変えない（読み取りの索引だけ）。
CREATE INDEX trade_signals_jev_decision_idx ON trade_signals (jev_decision_id);

-- calibration_outcome_origins: calibration_outcomes の各行が、エントリーした判断のものか、見送った判断の
-- ものかを見分ける印（行は増やさず、既存行の数値も書き換えない。トリガも持たず、読むたびに導く）。
--   origin 'entered'      その判断のシグナルが Policy・Risk を通った（risk_passed = 1。発注の案が作られた）。
--          'skipped'      LONG/SHORT と答えたがシグナルが通らなかった（シグナルが無い判断も含む）。
--          'no_direction' Jev の方向が NONE（jev_none）。方向がなく、学習行には入らない。
--   reject_reason         その判断の最新のシグナルの reject_reason（無ければ NULL）。
-- 学習に使うのは origin によらず従来どおり（ListTrainingRowsFrom は変わらない）。分析用の読み取り専用。
CREATE VIEW calibration_outcome_origins AS
SELECT o.id AS calibration_outcome_id,
    o.jev_decision_id AS jev_decision_id,
    o.horizon_minutes AS horizon_minutes,
    CASE
        WHEN d.direction IS NULL OR d.direction = 'NONE' THEN 'no_direction'
        WHEN EXISTS (SELECT 1 FROM trade_signals s WHERE s.jev_decision_id = d.id AND s.risk_passed = 1) THEN 'entered'
        ELSE 'skipped'
    END AS origin,
    (SELECT s2.reject_reason FROM trade_signals s2 WHERE s2.jev_decision_id = d.id ORDER BY s2.id DESC LIMIT 1) AS reject_reason
FROM calibration_outcomes o
JOIN jev_decisions d ON d.id = o.jev_decision_id;
