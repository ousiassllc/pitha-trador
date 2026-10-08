# ER / データモデル: テーブル定義（市場データ・Jev判断）

`docs/architecture/er.md` から分割した章。対象: `instruments` / `market_snapshots` / `jev_decisions` / `trade_signals`。型・規約と全体ER図は `docs/architecture/er.md` を参照。

## instruments

対象銘柄マスタ。

```mermaid
erDiagram
    instruments {
        integer id PK
        varchar symbol UK "銘柄コード（例: 7203）"
        varchar name "銘柄名"
        varchar market "市場区分（例: TSE Prime）"
        varchar sector "業種"
        varchar kind "stock / market_index / sector_index"
        boolean is_active "スキャン対象フラグ"
        text created_at
        text updated_at
    }
```

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| id | integer | PK（AUTOINCREMENT） | |
| symbol | varchar(10) | UNIQUE, NOT NULL | 証券コード |
| name | varchar(255) | NOT NULL | 銘柄名 |
| market | varchar(50) | NOT NULL | 市場区分 |
| sector | varchar(100) | NULL可 | 業種。株式の業種と`kind=sector_index`銘柄の`sector`が一致すると、その指数がsector_return_5m算出に使われる |
| kind | varchar(20) | NOT NULL, DEFAULT 'stock', CHECK IN ('stock','market_index','sector_index') | `stock`のみスクリーニング/売買対象。`market_index`（TOPIX/Nikkei225等）と`sector_index`（業種指数）は市場コンテキスト特徴量（market_return_1m/5m, sector_return_5m）の入力としてだけ追跡し、Fast Screener・バックテスト対象外 |
| is_active | boolean | NOT NULL, DEFAULT 1 | 0の場合Fast Screener対象外 |
| created_at | text | NOT NULL, DEFAULT (strftime ミリ秒3桁) | DEFAULTは本番経路では使わず、`InstrumentRepository`が`sqlutil.FormatTime`（固定9桁）で常に明示する（`er.md`「日時列のDEFAULT」） |
| updated_at | text | NOT NULL, DEFAULT (strftime ミリ秒3桁) | 同上 |

インデックス: `UNIQUE (symbol)`, `INDEX (is_active)`

投入: アプリ起動時に`internal/bootstrap/universe`が銘柄マスタCSVから`symbol`キーでupsertする（`environment/setup.md`「銘柄マスタの投入」）。新規行は`is_active=1`、既存行は`name`/`market`/`sector`/`kind`のみ更新し`is_active`は変更しない。内容が同一の行は更新せず`updated_at`も変わらない（冪等）。

## market_snapshots

Feature Engineが算出した1分足スナップショット。

