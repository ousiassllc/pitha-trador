-- research_horizon_labels: 研究用の固定ホライズンのラベル（issue #1074）。
--
-- 既存の 10 分・10bps のラベル（calibration_outcomes）はコスト控除後に優位性が見えない（11/11 時点で
-- コスト前 +1〜2bps）。保有時間を変えたときの「コスト前・コスト後の bps」を、取引判断とは無関係に
-- 全スナップショットについて残す、研究専用の記録。取引には使わない: Policy・Risk・発注・Paper ガード・
-- Live・Jev のプロンプト・RAG・メタモデルの学習・自己改善は読まない（internal/repository/researchlabel/
-- isolation_test.go が固定）。読むのは記録部品（internal/service/researchlabel）と、読み取り専用の
-- MCP ツール research_horizon_summary だけ。
-- market_snapshots・jev_decisions・calibration_outcomes・counterfactual_labels・メタラベルの表には
-- 列も制約もトリガも足さない（既存行の数値は書き換えない）。
-- 時刻はすべて UTC の RFC3339（小数 9 桁固定。docs/architecture/er.md §型・規約「日時」）。
--
-- 1 行は (スナップショット, ホライズン) の組。エントリーはそのスナップショットの足、出口は下の規則で決まる足。
--   snapshot_id      market_snapshots.id（外部キーにしない: 保持期間 90 日で market_snapshots が削除されても
--                    この表の行は残す。FK だと削除が止まる/連鎖する）。
--   instrument_id / symbol / snapshot_at  スナップショットの銘柄と時刻（market_snapshots.timestamp と同じ値。
--                    判断（jev_decisions）との結合は (instrument_id, snapshot_at = 判断の timestamp)）。
--   day              snapshot_at の JST の日付（YYYY-MM-DD）。日ごとの集計・保持期間の削除・期間の絞り込みの
--                    キー（日付の計算を行ごとにしないための導出列。記録部品が snapshot_at から決める）。
--   sample_bucket    snapshot_id の混ぜ関数（0〜99）。MCP ツールが中央値を求めるとき、行の一部（sample_bucket < k）だけを
--                    索引で読むための印（日・ホライズンごとに sample_bucket の範囲だけを読み、全行を読まずに 3 秒の読み取りに収める）。記録部品が snapshot_id から決める。
--   horizon          '30m' / '60m'＝エントリーの足の時刻＋30/60 分。足は目標時刻以前の最後の足で、目標との
--                    差が calibration.HorizonBarTolerance（2 分）以内のときだけ（ライブのラベルと同じ規則。
--                    昼休み・引け・欠測で足りないときは行を作らない＝短い窓は記録しない）。
--                    'close'＝同じ JST 日の最後の足（15:30 以前）。その時刻が 15:25 JST 以降のときだけ。
--   exit_at          出口の足の時刻。exit_price はその足の price。
--   entry_bid / entry_ask / entry_price  エントリーの足の板と価格。LONG は ask、SHORT は bid で入る。
--   spread_bps       エントリーの足の market_snapshots.spread_bps（NULL 可）。
--   long_* / short_*  ○○_gross_bps＝コスト前の方向調整済みリターン（calibration の grossReturnBps と同じ:
--                    (出口 - 入口) / 入口 × 10000 × 方向）。○○_cost_bps＝記録時点の設定で既存のラベルが
--                    その側に置くコスト（costsettings.LabelBarriers。ライブのラベラーと同じ関数）。
--                    ○○_net_bps＝gross - cost。その側に執行できる気配が無い（LONG は ask、SHORT は bid が
--                    無い）ときは 3 列とも NULL。両側とも無い足には行を作らない。
--   label_version    1＝この研究用の定義（固定ホライズンで決済。TP/SL バリアなし）。
CREATE TABLE research_horizon_labels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_id INTEGER NOT NULL,
    instrument_id INTEGER NOT NULL,
    symbol VARCHAR(10) NOT NULL,
    snapshot_at TEXT NOT NULL,
    day TEXT NOT NULL CHECK (length(day) = 10),
    sample_bucket INTEGER NOT NULL CHECK (sample_bucket >= 0 AND sample_bucket < 100),
    horizon TEXT NOT NULL CHECK (horizon IN ('30m', '60m', 'close')),
    exit_at TEXT NOT NULL,
    entry_bid NUMERIC,
    entry_ask NUMERIC,
    entry_price NUMERIC NOT NULL CHECK (entry_price > 0),
    exit_price NUMERIC NOT NULL CHECK (exit_price > 0),
    spread_bps NUMERIC,
    long_gross_bps NUMERIC,
    long_cost_bps NUMERIC,
    long_net_bps NUMERIC,
    short_gross_bps NUMERIC,
    short_cost_bps NUMERIC,
    short_net_bps NUMERIC,
    label_version INTEGER NOT NULL CHECK (label_version = 1),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (snapshot_id, horizon),
    CHECK ((long_gross_bps IS NULL) = (long_cost_bps IS NULL) AND (long_gross_bps IS NULL) = (long_net_bps IS NULL)),
    CHECK ((short_gross_bps IS NULL) = (short_cost_bps IS NULL) AND (short_gross_bps IS NULL) = (short_net_bps IS NULL)),
    CHECK (long_gross_bps IS NOT NULL OR short_gross_bps IS NOT NULL)
);

