# 設計: 適時開示による見送りルールの記録（Phase 1）

issue #935。要件は `requirements/functional/components-platform.md` §4.16 の FR-DISCLOSURE-SKIP-1〜5、テーブルは `architecture/er/tables-disclosure.md`、MCP は `api/endpoints/paper-results-mcp-disclosure-skip.md` §3.31。実装は `internal/service/disclosureskip`（ルール・記録部品・評価部品）、`internal/bootstrap/disclosurewire`（配線）、`internal/service/paperresultsmcp/disclosureskipdiag`（MCP の集計）。

## 1. 目的と、やらないこと

決算短信・業績予想の修正（上方・下方とも）の直後のエントリーを機械的に見送るルールが、勝ち負けに効くかを確かめるために、**取引に一切影響させずに**記録する。ルールに当てはまったか（`skip`）、当てはまらなかったか（`no_skip`）、フィードの欠けで分からなかったか（`unknown`）を、Jev のエントリー候補の判断ごとに残し、結果ラベル（`calibration_outcomes`）と突き合わせて MCP で読む。

- 取引・Policy の判断・Risk・発注・Paper ガード・Live 経路・設定の既定値・資金とリスクの値・費用上限・昇格の仕組み・Jev のプロンプトは**変えない**。この記録はエントリーを止めないし、Jev に渡さない（`news_context` にも足さない）。
- 新しい取得経路を作らない。News Ingest（FR-LUNA-1〜6）が既に叩いているやのしん TDnet WebAPI の応答を使う。公式 TDnet の閲覧サイトのスクレイピング・PDF の取得・J-Quants・有料または新規の外部サービスは使わない。リクエストの頻度は変えない（§6）。
- 開示は単独のエントリー起点にしない（`overview.md` の「含まないもの」は維持する）。

## 2. 見送りルール（純関数。`disclosureskip.Evaluate`）

入力は、判断の時刻 `t`・銘柄・保存済みの開示・その銘柄の取得の記録・東証カレンダー（`marketcalendar`）。時刻は JST で数える。

- 対象の開示: 表題が決算短信、または業績予想の修正（`業績予想` と `修正` を両方含む。上方・下方とも）。配当予想だけの修正・剰余金の配当・社債の償還完了・株主総会の招集などは対象外。語の定義は `disclosureskip/kind.go` の `kindRules` の 1 か所だけ。語を変えたら `RuleVersion` を上げる。
- 窓: `[WindowStart(t), t]`。`WindowStart` は **`t` の JST 日付より前の直近の東証営業日の 15:00 JST**。`t` 当日の全時間と、直前の営業日の 15:00 以降（15:00:00 を含み、14:59:59 は含まない）を覆う。土日・祝日・年末年始をまたぐ（金曜 15:00 以降 → 月曜、祝日の月曜をまたぐ火曜は金曜 15:00 以降）。営業日は `marketcalendar.Calendar.IsTradingDay`。
- **未来の開示は見ない**: `published_at <= t` のものだけ。`published_at` が `t` の 1 秒後でも該当にしない（14:50 の判断に 15:30 の開示は非該当）。
- 該当（`skip`）: 窓の中に対象の開示が 1 件以上ある。覆いの有無に関係なく確定する（存在が分かっている開示は、欠けで取り消されない）。
- 非該当（`no_skip`）: 該当がなく、窓が覆われている（§3）。
- 不明（`unknown`）: 該当がなく、窓が覆われていない。「該当なし」と区別する。理由は `no_fetch` / `incomplete_fetch`。

## 3. 「不明」の定義（取得の覆い）

やのしんの応答は、銘柄ごとに新しい順の最大 `newsfeed.YanoshinLimit`（10）件。ある取得 `f` が判断 `t` の窓を**覆う**のは、次の全てを満たすとき。

1. `f` の銘柄が判断の銘柄と同じで、成功した取得（失敗・タイムアウト・HTTP エラー・不正な応答は覆わない）。
2. `f` のリクエストの開始が `t + FetchLag`（5 分）以後。判断より前の取得は、判断の直前に出た開示を知らない。やのしんは非公式で、開示が出てから索引に載るまで遅れうるため、判断の直後の取得も覆いに数えない（実測した遅れではなく、守りの値）。
3. 開示時刻を読めなかった項目が 0 件。表題がなく News Ingest の戻り値から捨てられた項目も、時刻を読めなかった項目と同じく 0 件でなければならない（捨てられた項目は何の開示か分からないため）。
4. 件数上限に届いていない（応答が 10 件未満）、または届いていても応答の最古の開示時刻が窓の始まりより前（それより古い開示を切っただけで、窓の中は全部見えている）。件数は、表題がなく捨てられた項目も含む生の応答の件数で数える（捨てた結果 10 件未満に見えても、応答が 10 件に届いていれば切られている）。

