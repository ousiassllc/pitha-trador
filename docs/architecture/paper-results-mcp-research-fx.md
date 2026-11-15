# 設計: 研究用の為替 1 分足（USD/JPY）の記録と fx_series_summary

`docs/architecture/paper-results-mcp.md` §5 の分割章（行数上限のため別ファイル）。issue #1084 の **USD/JPY（為替）の部分だけ**。ツールの引数と出力は `docs/api/endpoints/paper-results-mcp-research-fx.md` §3.35、表は `docs/architecture/er/tables-research.md`、GMO のアダプタは `docs/architecture/overview/integrations.md` §14。

日経平均・TOPIX など指数の記録は**まだ実装していない**（立花の日中の時価の記録 #1083 が使えるようになってから別に行う）。指数の表・設定・ツールはこの章の範囲にない。

## 1. 目的と位置づけ

判断の良し悪しを地合いで切り分けるには、為替の系列が要る。いまの市場コンテキスト（`internal/service/featureengine/marketcontext`）は、監視している銘柄から計算した値が中心。GMOコイン 外国為替FX の公開 API の読み取り専用アダプタ（`internal/service/fx/gmo`、認証なし、#907）は実装済みだったがどこからも呼ばれていなかった。このアダプタで USD/JPY（設定で他の通貨ペアも可）の 1 分足を研究用の表に記録し、記録の件数・最終時刻・欠けを読み取り MCP で見られるようにし、評価ツール（`jev_evaluation`）が判断の時刻に合わせて結合できるようにする。**取引には使わない。**

**取引に影響しない**ことは次で担保する:

- **既定はオフ**: 記録は Settings の `research.fx.enabled`（`runtime_settings` の JSON 整数 1 / 0、既定 0。Settings「研究用の為替 1 分足の記録」。Paper 実績 MCP の `settings_set` からも変えられる）がオンのときだけ動く。オフ（未設定・0・壊れた値）の間、日次タスクは `maintenance.ErrSkipped` を返し、**外部への要求を 0 回**にし、表にも `runtime_settings`（ランナーの最終成功日 `system.maintenance.research_fx.last_success_date`）にも何も書かない。`researchfxjob` の `TestJob_IsOffByDefaultMakesNoRequestAndWritesNothingWhileOff` が、未設定・0・壊れた値・範囲外のそれぞれで、GMO のモックサーバが受けた要求数が 0、3 表（2 表と `runtime_settings`）の行数が変わらず、最終成功日が無いことを固定する。
- 書くのは新しい 2 表（`research_fx_candles` / `research_fx_days`）だけ。既存の表には列も制約もトリガも足さない。Feature Engine（市場コンテキストを含む）・Policy・Risk・発注・Paper ガード・Live・Jev のプロンプト・RAG・メタモデルの学習・自己改善・既存のシャドー（trader-v5）は変えず、この記録を読まない。
- **kabu の登録枠を使わない・登録要求を増やさない**: 記録は GMO の公開 API だけを呼ぶ。記録の 4 パッケージ（`domain` / `repository` / `service` / `bootstrap` の `researchfx*`）と設定のパッケージは、`go list -deps` で見て、ブローカー・kabu・立花のクライアント（`internal/service/broker`・`internal/service/marketdata`）にも、Feature Engine・Policy・Risk・発注・Jev のコードにも依存しない。`internal/repository/researchfx/isolation_test.go` の `TestRecordingPackagesDependOnNoBrokerOrTradingCode` がこれを固定する。監視銘柄の数も減らさない。
- 参照を固定する: 同じテストの `TestResearchFXRecordIsReadOnlyByItsRecorderAndTheResearchTools` が、表名・パッケージ・設定キー・GMO アダプタのシンボルを、記録部品・日次タスク・設定・読み取り専用の MCP ツール・`jev_evaluation` の結合・Settings の保存と画面のグループ・`db/migrations` のファイルだけに限る。`TestTradingSidePackagesDoNotMentionTheFXRecord` が、Feature Engine（`marketcontext` を含む）・Policy・Risk・execution・ブローカー・Jev・RAG などのパッケージが何も参照しないことを名指しで固定する。記録部品が書く表が自分の 2 表だけ、MCP の問い合わせは SELECT だけであることも同じテストが固定する。
- 有料 API は呼ばない（GMO の公開 API は無料・認証なし）。テストは `httptest` のモックだけで、本物の GMO には接続しない。キー・パスワードなどの秘密はコードにも出力にも無い。

