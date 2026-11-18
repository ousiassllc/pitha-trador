-- 開示・ニュースの情報源（EDINET の書類一覧・立花 e支店のニュース）の研究用の表（issue #1096、関連 #935・#725）。
--
-- 適時開示はやのしん TDnet WebAPI（disclosure_items、#935）で記録している。その補助として、EDINET（金融庁、
-- 有価証券報告書・大量保有報告など）の書類一覧のメタデータと、立花 e支店のニュースの見出しを、記録だけする。
-- 取引には使わない: ゲート・Policy・Risk・発注・Paper ガード・Live・Jev・RAG の索引・Calibration・メタモデルの学習・
-- 自己改善・既存の開示見送りルール（disclosureskip）は、これらの表を読まない。disclosure_items・jev_decisions・
-- jev_shadow_decisions・jev_trial_decisions・market_snapshots・daily_bars には何も書かず、列も制約も足さない。
-- 書類の本文は取得しない（EDINET は一覧の JSON だけ。書類の取得 API は呼ばない）。API キー・認証情報はどの表にも入れない。
-- 時刻はすべて UTC の RFC3339（小数 9 桁固定。docs/architecture/er.md §型・規約「日時」）。disclosure_items.published_at と
-- 同じ形式で、symbol は disclosure_items.symbol と同じ 4 桁の証券コード（結合キー: symbol と時刻）。

-- research_edinet_documents: EDINET API v2 の書類一覧（documents.json?type=2）の 1 書類 1 行（メタデータだけ）。
--   doc_id         EDINET の書類管理番号（例 S100XXXX）。一意（再取得は UPDATE で冪等）。
--   symbol         証券コード（4 桁。EDINET の secCode は 5 桁＝4 桁＋チェック桁なので先頭 4 桁。disclosure_items.symbol と同じ形）。
--                  secCode の無い書類（投資信託・個人の提出者など）は記録しない（日ごとの skipped_count に数える）。
--   sec_code       EDINET が返した secCode そのまま（5 桁）。
--   edinet_code    提出者の EDINET コード。 filer_name  提出者名。
--   doc_type_code  書類種別コード（例 120=有価証券報告書、350=大量保有報告書）。 title  docDescription（書類の説明）。
--   submitted_at   提出日時（EDINET は JST の「YYYY-MM-DD HH:MM」。UTC に直して保存）。disclosure_items.published_at と
--                  同じ形式・同じ時刻軸で、銘柄と時刻で結合できる。
--   submit_day     提出日（JST の YYYY-MM-DD。submitted_at から導出。日ごとの集計・保持期間の削除のキー）。
--   first_seen_at  初めて取得した時刻。 updated_at  最後に取得した時刻。
CREATE TABLE research_edinet_documents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    doc_id TEXT NOT NULL UNIQUE CHECK (doc_id <> ''),
    symbol TEXT NOT NULL CHECK (symbol <> ''),
    sec_code TEXT NOT NULL CHECK (sec_code <> ''),
    edinet_code TEXT NOT NULL DEFAULT '',
    filer_name TEXT NOT NULL DEFAULT '',
    doc_type_code TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    submitted_at TEXT NOT NULL,
    submit_day TEXT NOT NULL CHECK (length(submit_day) = 10),
    first_seen_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX research_edinet_documents_symbol_submitted_idx ON research_edinet_documents (symbol, submitted_at);
CREATE INDEX research_edinet_documents_submit_day_idx ON research_edinet_documents (submit_day);

-- research_edinet_fetch_days: 書類一覧を取った日（提出日 JST）ごとの記録。ある日の一覧を、その日が終わったあとに取って
-- 初めて「取り終えた」とみなす（fetched_at の JST の日が day より後）。途中で取った日は次の実行がやり直す。
--   total_count     その日の一覧の書類数。 recorded_count  そのうち記録した数。
--   skipped_count   記録しなかった数（secCode が無い・提出日時を読めない）。
CREATE TABLE research_edinet_fetch_days (
    day TEXT PRIMARY KEY CHECK (length(day) = 10),
    fetched_at TEXT NOT NULL,
    total_count INTEGER NOT NULL CHECK (total_count >= 0),
    recorded_count INTEGER NOT NULL CHECK (recorded_count >= 0),
    skipped_count INTEGER NOT NULL CHECK (skipped_count >= 0)
) WITHOUT ROWID;

