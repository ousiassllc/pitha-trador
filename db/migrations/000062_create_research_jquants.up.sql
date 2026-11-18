-- J-Quants 読み取りアダプタの研究用の表（issue #1093、関連 #1074）。
--
-- kabu には過去の日足・分足を取る API が無い。J-Quants（有料）の日足・分足・銘柄一覧を、契約したときだけ
-- 研究用の表に書く。取引には使わない: Policy・Risk・発注・Paper ガード・Live・Jev・RAG・Calibration・
-- メタモデルの学習・自己改善は読まない。market_snapshots / daily_bars / disclosure_items には混ぜない
-- （専用の表に分け、どの表にも source 列＝'jquants' を持たせる）。
-- J-Quants の利用条件（個人の私的利用・外部へ送らない・解約後は生データと元に戻せる派生物を削除）に
-- 合わせて、削除は source='jquants' の行だけを対象にできる形にしてある（docs/environment/setup/jquants.md）。
-- 時刻はすべて UTC の RFC3339（小数 9 桁固定。docs/architecture/er.md §型・規約「日時」）。日付は JST の YYYY-MM-DD。
-- 秘密（API キー）はどの表にも入れない。

-- research_jquants_symbols: 上場銘柄一覧（/v2/equities/master）の日ごとの写し。
--   as_of     一覧の日付（JST）。 code  J-Quants の銘柄コード（5 桁。例 72030）。
--   symbol    アプリが使う 4 桁の証券コード（72030 -> 7203。domain/jquants.SymbolOf）。
CREATE TABLE research_jquants_symbols (
    source TEXT NOT NULL DEFAULT 'jquants' CHECK (source = 'jquants'),
    as_of TEXT NOT NULL CHECK (length(as_of) = 10),
    code TEXT NOT NULL CHECK (length(code) > 0),
    symbol TEXT NOT NULL CHECK (length(symbol) > 0),
    company_name TEXT NOT NULL DEFAULT '',
    company_name_en TEXT NOT NULL DEFAULT '',
    sector17_code TEXT NOT NULL DEFAULT '',
    sector33_code TEXT NOT NULL DEFAULT '',
    scale_category TEXT NOT NULL DEFAULT '',
    market_code TEXT NOT NULL DEFAULT '',
    market_name TEXT NOT NULL DEFAULT '',
    fetched_at TEXT NOT NULL,
    PRIMARY KEY (as_of, code)
);
CREATE INDEX research_jquants_symbols_symbol_idx ON research_jquants_symbols (symbol, as_of);

-- research_jquants_daily_bars: 株価四本値（/v2/equities/bars/daily）。自然キーは (day, code)。同じ行の取り直しは上書き
-- （J-Quants はデータの訂正で値を書き換えることがある）。取引の無い日は価格・出来高が NULL（API が null を返す）。
--   open〜close / volume / turnover  調整前。adj_*  J-Quants の調整後（株式分割などを反映。後から再計算されうる）。
--   upper_limit / lower_limit        その日のストップ高・安に達したか（'0' / '1'。無ければ空）。
CREATE TABLE research_jquants_daily_bars (
    source TEXT NOT NULL DEFAULT 'jquants' CHECK (source = 'jquants'),
    day TEXT NOT NULL CHECK (length(day) = 10),
    code TEXT NOT NULL CHECK (length(code) > 0),
    symbol TEXT NOT NULL CHECK (length(symbol) > 0),
    open NUMERIC,
    high NUMERIC,
    low NUMERIC,
    close NUMERIC,
    volume NUMERIC,
    turnover NUMERIC,
    adj_factor NUMERIC,
    adj_open NUMERIC,
    adj_high NUMERIC,
    adj_low NUMERIC,
    adj_close NUMERIC,
    adj_volume NUMERIC,
    upper_limit TEXT NOT NULL DEFAULT '',
    lower_limit TEXT NOT NULL DEFAULT '',
    fetched_at TEXT NOT NULL,
    PRIMARY KEY (day, code)
);
CREATE INDEX research_jquants_daily_bars_symbol_idx ON research_jquants_daily_bars (symbol, day);

