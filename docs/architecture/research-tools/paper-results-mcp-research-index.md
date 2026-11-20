# 設計: 研究用の指数（日経平均・TOPIX）の 1 分値の記録と index_series_summary

`docs/architecture/paper-results-mcp.md` §5 の分割章（行数上限のため別ファイル）。issue #1084 の**主要指数の部分**。ツールの引数と出力は `docs/api/endpoints/research-tools/paper-results-mcp-research-index.md` §3.51、表は `docs/architecture/er/research-tools/tables-research-index.md`。為替（USD/JPY）の部分は `paper-results-mcp-research-fx.md`（別の記録・別のスイッチ）。

## 1. 目的と位置づけ

判断の良し悪しを地合いで切り分けるには、指数の系列が要る。いまの市場コンテキスト（`internal/service/featureengine/marketcontext`）は、監視している銘柄から計算した値が中心で、そこに日経平均・TOPIX の 1 分値は無い。issue の指定は「kabu の登録枠（50）を使わない方法で取る。立花の記録（#1083）が使える場合はそこから取り、使えない場合は『取れていない』と記録する」。

**取得元**: 立花証券 e支店 API（無料・個人向け。新しいキーも有料のサービスも要らない。#1083 がすでに使っている同じ認証ID・同じ EVENT 受信）。指数の値は、立花の日中の時価の記録（`research_quote_samples`、#1083）に残った指数コードの行から写す。**この記録は立花にも kabu にも GMO にも一切要求を出さない**（ローカルの表を読むだけ）。指数の銘柄コード（立花の「指数・為替」: `101`＝日経平均、`151`＝TOPIX）で立花が時価を返すかは、**立花の認証情報がある環境でしか確かめられない**（この変更のテストはモックの表で行い、立花へは接続していない）。値が無ければ、その日は `unavailable`（取れていない）と記録される。

**取引に影響しない**ことは次で担保する:

- **既定はオフ**: 記録は Settings の `research.index.enabled`（`runtime_settings` の JSON 整数 1 / 0、既定 0。Settings「研究用の為替・指数の 1 分値の記録」。Paper 実績 MCP の `settings_set` からも変えられる）がオンのときだけ動く。オフ（未設定・0・壊れた値）の間、日次タスクは `maintenance.ErrSkipped` を返し、**読み取りも書き込みも外部への要求も 0 回**で、表にも `runtime_settings`（ランナーの最終成功日 `system.maintenance.research_index.last_success_date`）にも何も書かない。`researchindexjob` の `TestJob_IsOffByDefaultAndTouchesNothingWhileOff` が、未設定・0・壊れた値・範囲外のそれぞれで、表の行数が増えず、HTTP の要求が 0 回であることを固定する。
- 書くのは新しい 2 表（`research_index_minutes` / `research_index_days`）だけ。既存の表（立花の `research_quote_samples` を含む）には列も制約もトリガも足さず、**書かない**。Feature Engine（市場コンテキストを含む）・Policy・Risk・発注・Paper ガード・Live・Jev のプロンプト・RAG・メタモデルの学習・自己改善・既存のシャドー（trader-v5）は変えず、この記録を読まない。
- **kabu の登録枠を使わない・登録要求を増やさない**: 記録の 4 パッケージ（`domain` / `repository` / `service` / `bootstrap` の `researchindex*`）と設定のパッケージは、`go list -deps` で見て、ブローカー・kabu・立花のクライアント（`internal/service/broker`・`internal/service/marketdata`）にも、Feature Engine・Policy・Risk・発注・Jev のコードにも依存しない。`internal/repository/researchindex/isolation_test.go` の `TestRecordingPackagesDependOnNoBrokerOrTradingCode` がこれを固定する。立花の時価の記録（`researchquotes`）・kabu の監視銘柄の選び方・監視銘柄の数は**変えない**（この変更はそれらのコードに触れない）。
- 参照を固定する: 同じテストの `TestResearchIndexRecordIsReadOnlyByItsRecorderAndTheResearchTools` が、表名・パッケージ・設定キーを、記録部品・日次タスク・設定・読み取り専用の MCP ツール・`jev_evaluation` の結合・Settings の保存と画面のグループ・`db/migrations` のファイルだけに限る。`TestTradingSidePackagesDoNotMentionTheIndexRecord` が、Feature Engine（`marketcontext` を含む）・Policy・Risk・execution・ブローカー・Jev・RAG などが何も参照しないことを名指しで固定する。記録部品が書く表が自分の 2 表だけ（`research_quote_samples` は SELECT だけ）、MCP の問い合わせは SELECT だけであることも同じテストで固定する。
- 有料 API は呼ばない。キー・パスワードなどの秘密はコードにも出力にも無い。

