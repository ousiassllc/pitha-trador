# 設計: 立花の時価の研究用記録と research_quote_source_compare

`docs/architecture/paper-results-mcp.md` §5 の分割章（行数上限のため別ファイル）。issue #1083（親: 立花を kabu と並べて比べる研究。日足側は issue #1082）。ツールの引数と出力は `docs/api/endpoints/paper-results-mcp-research-quotes.md`、表は `docs/architecture/er/tables-research.md`。

## 1. 目的と位置づけ

ブローカーは kabu のまま、立花 e支店の日中の時価を**別に**1 分に 1 回記録し、kabu の `market_snapshots` と欠測・値のずれを比べる。取引の入力ではない: Feature Engine・Scout・Trader・Paper・Policy・Risk・発注・Live・Jev のプロンプトは読まず、kabu の監視銘柄の決め方・ブローカー選択・既存のシャドー・`market_snapshots` の書き込みは変えない。

- **既定はすべてオフ**: `research.tachibana_quotes.enabled`（JSON 整数 1/0、既定 0）がオフの間は、記録部品は毎分状態を `disabled` にするだけで、立花への接続・ログイン・リクエストを一切しない（`Start` も呼ばない）。認証 ID・秘密鍵のパスが未設定でも、立花がブローカーのときも、ネットワークに出ない（`not_configured` / `broker_is_tachibana`）。
- 参照を固定する: `internal/repository/researchquote/isolation_test.go` が、表名・リポジトリ・記録部品・保持タスク・MCP の問い合わせのシンボルを、記録部品・保持タスク・退避・読み取り専用の MCP ツール・その登録と配線・`db/migrations` のファイルだけに限る。記録部品が `market_snapshots` の書き込み手段・取引系のパッケージを持たないことも固定する。
- 立花への要求は控えめに: 取得は EVENT 配信が主で、REST の時価は「値が 1 つも無い銘柄」の補助だけ。REST の間隔（`research.tachibana_quotes.rest_min_interval_seconds`、既定 300 秒）と 1 日の接続回数（`max_connects_per_day`、既定 10）は、EVENT フィード（`internal/service/broker/tachibana/event`）自身が守る。1 本の認証 ID は 1 セッションなので、日足の研究収集（#1082）と `internal/bootstrap/researchhub` の 1 つのアダプタを共有する。

## 2. 記録部品（internal/bootstrap/researchquotes）

`Recorder`（`New(Config)` / `Run(ctx)` / `Tick(ctx, now)`）。毎分 5 秒目に 1 サイクル。設定（`Config.Settings`）は毎サイクル読み直す。

1. スイッチがオフ → 状態 `disabled`、終わり（`Start`・フィードの呼び出しゼロ）。
2. 立会日の 08:50 JST〜15:30 の窓の外 → `waiting`（接続しない）。
3. 窓の中 → `Config.Start` で研究用フィードを取る（初回にログインと EVENT の起動。`ErrNotConfigured` → `not_configured`、`ErrBrokerIsTachibana` → `broker_is_tachibana`、いずれもネットワークには出ず、翌分にまた試す）。
4. 銘柄の計画（`plan.go`）: kabu が今見ている銘柄（`KabuSymbols`。読むだけ）を先に順に、続いて追加分（`research.tachibana_quotes.extra_source` が `manual` なら `extra_symbols`、`daily_screen` なら前の立会日の日足スクリーニングの上位。`ScreenSymbols`。日足は保存済みのものだけを読み、ブローカーには何も頼まない）。重複と不正なコードを除き、`max_symbols`（≦ 120 = EVENT の購読上限）で切る。**JST の日の中は粘着**: 追加だけで順序を変えず、新しい銘柄が増えたときだけ `SetWatch` を呼ぶ。購読の変更は再接続で、1 日の接続回数を使うため、最初の購読のあとは **10 分以上あけて**まとめて反映する（kabu のランキング監視は毎分近く入れ替わる）。記録するのは購読済みの銘柄だけ。値は同じ JST の日のものだけを使い、前日の値しかない銘柄は REST 補助（フィードの間隔の範囲）に回す。日足スクリーニングを読めない日は、追加銘柄なしで kabu の銘柄だけを記録し、10 分あけて読み直す。
5. 立会中（`InSession`。昼休みは記録しない）: 計画した銘柄ごとに `Peek`（EVENT・REST が合流した最新の値を、古くても返す。要求は出さない）。値が 1 つも無い銘柄だけ `Latest`（フィードが REST の間隔で絞る）。価格のない銘柄は行を作らない。1 分 1 トランザクションで `INSERT … ON CONFLICT DO NOTHING`（`(source, symbol, timestamp)` が一意なので、同じ分の再実行は増えない）。`timestamp` は分に切り捨てた UTC、`observed_at` はその値の最終更新時刻、`via` は最後の更新が EVENT か REST か、`session_phase` は 9:05 前 / 〜11:25 / 〜12:35 / 〜15:20 / 以降。
6. 状態: `running`（銘柄数・直近の分の行数・本日の接続回数）、セッションが無いと `error`（理由は平易な固定文で、認証情報・URL は入らない）。

失敗・panic は記録して続ける（取引を巻き込まない）。設定でオフにしたあとも、すでに張った EVENT 接続は日の終わりまで残る（購読の変更をしない）。

## 3. 保持と退避（internal/bootstrap/researchquotejob, internal/service/snapshotarchive）

日次メンテナンスタスク `research_quote_retention`（`services_build.go` の `scheduler.WithMaintenanceTask` に 1 行で登録。`researchlabeljob` と同じ作り）。表が空なら `maintenance.ErrSkipped`（何も書かない）。`research.tachibana_quotes.retention_days`（既定 90）より古い日を、`snapshotarchive.NewResearchQuotes` で `snapshot_archive/research_quote_samples/YYYY/research_quote_samples-YYYY-MM-DD.csv.gz` に退避して読み戻し検証し、**検証済みの日・退避時点の id までだけ**を 1000 行ずつ、200ms の間を置いて削除する（1 回 50 バッチまで。残りがあれば `maintenance.ErrMore` で、ランナーがロックを離す）。退避か検証に失敗したら何も削除しない（fail closed）。記録のスイッチには依存せず、古い行は常に消える。ファイルはこの PC の中だけに置き、どこへも送らない。`snapshotarchive` は表名を持つ `Archiver`（既定は `market_snapshots` で、既存の動作・パスは変わらない。マニフェストの `table` に表名を残す）に一般化した。

## 4. 読み取りツール（research_quote_source_compare）

`internal/service/paperresultsmcp/researchquotediag`。JST の日ごとに、その日の立花の記録と kabu の `market_snapshots`（`price` 列だけ。分ごとに最後の行）を読み、銘柄 × UTC の分で突き合わせる。1 回の呼び出しで読む行は 40 万行まで、超えるか時間のソフト期限に達したら、読み終えた日までを返して `scan.complete=false` とメモを付ける（日の一部だけを数えない）。立花の記録がない日は対象外。SELECT のみ。登録は `paperresultsmcp/research_quotes_tool.go`（1 ファイル）。