-- research_jquants_minute_bars: 株価分足（/v2/equities/bars/minute、翌日配信）。自然キーは (code, bar_at)。
--   bar_at  その 1 分の開始時刻（UTC。API の Time は JST の HH:mm）。 day  bar_at の JST の日付（日ごとの読み・削除・欠測の判定のキー）。
--   取引の無かった分は API が返さないので、行の無い分は「取引なし」であり欠測とは限らない。
CREATE TABLE research_jquants_minute_bars (
    source TEXT NOT NULL DEFAULT 'jquants' CHECK (source = 'jquants'),
    code TEXT NOT NULL CHECK (length(code) > 0),
    symbol TEXT NOT NULL CHECK (length(symbol) > 0),
    day TEXT NOT NULL CHECK (length(day) = 10),
    bar_at TEXT NOT NULL,
    open NUMERIC NOT NULL CHECK (open > 0),
    high NUMERIC NOT NULL CHECK (high > 0),
    low NUMERIC NOT NULL CHECK (low > 0),
    close NUMERIC NOT NULL CHECK (close > 0),
    volume NUMERIC NOT NULL CHECK (volume >= 0),
    turnover NUMERIC NOT NULL CHECK (turnover >= 0),
    fetched_at TEXT NOT NULL,
    PRIMARY KEY (code, bar_at)
);
CREATE INDEX research_jquants_minute_bars_day_symbol_idx ON research_jquants_minute_bars (day, symbol);

-- research_jquants_coverage: どの (データ種別, 日, 銘柄) を取り終えたかの印。埋め戻しは印の無いものだけを取る。
--   dataset  'symbols'（一覧。day＝取得した日）/ 'daily'（その日の全銘柄）/ 'minute'（その日のその銘柄）/
--            'relabel'（その日の研究用ラベルを分足で付け直した印）。
--   code     'minute' のときだけ銘柄コード。ほかは空文字。 row_count  その取得で書いた行数。
-- 生データの削除（解約時）では、この印も消す（消さないと、取り直しがその日を取得済みと見なす）。
CREATE TABLE research_jquants_coverage (
    source TEXT NOT NULL DEFAULT 'jquants' CHECK (source = 'jquants'),
    dataset TEXT NOT NULL CHECK (dataset IN ('symbols', 'daily', 'minute', 'relabel')),
    day TEXT NOT NULL CHECK (length(day) = 10),
    code TEXT NOT NULL DEFAULT '',
    row_count INTEGER NOT NULL CHECK (row_count >= 0),
    completed_at TEXT NOT NULL,
    PRIMARY KEY (dataset, day, code)
);

-- research_jquants_fetch_log: JST の日×データ種別ごとの取得の記録（件数・失敗・要求数。秘密も応答本文も入れない）。
-- 1 日の要求数の上限（jquants.daily_request_cap）は、この表の requests の日合計で数える。要求は送る前に数える
-- （失敗・中断でも超えない）。生データの削除でもこの表は消さない（市場データを含まず、消すと上限が日の途中で戻ってしまう）。
--   day        要求を送った日（JST）。 requests  その日に送った要求の数（ページ送りを含む）。
--   rows_stored 書いた行数。 failures  失敗した要求の数。
--   last_failure_code  最後の失敗の固定コード（'http_401' / 'http_429' / 'transport' / 'decode' など。応答本文は入れない）。空 = 失敗なし。
CREATE TABLE research_jquants_fetch_log (
    source TEXT NOT NULL DEFAULT 'jquants' CHECK (source = 'jquants'),
    day TEXT NOT NULL CHECK (length(day) = 10),
    dataset TEXT NOT NULL CHECK (dataset IN ('symbols', 'daily', 'minute')),
    requests INTEGER NOT NULL DEFAULT 0 CHECK (requests >= 0),
    rows_stored INTEGER NOT NULL DEFAULT 0 CHECK (rows_stored >= 0),
    failures INTEGER NOT NULL DEFAULT 0 CHECK (failures >= 0),
    last_failure_code TEXT NOT NULL DEFAULT '',
    last_request_at TEXT,
    last_success_at TEXT,
    PRIMARY KEY (day, dataset)
);

-- research_horizon_labels.exit_source: 研究用ラベル（#1074）の出口の足の出どころ。
--   'snapshot'  market_snapshots の足（#1074 の記録部品が書いた行。既存行はすべてこれ）。
--   'jquants'   market_snapshots に足の無かった窓を、J-Quants の分足で埋めて付け直した行（internal/service/researchlabel/jquantsrelabel）。
-- 既定は 'snapshot' で NOT NULL: 既存の読み手（列を名前で選ぶ）は変わらず、既存行の数値も書き換えない。
-- 解約時の削除は exit_source='jquants' の行だけを消す（部分索引で見つける）。
ALTER TABLE research_horizon_labels ADD COLUMN exit_source TEXT NOT NULL DEFAULT 'snapshot' CHECK (exit_source IN ('snapshot', 'jquants'));
CREATE INDEX research_horizon_labels_jquants_idx ON research_horizon_labels (day) WHERE exit_source = 'jquants';