## 2. 定義

| 項目 | 規則 |
|---|---|
| 立会日 | 東証の立会日（JST の日付。`marketcalendar.TSE.IsTradingDay`）。為替と違い 06:00 の切り替えは無い。立会が終わった（15:35 JST を過ぎた）ものを「完了」とする |
| 分 | 立会の 1 分（09:00〜11:30、12:30〜15:30 JST の 330 分）。値は立花の時価の記録がその分の 5 秒目に取った値で、始値でも終値でもない |
| 取れていない（`unavailable`） | 確認したが、立花の時価の記録にそのコードの値が立会の分に 1 件も無かった立会日。`research_index_days.status = 'unavailable'`。立花の記録がオフ・コードが購読の対象でない・立花が値を返さなかった、のどれか（区別はしない） |
| 記録済み（`recorded`） | 値が 1 件以上あった日 |
| 欠けの日（`missing`） | 記録を始めた立会日（最初の印の日）以降で、立会日が終わっているのに印の無い日。まだ確認していない・確認に失敗した・取得期間を過ぎて確認できなかった日 |
| 日の中の欠け（`interior_gaps`） | 値のある立会日の、最初の値と最後の値の**あいだ**にある、立会の分で値が 1 件も無い連続。昼休み（11:30〜12:30）は数えない。日の始まりが遅い・終わりが早いのは欠けに数えない |
| 時刻 | UTC・固定 9 桁（`docs/architecture/er.md` §型・規約「日時」）。ツールの出力は UTC の RFC3339（秒） |

## 3. 記録（internal/service/researchindex）

- 値の元は立花の時価の記録 `research_quote_samples`（`source = 'tachibana'`）。**指数コードの行がそこに入る条件**は、立花の時価の記録（`research.tachibana_quotes.enabled`、既定オフ）がオンで、追加銘柄（`research.tachibana_quotes.extra_symbols`）に指数コード（`101`、`151`）が入っていること。この記録の既定のコード（`research.index.symbols` の既定 `101,151`）を自動で購読には足さない（立花の購読の銘柄は #1083 のものを変えない）。入っていなければ、その日は `unavailable` になる。
- `Recorder.Run`: 設定の指数コードと「取得期間」の完了した立会日（直近 5 立会日。`Window`）のうち、`research_index_days` に**印のない**（コード, 日）を、日の古い順・コードは設定の順にすべて確認する。各（コード, 日）で、立花の記録のその JST 日の行から立会の分（`marketcalendar.TSE.IsOpen`）のものを `research_index_minutes` に写し、値と印を **1 トランザクション**で書く。値が 1 件も無ければ印だけ（`unavailable`）。
  - 失敗（読み取り・書き込みの失敗）は印を残さない。次の実行が同じ（コード, 日）をやり直す。
  - 書き込みは `INSERT ... ON CONFLICT (symbol, minute_time) DO NOTHING`（CHECK 違反は失敗）と、印の `INSERT OR REPLACE`。何度実行しても行は増えない。**印を付けた日は最終**で、あとから値が入っても写し直さない。