覆う取得がなく、成功した取得（2. を満たすもの）も無ければ `no_fetch`。成功した取得はあるが 3.・4. で覆えなければ `incomplete_fetch`。バックオフ中でリクエストを出さなかった回と、終了処理で中断した回は記録しない（フィードについて何も言えないため。そのぶん、覆う取得が減って「不明」になる）。

News Ingest が取得するのは東証の立会時間内（9:00–11:30 / 12:30–15:30）だけで、対象は Fast Screener 候補と保有銘柄だけ。そのため、次の判断は覆う取得が来にくい（issue #1053 で対策した。§4 の 3・5）。

- 前場引け際（11:25〜11:30）と大引け前 5 分（15:25〜15:30）の判断は、`FetchLag`（5 分）後が昼休みか場後になる。待つ上限を**立会時間の分で数える**ので（§4 の 3）、後場の最初の取得・翌営業日の最初の取得で覆えれば不明にならない（取得は判断より後に始まったものだけを数え、未来の開示は見ない）。
- 判断の直後に Fast Screener 候補・保有銘柄から外れた銘柄は、News Ingest の取得が止まる。**後追い取得**（§4 の 5）が、まだ記録が確定していない判断の銘柄にだけ、覆う取得を 1 回ずつ出す。

それでも覆えなかったものは「該当なし」ではなく「分からない」として記録し、理由の内訳（`unknown_detail`）を残す（§3.1）。

### 3.1 「不明」の理由の内訳（`unknown_detail`）

`unknown_reason`（`no_fetch` / `incomplete_fetch`）に、確定のときに次の内訳を足して `disclosure_skip_records.unknown_detail` に保存する（`Evaluate` が決める純関数。マイグレーション `000051`）。取得の記録（`disclosure_fetches`）は 7 日で消えるので、後から突き合わせて分類し直すのではなく、確定の時点の分類を記録に残す。古い行は付け直さない（NULL。MCP は `unclassified` に数える）。

`no_fetch`（`t` + `FetchLag` 以後に始まった成功取得が無い）は、次の順で最初に当てはまるものにする。

1. `fetch_failed`: 判断の後に始まった取得が失敗していた（失敗は覆いに数えない。バックオフでリクエストを出さなかった回は記録に無いので数えない）。
2. `lunch_break`: `t + FetchLag` が昼休み（11:30–12:30）で、後場の取得でも覆えなかった。
3. `after_close`: `t + FetchLag` が大引け後・開場前・休場日で、次の立会の取得でも覆えなかった。
4. `within_fetch_lag`: 判断の直後（`FetchLag` 未満）の成功取得しか無い。
5. `dropped_from_candidates`: 判断より前の成功取得しか無い（候補・建玉から外れ、後追いも届かなかった）。
6. `no_fetch_at_all`: 判断の前後とも、その銘柄の成功取得が無い。

`incomplete_fetch` は、`undated_items`（開示時刻を読めない・捨てられた項目があった取得がある）か `item_limit`（件数上限で切られ、窓の始まりまで届かなかった）。

## 4. 記録のタイミング（取引経路にフックを足さない）

