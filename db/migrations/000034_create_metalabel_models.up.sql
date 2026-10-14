-- metalabel_models / metalabel_predictions: メタラベリング（入る/見送る判定モデル）の
-- モデル保存と判断ごとの予測値（issue #757、親 #752、docs/architecture/er/tables-metalabel.md）。
-- 検証は paper のみ。モデルが無い・無効なときは呼び出し側が「見送り」に倒す。

-- metalabel_models: 学習ごとのモデルのバージョンとOOS（ウォークフォワードの検証窓）指標。
-- version は学習ごとに一意な識別子。status は active=採用可 / rejected=OOS指標が基準に届かず不採用（理由は reject_reason）。
-- 最新の有効モデルは status='active' で trained_at が最大の行。
-- train_* / oos_* は学習窓・OOS窓の期間（RFC3339 UTC）。Jev特徴量の評価はLLMのcutoff以降だけに
-- 限るため、OOS窓の期間を画面・ログに出せるよう必ず保存する。
-- label_version は学習ラベルの版（calibration_outcomes.label_version。2=トリプルバリア）。
-- feature_names_json は特徴量名の配列（学習時の並び）、params_json は推論に必要なパラメータ
-- （ロジスティック回帰の係数・標準化・較正器など。中身はアルゴリズムごとのJSON）。
-- oos_auc / oos_expected_value_bps はOOSのAUCと、較正後に take とした判断の平均EV（bps）。
-- 定義できない場合（OOSが片側のラベルだけ等）はNULL。oos_calibration_json は較正曲線
-- （[{"mean_predicted":…,"observed_rate":…,"count":…}]）。
CREATE TABLE metalabel_models (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    version TEXT NOT NULL UNIQUE CHECK (version <> ''),
    algorithm TEXT NOT NULL CHECK (algorithm <> ''),
    label_version INTEGER NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'rejected')),
    reject_reason TEXT NOT NULL DEFAULT '',
    trained_at TEXT NOT NULL,
    train_start TEXT NOT NULL,
    train_end TEXT NOT NULL,
    oos_start TEXT NOT NULL,
    oos_end TEXT NOT NULL,
    train_samples INTEGER NOT NULL CHECK (train_samples >= 0),
    oos_samples INTEGER NOT NULL CHECK (oos_samples >= 0),
    feature_names_json TEXT NOT NULL,
    params_json TEXT NOT NULL,
    oos_auc NUMERIC CHECK (oos_auc BETWEEN 0 AND 1),
    oos_expected_value_bps NUMERIC,
    oos_calibration_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL
);

CREATE INDEX metalabel_models_status_trained_at_idx ON metalabel_models (status, trained_at);

-- metalabel_predictions: 判断（jev_decisions）ごとのモデル予測値。同じモデルは1判断に1行。
-- raw_probability は較正前、calibrated_probability は較正後の「その一次シグナルに乗ると勝つ」確率。
-- expected_value_bps は較正後確率から求めたコスト控除後の期待値（bps）。
-- action は take/skip、size_fraction は take のときの建玉サイズ比率（0..1。skip は 0）。
-- reason は判定の根拠（skip の理由など）。features_json は推論に使った特徴量（監査・再現用）。
CREATE TABLE metalabel_predictions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    model_id INTEGER NOT NULL REFERENCES metalabel_models (id),
    jev_decision_id INTEGER NOT NULL REFERENCES jev_decisions (id),
    predicted_at TEXT NOT NULL,
    raw_probability NUMERIC NOT NULL CHECK (raw_probability BETWEEN 0 AND 1),
    calibrated_probability NUMERIC NOT NULL CHECK (calibrated_probability BETWEEN 0 AND 1),
    expected_value_bps NUMERIC NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('take', 'skip')),
    size_fraction NUMERIC NOT NULL CHECK (size_fraction BETWEEN 0 AND 1),
    reason TEXT NOT NULL DEFAULT '',
    features_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL
);

CREATE UNIQUE INDEX metalabel_predictions_model_decision_idx ON metalabel_predictions (model_id, jev_decision_id);
CREATE INDEX metalabel_predictions_decision_idx ON metalabel_predictions (jev_decision_id);