```mermaid
erDiagram
    instruments ||--o{ market_snapshots : ""
    market_snapshots {
        integer id PK
        integer instrument_id FK
        varchar symbol
        text timestamp
        numeric price
        numeric bid
        numeric ask
        numeric spread_bps
        integer volume
        numeric turnover
        numeric return_1m
        numeric return_5m
        numeric return_15m
        numeric vwap
        numeric price_vs_vwap_bps
        numeric volume_ratio_5m
        numeric orderbook_imbalance
        numeric realized_vol_5m
        numeric market_return_5m
        numeric sector_return_5m
        numeric market_return_1m
        numeric stock_vs_sector_relative_strength
        numeric market_breadth
        numeric return_3m
        numeric return_30m
        numeric high_distance_5m
        numeric low_distance_5m
        numeric session_high_distance
        numeric session_low_distance
        numeric vwap_slope
        integer vwap_cross_direction
        integer volume_1m
        integer volume_5m
        numeric volume_ratio_1m
        numeric turnover_1m
        numeric turnover_5m
        numeric atr_1m
        numeric atr_5m
        numeric realized_vol_15m
        numeric volatility_expansion_ratio
        numeric bid_depth
        numeric ask_depth
        numeric buy_trade_ratio
        numeric sell_trade_ratio
        numeric trade_flow_imbalance
        numeric microprice
        integer special_quote
        varchar price_limit
        integer lendable
        text raw_data_json
        text created_at
    }
```

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| id | integer | PK（AUTOINCREMENT） | |
| instrument_id | integer | FK → instruments.id, NOT NULL | |
| symbol | varchar(10) | NOT NULL | 非正規化（クエリ簡略化用） |
| timestamp | text | NOT NULL | スナップショット時刻（RFC3339、1分足） |
| price | numeric(12,2) | NOT NULL | |
| bid / ask | numeric(12,2) | NULL可 | 一般的な意味（bid=最良買気配、ask=最良売気配、bid < ask）。kabuステーションAPIの`AskPrice`（最良買気配）→bid、`BidPrice`（最良売気配）→askに入れ替えて保存する。板情報取得不可時はNULL |
| spread_bps | numeric(8,2) | NULL可 | (ask − bid)/mid×10000（0以上）。bid/ask欠損・mid=0・**逆転板（bid > ask、特別気配・寄り前後・片側が古い値）は欠損（NULL）**として扱う。上限比較のみのスプレッドガード（Fast Screener/Policy/Risk）が負値を素通りさせないため、NULLは層ごとに保守的に除外される: Fast Screenerは`missing_spread`（`domain.ScreenReasonMissingSpread`）、Policy Engineは`missing_data`（`policy.ReasonMissingData`）、Risk Engineは拒否理由文言`risk_engine_error: spread_bps missing (data_missing)`（`data_missing`は文言であり、独立したreasonコードは無い）（issue #465）。同じ理由で`microprice`も逆転板ではNULL。`orderbook_imbalance`は数量のみから算出するため逆転板でも算出する |
| volume | integer | NOT NULL | |
| turnover | numeric(18,2) | NOT NULL | |
| return_1m / return_5m / return_15m | numeric(8,4) | NULL可（起動直後等は算出不可） | **小数比**（0.004 = +0.4%）。Scanner API/画面は×100して%表示し、Fast Screenerの`min_abs_return_5m_pct`（%）との比較も×100して行う（`domain.RatioToPercent`） |
| vwap | numeric(12,2) | NOT NULL | |
| price_vs_vwap_bps | numeric(8,2) | NOT NULL | |
| volume_ratio_5m | numeric(8,4) | NULL可 | |
| orderbook_imbalance | numeric(6,4) | NULL可 | (bidQty − askQty)/(bidQty + askQty)。買い数量優勢で正 |
| realized_vol_5m | numeric(8,4) | NULL可 | |
| market_return_1m / market_return_5m | numeric | NULL可 | `kind=market_index`銘柄（TOPIX/Nikkei225）のreturnの平均 |
| sector_return_5m | numeric | NULL可 | 銘柄の`sector`と一致する`kind=sector_index`銘柄のreturn。該当指数なしはNULL |
| stock_vs_sector_relative_strength | numeric | NULL可 | return_5m − sector_return_5m |
| market_breadth | numeric | NULL可 | 許容年齢以内（ランキング監視は直近3分、`scan.full_scan_enabled: true`は`scan.full_scan_max_snapshot_age_seconds`＝同梱620秒。FR-FE-4）の全アクティブ株式の最新return_5mのうち（上昇数−下落数）/銘柄数。[-1, 1]。`scan.full_scan_enabled: true`では足が約8分間隔で各銘柄の`return_5m`が欠損（FR-FE-5）のため常にNULL（FR-SCHED-7） |
| return_3m / return_30m | numeric | NULL可 | 履歴不足時はNULL |
| high_distance_5m / low_distance_5m | numeric | NULL可 | 直近5分の高値/安値に対する `price/x − 1` |
| session_high_distance / session_low_distance | numeric | NULL可 | kabuの当日高値/安値（HighPrice/LowPrice）に対する `price/x − 1` |
| vwap_slope | numeric | NULL可 | 5分前のVWAPに対するVWAPの変化率 |
| vwap_cross_direction | integer | NULL可 | 直前の足からVWAPを下→上に抜けた場合+1、上→下は−1、クロスなしは0 |
| volume_1m / volume_5m | integer | NULL可 | 累積`TradingVolume`の差分（窓内出来高） |
| volume_ratio_1m | numeric | NULL可 | volume_ratio_5mと同じ算出の1分版 |
| turnover_1m / turnover_5m | numeric | NULL可 | 累積`turnover`の差分（窓内売買代金・円）。**`turnover`は当日累積値のため合算してはならない**。Fast Screenerの`min_turnover_5m_jpy`とPolicy Engineの「板が薄い」判定は同じturnover_5mを使う（`featureengine.TurnoverOverWindow`） |
| atr_1m / atr_5m | numeric | NULL可 | 真の値幅の平均（円）。サンプリング価格から作った足（1分×5本/5分×3本）に基づく |
| realized_vol_15m / volatility_expansion_ratio | numeric | NULL可 | 後者は realized_vol_5m / realized_vol_15m |
| bid_depth / ask_depth | numeric | NULL可 | 板の`Buy1..10`（bid側）/`Sell1..10`（ask側）の合計数量 |
| buy_trade_ratio / sell_trade_ratio / trade_flow_imbalance | numeric | NULL可 | 直近5分の出来高増分をティックルール（価格上昇=買い、下落=売り、同値=直前方向）で分類した比率と(買−売)/(買+売) |
| microprice | numeric | NULL可 | (bid×askQty + ask×bidQty)/(bidQty+askQty) |
| special_quote | integer | NOT NULL, DEFAULT 0, CHECK IN (0,1) | 特別気配（板の`BidSign`/`AskSign`が`0102`特別気配または`0108`停止前特別気配）なら1。Fast Screener（`special_quote`除外）とPolicy Engine（NONE）が約定不能として外す（issue #511、マイグレーション000026） |
| price_limit | varchar(10) | NOT NULL, DEFAULT '', CHECK IN ('','up','down') | ストップ高（`up`）/ストップ安（`down`）。取得時の現値が銘柄情報（`/symbol`）の`UpperLimit`/`LowerLimit`以上/以下のとき。該当なし・値幅不明は`''`。Fast Screener（`limit_up`/`limit_down`除外）とPolicy Engine（NONE）が使う |
| lendable | integer | NULL可, CHECK IN (0,1) | 貸借銘柄か（銘柄情報`MarginSell`＝制度信用売建可）。**NULL=不明**（000026以前の行・銘柄情報の取得失敗・指数）。0（貸借なし）のときだけPolicy Engineがショートを`not_lendable`で外す。銘柄情報は`symbolcache.Cache`が銘柄ごとに1営業日（JST）1回だけ取得する |
| raw_data_json | text | NOT NULL | ブローカーの生レスポンス（JSON文字列、再計算・監査用。ブローカー中立`broker.Quote.Raw`のJSON。kabuアダプタではkabuステーションAPIの板応答、立花証券アダプタでは立花の時価応答。**立花の応答も自己の証券投資目的のローカル保存に限り保存する**。第三者提供・再配信はしない（エラーログ・エクスポート等に出さない。`non-functional.md` §6）。仮想URL・認証ID・秘密鍵など認証情報は含めない） |
| created_at | text | NOT NULL | |

