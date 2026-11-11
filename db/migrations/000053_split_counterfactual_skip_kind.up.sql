-- 見送り候補の反実仮想ラベルに、見送りの種類と cutoff 未確認の印を足す（issue #1052 の判断反映、親 #752）。
--
-- 1. skip_kind: Risk Engine に却下された候補と、Policy のガードが見送った候補を、reject_reason の接頭辞から
--    後で推測せずに列で分ける。
--      risk_rejected  最新のシグナルの reject_reason が risk_engine_rejected（または risk_engine_rejected: で始まる）。
--                     Policy は通ったが Risk Engine が却下した。
--      guard_skipped  シグナルがあり、上以外の reject_reason（entry_quality_below_threshold・meta_skip・
--                     thin_liquidity など Policy 側のガード）。
--      no_signal      その判断のシグナルが 1 件も無い（Policy まで届かなかった）。
--    ビュー calibration_outcome_origins の origin も同じ語で分ける（旧 'skipped' は上の 3 つに分かれる）。
--    表の reject_reason（細かい理由の文字列）はそのまま残す。
--    Go 側の分類は internal/repository/counterlabel の SkipKindOf。ビューと表の既存行はこのファイルの SQL で
--    同じ規則を適用する（同じ規則であることはテストで固定）。
-- 2. cutoff_unverified: 記録したときの metalabel.llm_cutoff が「日付はあるがモデル未記録」（CutoffUnverified）だった行は 1。
--    記録は続けるが、学習と評価の行には入れない（ListTrainingRowsFrom の見送りラベル側が cutoff_unverified = 0 に絞る）。
--    確認済み（confirmed）で記録した行は 0。仮置き（placeholder）のあいだは今までどおり記録しない。
-- マイグレーション 000050 はこの変更のブランチだけに存在し、main には無い（リリース前）。000050 まで適用した
-- 開発用 DB にも当たるよう、別番号の追加にしてある。既存行の skip_kind は上の規則で導き、cutoff_unverified は
-- 0 にする（「未確認の印が付いていない」の意味。000050 時点の記録は確認済みの cutoff を条件にしていなかったが、
-- 印の記録が無いので後から導けない。必要なら運用者が UPDATE する）。
ALTER TABLE counterfactual_labels ADD COLUMN skip_kind TEXT NOT NULL DEFAULT 'guard_skipped'
    CHECK (skip_kind IN ('risk_rejected', 'guard_skipped', 'no_signal'));
ALTER TABLE counterfactual_labels ADD COLUMN cutoff_unverified INTEGER NOT NULL DEFAULT 0
    CHECK (cutoff_unverified IN (0, 1));

-- 既存行は、記録した reject_reason の複製ではなく、その判断の最新のシグナル（ビューと同じ元）から導く。
UPDATE counterfactual_labels SET skip_kind = CASE
    WHEN ls.id IS NULL THEN 'no_signal'
    WHEN ls.reject_reason = 'risk_engine_rejected' OR substr(ls.reject_reason, 1, 21) = 'risk_engine_rejected:' THEN 'risk_rejected'
    ELSE 'guard_skipped'
END
FROM (SELECT d.id AS decision_id, s.id, s.reject_reason
      FROM jev_decisions d
      LEFT JOIN trade_signals s ON s.id = (SELECT MAX(s2.id) FROM trade_signals s2 WHERE s2.jev_decision_id = d.id)) AS ls
WHERE ls.decision_id = counterfactual_labels.jev_decision_id;

DROP VIEW calibration_outcome_origins;
CREATE VIEW calibration_outcome_origins AS
SELECT o.id AS calibration_outcome_id,
    o.jev_decision_id AS jev_decision_id,
    o.horizon_minutes AS horizon_minutes,
    CASE
        WHEN d.direction IS NULL OR d.direction = 'NONE' THEN 'no_direction'
        WHEN EXISTS (SELECT 1 FROM trade_signals s WHERE s.jev_decision_id = d.id AND s.risk_passed = 1) THEN 'entered'
        WHEN ls.id IS NULL THEN 'no_signal'
        WHEN ls.reject_reason = 'risk_engine_rejected' OR substr(ls.reject_reason, 1, 21) = 'risk_engine_rejected:' THEN 'risk_rejected'
        ELSE 'guard_skipped'
    END AS origin,
    ls.reject_reason AS reject_reason
FROM calibration_outcomes o
JOIN jev_decisions d ON d.id = o.jev_decision_id
LEFT JOIN trade_signals ls ON ls.id = (SELECT MAX(s2.id) FROM trade_signals s2 WHERE s2.jev_decision_id = d.id);