-- 日×ホライズンの読み（記録部品の日次の集計と、MCP ツールの中央値の標本読み＝日・ホライズンを固定して
-- sample_bucket の範囲）と、銘柄×時刻での判断との結合。
CREATE INDEX research_horizon_labels_day_sample_idx ON research_horizon_labels (day, horizon, sample_bucket);
CREATE INDEX research_horizon_labels_instrument_at_idx ON research_horizon_labels (instrument_id, snapshot_at);

-- research_label_days: JST の日ごとの「記録を終えた」印と件数。記録部品は、ある日の全銘柄の行を書き終えて
-- 日次の集計（下の research_label_day_stats）を書いたあとに 1 行を足す。途中で止まった日は印が無いので、
-- 次の実行がその日をやり直す（行は UNIQUE と INSERT OR IGNORE で二重にならない）。market_snapshots が
-- 削除済みの日は対象にしない。印の無い日の行は、MCP ツールの集計に入れない（途中の日で偏らないため）。
--   day          JST の日付（YYYY-MM-DD）。
--   snapshots    その日に読んだ market_snapshots の足の数（スナップショットの削除後も残る分母）。
--   labeled_30m / labeled_60m / labeled_close  印を付けた時点のその日のホライズン別の行数。
--   completed_at 印を付けた時刻（UTC）。
CREATE TABLE research_label_days (
    day TEXT PRIMARY KEY CHECK (length(day) = 10),
    snapshots INTEGER NOT NULL CHECK (snapshots >= 0),
    labeled_30m INTEGER NOT NULL CHECK (labeled_30m >= 0),
    labeled_60m INTEGER NOT NULL CHECK (labeled_60m >= 0),
    labeled_close INTEGER NOT NULL CHECK (labeled_close >= 0),
    completed_at TEXT NOT NULL
);

-- research_label_day_stats: 日×ホライズン×側の和（印を付けるときに行から計算して書く）。MCP ツールは期間の
-- 平均・勝率・日単位の区間をこの表から読む（日ごとの和がそのままブートストラップの 1 まとまり）ので、
-- 数百万行を読まずに 3 秒の読み取りに収まる。保持期間で research_horizon_labels の古い行を消しても、
-- この表（1 日 6 行）は残るので、期間の平均は消えない（中央値は行が残る期間だけ）。
--   row_count      その側に結果のある行数（net が NULL でない行）。
--   sum_gross / sum_net / sum_cost  gross・net・コストの和（bps）。
--   wins           net > 0 の行数。
--   spread_rows    そのうち spread_bps のある行数。sum_spread はその spread_bps の和。
--   spread_above_cost  spread_bps > その行のコストの行数（spread_rows の内数）。
--   sum_gross_less_half_spread  spread_bps のある行の gross - spread_bps / 2 の和（net_after_spread_bps の分子）。
CREATE TABLE research_label_day_stats (
    day TEXT NOT NULL CHECK (length(day) = 10),
    horizon TEXT NOT NULL CHECK (horizon IN ('30m', '60m', 'close')),
    side TEXT NOT NULL CHECK (side IN ('LONG', 'SHORT')),
    row_count INTEGER NOT NULL CHECK (row_count > 0),
    sum_gross NUMERIC NOT NULL,
    sum_net NUMERIC NOT NULL,
    sum_cost NUMERIC NOT NULL,
    wins INTEGER NOT NULL CHECK (wins >= 0 AND wins <= row_count),
    spread_rows INTEGER NOT NULL CHECK (spread_rows >= 0 AND spread_rows <= row_count),
    sum_spread NUMERIC NOT NULL,
    spread_above_cost INTEGER NOT NULL CHECK (spread_above_cost >= 0 AND spread_above_cost <= spread_rows),
    sum_gross_less_half_spread NUMERIC NOT NULL,
    PRIMARY KEY (day, horizon, side)
) WITHOUT ROWID;
