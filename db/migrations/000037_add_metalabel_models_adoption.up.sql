-- metalabel_models: 採用判定の根拠（issue #780、親 #752）。
-- 採用基準を点推定ではなく信頼区間で判定し、基準値と結果（OOS件数・take件数・EV/AUCと信頼区間・
-- 現行モデルとの比較・棄却理由）を後から確認できるよう保存する。いずれも学習結果の集計指標・
-- 判定メタデータで、板・気配などの市場データは含まない（既存列の意味と保持は変えない）。
-- oos_takes: OOS上で較正後に take とした判断の件数（oos_expected_value_bps の母数）。
-- oos_days: OOS標本が属する取引日（JST）の数（ブートストラップの再抽出単位＝同日クラスタの数）。
-- oos_ev_ci_*_bps / oos_auc_ci_*: 同日・同銘柄の相関を考慮したクラスタ・ブートストラップの信頼区間
-- （下側・上側。定義できないときはNULL）。
-- adoption_json: 採用基準（criteria）・現行モデルとの比較（comparison）・変更頻度制限（gate）の詳細。
-- forward_json: 採用後の前向き評価（採用判断に使っていない、学習後に生じた判断での追跡評価）。夜間再学習が更新する。
ALTER TABLE metalabel_models ADD COLUMN oos_takes INTEGER NOT NULL DEFAULT 0 CHECK (oos_takes >= 0);
ALTER TABLE metalabel_models ADD COLUMN oos_days INTEGER NOT NULL DEFAULT 0 CHECK (oos_days >= 0);
ALTER TABLE metalabel_models ADD COLUMN oos_ev_ci_low_bps NUMERIC;
ALTER TABLE metalabel_models ADD COLUMN oos_ev_ci_high_bps NUMERIC;
ALTER TABLE metalabel_models ADD COLUMN oos_auc_ci_low NUMERIC;
ALTER TABLE metalabel_models ADD COLUMN oos_auc_ci_high NUMERIC;
ALTER TABLE metalabel_models ADD COLUMN adoption_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE metalabel_models ADD COLUMN forward_json TEXT NOT NULL DEFAULT '{}';
