-- verify-queries.sql — 実績検証クエリ（issue #777）。実スキーマ（db/migrations 000004,6,7,8,11,33,34,35,36）版。
--
-- 使い方: アプリを止めた状態のDB（またはバックアップのコピー）に対して、読み取り専用で実行する。
--   sqlite3 -readonly -header -column pitha.db ".read tools/verify-queries.sql"
-- 1本ずつ実行したいときは `-- name: Qn` の行から次の `-- name:` の直前までを切り出す。
-- go test ./internal/repository/actuals/ が、空のDBと合成DBに対して全クエリを実行し（構文・列名の検証）、
-- 合成DBでは手計算の期待値と照合する。
--
-- 規約:
--   * 全クエリは SELECT のみ（書き込みなし）。板・時価の生値（market_snapshots、jev_decisions.state_json、
--     response_json、metalabel_predictions.features_json）は読まない・出力しない。出力は集計値だけ。
--   * 期間: Q1〜Q5 は先頭の params CTE の from_ts / to_ts（UTC。保存形式は RFC3339 文字列なので辞書順比較で足りる）を
--     書き換える。既定は全期間。params を持たない Q0・Q6〜Q9 は全期間が対象（絞り方は docs/requirements/functional/performance-evaluation.md §6）。
--     initial_capital（Q3 の params）は config/risk.yaml の paper.initial_capital（Paper では
--     仮置きの値。口座残高ではない）に合わせて書き換える。
--   * 円÷円の比はSQLiteの整数除算に注意（NUMERIC列は整数値だと整数で保存されるため）。bpsは先に 10000.0 を掛けてから割る。
--   * 日付はJST（UTC+9）の暦日。週は strftime('%W')（月曜始まり・第0週あり。ISO週とは年初・年末で異なる）。
--   * 損益は positions.realized_pnl（円、手数料控除後、約定価格はスリッページ・スプレッド込み）。
--     スリッページは約定価格に含まれ、paper_orders.slippage_bps（基準価格より何bp不利だったか）から円換算する。
--   * calibration_outcomes は label_version で絞る（1=終値のみの旧ラベル、2=トリプルバリア。混ぜない）。
--     future_return は%単位、net_return_bps はコスト控除後のbps（label_version=2のみ）。同値（0）は的中・外れに数えない。
--   * 実取引と予測の結合: positions.entry_order_id -> paper_orders.trade_signal_id -> trade_signals.jev_decision_id
--     -> jev_decisions / metalabel_predictions（同じ判断に複数モデルの予測があるときは最新=最大idの1件）。

-- name: Q0 データの在庫（件数・期間・ラベル版・結合の取りこぼし）
SELECT 'closed_positions' AS item, COUNT(*) AS n,
       MIN(date(closed_at, '+9 hours')) AS first_day, MAX(date(closed_at, '+9 hours')) AS last_day,
       COUNT(DISTINCT date(closed_at, '+9 hours')) AS days
FROM positions WHERE closed_at IS NOT NULL AND realized_pnl IS NOT NULL
UNION ALL
SELECT 'open_positions', COUNT(*), NULL, NULL, NULL FROM positions WHERE closed_at IS NULL
UNION ALL
SELECT 'closed_without_signal_link', COUNT(*), NULL, NULL, NULL
FROM positions p JOIN paper_orders o ON o.id = p.entry_order_id
WHERE p.closed_at IS NOT NULL AND p.realized_pnl IS NOT NULL AND o.trade_signal_id IS NULL
UNION ALL
SELECT 'outcomes_label_version_' || label_version, COUNT(*), NULL, NULL, NULL
FROM calibration_outcomes GROUP BY label_version
UNION ALL
SELECT 'metalabel_models_' || status, COUNT(*), NULL, NULL, NULL FROM metalabel_models GROUP BY status;