## 2. 定義

| 項目 | 規則 |
|---|---|
| 取引日 | GMO の 1 分足の `date`。**06:00 JST 切り替え**（その日の 06:00 JST から翌 06:00 JST）。`research_fx_candles.trading_day` と `research_fx_days.trading_day` は、この日付（`YYYY-MM-DD`）。取引日が終わった（翌日の 06:00 JST を過ぎた）ものを「完了」とする |
| 足 | 1 分足。GMO の仕様で、BID（売値）と ASK（買値）を**別々に**取る。`open_time` で 1 行にまとめ、片側にしか足が無い分はもう片側の 4 列が NULL |
| 市場休み（`empty`） | GMO が答えたが、その取引日の足が BID・ASK とも 0 本だった日（土日・休日）。**欠けではない**。`research_fx_days.status = 'empty'` |
| 記録済み（`recorded`） | 足が 1 本以上あった日 |
| 欠けの日（`missing`） | 記録を始めた取引日（最初の印の日）以降で、取引日が終わっているのに印の無い日。まだ取っていない・取得に失敗した・取得期間を過ぎて取れなかった日。**欠け**（市場休みではない） |
| 日の中の欠け（`interior_gaps`） | 足のある取引日の、最初の足と最後の足の**あいだ**にある、足が 1 本も無い分の連続。`start`・`end`・`minutes` で返す。日の始まりが遅い・終わりが早いのは欠けに数えず、足の無い日や日をまたぐ区間は含まない（市場休みと取り違えない） |
| 時刻 | UTC・固定 9 桁（`docs/architecture/er.md` §型・規約「日時」）。ツールの出力は UTC の RFC3339（秒） |

## 3. 記録（internal/service/researchfx）

- 使うのは**既存のアダプタ** `internal/service/fx/gmo`（挙動は変えない）。1 秒に 1 回までの上限はアダプタの限度器（`MinInterval` は既定のまま。1 秒未満は設定できない）が守る。1 つの取引日 = 通貨ペア 1 つにつき `klines` を 2 回（BID・ASK）。
- `Recorder.Run`（1 回 = 1 チャンク）: 設定の通貨ペアと「取得期間」の完了した取引日のうち、`research_fx_days` に**印のない**（ペア, 日）を、日の古い順・ペアは設定の順に **1 つ**取り、BID と ASK を取って開始時刻でまとめ、足と印を **1 トランザクション**で書く。残りがあれば `Result.More` を立てる。
  - **取得期間**は直近 7 日（`DefaultLookbackDays`）。いまの取引日（まだ終わっていない日）は取らず、`gmo.MinuteCandlesSince`（2023-10-28）より前も取らない。期間より古い日で取れていないものは欠けのまま。
  - 失敗（ネットワーク・GMO のエラー・書き込みの失敗）は印を残さない。次の実行が同じ（ペア, 日）をやり直す。ただし GMO が拒み続ける日 1 つで後ろの日が止まらないよう、この実行の中で失敗した単位は他の単位のあとに回す。
  - 書き込みは `INSERT ... ON CONFLICT (symbol, open_time) DO NOTHING`（`INSERT OR IGNORE` ではなく CHECK 違反は失敗にする）と、印の `INSERT OR REPLACE`。何度実行しても行は増えない。印の `candle_count` は書いたあとのその日の行数。
  - **1 秒に 1 回の上限を、アダプタ（ペアごとのクライアント）をまたいでも守る**: 同じペアの 2 回はクライアントの限度器が 1 秒空け、別の実行・別のペアのあいだは `Recorder` が前の実行の要求が終わってから 1 秒（`gmo.MinCallInterval`）空ける。