インデックス: `UNIQUE (instrument_id, timestamp)`, `INDEX (symbol, timestamp DESC)`, `INDEX (timestamp)`（`(timestamp)`は保持期間パージ`internal/service/retention`の期限切れ`id`取得`WHERE timestamp < ? ORDER BY timestamp, id LIMIT ?`を範囲走査にし、整列も発生させないための索引。既存2本は`timestamp`単独の範囲を引けない。マイグレーション000027。issue #533）

bid/ask系カラムの注意（issue #458）: 修正前に保存された`bid`/`ask`/`bid_depth`/`ask_depth`/`spread_bps`/`orderbook_imbalance`/`microprice`は、kabuステーションAPIの売/買命名を入れ替えずに保存していたため、bid/ask・数量が逆で`spread_bps`が常に負だった。保持期間（90日）で自然に消えるため再計算は行わず、過去分をスクリーニング・分析に使う場合はこの点に留意する（`raw_data_json`に生のBidPrice/AskPriceが残る）。ただしRAG検索対象の派生インデックス`market_snapshot_vectors`は、修正前の符号反転ベクトルが類似事例を歪めるため、マイグレーション`000023`で全件削除済み（`market_snapshots`本体は残す。issue #469, #470）。

ベクトルインデックス: `market_snapshot_vectors`（後述「ベクトルインデックス」参照、`rowid = market_snapshots.id`）

運用上の注意: 高頻度書き込みテーブルのため、周期実行1サイクル分（スクリーニング対象銘柄分）を1トランザクションにまとめて書き込み、SQLiteのWAL書き込みコストを抑える。保持期間は90日で、Schedulerの日次（起動時catch-up付き）ジョブ（`internal/service/retention`）が`timestamp`が90日より古い行を`market_snapshot_vectors`の対応行とともにバッチ削除する（期限切れ`id`は上記`INDEX (timestamp)`の範囲走査で取得する。`non-functional.md` §3）。