-- name: Q1 Paper実績（全体）: 損益・件数・勝率・EV（円/bps）・Profit Factor・手数料
WITH params(from_ts, to_ts) AS (VALUES ('0000-01-01T00:00:00Z', '9999-12-31T00:00:00Z')),
t AS (
  SELECT p.id, p.realized_pnl AS pnl,
         p.realized_pnl * 10000.0 / (p.entry_price * p.quantity) AS ret_bps,
         eo.fees + COALESCE(x.fees, 0) AS fees,
         date(p.closed_at, '+9 hours') AS day
  FROM positions p
  JOIN paper_orders eo ON eo.id = p.entry_order_id
  LEFT JOIN paper_orders x ON x.id = p.exit_order_id, params
  WHERE p.closed_at IS NOT NULL AND p.realized_pnl IS NOT NULL
    AND p.closed_at >= from_ts AND p.closed_at < to_ts AND p.entry_price * p.quantity > 0
)
SELECT COUNT(*) AS n, COUNT(DISTINCT day) AS days,
       SUM(pnl > 0) AS wins, ROUND(1.0 * SUM(pnl > 0) / COUNT(*), 4) AS win_rate,
       ROUND(SUM(pnl), 2) AS net_pnl_jpy, ROUND(AVG(pnl), 2) AS ev_jpy, ROUND(AVG(ret_bps), 4) AS ev_bps,
       ROUND(SUM(fees), 2) AS fees_jpy,
       ROUND(1.0 * SUM(CASE WHEN pnl > 0 THEN pnl ELSE 0 END) / NULLIF(-SUM(CASE WHEN pnl < 0 THEN pnl ELSE 0 END), 0), 4) AS profit_factor
FROM t;

-- name: Q2 切り口別（日・週・方向・exit_reason・A/Bアーム・メタモデル版）の損益・件数・勝率・EV
WITH params(from_ts, to_ts) AS (VALUES ('0000-01-01T00:00:00Z', '9999-12-31T00:00:00Z')),
t AS (
  SELECT p.id, p.realized_pnl AS pnl, p.realized_pnl * 10000.0 / (p.entry_price * p.quantity) AS ret_bps,
         date(p.closed_at, '+9 hours') AS day,
         strftime('%Y-W%W', p.closed_at, '+9 hours') AS week,
         p.side AS direction, COALESCE(p.exit_reason, '') AS exit_reason,
         COALESCE(d.ab_arm, '') AS ab_arm, COALESCE(m.version, '') AS model_version
  FROM positions p
  JOIN paper_orders eo ON eo.id = p.entry_order_id
  LEFT JOIN trade_signals s ON s.id = eo.trade_signal_id
  LEFT JOIN jev_decisions d ON d.id = s.jev_decision_id
  LEFT JOIN metalabel_predictions mp ON mp.id = (SELECT MAX(id) FROM metalabel_predictions WHERE jev_decision_id = d.id)
  LEFT JOIN metalabel_models m ON m.id = mp.model_id, params
  WHERE p.closed_at IS NOT NULL AND p.realized_pnl IS NOT NULL
    AND p.closed_at >= from_ts AND p.closed_at < to_ts AND p.entry_price * p.quantity > 0
),
g AS (
  SELECT 'day' AS dim, day AS k, pnl, ret_bps FROM t
  UNION ALL SELECT 'week', week, pnl, ret_bps FROM t
  UNION ALL SELECT 'direction', direction, pnl, ret_bps FROM t
  UNION ALL SELECT 'exit_reason', exit_reason, pnl, ret_bps FROM t
  UNION ALL SELECT 'ab_arm', ab_arm, pnl, ret_bps FROM t
  UNION ALL SELECT 'meta_model', model_version, pnl, ret_bps FROM t
)
SELECT dim, k AS key, COUNT(*) AS n, ROUND(SUM(pnl), 2) AS net_pnl_jpy,
       ROUND(1.0 * SUM(pnl > 0) / COUNT(*), 4) AS win_rate,
       ROUND(AVG(pnl), 2) AS ev_jpy, ROUND(AVG(ret_bps), 4) AS ev_bps
FROM g GROUP BY dim, k ORDER BY dim, k;