1. **フィードの取得結果を横取りする**: `disclosureskip.RecordingFeed` が News Ingest の `newsfeed.Feed`（`GuardedFeed` の外側）を包む。`Fetch` は内側の戻り値・エラーを**そのまま返す**。取得結果（銘柄・開始時刻・成否・件数・件数上限への到達・開示時刻のない項目数・最古の開示時刻・各開示の ID・時刻・表題・URL）を `recover` の下でコピーし、上限付きのキュー（1024）へ**ブロックせず**入れる。満杯ならそのコピーを捨てる（ログに出す）。内側のリクエストは増やさない。表題がなく捨てられた項目の数は、1 回の `Fetch` ごとに別の `newsfeed.IndexStats` を `context` に載せて内側（`YanoshinClient`）に書かせて知る（戻り値・エラーは変えない）。生の件数を件数・件数上限への到達の判定に使い、捨てた項目の数を「開示時刻のない項目数」に足す。
2. **別のゴルーチンが書く**: キューを 1 つの書き手が読み、`disclosure_fetches` と `disclosure_items` へ 1 取得 1 トランザクションで書く。書き込みの失敗・パニックは握りつぶしてログに出し、次の取得は続ける。
3. **別のゴルーチンが後追いで評価する**: 評価部品が 1 分ごとに、`jev_decisions` の**読み取り**で、記録のないエントリー候補の判断（§5）を探し、覆う取得が現れた判断はその場で、現れないまま 60 分（`DefaultGiveUp`。News Ingest の最長バックオフ 30 分より長い。**立会時間の分で数える**: 昼休み・場後・夜間・休場日は数えず、判断から 60 分の立会時間がたった時刻 `SessionDeadline` が上限。1 つの立会の中では今までどおり 60 分）たった判断は「不明」として、`disclosure_skip_records`（と `disclosure_skip_matches`）に 1 判断 1 行を書く。上限から 5 時間より古い記録なしの判断は評価しない（アプリが止まっていた・記録部品が無かった判断。MCP は `not_recorded` に数える）。`jev_decisions` の行は書き換えない。保持期間は表で異なる: `disclosure_fetches`（取得の記録）は評価に使うだけなので 7 日で消す。`disclosure_items`・`disclosure_skip_records`・`disclosure_skip_matches` は MCP で後から見る履歴なので、無期限に保持する（消さない）。
4. **専用の DB 接続を使う**（`sqlitedb/sidecar.Open`）: アプリの `Open` が返す接続は、全ての書き込みの成否を DB 書き込み失敗の連続回数（`DBWriteFailures`。FR-RISK-2 の Kill Switch が読む）に流す。成功は連続回数を 0 に戻し、失敗は増やす。記録部品の書き込みがこれに触れると、取引の安全装置に影響する。そのため記録部品は、計装なしのドライバで開いた専用の接続（`busy_timeout` 5 秒・接続 1 本）を使い、`DBWriteFailures` に一切触れない。書き込みロックの待ちも 5 秒で諦める。

5. **後追い取得（`disclosureskip.Followup`。issue #1053）**: News Ingest の取得対象は Fast Screener 候補と保有銘柄だけなので、判断の直後に候補から外れた銘柄は `t + FetchLag` 以後の取得が来ない。後追い取得が 1 分ごとに、**記録がまだ確定していない判断**で、`t + FetchLag + 2 分`（News Ingest の通常の取得に先に覆わせる猶予）を過ぎ、上限（`SessionDeadline`）の前で、`t + FetchLag` 以後に始まった成功取得がまだ無い銘柄だけに、立会時間中に 1 回リクエストを出す（`RecordingFeed` を通すので記録される）。覆う取得ができれば、その判断にはもう出さない。書き込みは `RecordingFeed` の別ゴルーチンが非同期に行うので、その間に次の周期が来ても同じ銘柄をやのしんへ二重に出さないよう、成功した自分のリクエストの開始時刻を銘柄ごとにメモリに持ち、2 分（`followupWriteLag`）だけ覆いとして信用する（その後は DB だけを見るので、書き込みが失われた場合は 1 回だけ出し直す）。News Ingest ではない: 応答は Luna に送らず、News Ingest のキャッシュにも入れない（`news_context`・ニュースフラグ・TDnet スコアに出ない）。取得対象の集合（候補・建玉）にも足さない。全銘柄のポーリングはしない（#531）。失敗した取得は成功扱いにせず（覆いに数えず、失敗として記録される）、バックオフ中（`ErrBackingOff`）はリクエストを出さず次の周期で再試行するので、フィードの復帰後に覆える。News Ingest が動いているとき（`newsEnabled`）だけ起動する。新しい設定は無い。

News Ingest が止まっていれば（`NEWS_FEED_ENABLED=off`・Luna が使えない）、記録部品も動かない。`NEWS_FEED_URL` で汎用フィードに差し替えている場合は、やのしんの索引の前提（新しい順・件数上限）が成り立たないため、記録部品を起動しない（記録は 0 件、MCP は `not_recorded` を返す）。

## 5. 対象とした判断（エントリー候補）

`jev_decisions` のうち `decision_type = 'trader'` かつ `direction IN ('LONG', 'SHORT')` の行。Policy Engine が `trade_signals` の候補にする判断で、Outcome Labeling がラベルを付ける判断と同じ（`entry_quality_whatif` の対象と同じ）。scout の行と `NONE` の trader の行は対象外。シャドーの判定は別表（`jev_shadow_decisions`）なので含まれない。MCP の集計は、ライブの版と同じコホート（`jev.ForeignVersionsOfLive` の版を除く）だけを数え、`prompt_version` でその版だけに絞れる。

## 6. やのしんへのリクエスト頻度