立花証券選択時の保存方針（issue #721。#720の決定）: 取得した時価・板（`market_snapshots`の各カラムと`raw_data_json`）のローカル保存と分析（特徴量・バックテスト・キャリブレーション・RAG索引）は、自己の証券投資目的に限り行う。これは`non-functional.md` §6の方針（API専用ページの「蓄積、編集および加工等は禁止」とインターネット取引規程第18条を、自己利用の範囲で解釈）に従う。**第三者への提供・再配信の経路は作らない**（DBバックアップは操作者本人の退避先に限る）。保存量は日中に全銘柄を巡回しないため監視銘柄（最大120件）に限られ、保持期間は同じ90日。夜間の日足スクリーニングの結果と翌日の監視リスト用テーブルは#726で追加する（本方針に従う）。kabu選択時の`/ranking`の値は従来どおり保存しない（FR-SCHED-8）。

## jev_decisions

Jev Scout/Traderの入出力ログ（Calibrationの基礎データ）。

```mermaid
erDiagram
    instruments ||--o{ jev_decisions : ""
    jev_decisions {
        integer id PK
        integer instrument_id FK
        varchar symbol
        text timestamp
        varchar decision_type "scout | trader"
        varchar state_hash
        text state_json
        varchar question_version
        text response_json
        varchar direction "LONG | SHORT | NONE, trader時のみ"
        numeric confidence
        integer latency_ms
        varchar model_id
        numeric request_cost
        text created_at
    }
```

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| id | integer | PK（AUTOINCREMENT） | |
| instrument_id | integer | FK → instruments.id, NOT NULL | |
| symbol | varchar(10) | NOT NULL | |
| timestamp | text | NOT NULL | Jev呼び出し時刻（RFC3339） |
| decision_type | varchar(10) | NOT NULL, CHECK IN ('scout','trader') | |
| state_hash | varchar(64) | NOT NULL | 入力状態のハッシュ。再評価抑制の判定に使用（`functional.md` FR-SCAN-2） |
| state_json | text | NOT NULL | Jevへの入力（JSON文字列、`functional.md` §4.4/4.5参照） |
| question_version | varchar(20) | NOT NULL | プロンプト/質問セットのバージョン（例: `scout-v3`/`trader-v3`。`scout-v2`/`trader-v2`はRAG文脈に実結果が無いと案内していた旧プロンプトの値、`scout-v1`/`trader-v1`は旧独自スキーマ時代の値） |
| response_json | text | NOT NULL | Jev回答を変換した`ScoutResponse`/`TraderResponse`のJSON文字列（公式APIの生レスポンスそのものではない） |
| direction | varchar(10) | NULL可, CHECK IN ('LONG','SHORT','NONE') | decision_type=trader時のみ設定 |
| confidence | numeric(5,4) | NULL可 | |
| latency_ms | integer | NOT NULL | |
| model_id | varchar(100) | NOT NULL | |
| request_cost | numeric(10,6) | NULL可 | API課金額（USD等）。TypeSafe AI公式API（`/v1/systemone`）は課金額を返さず`usage`のトークン数のみのため、現行実装は常にNULL |
| created_at | text | NOT NULL | |

インデックス: `INDEX (instrument_id, timestamp DESC)`, `INDEX (decision_type)`, `INDEX (state_hash)`, `INDEX (decision_type, timestamp)`, `INDEX (timestamp)`, `INDEX (instrument_id, decision_type, timestamp DESC, id DESC)`, `INDEX (decision_type, direction, confidence)`（`(decision_type, direction, confidence)`はJev Traderジョブごとのキャリブレーション判定`CountLabeledSamplesInConfidenceRange`がconfidence帯を索引範囲検索し、閾値（`MinCalibrationSamples`）件で`LIMIT`打ち切りして履歴行数に依存させないための索引。マイグレーション000028、`internal/repository/calibration/calibration_plan_test.go`が`EXPLAIN QUERY PLAN`で固定。issue #603）（`INDEX (instrument_id, decision_type, timestamp DESC, id DESC)`は銘柄ごとの最新Trader/Scout判断`LatestTraderByInstruments`/`LatestTrader`/`LatestScout`が銘柄駆動で先頭1行だけ索引seekするための索引で、全Trader行の走査を避ける。マイグレーション000025、`internal/repository/judgement/decision_plan_test.go`が`EXPLAIN QUERY PLAN`で固定。issue #498）（`(decision_type, timestamp)`と`(timestamp)`はActivity Logの直近判断`ListRecent`が`ORDER BY timestamp DESC, id DESC LIMIT N`を全走査・整列なしで引くため。マイグレーション000020）。`(decision_type, timestamp)`は`ListRecent`専用ではなく、Outcome Labeling（FR-CAL-4）が毎分走る`CalibrationRepository.PendingLabels`（`decision_type='trader'`かつ`timestamp`の範囲、下限は`now-24h`）の範囲走査にも必須で（issue #484）、`internal/repository/calibration/calibration_plan_test.go`が`EXPLAIN QUERY PLAN`で`jev_decisions_type_timestamp_idx`の範囲検索を固定している。この索引を変更・削除すると`jev_decisions`全履歴（保持期間なし）の走査へ退行する