-- name: Q3 最大ドローダウン（円・資金比）: 累積損益の山（開始=0）からの最大下落
WITH params(from_ts, to_ts, initial_capital) AS (VALUES ('0000-01-01T00:00:00Z', '9999-12-31T00:00:00Z', 30000000.0)),
c AS (
  SELECT p.closed_at, SUM(p.realized_pnl) OVER (ORDER BY p.closed_at, p.id) AS cum
  FROM positions p, params
  WHERE p.closed_at IS NOT NULL AND p.realized_pnl IS NOT NULL AND p.closed_at >= from_ts AND p.closed_at < to_ts
),
d AS (
  SELECT cum, MAX(0, MAX(cum) OVER (ORDER BY closed_at ROWS UNBOUNDED PRECEDING)) - cum AS dd FROM c
)
SELECT COUNT(*) AS n, ROUND(COALESCE(MAX(dd), 0), 2) AS max_drawdown_jpy,
       (SELECT initial_capital FROM params) AS initial_capital,
       ROUND(COALESCE(MAX(dd), 0) / (SELECT initial_capital FROM params) * 100.0, 4) AS max_drawdown_pct_of_capital
FROM d;

-- name: Q4 コスト内訳: グロス（コスト前）とネット、手数料、約定価格に含まれるスリッページ（円）
-- slippage_bps は約定が基準価格より何bp不利だったか。買い約定は 価格=基準*(1+b)、売り約定は 価格=基準*(1-b)。
-- LONGは買いで入り売りで出る、SHORTは売りで入り買いで出る。記録が無い約定（NULL）は0円として数える。
WITH params(from_ts, to_ts) AS (VALUES ('0000-01-01T00:00:00Z', '9999-12-31T00:00:00Z')),
f AS (
  SELECT p.realized_pnl AS net, eo.fees + COALESCE(x.fees, 0) AS fees,
         CASE WHEN eo.slippage_bps IS NULL THEN 0 ELSE p.quantity * p.entry_price * (eo.slippage_bps / 10000.0)
              / (1 + CASE WHEN p.side = 'SHORT' THEN -1 ELSE 1 END * eo.slippage_bps / 10000.0) END AS entry_slip,
         CASE WHEN x.slippage_bps IS NULL OR x.filled_price IS NULL THEN 0 ELSE p.quantity * x.filled_price * (x.slippage_bps / 10000.0)
              / (1 + CASE WHEN p.side = 'SHORT' THEN 1 ELSE -1 END * x.slippage_bps / 10000.0) END AS exit_slip,
         (eo.slippage_bps IS NOT NULL) + (x.slippage_bps IS NOT NULL) AS slip_recorded_fills
  FROM positions p
  JOIN paper_orders eo ON eo.id = p.entry_order_id
  LEFT JOIN paper_orders x ON x.id = p.exit_order_id, params
  WHERE p.closed_at IS NOT NULL AND p.realized_pnl IS NOT NULL AND p.closed_at >= from_ts AND p.closed_at < to_ts
)
SELECT COUNT(*) AS n, ROUND(SUM(net), 2) AS net_pnl_jpy, ROUND(SUM(fees), 2) AS fees_jpy,
       ROUND(SUM(net + fees), 2) AS gross_before_fees_jpy,
       ROUND(SUM(entry_slip + exit_slip), 2) AS slippage_jpy,
       ROUND(SUM(net + fees + entry_slip + exit_slip), 2) AS gross_before_costs_jpy,
       SUM(slip_recorded_fills) AS slippage_recorded_fills
FROM f;

-- name: Q5 Jevの方向的中率とBrier（label_version別。同値は除く。水平線別）
WITH params(from_ts, to_ts) AS (VALUES ('0000-01-01T00:00:00Z', '9999-12-31T00:00:00Z'))
SELECT o.label_version, o.horizon_minutes, COUNT(*) AS n,
       ROUND(AVG(o.was_direction_correct), 4) AS hit_rate,
       ROUND(AVG((d.confidence - o.was_direction_correct) * (d.confidence - o.was_direction_correct)), 4) AS brier
FROM calibration_outcomes o
JOIN jev_decisions d ON d.id = o.jev_decision_id, params
WHERE d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT') AND d.confidence IS NOT NULL
  AND o.was_direction_correct IS NOT NULL AND d.timestamp >= from_ts AND d.timestamp < to_ts
  AND NOT ((o.label_version = 1 AND o.future_return = 0) OR (o.label_version <> 1 AND COALESCE(o.net_return_bps, 1) = 0))
GROUP BY o.label_version, o.horizon_minutes ORDER BY o.label_version, o.horizon_minutes;