-- research_tachibana_news: 立花 e支店のニュースの 見出し・時刻・関連銘柄。（news_id, symbol）で一意。
-- 関連銘柄が複数あるニュースは銘柄ごとに 1 行、関連銘柄の無いニュースは symbol='' の 1 行（結合では symbol <> '' の行だけ）。
-- 立花の実機計測（#725）が終わるまで本番には繋がない: 取得は NewsFetcher のインターフェースの向こうで、
-- この表に書くのは記録部品とそのテストだけ（本番の起動では書き手を登録しない）。
--   news_id        立花のニュースの識別子。 headline  見出し（本文は持たない）。
--   published_at   ニュースの時刻（UTC）。 first_seen_at  初めて取得した時刻。
CREATE TABLE research_tachibana_news (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    news_id TEXT NOT NULL CHECK (news_id <> ''),
    symbol TEXT NOT NULL DEFAULT '',
    headline TEXT NOT NULL,
    published_at TEXT NOT NULL,
    first_seen_at TEXT NOT NULL,
    UNIQUE (news_id, symbol)
);

CREATE INDEX research_tachibana_news_symbol_published_idx ON research_tachibana_news (symbol, published_at);
CREATE INDEX research_tachibana_news_published_idx ON research_tachibana_news (published_at);

-- research_disclosure_source_status: 情報源ごとの取得状況（1 源 1 行。MCP の disclosure_sources_status が読む）。
--   requests_day / requests_today  1 日の要求数の上限（disclosure_src.<源>.daily_request_cap）の判定に数える、JST の日と
--                                  その日に出した要求の数（失敗した要求も数える）。
--   request_count  累計の要求数。 failure_count  累計の失敗数。
--   last_fetched_at  最後に要求を出した時刻。 last_success_at / last_failure_at  最後の成功・失敗の時刻。
--   last_failure_code  最後の失敗の短い固定コード（http_401 / http_403 / http_429 / http_4xx / http_5xx / timeout /
--                      transport / invalid_response / api_error）。エラーの本文・URL は残さない（キーが URL に入るため）。
CREATE TABLE research_disclosure_source_status (
    source TEXT PRIMARY KEY CHECK (source IN ('edinet', 'tachibana_news')),
    requests_day TEXT NOT NULL DEFAULT '',
    requests_today INTEGER NOT NULL DEFAULT 0 CHECK (requests_today >= 0),
    request_count INTEGER NOT NULL DEFAULT 0 CHECK (request_count >= 0),
    failure_count INTEGER NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
    last_fetched_at TEXT,
    last_success_at TEXT,
    last_failure_at TEXT,
    last_failure_code TEXT NOT NULL DEFAULT ''
) WITHOUT ROWID;

-- research_disclosure_source_events_v: 2 つの情報源を (source, ref_id, symbol, event_at, headline) に揃えた読み取り用のビュー。
-- disclosure_items と同じ銘柄・時刻で結合する:
--   SELECT i.symbol, i.published_at, e.source, e.event_at FROM disclosure_items i
--   JOIN research_disclosure_source_events_v e ON e.symbol = i.symbol AND e.event_at BETWEEN i.published_at AND <窓の終わり>
CREATE VIEW research_disclosure_source_events_v AS
SELECT 'edinet' AS source, doc_id AS ref_id, symbol, submitted_at AS event_at, title AS headline
FROM research_edinet_documents
UNION ALL
SELECT 'tachibana_news', news_id, symbol, published_at, headline
FROM research_tachibana_news WHERE symbol <> '';