ベクトルインデックス: `jev_decision_vectors`（`rowid = jev_decisions.id`）

## trade_signals

Policy Engineが生成した取引候補。

```mermaid
erDiagram
    instruments ||--o{ trade_signals : ""
    jev_decisions ||--o{ trade_signals : ""
    trade_signals {
        integer id PK
        integer instrument_id FK
        integer jev_decision_id FK
        varchar symbol
        text timestamp
        varchar direction "LONG | SHORT | NONE"
        numeric score
        numeric entry_price_reference
        varchar policy_version
        boolean risk_passed
        varchar reject_reason
        text created_at
    }
```

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| id | integer | PK（AUTOINCREMENT） | |
| instrument_id | integer | FK → instruments.id, NOT NULL | |
| jev_decision_id | integer | FK → jev_decisions.id, NULL可 | 根拠となったJev Trader判断 |
| symbol | varchar(10) | NOT NULL | |
| timestamp | text | NOT NULL | |
| direction | varchar(10) | NOT NULL, CHECK IN ('LONG','SHORT','NONE') | |
| score | numeric(6,4) | NULL可 | Policy Engine内部スコア |
| entry_price_reference | numeric(12,2) | NULL可 | |
| policy_version | varchar(20) | NOT NULL | Policy Engineのロジック版`policy-v1`。自己改善の適用提案のしきい値が有効な間は`policy-v1+sol-12`のように`policy_proposals.applied_policy_version`を付加（`functional.md` FR-POLICY-4/5, FR-SELFIMPROVE-5） |
| risk_passed | boolean | NOT NULL | Risk Engine通過可否 |
| reject_reason | varchar(255) | NULL可 | risk_passed=false時の理由。Policy Engineの理由（`spread_too_wide`等）、またはRisk Engine拒否時の`risk_engine_rejected: <Risk理由>`（例: `risk_engine_rejected: market_adverse_to_direction`。Risk理由が空なら`risk_engine_rejected`のみ） |
| created_at | text | NOT NULL | |

インデックス: `INDEX (instrument_id, timestamp DESC)`, `INDEX (risk_passed)`

## daily_bars

立花証券 e支店APIの夜間バッチ（issue #729、親 #726）が取り込む日足。1銘柄1立会日1行。**自己利用のローカル保存に限り、外部へ出す経路は作らない**（#720）。`instruments`には紐付けない（スクリーニングの母集団は監視対象の`instruments`より広い全銘柄のため、`symbol`は`instruments`にない銘柄も持つ）。マイグレーションは`000030_create_daily_bars_tables`。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| symbol | varchar(10) | NOT NULL, PK（`trade_date`と複合） | 銘柄コード |
| trade_date | text | NOT NULL, CHECK `YYYY-MM-DD` | 立会日（JST） |
| open / high / low / close | numeric | NOT NULL | 無調整の4本値（`pDOP`/`pDHP`/`pDLP`/`pDPP`） |
| volume | numeric | NOT NULL | 無調整の出来高（`pDV`） |
| adj_open / adj_high / adj_low / adj_close / adj_volume | numeric | NOT NULL | 株式分割換算係数で換算した値（`pDOPxK`等）。応答に無いときは無調整値と同じ。売買代金は応答に無いため持たない |
| created_at | text | NOT NULL, DEFAULT | 保存時刻（上書きでも更新される） |