- 日次タスク `research_fx`（`internal/bootstrap/researchfxjob`）: メンテナンスタスクとして `internal/bootstrap/services_build.go` の `scheduler.WithMaintenanceTask` に 1 行で登録する（`research_labels` と同じ作り）。**7:00 JST より前は `maintenance.ErrSkipped`**（06:00 の取引日の切り替えのあと、前の取引日が完了している）。設定（オン・オフ・通貨ペア）は `system.RuntimeSettingsRepository` から実行ごとに読むので、保存すると再起動なしで次のチェック（10 分以内）から効く。`maintenance.ErrMore` で 1 チャンクごとにランナーの `mu` を放す。パニックはエラーにして返し、取引のプロセスを落とさない。失敗はランナーが他のタスクと隔離して再試行する。
- 初回（スイッチをオンにした直後）は取得期間の 7 日を 1 日ずつで追いつく。日次の実行では、新しく完了した 1 日（ペアごとに 2 回の要求）だけを取る。

## 4. 読み取りツール（fx_series_summary）

- 実装: `internal/service/paperresultsmcp/fxdiag`（SQL と集計）、登録は `internal/service/paperresultsmcp/jev_models_tool.go`（`queryTool`。`init()` で `tools` に追加。ほかの読み取り専用の研究ツールと同じ場所）。テストは `internal/service/paperresultsmcp/fxtests`。読み取り専用接続（`mode=ro`）で SELECT だけを発行する。
- 系列（通貨ペア）ごとに、期間（取引日の `[from, to)`）の足の件数・最初と最後の足の `open_time`・BID だけ／ASK だけの分の数、取引日の内訳（`recorded` / `market_closed` / `missing` の件数と日付の一覧）、`interior_gaps`（連続した欠けを `start`・`end`・`minutes` で。一覧は最大 50 件・`truncated`。`runs` と `missing_minutes` の合計は全件）を返す。日の中の欠けは SQL の窓関数（`LAG`）で隣り合う足の間隔が 1 分より大きいところだけを読む。
- `recording` に記録のオン・オフ（`research.fx.enabled`）・通貨ペア（`research.fx.symbols`）・取得期間の日数を返す。`runtime_settings` は、この 2 キーだけを読める（他のキーは SQL を発行する前に拒否）。
- 欠けの日は「最初の印の日」以降の、完了した取引日のうち印の無い日。最初の印より前の日と、いまの取引日は数えない。印の無い日に足が残っていても（印を足と同じトランザクションで書くのでふつうは無い）その日は「印なし」と数える。
- `notice` と `definitions` を毎回返し、しきい値の判定・推奨は返さない。

## 5. 評価ツールとの結合（at-or-before）

- `fxdiag.AtOrBefore(ctx, q, symbol, times, staleness)`: 判断の時刻ごとに、**その時刻以前で最新の、BID・ASK の両方がある足**を返す。足の `open_time` が時刻から `staleness`（既定 5 分）より古ければ「一致なし」（nil）。片側だけの分は飛ばす。主キー `(symbol, open_time)` の範囲で 1 件を引く読み取り専用の関数で、同じ分の時刻は結果を使い回す。価格は足の**始値**（`MidOpen` = BID 始値と ASK 始値の平均）を使う。終値は 1 分あとにしか分からないので、判断の時刻より後の価格を見ない。
- `jev_evaluation` の任意の引数 `fx_symbol`（既定なし）: 指定すると、各 cohort に `fx_context` を足す。**llm アームの行**を、判断の時刻の直前 60 分の mid 価格の変化の符号（`up` / `flat`（変化なし）/ `down` / `unmatched`（判断の時刻・60 分前のどちらかに足が無い））に分け、件数と平均 net bps を返す。記述のみで、しきい値は無く、取引には使わない。指定しなければ `fx_context` というキーはなく、出力は引数が無かった頃と**バイト単位で同じ**（`evaltests` の `TestWithoutFXSymbolTheOutputIsByteIdentical` が、引数の追加前の `main` で取った応答と一致することを固定する）。実装は `internal/service/paperresultsmcp/evaldiag/fxcontext.go`（新ファイル）で、既存のロジックへの変更は決定の時刻を行に持たせる 1 フィールドと呼び出し 1 か所だけ。

## 6. 量

1 ペアで 1 日 1440 本ぐらい（為替は 24 時間・土日は無い）。1 行は主キー込みで約 150 バイト [INFERENCE]。1 ペア・1 年で約 52 万行・約 80MB。この記録には**削除（保持期間）を設けていない**（量が小さく、研究のために長く残すため）。ペアは最大 5 件。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-15 | 新規（USD/JPY の部分。指数は未実装で #1083 待ち） | issue #1084 |