記録部品（`RecordingFeed`）はリクエストを出さず、News Ingest の取得の戻り値を見るだけ。News Ingest の頻度は FR-LUNA-1 のまま: 東証立会時間中、1 分周期で、Fast Screener 候補と保有銘柄の各 1 リクエスト（`GET /webapi/tdnet/list/{銘柄}.json?limit=10`）を並列 4 で。失敗が続くと `GuardedFeed` が 2 分から最長 30 分のバックオフで間引く。

後追い取得（§4 の 5。issue #1053）だけが、取得対象から外れた銘柄に追加のリクエストを出す。出すのは、記録が確定していない判断の銘柄で、`t + FetchLag` 以後の成功取得がまだ無いものだけ。1 判断につき、覆う取得ができるまで 1 分に 1 回まで（通常は 1 回で終わる）、立会時間中だけ、並列 4・1 周期 50 銘柄まで。`GuardedFeed` のバックオフを News Ingest と共有するので、フィードが失敗しているときに News Ingest より多く叩くことはない。取得対象の集合は広げない（全銘柄のポーリングはしない。#531）。

## 7. 取引に影響しないことの確認

- `RecordingFeed.Fetch` の戻り値とエラーは内側と同一（テスト: `TestRecordingFeed_ReturnsTheInnerResultUnchangedAndRecordsTheFetch` ほか）。書き込みのエラー・パニック・キュー満杯でも同じ。News Ingest のキャッシュ（`NewsContext`）・ニュースフラグ（`TakeNewsFlag`）・TDnet スコア・Policy Engine の判断が、記録の有無と記録の失敗の種類によらず同一であることを `TestRecordingFeed_NewsIngestAndPolicyDecideTheSameWithAndWithoutRecording` で確かめる。
- 後追い取得（`disclosureskip.Followup`）は `newsfeed.Feed` だけを受け取り、News Ingest のサービス（キャッシュ・Luna への送信・ニュースフラグ）に触れない。取得対象から外れた銘柄の応答が `news_context`・ニュースフラグ・TDnet スコアに出ないこと、対象の銘柄の `news_context`・フラグが後追いの有無で同一であることを `TestFollowup_DoesNotTouchNewsIngest` で確かめる。News Ingest が動いていないとき（`NEWS_FEED_ENABLED=off`・Luna が無い）は後追いも起動しない（`TestWire_FollowsUpAWaitingDecisionOnlyWhenNewsIngestRuns`）。
- 取引の経路（`internal/service/{policy,risk,execution,broker,jev,jevprompt,selfimprove,changeguard,metalabel,screener}`、`internal/config`、`internal/domain`、`config/*.yaml`）には差分を出さない。
- 記録部品が書く表は §4 の 4 表だけ。既存の表の列・制約は変えない（`disclosure_skip_records` にこの記録自身の列 `unknown_detail` をマイグレーション `000051` で足しただけ）。

## 変更履歴

| 版 | 日付 | 内容 | 出典 |
|---|---|---|---|
| 1.0 | 2026-10-30 | 新規。判断・発注・設定の既定値は変わらない | issue #935 |
| 1.1 | 2026-10-30 | 表題がなく捨てられた項目も、件数上限への到達と「開示時刻のない項目」に数える（`newsfeed.IndexStats`）。立会時間外の判断が「不明」になりうること、各表の保持期間を明記。判断・発注・設定の既定値は変わらない | issue #935 |
| 1.2 | 2026-11-11 | 「不明（`no_fetch`）」を減らし、理由を見えるようにする。待つ上限（`DefaultGiveUp` 60 分）を立会時間の分で数える（昼休み・大引けをまたぐ判断を不当に打ち切らない。未来の開示は見ない・`t + FetchLag` 以後の取得だけが覆うルールは同じ）、候補から外れた銘柄への後追い取得（News Ingest ではなく、キャッシュ・Luna・取得対象に触れない。新しい設定は無い）、`unknown_detail`（確定時に記録へ保存する理由の内訳。マイグレーション `000051`。MCP の `unknown_reasons[].details`）。取引・Policy・Risk・発注・Jev の入力・設定の既定値は変わらない | issue #1053 |
| 1.3 | 2026-11-12 | 後追い取得が、記録の非同期書き込みが届くまでの 2 分（`followupWriteLag`）は自分の成功したリクエストを覆いとして信用し、同じ銘柄をやのしんに二重に出さないようにした。`pendingGrace`（上限から 5 時間）が単一の立会では旧 6 時間（判断から）と同じ長さであることを確認（テスト `TestEvaluator_RecordsADecisionAfterARestartUpToFiveHoursPastItsGiveUpTime`）。判断・Policy・Risk・発注・既定値は変わらない | issue #1053 |