`WITHOUT ROWID`（主キーは`(symbol, trade_date)`）。同じ立会日を再保存すると上書きする。取得は`CLMMfdsGetMarketPriceHistory`で、2回目以降は保存済みの最新日より新しい立会日だけを書く。最新日の換算値（`adj_close`/`adj_volume`）が変わった銘柄は株式分割とみなし、その銘柄の履歴を取得し直して置き換える。

## daily_bar_runs

夜間の日足バッチの実行記録（1夜1行。issue #729）。二重実行の防止、中断した夜の再開位置、失敗した夜の判定（後続の監視リスト確定が前営業日のリストの引き継ぎや`fixed`への切り替えに使う）に使う。

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| run_date | text | PK, CHECK `YYYY-MM-DD` | 夜の基準日（その夜が属する立会日。00:00〜07:59は前日の夜） |
| status | text | NOT NULL, CHECK IN ('running','succeeded','failed') | `succeeded`は全対象銘柄を試した夜（全銘柄が失敗した夜は`failed`）。`failed`は中断した夜（`error`に理由） |
| started_at / finished_at | text | started_at NOT NULL | 最初の開始と直近の終了（UTC。固定9桁の小数秒） |
| symbols | integer | NOT NULL | 対象ユニバースの銘柄数 |
| requests | integer | NOT NULL | 日足の要求数（再開分を含む累計） |
| saved_bars | integer | NOT NULL | 保存した日足の本数 |
| failed | integer | NOT NULL | 要求に失敗した銘柄数 |
| duration_ms | integer | NOT NULL | 所要時間（再開分を含む累計） |
| cursor | text | NOT NULL, DEFAULT '' | 最後に処理した銘柄コード（再開はその次から） |
| error | text | NOT NULL, DEFAULT '' | 中断の理由（価格の生値は含めない） |

## watch_lists / watch_list_entries

立花証券選択時の監視リスト（最大120件。issue #730、親 #726）。引け後に翌立会日分を1日1件確定して保存し、翌朝のEVENT購読・market-data投入・Fast Screenerの母集団になる（接続は#731）。`daily_bars`と同じく自己利用のローカル保存で、持つのは銘柄コードと選定理由だけ（価格の生値は持たない）。直近60日より古いリストは保存のたびに削除する。

### watch_lists

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| list_date | text | PK, CHECK `YYYY-MM-DD` | リストが使われる立会日（JST）。休場日は飛ばした翌立会日 |
| source | text | NOT NULL, CHECK IN ('daily_screen','fixed','carried_over','fixed_fallback') | 確定方法。`daily_screen`＝日足スクリーニング、`fixed`＝運用者の固定リスト、`carried_over`＝日足を使えず前営業日のリストを引き継いだ、`fixed_fallback`＝日足を使えず固定リスト（固定リストが無ければ保有・注文中のみ）へ切り替えた |
| reason | text | NOT NULL, DEFAULT '' | 確定の説明。代替リスト（`carried_over`/`fixed_fallback`）では日足を使えなかった理由と切り替え先（バナー・Activityにもこの文を出す） |
| basis_date | text | NOT NULL, DEFAULT '' | スクリーニングに使った日足の立会日（使わないときは空） |
| decided_at | text | NOT NULL | 確定時刻（UTC。固定9桁の小数秒） |

### watch_list_entries

| カラム | 型 | 制約 | 説明 |
|-------|-----|------|------|
| list_date | text | NOT NULL, PK（`position`と複合） | `watch_lists.list_date`（FKは張らない。`Save`が1トランザクションで入れ替える） |
| position | integer | NOT NULL, CHECK >= 0 | 購読・表示順（0起点。保有・注文中が先頭） |
| symbol | varchar(10) | NOT NULL | 銘柄コード |
| origin | text | NOT NULL, CHECK IN ('held','manual','screen','fixed') | 枠の由来。`held`＝保有・注文中の固定枠、`manual`＝手動指定、`screen`＝スクリーニング上位、`fixed`＝固定リスト |
| indicators | text | NOT NULL, DEFAULT '' | `screen`のとき選ばれた指標（`gain_rate`/`loss_rate`/`volume`/`turnover`/`volume_surge`/`turnover_surge`/`range_rate`のカンマ区切り。設定順） |

`WITHOUT ROWID`（主キーは`(list_date, position)`）。同じ`list_date`を再保存するとエントリごと置き換える。`GET /watchlist`が直近7立会日分を表示する。
