-- metalabel_retrain_skips に、見送りの内訳の件数を足す（issue #862、関連 #799）。
-- 見送った夜に「履歴が無い」のか「あるのに使えない」のかを、ログを開かずに判別できるようにする。
-- 件数だけで、板・価格・自由文は含まない。
--
-- **5 列とも NULL 可。NULL は「未記録」**（この列ができる前の見送り行、または件数を読めなかった夜）。
-- 既存行は UPDATE せず NULL のまま。この列ができたあとに書く行は、実際の値（0 件なら 0）を入れる。
-- 0 を「未記録」の意味には使わない。読む側も NULL を 0 に読み替えない。
--
-- dropped_incomplete   BuildRows が特徴量の欠けで落とした行数（ラベルはあるが 8 特徴量がそろわない）
-- label_v1_rows        lookback 内のラベル付き判断のうち label_version 1（終値だけのラベル。OFI・microprice が無い）の件数
-- label_v2_rows        同じく label_version 2（トリプルバリア。学習に使えるラベル）の件数
-- oos_rows             見送った時点で OOS 窓にあった行数
-- required_oos_rows    OOS 窓がさかのぼって集める行数（採用条件の MinOOSSamples）
-- 学習側の件数は既存の usable_rows / required_rows（「必要130件中 x件」）が担う。
ALTER TABLE metalabel_retrain_skips ADD COLUMN dropped_incomplete INTEGER CHECK (dropped_incomplete IS NULL OR dropped_incomplete >= 0);
ALTER TABLE metalabel_retrain_skips ADD COLUMN label_v1_rows INTEGER CHECK (label_v1_rows IS NULL OR label_v1_rows >= 0);
ALTER TABLE metalabel_retrain_skips ADD COLUMN label_v2_rows INTEGER CHECK (label_v2_rows IS NULL OR label_v2_rows >= 0);
ALTER TABLE metalabel_retrain_skips ADD COLUMN oos_rows INTEGER CHECK (oos_rows IS NULL OR oos_rows >= 0);
ALTER TABLE metalabel_retrain_skips ADD COLUMN required_oos_rows INTEGER CHECK (required_oos_rows IS NULL OR required_oos_rows >= 0);