-- name: Q6 確信度帯別のトリプルバリアEV（label_version=2、コスト控除後bps）。固定の0.1%コスト仮定ではなく実ラベルの値
SELECT CASE WHEN d.confidence < 0.6 THEN '<0.60' WHEN d.confidence < 0.7 THEN '0.60-0.70'
            WHEN d.confidence < 0.8 THEN '0.70-0.80' ELSE '>=0.80' END AS confidence_band,
       COUNT(*) AS n, ROUND(AVG(o.net_return_bps), 4) AS ev_net_bps, ROUND(AVG(o.was_direction_correct), 4) AS hit_rate
FROM calibration_outcomes o JOIN jev_decisions d ON d.id = o.jev_decision_id
WHERE o.label_version = 2 AND o.net_return_bps IS NOT NULL
  AND d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT') AND d.confidence IS NOT NULL
GROUP BY confidence_band ORDER BY confidence_band;

-- name: Q7 入力の古さ（jev_decisions.data_age_ms）別の的中率（label_version=2）。created_at-timestamp の代わり
SELECT CASE WHEN d.data_age_ms IS NULL THEN 'unknown' WHEN d.data_age_ms < 1000 THEN '<1s'
            WHEN d.data_age_ms < 5000 THEN '1-5s' ELSE '>=5s' END AS data_age,
       COUNT(*) AS n, ROUND(AVG(o.was_direction_correct), 4) AS hit_rate, ROUND(AVG(o.net_return_bps), 4) AS ev_net_bps
FROM calibration_outcomes o JOIN jev_decisions d ON d.id = o.jev_decision_id
WHERE o.label_version = 2 AND o.was_direction_correct IS NOT NULL AND COALESCE(o.net_return_bps, 1) <> 0
  AND d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT')
GROUP BY data_age ORDER BY data_age;

-- name: Q8 メタモデル: OOS指標と、同モデルが予測した判断で実際に入ったPaper取引の実現EV（予測EVとの差）
WITH last_pred AS (
  SELECT jev_decision_id, MAX(id) AS id FROM metalabel_predictions GROUP BY jev_decision_id
),
linked AS (
  SELECT m.version, p.realized_pnl * 10000.0 / (p.entry_price * p.quantity) AS ret_bps, mp.expected_value_bps AS predicted_bps
  FROM positions p
  JOIN paper_orders eo ON eo.id = p.entry_order_id
  JOIN trade_signals s ON s.id = eo.trade_signal_id
  JOIN last_pred lp ON lp.jev_decision_id = s.jev_decision_id
  JOIN metalabel_predictions mp ON mp.id = lp.id
  JOIN metalabel_models m ON m.id = mp.model_id
  WHERE p.closed_at IS NOT NULL AND p.realized_pnl IS NOT NULL AND p.entry_price * p.quantity > 0
)
SELECT m.version, m.status, m.label_version, m.oos_start, m.oos_end, m.oos_samples, m.oos_auc, m.oos_expected_value_bps,
       COUNT(l.version) AS linked_trades, ROUND(AVG(l.predicted_bps), 4) AS predicted_ev_bps,
       ROUND(AVG(l.ret_bps), 4) AS realized_ev_bps, ROUND(AVG(l.ret_bps - l.predicted_bps), 4) AS gap_bps
FROM metalabel_models m LEFT JOIN linked l ON l.version = m.version
GROUP BY m.id ORDER BY m.trained_at, m.id;

-- name: Q9 メタモデルのOOS窓と同じ期間のPaper実績EV（どのモデルを使ったかによらない）
SELECT m.version, m.oos_start, m.oos_end, m.oos_expected_value_bps,
       COUNT(p.id) AS paper_trades, ROUND(AVG(p.realized_pnl * 10000.0 / (p.entry_price * p.quantity)), 4) AS paper_ev_bps,
       ROUND(SUM(p.realized_pnl), 2) AS paper_net_pnl_jpy
FROM metalabel_models m
LEFT JOIN positions p ON p.closed_at IS NOT NULL AND p.realized_pnl IS NOT NULL AND p.entry_price * p.quantity > 0
  AND p.closed_at >= m.oos_start AND p.closed_at < m.oos_end
GROUP BY m.id ORDER BY m.trained_at, m.id;