- 日次タスク `research_index`（`internal/bootstrap/researchindexjob`）: メンテナンスタスクとして `internal/bootstrap/researchextrawire` から `scheduler.WithMaintenanceTask` に 1 行で登録する（`research_fx` と同じ作り。`services_build.go` には載せない）。**15:35 JST より前は `maintenance.ErrSkipped`**（その日の最後の分が記録されてから）。設定（オン・オフ・コード）は `system.RuntimeSettingsRepository` から実行ごとに読むので、保存すると再起動なしで次のチェック（10 分以内）から効く。パニックはエラーにして返し、取引のプロセスを落とさない。失敗はランナーが他のタスクから隔離して遅延後に再試行する。スイッチが読めないときは警告ログを出して `ErrSkipped`（失敗として数えず、オンとは見なさない）。
- 立花の記録は既定 90 日で退避・削除されるが、この記録は写した値を削除しない。写すのは日次なので、立花の保持期間（1〜3650 日）が 5 日より短くない限り値は失われない。

## 4. 読み取りツール（index_series_summary）

- 実装: `internal/service/paperresultsmcp/indexdiag`（SQL と集計）、登録は `internal/service/paperresultsmcp/jev_models_tool.go`（`queryTool`）。テストは `internal/service/paperresultsmcp/indextests`。読み取り専用接続（`mode=ro`）で SELECT だけを発行する。
- 系列（指数コード）ごとに、期間（立会日の `[from, to)`）の値の件数・最初と最後の分の `open_time`・立花の記録にそのコードの行がいくつあるか（`source_samples`。0 なら、記録の元に指数が入っていない）、立会日の内訳（`recorded` / `unavailable` / `missing` の件数と日付の一覧）、`interior_gaps`（連続した欠けを `start`・`end`・`minutes` で。昼休みを除く立会の分だけ。一覧は最大 50 件・`truncated`。`runs` と `missing_minutes` の合計は全件）を返す。
- `recording` に記録のオン・オフ（`research.index.enabled`）・指数コード（`research.index.symbols`）・取得期間の日数を返す。`runtime_settings` は、この 2 キーだけを読める（他のキーは SQL を発行する前に拒否）。
- 欠けの日は「最初の印の日」以降の、完了した**立会日**のうち印の無い日。最初の印より前の日と、まだ終わっていない今日は数えない。土日・祝日は数えない。
- `notice` と `definitions` を毎回返し、しきい値の判定・推奨は返さない。

## 5. 評価ツールとの結合（at-or-before）

- `indexdiag.AtOrBefore(ctx, q, symbol, times, staleness)`: 判断の時刻ごとに、**その時刻の時点で取得済みの最新の値**を返す。値は分の開始から 5 秒後（`SampleLag`）に取るので、`minute_time + 5 秒 <= 判断の時刻` の分だけが見える（判断の時刻より後の値を見ない）。分の開始が `staleness`（既定 5 分）より古ければ「一致なし」（nil）。立会外・昼休み・記録の欠けは「一致なし」で、直前の値を持ち越さない。主キー `(symbol, minute_time)` の範囲で 1 件を引く読み取り専用の関数で、同じ秒の時刻は結果を使い回す。
- `jev_evaluation` の任意の引数 `index_symbol`（既定なし）: 指定すると、各 cohort に `index_context` を足す。**llm アームの行**を、判断の時刻の直前 30 分の指数の変化の符号（`up` / `flat`（変化なし）/ `down` / `unmatched`（判断の時刻・30 分前のどちらかに値が無い））に分け、件数と平均 net bps を返す。為替（`fx_symbol`、60 分）より窓が短いのは、指数は立会の分にしか値が無く、60 分では午前の判断の多くが一致なしになるため。記述のみで、しきい値は無く、取引には使わない。指定しなければ `index_context` というキーはなく、出力は引数が無かった頃と**バイト単位で同じ**（`evaltests` の `TestWithoutIndexSymbolTheOutputIsByteIdentical`）。`fx_symbol` と同時に指定できる。

## 6. 量

1 コードで 1 日 330 行、1 行は約 100 バイト [INFERENCE]。1 コード・1 年で約 8 万行・約 8MB。この記録には削除（保持期間）を設けていない。コードは最大 5 件。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-11-20 | 新規（主要指数の部分。為替は別の記録） | issue #1084 |
