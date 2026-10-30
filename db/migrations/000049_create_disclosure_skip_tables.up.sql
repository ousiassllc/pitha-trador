-- 適時開示による見送りルールの記録（issue #935、やのしん TDnet WebAPI・Phase 1）。
-- 記録だけで、取引・Policy・Risk・発注・Jev のプロンプトのどれにも使わない（FR-DISCLOSURE-SKIP-1〜5）。
-- 書くのは News Ingest のフィード取得結果を横取りする記録部品と、その後に Jev の判断を後追いで評価する評価部品だけ。
-- jev_decisions / calibration_outcomes は読むだけで、これらの表には既存の列も制約も足さない。
-- 時刻はすべて UTC の RFC3339（小数 9 桁固定。docs/architecture/er.md §型・規約「日時」）。

-- disclosure_items: やのしんの開示インデックス（銘柄・開示時刻・表題・URL）。PDF の本文は取得しない。
--   source_id    やのしんの開示 ID（無ければ表題。News Ingest の重複排除キーと同じ）。(symbol, source_id) で一意。
--   published_at 開示時刻（やのしんの pubdate は JST なので UTC に直して保存）。
--   document_url やのしんが返した文書 URL（無ければ NULL）。取りに行かない。
--   first_seen_at 初めて取得した時刻（取得の遅れを後から調べる用）。
CREATE TABLE disclosure_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol TEXT NOT NULL,
    source_id TEXT NOT NULL,
    published_at TEXT NOT NULL,
    title TEXT NOT NULL,
    document_url TEXT,
    first_seen_at TEXT NOT NULL,
    UNIQUE (symbol, source_id)
);

CREATE INDEX disclosure_items_symbol_published_idx ON disclosure_items (symbol, published_at);

-- disclosure_fetches: News Ingest の 1 回の取得の成否（取得の穴を「該当なし」と区別して「不明」にするための根拠）。
--   fetched_at          リクエストを始めた時刻（この応答は少なくともこの時刻の状態を映す）。
--   outcome             'ok' は応答を受け取れた、'error' は失敗（タイムアウト・HTTP エラー・不正な応答）。
--                       バックオフ中でリクエストを出さなかった回と、終了処理で中断した回は行を作らない。
--   item_count          応答の項目数（'ok' のときだけ意味を持つ）。
--   truncated           応答が件数上限（newsfeed.YanoshinLimit）に届き、古い開示が切れている可能性がある。
--   undated_count       開示時刻を読めなかった項目の数（1 件でもあれば、その取得は範囲を覆ったとみなさない）。
--   oldest_published_at 応答のうち最も古い開示時刻（開示時刻のある項目が無ければ NULL）。
-- 判断の評価に要る期間（判断の直後）より古い行は評価部品が消す（保持 7 日）。
CREATE TABLE disclosure_fetches (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol TEXT NOT NULL,
    fetched_at TEXT NOT NULL,
    outcome TEXT NOT NULL CHECK (outcome IN ('ok', 'error')),
    item_count INTEGER NOT NULL DEFAULT 0 CHECK (item_count >= 0),
    truncated INTEGER NOT NULL DEFAULT 0 CHECK (truncated IN (0, 1)),
    undated_count INTEGER NOT NULL DEFAULT 0 CHECK (undated_count >= 0),
    oldest_published_at TEXT
);

CREATE INDEX disclosure_fetches_symbol_fetched_idx ON disclosure_fetches (symbol, fetched_at);

-- disclosure_skip_records: Jev のエントリー候補の判断（jev_decisions の decision_type='trader' かつ direction が
-- 'LONG' / 'SHORT'）1 件につき 1 行。見送りルールに当てはまったか（'skip'）、当てはまらなかったか（'no_skip'）、
-- フィードの欠けで分からなかったか（'unknown'）。jev_decisions の行は書き換えない。
--   window_from         ルールの窓の始まり（判断日の直前の東証営業日の 15:00 JST）。窓は [window_from, 判断時刻]。
--   unknown_reason      'no_fetch' は判断の後に成功した取得がなかった、'incomplete_fetch' は成功した取得はあったが
--                       件数上限で窓の始まりに届かない・開示時刻が読めない項目があった。unknown のときだけ入る。
--   covered_by_fetch_at 窓を覆った取得の時刻（覆えていなければ NULL。'skip' は覆えていなくても definite）。
--   rule_version        ルール（対象の語・窓の定義）の版。
CREATE TABLE disclosure_skip_records (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    jev_decision_id INTEGER NOT NULL UNIQUE REFERENCES jev_decisions (id),
    symbol TEXT NOT NULL,
    decided_at TEXT NOT NULL,
    verdict TEXT NOT NULL CHECK (verdict IN ('skip', 'no_skip', 'unknown')),
    unknown_reason TEXT CHECK (unknown_reason IN ('no_fetch', 'incomplete_fetch')),
    window_from TEXT NOT NULL,
    covered_by_fetch_at TEXT,
    rule_version INTEGER NOT NULL,
    evaluated_at TEXT NOT NULL,
    CHECK ((verdict = 'unknown') = (unknown_reason IS NOT NULL))
);

CREATE INDEX disclosure_skip_records_decided_idx ON disclosure_skip_records (decided_at);

-- disclosure_skip_matches: 'skip' になった判断の該当開示（時刻・表題・種類・URL）。1 判断に複数ありうる。
--   kind 'earnings'（決算短信）/ 'forecast_revision'（業績予想の修正。上方・下方とも）。
CREATE TABLE disclosure_skip_matches (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    record_id INTEGER NOT NULL REFERENCES disclosure_skip_records (id) ON DELETE CASCADE,
    published_at TEXT NOT NULL,
    title TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('earnings', 'forecast_revision')),
    document_url TEXT
);

CREATE INDEX disclosure_skip_matches_record_idx ON disclosure_skip_matches (record_id);
